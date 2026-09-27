package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"
)

// spy captures what the scanner asked the todo layer to do so tests
// can assert on the fanned-out set.
type spy struct {
	mu        sync.Mutex
	created   []TodoRequest
	existing  []ExistingLifecycleTodo
	cancelled []string
}

func (s *spy) create(_ context.Context, req TodoRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, req)
	// Simulate idempotency: also make the request visible to
	// ExistingLifecycleTodos so a second scan doesn't cancel-then-create.
	s.existing = append(s.existing, ExistingLifecycleTodo{
		ID:      "id-" + req.NodeKey,
		NodeKey: req.NodeKey,
	})
	return nil
}

func (s *spy) list(_ context.Context) ([]ExistingLifecycleTodo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ExistingLifecycleTodo, len(s.existing))
	copy(out, s.existing)
	return out, nil
}

func (s *spy) cancel(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = append(s.cancelled, id)
	// Remove from existing so the next scan does not try to cancel it
	// again.
	kept := s.existing[:0]
	for _, e := range s.existing {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	s.existing = kept
	return nil
}

// A rule with scope=/qms/.* and stale:30d, fed a mix of pages and
// attestations, should create exactly one todo per stale page.
func TestScanner_FanOutOneTodoPerStalePage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rule := Rule{
		ID:         "R1",
		SourcePage: "/admin/policies",
		ScopeRegex: "^/qms/.*",
		Condition:  Condition{Kind: "stale", Duration: 30 * 24 * time.Hour},
		Todo:       TodoTemplate{Title: "review {{path}}", Assign: "alice"},
	}
	pages := []string{"/qms/a", "/qms/b", "/qms/c", "/other/x", "/admin/policies"}
	updated := map[string]time.Time{
		"/qms/a":          now.Add(-40 * 24 * time.Hour),  // stale
		"/qms/b":          now.Add(-10 * 24 * time.Hour),  // fresh
		"/qms/c":          now.Add(-100 * 24 * time.Hour), // stale
		"/other/x":        now.Add(-500 * 24 * time.Hour), // stale but out of scope
		"/admin/policies": now.Add(-500 * 24 * time.Hour), // is the source page itself; must be skipped
	}

	s := &spy{}
	scanner := NewScanner(Deps{
		Rules:                  func() ([]Rule, error) { return []Rule{rule}, nil },
		ListPages:              func() []string { return pages },
		PageTags:               func(string) []string { return nil },
		Attesters:              []AttestationSource{func(p string) time.Time { return updated[p] }},
		CreateTodo:             s.create,
		ExistingLifecycleTodos: s.list,
		CancelTodo:             s.cancel,
		Now:                    func() time.Time { return now },
	})
	created, cancelled, err := scanner.Run(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if created != 2 {
		t.Errorf("created = %d, want 2 (/qms/a and /qms/c)", created)
	}
	if cancelled != 0 {
		t.Errorf("cancelled = %d, want 0", cancelled)
	}
	// Node keys should target the stale pages, NOT the source page.
	got := map[string]bool{}
	for _, req := range s.created {
		got[req.SourcePage] = true
	}
	if !got["/qms/a"] || !got["/qms/c"] {
		t.Errorf("expected fanout to /qms/a and /qms/c, got %+v", got)
	}
	if got["/qms/b"] || got["/other/x"] || got["/admin/policies"] {
		t.Errorf("unexpected fanout: %+v", got)
	}
}

// Re-running the scan with the same state must not create duplicates
// (idempotent by NodeKey via ExistingLifecycleTodos check).
func TestScanner_IdempotentAcrossRuns(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rule := Rule{
		ID:         "R2",
		SourcePage: "/rules",
		ScopeRegex: "^/x/.*",
		Condition:  Condition{Kind: "stale", Duration: 24 * time.Hour},
		Todo:       TodoTemplate{Title: "t", Assign: "a"},
	}
	s := &spy{}
	scanner := NewScanner(Deps{
		Rules:                  func() ([]Rule, error) { return []Rule{rule}, nil },
		ListPages:              func() []string { return []string{"/x/1"} },
		PageTags:               func(string) []string { return nil },
		Attesters:              []AttestationSource{func(string) time.Time { return now.Add(-48 * time.Hour) }},
		CreateTodo:             s.create,
		ExistingLifecycleTodos: s.list,
		CancelTodo:             s.cancel,
		Now:                    func() time.Time { return now },
	})
	if _, _, err := scanner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(s.created)
	if _, _, err := scanner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.created) != before {
		t.Errorf("second scan created more todos: before=%d after=%d", before, len(s.created))
	}
}

