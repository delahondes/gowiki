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

	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/config"
	"gowiki/backend/internal/reviewflow"
	"gowiki/backend/internal/storage"
)

// The template handlers touch a real FileStore (only real *FileStore
// satisfies TemplateResolver / TemplateMultiResolver / TemplateLister),
// plus optional ACL / reviewflow / user stores. Every test builds its
// fixtures inside t.TempDir() so runs are hermetic.

func newTemplateTestServer(t *testing.T, withReviewflow bool) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	contentRoot := filepath.Join(root, "content")
	if err := os.MkdirAll(contentRoot, 0o755); err != nil {
		t.Fatalf("mkdir content: %v", err)
	}
	store, err := storage.NewFileStore(contentRoot)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	s := &Server{store: store}
	if withReviewflow {
		metaRoot := filepath.Join(root, "meta")
		if err := os.MkdirAll(metaRoot, 0o755); err != nil {
			t.Fatalf("mkdir meta: %v", err)
		}
		rfStore := reviewflow.NewStore(metaRoot)
		cfgStore := &config.Store{}
		svc := reviewflow.NewService(rfStore, store.Attic, cfgStore)
		svc.SetPageReader(store)
		s.reviewflowService = svc
	}
	return s, contentRoot
}

// writeContent writes a page file directly on disk, bypassing PageStore
// so we don't fire the ref/include/attic indices — for fixtures those
// side-effects are noise.
func writeContent(t *testing.T, contentRoot, relPath, body string) {
	t.Helper()
	abs := filepath.Join(contentRoot, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// contextWithUser embeds `username` into a request context under the same
// key requireAuth would set. handleCreatePageFromTemplate reads it via
// UsernameFromContext.
func contextWithUser(username string) context.Context {
	return context.WithValue(context.Background(), usernameKey, username)
}

// requestForPagePath simulates chi's URL-param routing so `chi.URLParam(r, "*")`
// returns the given rest-path (like the wildcard route the real router uses).
func requestForPagePath(t *testing.T, method, wildcard string, body []byte) *http.Request {
	t.Helper()
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, "/api/template/"+wildcard, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, "/api/template/"+wildcard, nil)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", wildcard)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// ─── handleListTemplates ─────────────────────────────────

func TestListTemplates_Empty(t *testing.T) {
	t.Parallel()
	s, _ := newTemplateTestServer(t, false)

	rec := httptest.NewRecorder()
	s.handleListTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/templates", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Templates []storage.TemplateEntry `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Templates == nil {
		t.Errorf("templates is nil, want []")
	}
	if len(body.Templates) != 0 {
		t.Errorf("templates = %+v, want empty", body.Templates)
	}
}

func TestListTemplates_ReturnsAllVariants(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	// Root-level default and a named variant, plus a nested constrained one.
	writeContent(t, contentRoot, "_template.md", "{template}\n\nBody\n")
	writeContent(t, contentRoot, "_templatesop.md", "{template}\n\nSOP body\n")
	writeContent(t, contentRoot, "docs/_template_note.md", "{template}\n\nNote body\n")

	rec := httptest.NewRecorder()
	s.handleListTemplates(rec, httptest.NewRequest(http.MethodGet, "/api/templates", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Templates []storage.TemplateEntry `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(body.Templates) != 3 {
		t.Fatalf("templates count = %d, want 3 (%+v)", len(body.Templates), body.Templates)
	}
}

// ─── handleGetTemplate ───────────────────────────────────

func TestGetTemplate_NoTemplate(t *testing.T) {
	t.Parallel()
	s, _ := newTemplateTestServer(t, false)

	req := requestForPagePath(t, http.MethodGet, "somewhere/page", nil)
	rec := httptest.NewRecorder()
	s.handleGetTemplate(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetTemplate_ResolvesDefault(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md", "{template}\n\n# Default title\n\nBody.\n")

	req := requestForPagePath(t, http.MethodGet, "docs/newpage", nil)
	rec := httptest.NewRecorder()
	s.handleGetTemplate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Markdown     string `json:"markdown"`
		TemplatePath string `json:"template_path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(body.Markdown, "{template}") {
		t.Errorf("markdown missing {template} marker: %q", body.Markdown)
	}
	if !strings.Contains(body.TemplatePath, "_template") {
		t.Errorf("template_path = %q, want to contain _template", body.TemplatePath)
	}
}

func TestGetTemplate_MissingPath_400(t *testing.T) {
	t.Parallel()
	s, _ := newTemplateTestServer(t, false)

	// Empty wildcard.
	req := requestForPagePath(t, http.MethodGet, "", nil)
	rec := httptest.NewRecorder()
	s.handleGetTemplate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── handleGetTemplatesFor ───────────────────────────────

func TestGetTemplatesFor_ExactAndNamespaceInheritance(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "_template.md", "{template}\n\nRoot default\n")
	writeContent(t, contentRoot, "docs/_templatesop.md", "{template}\n\nSOP template\n")
	writeContent(t, contentRoot, "docs/_template_narrow.md", "{template}\n\nNarrow template\n")

	req := requestForPagePath(t, http.MethodGet, "docs/narrowfoo", nil)
	rec := httptest.NewRecorder()
	s.handleGetTemplatesFor(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Templates []storage.TemplateMatch `json:"templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	// Expect: root default + SOP (named) + narrow (constrained, matches "narrowfoo").
	seen := map[string]bool{}
	for _, m := range body.Templates {
		seen[m.Slug] = true
	}
	if !seen[""] || !seen["sop"] || !seen["narrow"] {
		t.Errorf("expected default+sop+narrow slugs; got %+v", body.Templates)
	}
}

func TestGetTemplatesFor_ConstraintFilters(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "_template_sop.md", "{template}\n\nSOP template\n")

	// Target page filename that does NOT start with "sop" — the constrained
	// template must not match.
	req := requestForPagePath(t, http.MethodGet, "index", nil)
	rec := httptest.NewRecorder()
	s.handleGetTemplatesFor(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Templates []storage.TemplateMatch `json:"templates"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Templates) != 0 {
		t.Errorf("constrained template leaked to non-matching leaf: %+v", body.Templates)
	}
}

func TestGetTemplatesFor_EmptyReturnsList(t *testing.T) {
	t.Parallel()
	s, _ := newTemplateTestServer(t, false)

	req := requestForPagePath(t, http.MethodGet, "anywhere/page", nil)
	rec := httptest.NewRecorder()
	s.handleGetTemplatesFor(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	// Body must be a valid JSON with `templates: []`, not `null`.
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"templates":[]`)) {
		t.Errorf("expected empty array in response; got %s", rec.Body.String())
	}
}

// ─── handleCreatePageFromTemplate ────────────────────────

func postCreate(t *testing.T, s *Server, req TemplateCreateRequest, user string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/pages/from-template", bytes.NewReader(body))
	if user != "" {
		r = r.WithContext(contextWithUser(user))
	}
	rec := httptest.NewRecorder()
	s.handleCreatePageFromTemplate(rec, r)
	return rec
}

func TestCreateFromTemplate_HappyPath_NoReviewflow(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md", "{template}\n\n{template-title}\n# Placeholder title\n\n{template-stamp}\n\nBody.\n")

	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/newpage",
		Title:        "My New Page",
		Summary:      "creating",
	}, "alice")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var result TemplateCreateResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Path != "/docs/newpage" {
		t.Errorf("Path = %q, want /docs/newpage", result.Path)
	}
	if result.TemplatePath != "/docs/_template" {
		t.Errorf("TemplatePath = %q, want /docs/_template", result.TemplatePath)
	}
	if result.Version <= 0 {
		t.Errorf("Version = %d, want positive", result.Version)
	}
	// Read the created page back and check the resolved title + stamp landed.
	page, err := s.store.Get("docs/newpage")
	if err != nil {
		t.Fatalf("get new page: %v", err)
	}
	if !strings.Contains(page.Markdown, "# My New Page") {
		t.Errorf("title override missing in created page: %q", page.Markdown)
	}
	if !strings.Contains(page.Markdown, "Created from template") {
		t.Errorf("template stamp sentence missing: %q", page.Markdown)
	}
}

func TestCreateFromTemplate_MissingTitle_409(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md", "{template}\n\nBody\n")

	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/newpage",
		// Title omitted.
	}, "alice")

	// invalid_target maps to 409 (see (*TemplateCreateError).httpStatus).
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 for missing title", rec.Code)
	}
	var body struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Kind != "invalid_target" {
		t.Errorf("kind = %q, want invalid_target", body.Kind)
	}
}

// When the template pins `target=`, the caller MUST land at the
// resolved path — otherwise the emitted document could end up in the
// wrong namespace and the template's authored rule would be silently
// ignored.

func TestCreateFromTemplate_TargetPinnedMismatch_Refuses(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md",
		"{template target=/docs/campaigns/{{slug}}}\n\n{template-title}\n# Placeholder\n\nBody.\n",
	)

	// Caller asks for a path the template's target= doesn't sanction.
	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/elsewhere",
		Title:        "Alpha",
		Summary:      "creating",
	}, "alice")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (target mismatch), body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Kind  string `json:"kind"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Kind != "invalid_target" {
		t.Errorf("kind = %q, want invalid_target", body.Kind)
	}
	if !strings.Contains(body.Error, "target") {
		t.Errorf("error message should explain the target pin, got %q", body.Error)
	}
	// No document created.
	if s.store.Exists("docs/elsewhere") {
		t.Errorf("document must not have been created when target= refused")
	}
}

func TestCreateFromTemplate_TargetPinnedMatch_Creates(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md",
		"{template target=/docs/campaigns/{{slug}}}\n\n{template-title}\n# Placeholder\n\nBody.\n",
	)

	// Caller respects the pinned target.
	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/campaigns/alpha-beta",
		Title:        "Alpha Beta",
		Summary:      "creating",
	}, "alice")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !s.store.Exists("docs/campaigns/alpha-beta") {
		t.Errorf("document should have been created at the pinned target")
	}
}

