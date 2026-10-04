package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/config"
	"gowiki/backend/internal/reviewflow"
	"gowiki/backend/internal/storage"
)

// newDraftsPrefillTestServer builds a Server with every dependency the
// _template.md prefill touches: FileStore (for TemplateResolver +
// Draft + Attic), reviewflow service (for the validation gate), and
// no ACL (prefill is reached through requireAuth upstream).
func newDraftsPrefillTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	contentRoot := filepath.Join(root, "content")
	metaRoot := filepath.Join(root, "meta")
	if err := os.MkdirAll(contentRoot, 0o755); err != nil {
		t.Fatalf("mkdir content: %v", err)
	}
	if err := os.MkdirAll(metaRoot, 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}
	store, err := storage.NewFileStore(contentRoot)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	rfStore := reviewflow.NewStore(metaRoot)
	cfgStore := &config.Store{}
	svc := reviewflow.NewService(rfStore, store.Attic, cfgStore)
	svc.SetPageReader(store)
	return &Server{
		store:             store,
		draftManager:      store.Drafts,
		atticStore:        store.Attic,
		reviewflowService: svc,
	}, contentRoot
}

// writeContentFile drops a markdown file straight on disk (bypasses the
// PageStore side-effects, same shortcut templates_test.go uses).
func writeContentFile(t *testing.T, contentRoot, relPath, body string) {
	t.Helper()
	abs := filepath.Join(contentRoot, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// callEnterEdit invokes handleEnterEdit with the given wildcard page
// path (no leading slash — the router feeds chi.URLParam the rest-of-url).
func callEnterEdit(t *testing.T, s *Server, pagePath, user string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/edit/"+pagePath, bytes.NewReader(nil))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", pagePath)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if user != "" {
		ctx = context.WithValue(ctx, usernameKey, user)
	}
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	s.handleEnterEdit(rec, req)
	return rec
}

// ── _template.md with NO template directives at all: raw prefill ─────

func TestEnterEdit_LegacyUnderscoreTemplate_RawPrefill(t *testing.T) {
	t.Parallel()
	s, contentRoot := newDraftsPrefillTestServer(t)
	// Pure DokuWiki-style boilerplate — no {template}, no {template-*}.
	// Prefill copies it verbatim; the resolver has nothing to do.
	writeContentFile(t, contentRoot, "docs/_template.md",
		"# Placeholder\n\nBody paragraph that mentions {something-else}.\n")

	rec := callEnterEdit(t, s, "docs/newpage", "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body.Markdown, "# Placeholder") {
		t.Errorf("heading should survive raw prefill; got %q", body.Markdown)
	}
	if !strings.Contains(body.Markdown, "{something-else}") {
		t.Errorf("non-template directive-shaped text must pass through; got %q", body.Markdown)
	}
}

// ── _template.md with ONLY {template-*} directives (row-bound-template
//    convention): resolve the directives, no {template} marker needed ─

func TestEnterEdit_UnderscoreTemplateWithoutMarker_StillResolves(t *testing.T) {
	t.Parallel()
	s, contentRoot := newDraftsPrefillTestServer(t)
	// Row-bound template layout: no {template} marker (the Create
	// button would be misleading on a template whose documents come
	// from database row insertion), but {template-stamp} and
	// {template-todo} still need to resolve on every row-bound page's
	// prefill. New rule: ANY {template-*} is enough.
	writeContentFile(t, contentRoot, "docs/_template.md",
		"# Row page\n\n"+
			"{template-stamp}\n\n"+
			"{template-todo title=\"Fill in row data\" assign=alice}\n\n"+
			"Row body.\n")

	rec := callEnterEdit(t, s, "docs/newrow", "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	// Stamp substituted to the "Created from template..." sentence.
	if !strings.Contains(body.Markdown, "Created from template") {
		t.Errorf("stamp should have been resolved despite no {template} marker; got %q", body.Markdown)
	}
	// {template-todo} became a real {todo} with args passed through.
	if !strings.Contains(body.Markdown, "{todo") || !strings.Contains(body.Markdown, "assign=alice") {
		t.Errorf("{template-todo} should resolve to {todo}; got %q", body.Markdown)
	}
	if strings.Contains(body.Markdown, "{template-stamp}") || strings.Contains(body.Markdown, "{template-todo") {
		t.Errorf("template-* placeholders must be consumed; got %q", body.Markdown)
	}
	// Entire file is payload — the heading above the first template-*
	// directive survives (no {template} marker to split at).
	if !strings.Contains(body.Markdown, "Row body.") {
		t.Errorf("body text after the directives must survive; got %q", body.Markdown)
	}
}

// ── _template.md WITH {template} marker: full resolution ─────────────

func TestEnterEdit_UnderscoreTemplateWithMarker_ResolvesPayload(t *testing.T) {
	t.Parallel()
	s, contentRoot := newDraftsPrefillTestServer(t)
	writeContentFile(t, contentRoot, "docs/_template.md",
		"Tracking metadata lives here.\n\n"+
			"{template}\n\n"+
			"{template-title}\n# Placeholder title\n\n"+
			"{template-stamp}\n\n"+
			"{template-todo title=\"Read and acknowledge\" assign=alice}\n\n"+
			"Body paragraph.\n",
	)

	rec := callEnterEdit(t, s, "docs/newpage", "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	// Title substitution: leaf of pagePath is "newpage".
	if !strings.Contains(body.Markdown, "# newpage") {
		t.Errorf("title substitution missing (expected '# newpage'); got %q", body.Markdown)
	}
	// {template-title} marker is stripped.
	if strings.Contains(body.Markdown, "{template-title}") {
		t.Errorf("{template-title} marker should be dropped; got %q", body.Markdown)
	}
	// {template-stamp} becomes a stamp sentence.
	if !strings.Contains(body.Markdown, "Created from template") {
		t.Errorf("template stamp sentence missing; got %q", body.Markdown)
	}
	// {template-todo} becomes real {todo}.
	if !strings.Contains(body.Markdown, "{todo") || !strings.Contains(body.Markdown, "assign=alice") {
		t.Errorf("{template-todo} should resolve to {todo}; got %q", body.Markdown)
	}
	// Tracking metadata above {template} must not leak into the payload.
	if strings.Contains(body.Markdown, "Tracking metadata lives here") {
		t.Errorf("tracking metadata above {template} must not reach prefill; got %q", body.Markdown)
	}
}

// ── Validation gate: refuses prefill when reviewflow is open ─────────

func TestEnterEdit_UnderscoreTemplateWithUnvalidatedReviewflow_409(t *testing.T) {
	t.Parallel()
	s, contentRoot := newDraftsPrefillTestServer(t)
	// Template declares a reviewflow but nothing is signed → the
	// prefill guard must refuse exactly like the explicit Create path.
	writeContentFile(t, contentRoot, "docs/_template.md",
		"{reviewflow version=1.0 author=alice reviewer=bob}\n\n"+
			"{template}\n\n"+
			"{template-title}\n# T\n\nBody\n")
	_, _ = s.reviewflowService.EnsureState("docs/_template")

	rec := callEnterEdit(t, s, "docs/newpage", "alice")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (not_validated); body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Kind         string `json:"kind"`
		TemplatePath string `json:"template_path"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Kind != "not_validated" {
		t.Errorf("kind = %q, want not_validated", body.Kind)
	}
	if body.TemplatePath != "/docs/_template" {
		t.Errorf("template_path = %q, want /docs/_template", body.TemplatePath)
	}
}

// ── Target pattern: pin mismatch refuses prefill ──────────────────────

func TestEnterEdit_UnderscoreTemplateTargetPinned_Mismatch_409(t *testing.T) {
	t.Parallel()
	s, contentRoot := newDraftsPrefillTestServer(t)
	writeContentFile(t, contentRoot, "docs/_template.md",
		"{template target=/docs/campaigns/{{slug}}}\n\n"+
			"{template-title}\n# Placeholder\n\nBody.\n",
	)

	// User tries to enter edit on a path that doesn't match the pinned target.
	rec := callEnterEdit(t, s, "docs/elsewhere", "alice")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (invalid_target); body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Kind != "invalid_target" {
		t.Errorf("kind = %q, want invalid_target", body.Kind)
	}
}

// ── Target pattern: pin match accepts and resolves ────────────────────

func TestEnterEdit_UnderscoreTemplateTargetPinned_Match_Resolves(t *testing.T) {
	t.Parallel()
	s, contentRoot := newDraftsPrefillTestServer(t)
	writeContentFile(t, contentRoot, "docs/_template.md",
		"{template target=/docs/campaigns/{{slug}}}\n\n"+
			"{template-title}\n# Placeholder\n\nBody.\n",
	)

	// Caller lands on the resolved pattern — slug comes from leaf name.
	rec := callEnterEdit(t, s, "docs/campaigns/alpha-beta", "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body.Markdown, "# alpha-beta") {
		t.Errorf("title substitution missing (expected '# alpha-beta'); got %q", body.Markdown)
	}
}

// ── titleFromPagePath helper ──────────────────────────────────────────

func TestTitleFromPagePath(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"/foo/bar", "bar"},
		{"/foo/bar/", "bar"},
		{"foo/bar", "bar"},
		{"/", "index"},
		{"", "index"},
		{"single", "single"},
	}
	for _, c := range cases {
		if got := titleFromPagePath(c.in); got != c.want {
			t.Errorf("titleFromPagePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
