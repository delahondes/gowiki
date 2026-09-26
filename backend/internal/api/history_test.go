package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/storage"
)

// newHistoryServer builds a Server wired to a real FileStore + Attic +
// DraftStore in a tempdir. Just enough surface for the three history
// handlers under test — no auth or database.
func newHistoryServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	contentRoot := root + "/content"
	store, err := storage.NewFileStore(contentRoot)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return &Server{
		store:        store,
		atticStore:   store.Attic,
		draftManager: store.Drafts,
	}
}

// urlWithParam invokes a chi-routed handler with `*` set to the given
// page path.
func urlWithParam(handler http.HandlerFunc, method, url, pathParam string, ctx context.Context) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", pathParam)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// putPage writes a page through the store and returns its post-save version.
func putPage(t *testing.T, s *Server, pagePath, content string) int64 {
	t.Helper()
	pageStore, ok := s.store.(*storage.FileStore)
	if !ok {
		t.Fatal("store is not FileStore")
	}
	res, err := pageStore.Put(pagePath, content, "tester")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return res.Page.Meta.Version
}

// ── handlePageHistory ────────────────────────────────────

func TestHandlePageHistory_UnknownPage_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	rec := urlWithParam(s.handlePageHistory, http.MethodGet, "/api/history/nope", "nope", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	// The store returns nil entries for unknown pages, which becomes [].
	if versions, ok := body["versions"].([]any); !ok || len(versions) != 0 {
		t.Errorf("versions = %v, want []", body["versions"])
	}
}

func TestHandlePageHistory_MultipleVersions(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "v1")
	putPage(t, s, "/doc", "v2")
	putPage(t, s, "/doc", "v3")

	rec := urlWithParam(s.handlePageHistory, http.MethodGet, "/api/history/doc", "doc", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Versions []storage.AtticEntry `json:"versions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(body.Versions))
	}
	for i, want := range []int64{1, 2, 3} {
		if body.Versions[i].Version != want {
			t.Errorf("versions[%d].Version = %d, want %d", i, body.Versions[i].Version, want)
		}
		if body.Versions[i].Author != "tester" {
			t.Errorf("versions[%d].Author = %q, want tester", i, body.Versions[i].Author)
		}
	}
}

func TestHandlePageHistory_MissingPath_400(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	rec := urlWithParam(s.handlePageHistory, http.MethodGet, "/api/history/", "", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ── handlePageVersion ────────────────────────────────────

func TestHandlePageVersion_ReturnsExactBytes(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "first")
	putPage(t, s, "/doc", "second")

	rec := urlWithParam(s.handlePageVersion, http.MethodGet, "/api/versions/doc?v=1", "doc", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Version  int64  `json:"version"`
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Version != 1 || body.Markdown != "first" {
		t.Errorf("body = %+v, want {v=1, md=first}", body)
	}
}

func TestHandlePageVersion_Unknown_404(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "only")
	rec := urlWithParam(s.handlePageVersion, http.MethodGet, "/api/versions/doc?v=42", "doc", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandlePageVersion_InvalidVersion_400(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "only")
	for _, v := range []string{"", "abc", "0", "-1"} {
		rec := urlWithParam(s.handlePageVersion, http.MethodGet, "/api/versions/doc?v="+v, "doc", nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("v=%q: status = %d, want 400", v, rec.Code)
		}
	}
}

// ── handlePageDiff ──────────────────────────────────────

func TestHandlePageDiff_BetweenAtticVersions(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "line one\n")
	putPage(t, s, "/doc", "line one\nline two\n")

	rec := urlWithParam(s.handlePageDiff, http.MethodGet, "/api/diff/doc?from=1&to=2", "doc", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		From  int64              `json:"from"`
		To    int64              `json:"to"`
		Hunks []storage.DiffHunk `json:"hunks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.From != 1 || body.To != 2 {
		t.Errorf("from/to = %d/%d, want 1/2", body.From, body.To)
	}
	// There must be at least one hunk: something changed between v1 and v2.
	if len(body.Hunks) == 0 {
		t.Errorf("hunks empty; expected diff between v1 and v2")
	}
}

func TestHandlePageDiff_ToCurrentPublished(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "original\n")
	putPage(t, s, "/doc", "current text\n")

	rec := urlWithParam(s.handlePageDiff, http.MethodGet, "/api/diff/doc?from=1&to=0", "doc", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		To    int64              `json:"to"`
		Hunks []storage.DiffHunk `json:"hunks"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.To != 0 {
		t.Errorf("to = %d, want 0 (current published)", body.To)
	}
	// Reconstruct the "to" text from the hunks — one of them must include
	// the current text.
	joined := renderHunks(body.Hunks)
	if !strings.Contains(joined, "current text") {
		t.Errorf("hunks don't include current published text: %s", joined)
	}
}

func TestHandlePageDiff_InvalidFrom_400(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "v1")
	rec := urlWithParam(s.handlePageDiff, http.MethodGet, "/api/diff/doc?from=-1&to=1", "doc", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// draftToken enters edit mode as `owner` and returns the edit token so
// the test can assert draft-diff paths as that owner.
func draftToken(t *testing.T, s *Server, pagePath, owner string) string {
	t.Helper()
	fs := s.store.(*storage.FileStore)
	_, token, err := fs.Drafts.EnterEditMode(pagePath, owner, false, "current published")
	if err != nil {
		t.Fatalf("EnterEditMode: %v", err)
	}
	if err := fs.Drafts.SaveDraft(pagePath, owner, token, "drafted content\n"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	return token
}

func ctxWithUser(username string) context.Context {
	return context.WithValue(context.Background(), usernameKey, username)
}

func TestHandlePageDiff_DraftTo_Unauthenticated_401(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "published\n")
	_ = draftToken(t, s, "/doc", "alice")

	rec := urlWithParam(s.handlePageDiff, http.MethodGet, "/api/diff/doc?from=1&to=-1", "doc", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandlePageDiff_DraftTo_WrongOwner_403(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "published\n")
	_ = draftToken(t, s, "/doc", "alice")

	rec := urlWithParam(s.handlePageDiff, http.MethodGet, "/api/diff/doc?from=1&to=-1", "doc", ctxWithUser("bob"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (bob is not the draft owner)", rec.Code)
	}
}

func TestHandlePageDiff_DraftTo_OwnerSucceeds(t *testing.T) {
	t.Parallel()
	s := newHistoryServer(t)
	putPage(t, s, "/doc", "published\n")
	_ = draftToken(t, s, "/doc", "alice")

	rec := urlWithParam(s.handlePageDiff, http.MethodGet, "/api/diff/doc?from=1&to=-1", "doc", ctxWithUser("alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		To    int64              `json:"to"`
		Hunks []storage.DiffHunk `json:"hunks"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.To != -1 {
		t.Errorf("to = %d, want -1 (draft)", body.To)
	}
	if !strings.Contains(renderHunks(body.Hunks), "drafted content") {
		t.Errorf("draft content missing from hunks: %s", renderHunks(body.Hunks))
	}
}

// renderHunks concatenates every text line from the diff hunks so we can
// grep for expected content without depending on the exact hunk shape.
func renderHunks(hunks []storage.DiffHunk) string {
	var sb strings.Builder
	for _, h := range hunks {
		fmt.Fprintf(&sb, "%+v\n", h)
	}
	return sb.String()
}