// When a rule disappears from the store, its previously-spawned todos
// must be reconciled away by the next scan.
func TestScanner_ReconcileAwayOrphanTodos(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rule := Rule{
		ID:         "R3",
		SourcePage: "/rules",
		ScopeRegex: "^/x/.*",
		Condition:  Condition{Kind: "stale", Duration: 24 * time.Hour},
		Todo:       TodoTemplate{Title: "t", Assign: "a"},
	}
	s := &spy{}
	present := []Rule{rule}
	scanner := NewScanner(Deps{
		Rules:                  func() ([]Rule, error) { return present, nil },
		ListPages:              func() []string { return []string{"/x/1"} },
		PageTags:               func(string) []string { return nil },
		Attesters:              []AttestationSource{func(string) time.Time { return now.Add(-48 * time.Hour) }},
		CreateTodo:             s.create,
		ExistingLifecycleTodos: s.list,
		CancelTodo:             s.cancel,
		Now:                    func() time.Time { return now },
	})
	// First run: rule fires, todo appears.
	_, _, _ = scanner.Run(context.Background())
	if len(s.created) != 1 || len(s.cancelled) != 0 {
		t.Fatalf("after first run created=%d cancelled=%d", len(s.created), len(s.cancelled))
	}
	// Rule deleted from the store; second run must reconcile the todo away.
	present = nil
	_, cancelled, err := scanner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 {
		t.Errorf("expected 1 cancellation, got %d", cancelled)
	}
}

// When a stale page is re-attested (edited or validated), the next scan
// should cancel its lifecycle todo.
func TestScanner_ReconcileAwayFreshPage(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rule := Rule{
		ID:         "R4",
		SourcePage: "/rules",
		ScopeRegex: "^/x/.*",
		Condition:  Condition{Kind: "stale", Duration: 24 * time.Hour},
		Todo:       TodoTemplate{Title: "t", Assign: "a"},
	}
	s := &spy{}
	stale := now.Add(-48 * time.Hour)
	updatedAt := stale
	scanner := NewScanner(Deps{
		Rules:                  func() ([]Rule, error) { return []Rule{rule}, nil },
		ListPages:              func() []string { return []string{"/x/1"} },
		PageTags:               func(string) []string { return nil },
		Attesters:              []AttestationSource{func(string) time.Time { return updatedAt }},
		CreateTodo:             s.create,
		ExistingLifecycleTodos: s.list,
		CancelTodo:             s.cancel,
		Now:                    func() time.Time { return now },
	})
	_, _, _ = scanner.Run(context.Background())
	if len(s.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(s.created))
	}
	// Page edited — fresh timestamp.
	updatedAt = now.Add(-1 * time.Minute)
	_, cancelled, _ := scanner.Run(context.Background())
	if cancelled != 1 {
		t.Errorf("expected 1 cancellation after page becomes fresh, got %d", cancelled)
	}
}

// DryRun returns the pages a rule fires on WITHOUT touching the todo
// store. Distinct from Run: no create, no cancel, no
// ExistingLifecycleTodos consultation. Used by the rendering box on
// the source page to show a live count.
func TestScanner_DryRun_FiresPagesOnly(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rule := Rule{
		ID:         "R6",
		SourcePage: "/admin/policies",
		ScopeRegex: "^/qms/.*",
		Condition:  Condition{Kind: "stale", Duration: 30 * 24 * time.Hour},
		Todo:       TodoTemplate{Title: "t", Assign: "a"},
	}
	pages := []string{"/qms/a", "/qms/b", "/other/x", "/admin/policies"}
	updated := map[string]time.Time{
		"/qms/a":          now.Add(-40 * 24 * time.Hour),
		"/qms/b":          now.Add(-10 * 24 * time.Hour),
		"/other/x":        now.Add(-500 * 24 * time.Hour),
		"/admin/policies": now.Add(-500 * 24 * time.Hour),
	}
	s := &spy{}
	scanner := NewScanner(Deps{
		Rules:                  func() ([]Rule, error) { return []Rule{rule}, nil },
		ListPages:              func() []string { return pages },
		PageTags:               func(string) []string { return nil },
		Attesters:              []AttestationSource{func(p string) time.Time { return updated[p] }},
		CreateTodo:             s.create,
		ExistingLifecycleTodos: s.list,
		CancelTodo:             s.cancel,
		Now:                    func() time.Time { return now },
	})
	fires := scanner.DryRun(rule)
	if len(fires) != 1 || fires[0] != "/qms/a" {
		t.Errorf("fires = %v, want [/qms/a]", fires)
	}
	if len(s.created) != 0 {
		t.Errorf("DryRun must not create todos, got %d", len(s.created))
	}
	if len(s.cancelled) != 0 {
		t.Errorf("DryRun must not cancel todos, got %d", len(s.cancelled))
	}
}