func TestCreateFromTemplate_NotAuthenticated_401(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md", "{template}\n\nBody\n")

	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/newpage",
		Title:        "T",
	}, "") // no user

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestCreateFromTemplate_TemplateNotFound_404(t *testing.T) {
	t.Parallel()
	s, _ := newTemplateTestServer(t, false)

	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/nope",
		Path:         "/docs/newpage",
		Title:        "T",
	}, "alice")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for missing template", rec.Code)
	}
}

func TestCreateFromTemplate_SourceIsNotATemplate_409(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	// No {template} directive → not a template page.
	writeContent(t, contentRoot, "docs/regular.md", "# Regular page\n\nContent.\n")

	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/regular",
		Path:         "/docs/newpage",
		Title:        "T",
	}, "alice")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (kind=not_template)", rec.Code)
	}
	var body struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Kind != "not_template" {
		t.Errorf("kind = %q, want not_template", body.Kind)
	}
}

func TestCreateFromTemplate_DestinationExists_409(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md", "{template}\n\nBody\n")
	writeContent(t, contentRoot, "docs/existing.md", "already here\n")

	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/existing",
		Title:        "T",
	}, "alice")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (kind=destination_exists)", rec.Code)
	}
	var body struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Kind != "destination_exists" {
		t.Errorf("kind = %q, want destination_exists", body.Kind)
	}
}

