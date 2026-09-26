//go:build integration

package todo

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Store tests exercise the full Postgres schema: todo_tasks, todo_completions,
// todo_notifications_sent. Each test gets its own database via newTestPool.

func newStoreForTest(t *testing.T) (*TodoStore, context.Context) {
	t.Helper()
	pool := newTestPool(t)
	return NewTodoStore(pool), context.Background()
}

func mustCreate(t *testing.T, s *TodoStore, req CreateRequest) *Task {
	t.Helper()
	task, err := s.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return task
}

func TestTodoStore_CreateGetDelete_RoundTrip(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)

	req := CreateRequest{
		Title:       "Ship RC",
		Description: "cut the tag",
		Source:      SourceAPI,
		Assignee:    Assignee{Type: "user", Target: "alice", Resolution: "any"},
		DueDate:     "2026-12-01",
		Tags:        "release",
		Priority:    PriorityHigh,
		CreatedBy:   "alice",
	}
	created := mustCreate(t, s, req)
	if created.ID == "" || created.Status != StatusOpen {
		t.Errorf("created shape wrong: %+v", created)
	}

	got, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Ship RC" || got.DueDate != "2026-12-01" ||
		got.Assignee.Target != "alice" || got.Priority != PriorityHigh {
		t.Errorf("round-trip lost data: %+v", got)
	}

	if err := s.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(ctx, created.ID); err == nil {
		t.Errorf("get after delete should fail")
	}
}

func TestTodoStore_Update_PartialPatch(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{
		Title:    "Original",
		Assignee: Assignee{Type: "user", Target: "alice", Resolution: "any"},
		Priority: PriorityNormal,
	})

	newTitle := "Renamed"
	newPriority := PriorityUrgent
	updated, err := s.Update(ctx, orig.ID, Patch{Title: &newTitle, Priority: &newPriority})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "Renamed" || updated.Priority != PriorityUrgent {
		t.Errorf("patch not applied: %+v", updated)
	}
	// Fields NOT in the patch must survive.
	if updated.Assignee.Target != "alice" || updated.DueDate != orig.DueDate {
		t.Errorf("non-patched fields drifted: %+v", updated)
	}
}

func TestTodoStore_Update_EmptyPatchNoOp(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{Title: "X", Assignee: Assignee{Type: "user", Target: "a"}})
	got, err := s.Update(ctx, orig.ID, Patch{})
	if err != nil {
		t.Fatalf("update empty: %v", err)
	}
	if got.Title != "X" {
		t.Errorf("empty patch mutated title")
	}
}

func TestTodoStore_Update_NotFound(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	newTitle := "x"
	if _, err := s.Update(ctx, "not-a-real-id", Patch{Title: &newTitle}); err == nil {
		t.Errorf("expected error for unknown id")
	}
}

func TestTodoStore_Complete_SingleUserPromotes(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{
		Title:    "Ship it",
		Assignee: Assignee{Type: "user", Target: "alice", Resolution: "any"},
	})
	task, promoted, err := s.Complete(ctx, orig.ID, "alice", nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !promoted {
		t.Errorf("single-user task should promote on first completion")
	}
	if task.Status != StatusDone {
		t.Errorf("status = %s, want done", task.Status)
	}
	completions, _ := s.ListCompletions(ctx, orig.ID)
	if len(completions) != 1 || completions[0].UserID != "alice" {
		t.Errorf("completions = %+v", completions)
	}
}

func TestTodoStore_Complete_ResolutionAll_ReturnsBeforeAll(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	// group:reviewers with 3 members, resolution=all — needs all 3 to complete.
	orig := mustCreate(t, s, CreateRequest{
		Title:    "Sign off",
		Assignee: Assignee{Type: "group", Target: "group:reviewers", Resolution: "all"},
	})
	resolver := stubResolver{groups: map[string][]string{
		"reviewers": {"alice", "bob", "carol"},
	}}
	task, promoted, err := s.Complete(ctx, orig.ID, "alice", resolver)
	if err != nil {
		t.Fatalf("complete alice: %v", err)
	}
	if promoted {
		t.Errorf("should not promote until all members complete")
	}
	if task.Status != StatusOpen {
		t.Errorf("status = %s, want still open", task.Status)
	}
	_, promoted, _ = s.Complete(ctx, orig.ID, "bob", resolver)
	if promoted {
		t.Errorf("still shouldn't promote after 2/3")
	}
	task, promoted, _ = s.Complete(ctx, orig.ID, "carol", resolver)
	if !promoted {
		t.Errorf("should promote after all 3 members complete")
	}
	if task.Status != StatusDone {
		t.Errorf("status = %s, want done", task.Status)
	}
}