// A rule must never fire on its own source page in DryRun either.
// Same guarantee as Run — kept in a dedicated test so an accidental
// asymmetry between the two paths is caught.
func TestScanner_DryRun_SkipsSourcePage(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rule := Rule{
		ID:         "R7",
		SourcePage: "/qms/rules",
		ScopeRegex: "^/qms/.*",
		Condition:  Condition{Kind: "stale", Duration: 1 * time.Hour},
		Todo:       TodoTemplate{Title: "t", Assign: "a"},
	}
	// Source page's own timestamp is ancient; if DryRun didn't skip it
	// the source page would appear in the fires list.
	scanner := NewScanner(Deps{
		Rules:     func() ([]Rule, error) { return []Rule{rule}, nil },
		ListPages: func() []string { return []string{"/qms/rules", "/qms/other"} },
		PageTags:  func(string) []string { return nil },
		Attesters: []AttestationSource{func(string) time.Time { return now.Add(-10 * 24 * time.Hour) }},
		Now:       func() time.Time { return now },
	})
	fires := scanner.DryRun(rule)
	for _, p := range fires {
		if p == "/qms/rules" {
			t.Errorf("DryRun fired on its own source page: %v", fires)
		}
	}
	if len(fires) != 1 || fires[0] != "/qms/other" {
		t.Errorf("fires = %v, want [/qms/other]", fires)
	}
}

// DryRun must honour the same selector semantics as Run: OR-of-tags,
// NAND-of-exclude-tags. Regression guard against a shared-code
// divergence.
func TestScanner_DryRun_TagSelectors(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	rule := Rule{
		ID:          "R8",
		SourcePage:  "/rules",
		Tags:        []string{"sop", "rec"},
		ExcludeTags: []string{"archived"},
		Condition:   Condition{Kind: "stale", Duration: 1 * time.Hour},
		Todo:        TodoTemplate{Title: "t", Assign: "a"},
	}
	tagsByPage := map[string][]string{
		"/a": {"sop"},                // in scope, fires
		"/b": {"rec"},                // in scope, fires
		"/c": {"tpl"},                // out of scope (no matching tag)
		"/d": {"sop", "archived"},    // excluded
		"/e": {"rec", "sop", "misc"}, // in scope (OR: any matching tag is enough)
	}
	pages := []string{"/a", "/b", "/c", "/d", "/e"}
	scanner := NewScanner(Deps{
		Rules:     func() ([]Rule, error) { return []Rule{rule}, nil },
		ListPages: func() []string { return pages },
		PageTags:  func(p string) []string { return tagsByPage[p] },
		Attesters: []AttestationSource{func(string) time.Time { return now.Add(-24 * time.Hour) }},
		Now:       func() time.Time { return now },
	})
	fires := scanner.DryRun(rule)
	got := map[string]bool{}
	for _, p := range fires {
		got[p] = true
	}
	if !got["/a"] || !got["/b"] || !got["/e"] {
		t.Errorf("expected /a,/b,/e in fires, got %v", fires)
	}
	if got["/c"] || got["/d"] {
		t.Errorf("unexpected fires: %v", fires)
	}
}

// {{path}} and {{stale_days}} in the todo title must be substituted.
func TestScanner_TemplateVarsInTitle(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rule := Rule{
		ID:         "R5",
		SourcePage: "/rules",
		ScopeRegex: "^/x/.*",
		Condition:  Condition{Kind: "stale", Duration: 24 * time.Hour},
		Todo:       TodoTemplate{Title: "review {{path}} (stale {{stale_days}} days)", Assign: "a"},
	}
	s := &spy{}
	scanner := NewScanner(Deps{
		Rules:                  func() ([]Rule, error) { return []Rule{rule}, nil },
		ListPages:              func() []string { return []string{"/x/foo"} },
		PageTags:               func(string) []string { return nil },
		Attesters:              []AttestationSource{func(string) time.Time { return now.Add(-100 * 24 * time.Hour) }},
		CreateTodo:             s.create,
		ExistingLifecycleTodos: s.list,
		CancelTodo:             s.cancel,
		Now:                    func() time.Time { return now },
	})
	_, _, _ = scanner.Run(context.Background())
	if len(s.created) != 1 {
		t.Fatalf("expected 1 create")
	}
	want := "review /x/foo (stale 100 days)"
	if s.created[0].Title != want {
		t.Errorf("title = %q, want %q", s.created[0].Title, want)
	}
}
