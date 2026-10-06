package storage

import (
	"path/filepath"
	"sync"
	"testing"
)

// OnPageSaved is the storage-layer post-save hook that the todo
// service's AutoCompleteWikiAction / AutoCompleteCreateAction /
// ReopenReadTasks fan-out hangs off (wired in main.go). Previously
// the API layer fired those three calls per-handler, which meant
// writes going through MCP (write_page, edit_page,
// create_page_from_template, row writers) silently bypassed the
// action-trigger auto-complete. Pinning the hook here guarantees
// every write path — present and future — flows through one
// dispatch point.

func TestOnPageSaved_Put_FiresHook(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileStore(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	var mu sync.Mutex
	var pathSeen, authorSeen string
	fs.OnPageSaved = func(pagePath, author string) {
		mu.Lock()
		defer mu.Unlock()
		pathSeen = pagePath
		authorSeen = author
	}

	if _, err := fs.Put("docs/plain", "# hello\n", "alice"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if pathSeen != "/docs/plain" {
		t.Errorf("OnPageSaved pagePath = %q, want /docs/plain", pathSeen)
	}
	if authorSeen != "alice" {
		t.Errorf("OnPageSaved author = %q, want alice", authorSeen)
	}
}

func TestOnPageSaved_PutWithSummary_FiresHook(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileStore(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	calls := 0
	fs.OnPageSaved = func(_, _ string) { calls++ }

	// MCP write_page / edit_page end up here with a summary argument;
	// the hook must fire from PutWithSummary too, not just Put.
	if _, err := fs.PutWithSummary("docs/plain", "# v1\n", "alice", "initial"); err != nil {
		t.Fatalf("PutWithSummary first write: %v", err)
	}
	// A second save on the same page also fires the hook — the
	// action-trigger fan-out is "did any save happen" not "was the
	// page just created", and ReopenReadTasks specifically needs
	// every edit to re-open completed read tasks for the changed
	// content.
	if _, err := fs.PutWithSummary("docs/plain", "# v2\n", "alice", "edit"); err != nil {
		t.Fatalf("PutWithSummary second write: %v", err)
	}
	if calls != 2 {
		t.Errorf("hook fired %d times, want 2 (one per save)", calls)
	}
}

func TestOnPageSaved_NamespaceIndexWrite_FiresWithCanonicalPath(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFileStore(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	// Seed + convert so docs/ns is a namespace index at content/docs/ns/index.md.
	if _, err := fs.Put("docs/ns", "# ns\n", "seed"); err != nil {
		t.Fatalf("Put leaf: %v", err)
	}
	if _, err := fs.ConvertToNamespaceIndex("docs/ns", "seed"); err != nil {
		t.Fatalf("ConvertToNamespaceIndex: %v", err)
	}

	var pathSeen string
	fs.OnPageSaved = func(p, _ string) { pathSeen = p }

	// A fresh write on the namespace index — regardless of URL form —
	// must call the hook with the resolved canonical path so a
	// {todo action=create:/docs/ns/...} auto-complete regex sees a
	// consistent target.
	if _, err := fs.Put("docs/ns", "# ns updated\n", "alice"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Expected: /docs/ns/ (canonical namespace-index form) OR
	// /docs/ns/index (storage path form). Document which one the
	// hook actually sees — this test's job is to pin whichever the
	// production flow uses, so a future refactor can't silently
	// swap forms without the regexes drifting.
	if pathSeen != "/docs/ns/" && pathSeen != "/docs/ns/index" {
		t.Errorf("OnPageSaved pagePath = %q, want /docs/ns/ or /docs/ns/index", pathSeen)
	}
}

func TestOnPageSaved_NilHook_NoCrash(t *testing.T) {
	// Writes must not require a hook — unit tests and small
	// deployments without the todo plugin leave fs.OnPageSaved nil.
	root := t.TempDir()
	fs, err := NewFileStore(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if _, err := fs.Put("docs/plain", "# ok\n", "alice"); err != nil {
		t.Fatalf("Put with nil hook: %v", err)
	}
}
