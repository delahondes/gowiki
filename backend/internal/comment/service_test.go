package comment

import "testing"

// CountOpenThreads is the probe the lifecycle plugin's `comments_open`
// rule consults on every scan. Its correctness is what makes the
// QARA-style "which documents still carry unresolved reader feedback"
// overview honest; a miscount here would either surface docs the
// author has actually cleared or silently hide docs still needing
// attention. Pin the counting rules.

func newTestService(t *testing.T) *Service {
	t.Helper()
	return NewService(NewStore(t.TempDir()))
}

func mustCreate(t *testing.T, svc *Service, page, text, author, parentID string) Comment {
	t.Helper()
	c, err := svc.Create(page, Anchor{Selected: text}, text, author, parentID, false)
	if err != nil {
		t.Fatalf("Create %s: %v", text, err)
	}
	return c
}

func TestCountOpenThreads_NoState(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	if got := svc.CountOpenThreads("/never-touched"); got != 0 {
		t.Errorf("no state file → 0, got %d", got)
	}
}

func TestCountOpenThreads_EmptyList(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	// Create then remove — file exists but list is empty.
	c := mustCreate(t, svc, "/p", "hello", "alice", "")
	if err := svc.Delete("/p", c.ID, "alice", true); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := svc.CountOpenThreads("/p"); got != 0 {
		t.Errorf("empty list → 0, got %d", got)
	}
}

func TestCountOpenThreads_MixedResolution(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	// Two top-level threads, one still open.
	open := mustCreate(t, svc, "/p", "still open", "alice", "")
	resolved := mustCreate(t, svc, "/p", "settled", "bob", "")
	if err := svc.Resolve("/p", resolved.ID, "bob"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := svc.CountOpenThreads("/p"); got != 1 {
		t.Errorf("1 open of 2 threads → 1, got %d", got)
	}
	_ = open
}

func TestCountOpenThreads_RepliesDontCount(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	// One top-level thread with two replies. The replies inherit the
	// parent's resolution state and must NOT be counted as their own
	// threads — resolution is per-thread, not per-message.
	parent := mustCreate(t, svc, "/p", "root", "alice", "")
	mustCreate(t, svc, "/p", "reply 1", "bob", parent.ID)
	mustCreate(t, svc, "/p", "reply 2", "carol", parent.ID)
	if got := svc.CountOpenThreads("/p"); got != 1 {
		t.Errorf("1 thread with 2 replies → 1 (replies don't count), got %d", got)
	}
	// Resolving the top-level thread also removes its replies from the
	// count.
	if err := svc.Resolve("/p", parent.ID, "alice"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := svc.CountOpenThreads("/p"); got != 0 {
		t.Errorf("thread resolved → 0, got %d", got)
	}
}

func TestCountOpenThreads_AllResolved(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	a := mustCreate(t, svc, "/p", "one", "alice", "")
	b := mustCreate(t, svc, "/p", "two", "bob", "")
	c := mustCreate(t, svc, "/p", "three", "carol", "")
	for _, id := range []string{a.ID, b.ID, c.ID} {
		if err := svc.Resolve("/p", id, "alice"); err != nil {
			t.Fatalf("Resolve %s: %v", id, err)
		}
	}
	if got := svc.CountOpenThreads("/p"); got != 0 {
		t.Errorf("all resolved → 0, got %d", got)
	}
}