func TestCreateFromTemplate_SameAsTemplate_400(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md", "{template}\n\nBody\n")

	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/_template",
		Title:        "T",
	}, "alice")

	if rec.Code == http.StatusOK {
		t.Errorf("accepted destination equal to template — expected refusal")
	}
}

func TestCreateFromTemplate_ReviewflowNotValidated_409(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, true)
	// Template with a reviewflow directive but no confirmations recorded
	// against it — service.GetStatus returns IsFullyValidated=false.
	writeContent(t, contentRoot, "docs/_template.md",
		"{reviewflow version=1.0 author=alice reviewer=bob}\n\n"+
			"{template}\n\n"+
			"{template-title}\n# T\n\nBody\n")
	// Bootstrap reviewflow state from the directive. No confirmations yet,
	// so the resulting state is not-fully-validated — the invariant this
	// test pins. Errors here are non-fatal because a missing state file
	// is normal for a page never opened before.
	_, _ = s.reviewflowService.EnsureState("docs/_template")

	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/newpage",
		Title:        "New",
	}, "alice")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (kind=not_validated); body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Kind != "not_validated" {
		t.Errorf("kind = %q, want not_validated", body.Kind)
	}
}

// ─── ACL gating ──────────────────────────────────────────

func TestCreateFromTemplate_ACLDenied_403(t *testing.T) {
	t.Parallel()
	s, contentRoot := newTemplateTestServer(t, false)
	writeContent(t, contentRoot, "docs/_template.md", "{template}\n\nBody\n")

	metaRoot := filepath.Dir(contentRoot) + "/meta"
	if err := os.MkdirAll(metaRoot, 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}
	aclStore, err := auth.NewACLStore(metaRoot)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}
	userStore, err := auth.NewUserStore(metaRoot)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	s.aclStore = aclStore
	s.userStore = userStore

	// Default ACL is deny; without any rule granting alice, edit is refused.
	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/newpage",
		Title:        "New",
	}, "alice")

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// EF07 regulatory audit: a document issued from a `{template}` carries
// a stamp naming the template AND the exact version it was issued
// from. When the template is later modified — new heading, new
// version — the stamp on the previously-issued document MUST NOT
// silently follow along. A regulator inspecting a document in 2028
// must see the template state as it was on the day of issuance,
// not the current source. That's why the stamp is resolved once at
// creation time and written into the document's markdown, never
// re-resolved on read.
func TestCreateFromTemplate_Stamp_StaysFrozenWhenTemplateChanges(t *testing.T) {
	t.Parallel()
	s, _ := newTemplateTestServer(t, false)

	// Seed the template via PageStore so it carries a real version.
	if _, err := s.store.(*storage.FileStore).Put(
		"/docs/_template",
		"{template}\n\n{template-title}\n# Placeholder\n\n{template-stamp}\n\nOriginal body.\n",
		"alice",
	); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	tplBefore, err := s.store.Get("/docs/_template")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	issuedFromVersion := tplBefore.Meta.Version
	if issuedFromVersion <= 0 {
		t.Fatalf("template version = %d, want positive", issuedFromVersion)
	}

	// Issue a document from the template's current version.
	rec := postCreate(t, s, TemplateCreateRequest{
		TemplatePath: "/docs/_template",
		Path:         "/docs/issued",
		Title:        "Issued document",
		Summary:      "issuance",
	}, "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("issuance: status = %d, body=%s", rec.Code, rec.Body.String())
	}

	// The document's markdown must contain the resolved stamp with
	// the version it was issued from (a ?v=N link).
	issued, err := s.store.Get("/docs/issued")
	if err != nil {
		t.Fatalf("read issued: %v", err)
	}
	frozenSnippet := "?v=" + itoa(issuedFromVersion)
	if !strings.Contains(issued.Markdown, frozenSnippet) {
		t.Fatalf("issued document must reference template version %d, got:\n%s", issuedFromVersion, issued.Markdown)
	}
	if strings.Contains(issued.Markdown, "{template-stamp}") {
		t.Errorf("issued document still carries the unresolved {template-stamp} directive: %s", issued.Markdown)
	}

	// Modify the template — this bumps its version. Anything that
	// re-resolves the stamp dynamically would now silently retarget
	// the issued document at the newer version.
	if _, err := s.store.PutWithSummary(
		"/docs/_template",
		"{template}\n\n{template-title}\n# Placeholder\n\n{template-stamp}\n\nCompletely rewritten body.\n",
		"bob",
		"template overhaul",
	); err != nil {
		t.Fatalf("modify template: %v", err)
	}
	tplAfter, err := s.store.Get("/docs/_template")
	if err != nil {
		t.Fatalf("re-read template: %v", err)
	}
	if tplAfter.Meta.Version <= issuedFromVersion {
		t.Fatalf("template version didn't bump: before=%d after=%d", issuedFromVersion, tplAfter.Meta.Version)
	}

	// Re-read the issued document — its stamp MUST still point at the
	// original version. Freeze-at-creation is the regulatory
	// invariant this test is here to protect.
	stillIssued, err := s.store.Get("/docs/issued")
	if err != nil {
		t.Fatalf("re-read issued: %v", err)
	}
	if !strings.Contains(stillIssued.Markdown, frozenSnippet) {
		t.Errorf("stamp lost the frozen version reference after template change; got:\n%s", stillIssued.Markdown)
	}
	newerSnippet := "?v=" + itoa(tplAfter.Meta.Version)
	if strings.Contains(stillIssued.Markdown, newerSnippet) {
		t.Errorf("stamp drifted to the newer template version %d — a regulator would see the wrong source:\n%s", tplAfter.Meta.Version, stillIssued.Markdown)
	}
	// The body still contains what was in the template AT issuance —
	// "Original body" — not the later rewrite.
	if !strings.Contains(stillIssued.Markdown, "Original body") {
		t.Errorf("issued document lost its original body:\n%s", stillIssued.Markdown)
	}
	if strings.Contains(stillIssued.Markdown, "Completely rewritten body") {
		t.Errorf("issued document leaked the newer template body — content freeze broken:\n%s", stillIssued.Markdown)
	}
}

// itoa avoids pulling strconv into this test file just for the tiny
// version-to-string interpolation the stamp assertion needs.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