func TestTodoStore_Complete_IdempotentOnAlreadyDone(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{
		Title:    "T",
		Assignee: Assignee{Type: "user", Target: "alice"},
	})
	_, _, _ = s.Complete(ctx, orig.ID, "alice", nil)
	task, promoted, err := s.Complete(ctx, orig.ID, "alice", nil)
	if err != nil {
		t.Fatalf("second complete: %v", err)
	}
	if promoted {
		t.Errorf("second complete shouldn't re-promote")
	}
	if task.Status != StatusDone {
		t.Errorf("status = %s, want still done", task.Status)
	}
}

func TestTodoStore_ReopenClearsCompletions(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{
		Title:    "T",
		Assignee: Assignee{Type: "user", Target: "alice"},
	})
	_, _, _ = s.Complete(ctx, orig.ID, "alice", nil)
	reopened, err := s.Reopen(ctx, orig.ID)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Status != StatusOpen {
		t.Errorf("status = %s, want open", reopened.Status)
	}
	completions, _ := s.ListCompletions(ctx, orig.ID)
	if len(completions) != 0 {
		t.Errorf("Reopen should clear completions, got %d", len(completions))
	}
}

func TestTodoStore_Cancel_Sets_Status(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{Title: "T", Assignee: Assignee{Type: "user", Target: "alice"}})
	got, err := s.Cancel(ctx, orig.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got.Status != StatusCancelled {
		t.Errorf("status = %s, want cancelled", got.Status)
	}
}

func TestTodoStore_MarkDone_Direct(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{Title: "T", Assignee: Assignee{Type: "user", Target: "alice"}})
	got, err := s.MarkDone(ctx, orig.ID)
	if err != nil {
		t.Fatalf("markdone: %v", err)
	}
	if got.Status != StatusDone {
		t.Errorf("status = %s, want done", got.Status)
	}
	// No completion row required — MarkDone is the admin-reconcile path.
	completions, _ := s.ListCompletions(ctx, orig.ID)
	if len(completions) != 0 {
		t.Errorf("MarkDone should not create a completion row, got %d", len(completions))
	}
}

