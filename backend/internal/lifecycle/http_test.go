package lifecycle

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// newTestStore + newTestScanner build the minimum lifecycle plumbing an
// HTTP handler test needs: a real Store backed by a temp dir (so
// RulesForSourcePage exercises the JSON round-trip), plus a Scanner
// wired to fake page/attestation sources. Kept in this file so the
// test-only helper stays close to the test.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "meta")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return s
}

func newTestScanner(pages []string, updated map[string]time.Time, tags map[string][]string, now time.Time) *Scanner {
	return NewScanner(Deps{
		ListPages: func() []string { return pages },
		PageTags:  func(p string) []string { return tags[p] },
		Attesters: []AttestationSource{func(p string) time.Time { return updated[p] }},
		Now:       func() time.Time { return now },
	})
}

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mount(store *Store, scanner *Scanner) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/plugin/lifecycle/v1", func(r chi.Router) {
		RegisterRoutes(r, store, scanner)
	})
	return r
}

// Missing source_page must be rejected — the endpoint has no other
// way to identify what rules to report on.
func TestHTTP_Status_MissingSourcePage(t *testing.T) {
	t.Parallel()
	h := mount(newTestStore(t), nil)
	rec := doGet(t, h, "/api/plugin/lifecycle/v1/status")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", rec.Code)
	}
}

// A source page with no rules must return an empty (but well-formed)
// list — the frontend treats this as "no lifecycle to report."
func TestHTTP_Status_EmptyRules(t *testing.T) {
	t.Parallel()
	h := mount(newTestStore(t), nil)
	rec := doGet(t, h, "/api/plugin/lifecycle/v1/status?source_page=/admin/policies")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 0 {
		t.Errorf("rules = %d, want 0", len(resp.Rules))
	}
}

// One rule, zero fires: the rendering box's "all in order" branch. The
// endpoint must return fires_count=0 with no sample_pages field
// serialized (omitempty semantics).
func TestHTTP_Status_ZeroFires(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := newTestStore(t)
	rules, errs := Parse("/admin/policies", `{lifecycle scope="^/qms/.*" when=stale:30d title="review {{path}}" assign=alice}`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if err := store.SetPageRules("/admin/policies", rules); err != nil {
		t.Fatalf("set: %v", err)
	}
	// All pages are fresh — no fires.
	scanner := newTestScanner(
		[]string{"/qms/a", "/qms/b"},
		map[string]time.Time{
			"/qms/a": now.Add(-1 * 24 * time.Hour),
			"/qms/b": now.Add(-2 * 24 * time.Hour),
		},
		nil,
		now,
	)
	rec := doGet(t, mount(store, scanner), "/api/plugin/lifecycle/v1/status?source_page=/admin/policies")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(resp.Rules))
	}
	if resp.Rules[0].FiresCount != 0 {
		t.Errorf("fires_count = %d, want 0", resp.Rules[0].FiresCount)
	}
	if len(resp.Rules[0].SamplePages) != 0 {
		t.Errorf("sample_pages = %v, want empty", resp.Rules[0].SamplePages)
	}
	// The reconstructed when= should be days-based since we don't keep
	// the original text; verify the round-trip yields something the
	// frontend can pretty-print.
	if resp.Rules[0].Rule.When != "stale:30d" {
		t.Errorf("when = %q, want stale:30d", resp.Rules[0].Rule.When)
	}
}

