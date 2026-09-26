//go:build integration

package todo

import (
	"context"
	"testing"
)

// TodoSyncer.SyncPageRows / RemovePageRows delegate to UpsertForPage /
// CancelAllForPage. The tests below verify the end-to-end path: markdown
// in, tasks out (or cancelled). SSE hub is the real one — the tests
// observe DB state rather than events (event coverage lives in the SSE
// path, out of scope for wiki_hooks).

func TestTodoSyncer_SyncPageRows_CreatesTasksFromDirectives(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	store := NewTodoStore(pool)
	hub := NewHub()
	syncer := NewTodoSyncer(store, hub, nil)

	md := "" +
		"# Plan\n\n" +
		"{todo title=\"Ship RC\" assign=alice priority=high due=2026-12-01}\n\n" +
		"{todo title=\"Docs\" assign=bob}\n"
	syncer.SyncPageRows("/plans/release", md)

	got, err := store.ListForPage(context.Background(), "/plans/release")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2", len(got))
	}
	byTitle := map[string]*Task{}
	for _, t := range got {
		byTitle[t.Title] = t
	}
	if byTitle["Ship RC"] == nil || byTitle["Ship RC"].Priority != PriorityHigh {
		t.Errorf("Ship RC missing or wrong priority: %+v", byTitle["Ship RC"])
	}
	if byTitle["Docs"] == nil || byTitle["Docs"].Assignee.Target != "bob" {
		t.Errorf("Docs missing or wrong assignee: %+v", byTitle["Docs"])
	}
}

func TestTodoSyncer_SyncPageRows_Reconciles(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	store := NewTodoStore(pool)
	hub := NewHub()
	syncer := NewTodoSyncer(store, hub, nil)
	page := "/plans/release"

	// First save: two todos.
	syncer.SyncPageRows(page, "{todo title=A assign=alice}\n{todo title=B assign=bob}\n")
	if got, _ := store.ListForPage(context.Background(), page); len(got) != 2 {
		t.Fatalf("first sync: %d tasks", len(got))
	}

	// Second save: A modified, B removed, C added.
	syncer.SyncPageRows(page, "{todo title=A assign=alice priority=urgent}\n{todo title=C assign=carol}\n")
	got, _ := store.ListForPage(context.Background(), page)
	titles := map[string]Priority{}
	for _, t := range got {
		titles[t.Title] = t.Priority
	}
	if _, has := titles["B"]; has {
		t.Errorf("B should have been deleted")
	}
	if titles["A"] != PriorityUrgent {
		t.Errorf("A priority not updated to urgent: %v", titles)
	}
	if titles["C"] != PriorityNormal {
		t.Errorf("C missing or wrong priority: %v", titles)
	}
}

func TestTodoSyncer_SyncPageRows_NoDirectivesNoExisting_EarlyReturn(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	store := NewTodoStore(pool)
	hub := NewHub()
	syncer := NewTodoSyncer(store, hub, nil)

	// Empty markdown for a page with no prior tasks — should not error, no side effects.
	syncer.SyncPageRows("/new/page", "just prose, no todos\n")

	got, _ := store.ListForPage(context.Background(), "/new/page")
	if len(got) != 0 {
		t.Errorf("no directives + no existing → no tasks; got %d", len(got))
	}
}

func TestTodoSyncer_RemovePageRows_CancelsAllTasks(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	store := NewTodoStore(pool)
	hub := NewHub()
	syncer := NewTodoSyncer(store, hub, nil)
	page := "/going/away"

	// Seed via sync.
	syncer.SyncPageRows(page, "{todo title=One assign=alice}\n{todo title=Two assign=bob}\n")

	syncer.RemovePageRows(page)

	got, _ := store.ListForPage(context.Background(), page)
	for _, task := range got {
		if task.Status != StatusCancelled {
			t.Errorf("task %q should be cancelled after RemovePageRows, got %s", task.Title, task.Status)
		}
	}
}
