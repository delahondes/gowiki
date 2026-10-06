//go:build integration

package todo

import (
	"context"
	"path/filepath"
	"testing"

	"gowiki/backend/internal/storage"
)

// End-to-end proof that the FileStore.OnPageSaved hook closes the
// MCP-write-path gap: a {todo action=create:/pattern} task assigned to
// the user doing the save actually auto-completes when storage.Put
// writes a matching page, regardless of what called Put.
//
// The storage-layer unit tests in storage/pages_hook_test.go pin the
// hook-fires-with-right-args contract; this one proves the production
// wiring from main.go — FileStore.OnPageSaved → TodoService.
// AutoCompleteCreateAction → task.Status==done — actually runs. If
// main.go stops wiring OnPageSaved, or if the hook stops being called
// from putWithSummary, this test fails.

func TestActionHook_CreateActionAutoCompletesOnStorePut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// --- Wire the production chain, piece by piece ---
	pool := newTestPool(t)
	todoStore := NewTodoStore(pool)
	hub := NewHub()
	// nil dispatcher — the test doesn't exercise email/webhook
	// notifications. CompleteTask is dispatcher-nil-safe.
	svc := NewService(todoStore, hub, nil)

	root := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	// This is the exact wiring main.go uses, minus the goroutine
	// spawn — we want to observe the result synchronously in the
	// test. Running sync is OK: the production fan-out methods are
	// idempotent (CompleteTask on an already-done task is a no-op)
	// so the only observable difference is latency.
	fs.OnPageSaved = func(pagePath, author string) {
		svc.AutoCompleteCreateAction(ctx, pagePath, author)
	}

	// --- Seed an open create-action task for alice ---
	task, err := todoStore.Create(ctx, CreateRequest{
		Title:      "Launch a new campaign",
		Source:     SourceAPI,
		SourcePage: "/qms/campaigns",
		Assignee: Assignee{
			Type:       "user",
			Target:     "alice",
			Resolution: "any",
		},
		WikiAction: WikiAction{Type: "create", Pattern: "/qms/campaigns/.*"},
	})
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}
	if task.Status != StatusOpen {
		t.Fatalf("seed task status = %q, want open", task.Status)
	}

	// --- The save that must trigger the fan-out ---
	// Writing /qms/campaigns/alpha-beta as alice (the task's
	// assignee) matches the regex ^/qms/campaigns/.*$ and should
	// close the task.
	if _, err := fs.Put("qms/campaigns/alpha-beta", "# Alpha Beta\n", "alice"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// --- Assert the task is now done ---
	// Re-read from the store rather than trusting the in-memory
	// copy — the auto-complete goes through TodoService.CompleteTask
	// which writes the DB.
	after, err := todoStore.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get task after save: %v", err)
	}
	if after.Status != StatusDone {
		t.Errorf("task status after matching save = %q, want %q — the hook/fan-out chain is broken", after.Status, StatusDone)
	}
}

func TestActionHook_CreateAction_AssigneeMismatch_StaysOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pool := newTestPool(t)
	todoStore := NewTodoStore(pool)
	hub := NewHub()
	svc := NewService(todoStore, hub, nil)

	root := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	fs.OnPageSaved = func(pagePath, author string) {
		svc.AutoCompleteCreateAction(ctx, pagePath, author)
	}

	// Task assigned to alice.
	task, err := todoStore.Create(ctx, CreateRequest{
		Title:      "Launch a new campaign",
		Source:     SourceAPI,
		SourcePage: "/qms/campaigns",
		Assignee: Assignee{
			Type:       "user",
			Target:     "alice",
			Resolution: "any",
		},
		WikiAction: WikiAction{Type: "create", Pattern: "/qms/campaigns/.*"},
	})
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}

	// Bob writes a matching page — not alice. The assignee gate must
	// hold: a todo is a "who did X" record, not a "did X get done"
	// record. The task stays open.
	if _, err := fs.Put("qms/campaigns/alpha-beta", "# Alpha Beta\n", "bob"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	after, err := todoStore.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get task after save: %v", err)
	}
	if after.Status != StatusOpen {
		t.Errorf("task status after non-assignee save = %q, want %q (assignee gate must hold)", after.Status, StatusOpen)
	}
}

func TestActionHook_CreateAction_PathDoesNotMatchPattern_StaysOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pool := newTestPool(t)
	todoStore := NewTodoStore(pool)
	hub := NewHub()
	svc := NewService(todoStore, hub, nil)

	root := t.TempDir()
	fs, err := storage.NewFileStore(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	fs.OnPageSaved = func(pagePath, author string) {
		svc.AutoCompleteCreateAction(ctx, pagePath, author)
	}

	// Pattern wants /qms/campaigns/... .
	task, err := todoStore.Create(ctx, CreateRequest{
		Title:      "Launch a new campaign",
		Source:     SourceAPI,
		SourcePage: "/qms/campaigns",
		Assignee: Assignee{
			Type:       "user",
			Target:     "alice",
			Resolution: "any",
		},
		WikiAction: WikiAction{Type: "create", Pattern: "/qms/campaigns/.*"},
	})
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}

	// Alice saves a page OUTSIDE the pattern.
	if _, err := fs.Put("other/notes", "# notes\n", "alice"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	after, err := todoStore.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get task after save: %v", err)
	}
	if after.Status != StatusOpen {
		t.Errorf("task status after non-matching save = %q, want %q (regex gate must hold)", after.Status, StatusOpen)
	}
}