// One rule fires on several pages: the frontend uses this count to
// render the loud red variant. Sample pages must be present and
// bounded.
func TestHTTP_Status_FiresWithSampleCap(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := newTestStore(t)
	rules, errs := Parse("/admin/policies", `{lifecycle scope="^/qms/.*" when=stale:30d title="review {{path}}" assign=alice}`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if err := store.SetPageRules("/admin/policies", rules); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Ten stale pages — sample must cap at 5.
	pages := make([]string, 0, 10)
	updated := make(map[string]time.Time, 10)
	for i := 0; i < 10; i++ {
		p := "/qms/p" + string(rune('0'+i))
		pages = append(pages, p)
		updated[p] = now.Add(-90 * 24 * time.Hour)
	}
	scanner := newTestScanner(pages, updated, nil, now)
	rec := doGet(t, mount(store, scanner), "/api/plugin/lifecycle/v1/status?source_page=/admin/policies")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 1 {
		t.Fatalf("rules = %d", len(resp.Rules))
	}
	if resp.Rules[0].FiresCount != 10 {
		t.Errorf("fires_count = %d, want 10", resp.Rules[0].FiresCount)
	}
	if got := len(resp.Rules[0].SamplePages); got != 5 {
		t.Errorf("sample_pages length = %d, want 5 (capped)", got)
	}
}

// When no scanner is wired (nil), the endpoint must still return the
// rules — just with fires_count=0 — so a mis-wired instance degrades
// to the passive-safe reading instead of a 500.
func TestHTTP_Status_NoScanner(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	rules, errs := Parse("/admin/policies", `{lifecycle scope="^/qms/.*" when=stale:30d title=review assign=alice}`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if err := store.SetPageRules("/admin/policies", rules); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, mount(store, nil), "/api/plugin/lifecycle/v1/status?source_page=/admin/policies")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 1 || resp.Rules[0].FiresCount != 0 {
		t.Errorf("expected 1 rule with fires_count=0, got %+v", resp.Rules)
	}
}

// The RuleView must echo every attribute the frontend uses to match
// its own PM node against the returned rule set (title + assign are
// the identity pair, and the extra fields are what the details
// tooltip renders).
func TestHTTP_Status_RuleViewShape(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	rules, errs := Parse("/admin/policies", `{lifecycle scope="^/qms/.*" tags=sop,rec exclude_tags=archived when=stale:60d title="check {{path}}" assign=qms-lead priority=high action="edit:."}`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if err := store.SetPageRules("/admin/policies", rules); err != nil {
		t.Fatal(err)
	}
	rec := doGet(t, mount(store, nil), "/api/plugin/lifecycle/v1/status?source_page=/admin/policies")
	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rules) != 1 {
		t.Fatalf("rules = %d", len(resp.Rules))
	}
	rv := resp.Rules[0].Rule
	if rv.Scope != "^/qms/.*" {
		t.Errorf("scope = %q", rv.Scope)
	}
	// Tags come back alphabetically sorted — parse.go normalises the
	// slice for deterministic ID hashing, so "sop,rec" becomes rec,sop.
	if strings.Join(rv.Tags, ",") != "rec,sop" {
		t.Errorf("tags = %v", rv.Tags)
	}
	if strings.Join(rv.ExcludeTags, ",") != "archived" {
		t.Errorf("exclude_tags = %v", rv.ExcludeTags)
	}
	if rv.When != "stale:60d" {
		t.Errorf("when = %q", rv.When)
	}
	if rv.Title != "check {{path}}" {
		t.Errorf("title = %q", rv.Title)
	}
	if rv.Assign != "qms-lead" {
		t.Errorf("assign = %q", rv.Assign)
	}
	if rv.Priority != "high" {
		t.Errorf("priority = %q", rv.Priority)
	}
	if rv.Action != "edit:." {
		t.Errorf("action = %q", rv.Action)
	}
	if resp.Rules[0].SourcePage != "/admin/policies" {
		t.Errorf("source_page = %q", resp.Rules[0].SourcePage)
	}
}

// formatCondition is unexported plumbing — test it directly to pin
// the days-based reconstruction, since the endpoint round-trips
// through it and the frontend expects a canonical shape.
func TestFormatCondition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   Condition
		want string
	}{
		{Condition{}, ""},
		{Condition{Kind: "stale", Duration: 30 * 24 * time.Hour}, "stale:30d"},
		{Condition{Kind: "stale", Duration: 900 * 24 * time.Hour}, "stale:900d"},
		{Condition{Kind: "unknown"}, "unknown"},
	}
	for _, c := range cases {
		if got := formatCondition(c.in); got != c.want {
			t.Errorf("formatCondition(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}