func TestTodoStore_List_FiltersAndPaginate(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	// Seed a mix of tasks.
	for i, spec := range []struct {
		title, target string
		priority      Priority
		tags          string
	}{
		{"A", "alice", PriorityHigh, "release"},
		{"B", "bob", PriorityNormal, "docs"},
		{"C", "alice", PriorityLow, "release,ops"},
	} {
		_ = i
		mustCreate(t, s, CreateRequest{
			Title:    spec.title,
			Assignee: Assignee{Type: "user", Target: spec.target},
			Priority: spec.priority,
			Tags:     spec.tags,
		})
	}
	// Filter by assignee.
	got, _, err := s.List(ctx, ListOptions{Assignee: "alice"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("assignee=alice → %d tasks, want 2", len(got))
	}
	// Filter by tag.
	got, _, _ = s.List(ctx, ListOptions{Tag: "docs"})
	if len(got) != 1 || got[0].Title != "B" {
		t.Errorf("tag=docs → %+v", got)
	}
	// Pagination cursor: request 2 at a time.
	page1, cursor, _ := s.List(ctx, ListOptions{Limit: 2})
	if len(page1) != 2 || cursor == "" {
		t.Errorf("page1 = %d tasks / cursor=%q", len(page1), cursor)
	}
	page2, _, _ := s.List(ctx, ListOptions{Limit: 2, Cursor: cursor})
	if len(page2) < 1 {
		t.Errorf("page2 should contain the remainder, got %d", len(page2))
	}
}

func TestTodoStore_ListMine_UnionsUserAndGroups(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	// One task assigned directly to alice, one to a group she belongs to.
	mustCreate(t, s, CreateRequest{Title: "Direct", Assignee: Assignee{Type: "user", Target: "alice"}})
	mustCreate(t, s, CreateRequest{Title: "GroupTask", Assignee: Assignee{Type: "group", Target: "group:editors"}})
	// One task for someone unrelated.
	mustCreate(t, s, CreateRequest{Title: "Other", Assignee: Assignee{Type: "user", Target: "bob"}})

	mine, err := s.ListMine(ctx, "alice", []string{"editors"})
	if err != nil {
		t.Fatalf("ListMine: %v", err)
	}
	titles := map[string]bool{}
	for _, t := range mine {
		titles[t.Title] = true
	}
	if !titles["Direct"] || !titles["GroupTask"] {
		t.Errorf("expected Direct + GroupTask, got %v", titles)
	}
	if titles["Other"] {
		t.Errorf("should not include bob's task")
	}
}

func TestTodoStore_UpsertForPage_CreateUpdateCancel(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	page := "/team/plan"

	// First sync: two directives.
	first := []ParsedDirective{
		{Title: "One", Assign: "alice", Priority: "high", NodeKey: computeNodeKey(page, "One", "alice")},
		{Title: "Two", Assign: "bob", NodeKey: computeNodeKey(page, "Two", "bob")},
	}
	if err := s.UpsertForPage(ctx, page, first, "system"); err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	got, _ := s.ListForPage(ctx, page)
	if len(got) != 2 {
		t.Fatalf("after first upsert = %d tasks, want 2", len(got))
	}

	// Second sync: One's priority changed, Two is gone, Three is new.
	second := []ParsedDirective{
		{Title: "One", Assign: "alice", Priority: "urgent", NodeKey: computeNodeKey(page, "One", "alice")},
		{Title: "Three", Assign: "carol", NodeKey: computeNodeKey(page, "Three", "carol")},
	}
	if err := s.UpsertForPage(ctx, page, second, "system"); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	got, _ = s.ListForPage(ctx, page)
	titles := map[string]Priority{}
	for _, t := range got {
		titles[t.Title] = t.Priority
	}
	// Two is deleted, Three is created, One's priority updated.
	if _, has := titles["Two"]; has {
		t.Errorf("Two should be deleted")
	}
	if titles["Three"] != PriorityNormal {
		t.Errorf("Three missing or wrong priority: %v", titles)
	}
	if titles["One"] != PriorityUrgent {
		t.Errorf("One's priority not updated: %v", titles)
	}
}

func TestTodoStore_CancelAllForPage(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	page := "/gone"
	mustCreate(t, s, CreateRequest{Title: "A", SourcePage: page, Assignee: Assignee{Type: "user", Target: "alice"}})
	mustCreate(t, s, CreateRequest{Title: "B", SourcePage: page, Assignee: Assignee{Type: "user", Target: "bob"}})

	if err := s.CancelAllForPage(ctx, page); err != nil {
		t.Fatalf("cancel all: %v", err)
	}
	got, _ := s.ListForPage(ctx, page)
	for _, task := range got {
		if task.Status != StatusCancelled {
			t.Errorf("task %q status = %s, want cancelled", task.Title, task.Status)
		}
	}
}

func TestTodoStore_Notifications_UpsertAndCheck(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{Title: "T", Assignee: Assignee{Type: "user", Target: "alice"}})

	if sent, _ := s.HasNotificationBeenSent(ctx, orig.ID, "assigned"); sent {
		t.Errorf("should not be sent initially")
	}
	if err := s.RecordNotificationSent(ctx, orig.ID, "assigned"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if sent, _ := s.HasNotificationBeenSent(ctx, orig.ID, "assigned"); !sent {
		t.Errorf("should be sent after record")
	}
	when, _ := s.GetNotificationSentAt(ctx, orig.ID, "assigned")
	if when.IsZero() {
		t.Errorf("sent-at should be non-zero")
	}
	// Upsert: re-record should not error.
	if err := s.RecordNotificationSent(ctx, orig.ID, "assigned"); err != nil {
		t.Fatalf("re-record: %v", err)
	}
}

func TestTodoStore_ListDueBetween_And_ListOverdue(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	// Three tasks with varying dues.
	mustCreate(t, s, CreateRequest{Title: "past", DueDate: "2026-01-01", Assignee: Assignee{Type: "user", Target: "a"}})
	mustCreate(t, s, CreateRequest{Title: "soon", DueDate: "2026-06-15", Assignee: Assignee{Type: "user", Target: "a"}})
	mustCreate(t, s, CreateRequest{Title: "future", DueDate: "2027-01-01", Assignee: Assignee{Type: "user", Target: "a"}})

	between, _ := s.ListDueBetween(ctx,
		mustTime(t, "2026-06-01"), mustTime(t, "2026-07-01"))
	if len(between) != 1 || between[0].Title != "soon" {
		t.Errorf("between = %+v", between)
	}
	overdue, _ := s.ListOverdue(ctx, mustTime(t, "2026-05-01"))
	if len(overdue) != 1 || overdue[0].Title != "past" {
		t.Errorf("overdue = %+v", overdue)
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return tt
}

func TestTodoStore_Acknowledge_RecordsVersion(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{
		Title:      "Read policy",
		Assignee:   Assignee{Type: "user", Target: "alice"},
		WikiAction: WikiAction{Type: "read", Page: "/policy"},
	})

	task, promoted, err := s.Acknowledge(ctx, orig.ID, "alice", 42, nil)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if !promoted {
		t.Errorf("single-user ack should promote")
	}
	if task.Status != StatusDone {
		t.Errorf("status = %s, want done", task.Status)
	}
	completions, _ := s.ListCompletions(ctx, orig.ID)
	if len(completions) != 1 || completions[0].AcknowledgedVersion != 42 {
		t.Errorf("ack version not persisted: %+v", completions)
	}

	// Second ack after promotion is a no-op — Acknowledge short-circuits
	// on StatusDone. To exercise the upsert branch, re-open first (keeping
	// completions) then ack with a new version.
	if _, err := s.ReopenKeepCompletions(ctx, orig.ID); err != nil {
		t.Fatalf("reopen keep: %v", err)
	}
	_, _, err = s.Acknowledge(ctx, orig.ID, "alice", 43, nil)
	if err != nil {
		t.Fatalf("second ack: %v", err)
	}
	completions, _ = s.ListCompletions(ctx, orig.ID)
	if len(completions) != 1 || completions[0].AcknowledgedVersion != 43 {
		t.Errorf("upsert should update version to 43, got %+v", completions)
	}
}

func TestTodoStore_ReopenKeepCompletions_PreservesRows(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	orig := mustCreate(t, s, CreateRequest{
		Title:      "read",
		Assignee:   Assignee{Type: "user", Target: "alice"},
		WikiAction: WikiAction{Type: "read", Page: "/x"},
	})
	_, _, _ = s.Acknowledge(ctx, orig.ID, "alice", 5, nil)

	reopened, err := s.ReopenKeepCompletions(ctx, orig.ID)
	if err != nil {
		t.Fatalf("reopen keep: %v", err)
	}
	if reopened.Status != StatusOpen {
		t.Errorf("status = %s, want open", reopened.Status)
	}
	// Reopen kept completions.
	completions, _ := s.ListCompletions(ctx, orig.ID)
	if len(completions) != 1 || completions[0].AcknowledgedVersion != 5 {
		t.Errorf("completions should survive: %+v", completions)
	}
}

func TestTodoStore_ListPendingAcks_ScopedToUserAndGroups(t *testing.T) {
	t.Parallel()
	s, ctx := newStoreForTest(t)
	page := "/team/policy"
	mustCreate(t, s, CreateRequest{
		Title:      "Read pol",
		Assignee:   Assignee{Type: "user", Target: "alice"},
		WikiAction: WikiAction{Type: "read", Page: page},
	})
	mustCreate(t, s, CreateRequest{
		Title:      "Read pol group",
		Assignee:   Assignee{Type: "group", Target: "group:editors"},
		WikiAction: WikiAction{Type: "read", Page: page},
	})
	// Task not for alice or her groups.
	mustCreate(t, s, CreateRequest{
		Title:      "for bob",
		Assignee:   Assignee{Type: "user", Target: "bob"},
		WikiAction: WikiAction{Type: "read", Page: page},
	})

	acks, err := s.ListPendingAcks(ctx, page, "alice", []string{"editors"})
	if err != nil {
		t.Fatalf("list pending acks: %v", err)
	}
	if len(acks) != 2 {
		t.Errorf("expected 2 pending acks for alice+editors, got %d", len(acks))
	}
	for _, a := range acks {
		if strings.Contains(a.Title, "bob") {
			t.Errorf("shouldn't include bob's task: %+v", a)
		}
	}
}
