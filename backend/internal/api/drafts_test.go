package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/storage"
)

// newDraftsTestServer builds a Server wired to a real FileStore (which owns
// Attic + Drafts). The four HTTP handlers under test read/write the draft
// store and the page store; no auth, no ACL, no database needed.
func newDraftsTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewFileStore(root + "/content")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return &Server{
		store:        store,
		draftManager: store.Drafts,
		atticStore:   store.Attic,
	}
}

// callDraft runs a handler with a chi `*` param (pagePath), optional query,
// body, and caller username. Handlers read the page path via chi.URLParam
// with the "*" key — the router uses a wildcard route.
func callDraft(handler http.HandlerFunc, method, url, pagePath string, body []byte, callerUsername string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, url, reader)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", pagePath)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if callerUsername != "" {
		ctx = context.WithValue(ctx, usernameKey, callerUsername)
	}
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// putPage writes through the store so subsequent edit-mode entry sees
// a real published version to fall back on.
func putPageDraft(t *testing.T, s *Server, pagePath, content string) {
	t.Helper()
	fs := s.store.(*storage.FileStore)
	if _, err := fs.Put(pagePath, content, "seed"); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

// ─── handleEnterEdit ────────────────────────────────────

// TestEnterEdit_HappyReturnsToken — first-time entry on an existing
// published page returns an edit_token and the current markdown.
func TestEnterEdit_HappyReturnsToken(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "published body\n")

	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Markdown  string `json:"markdown"`
		EditToken string `json:"edit_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.EditToken == "" {
		t.Errorf("no edit_token in response")
	}
	if body.Markdown != "published body\n" {
		t.Errorf("markdown = %q, want published body", body.Markdown)
	}
}

// TestEnterEdit_MissingPath_400.
func TestEnterEdit_MissingPath_400(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/", "", nil, "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestEnterEdit_OtherUserLock_423 — a second user hitting a page that
// is already locked gets 423 (WebDAV "Locked") with the current
// owner's username in the body.
func TestEnterEdit_OtherUserLock_423(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "seed\n")

	// Alice locks.
	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("alice locking: %d", rec.Code)
	}

	// Bob attempts.
	rec = callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "bob")
	if rec.Code != http.StatusLocked {
		t.Fatalf("bob: status = %d, want 423 Locked: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error    string `json:"error"`
		LockedBy string `json:"locked_by"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.LockedBy != "alice" {
		t.Errorf("locked_by = %q, want alice", body.LockedBy)
	}
}

// TestEnterEdit_SameUserWithoutForce_409 — alice re-enters her own
// existing session and gets 409 (superseded) unless force=true.
func TestEnterEdit_SameUserWithoutForce_409(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "seed\n")

	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("first entry: %d", rec.Code)
	}
	rec = callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	if rec.Code != http.StatusConflict {
		t.Errorf("second entry (no force): %d, want 409", rec.Code)
	}
}

// TestEnterEdit_SameUserWithForce — alice explicitly reclaims her own
// session via ?force=true.
func TestEnterEdit_SameUserWithForce(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "seed\n")

	_ = callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	// Force reclaim via ?force=true — encoded in URL query, which
	// callDraft passes through the request URL.
	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc?force=true", "doc", nil, "alice")
	if rec.Code != http.StatusOK {
		t.Errorf("force reclaim: %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// TestEnterEdit_InitialMarkdownForNewPage — for a page that doesn't
// exist yet, the caller can supply the initial markdown in the body.
// Verifies the JSON body path (Content-Type + ContentLength check).
func TestEnterEdit_InitialMarkdownForNewPage(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)

	body, _ := json.Marshal(map[string]string{"initial_markdown": "# Fresh\n"})
	req := httptest.NewRequest(http.MethodPost, "/api/edit/newpage", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "newpage")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, usernameKey, "alice")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	s.handleEnterEdit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Markdown string `json:"markdown"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Markdown != "# Fresh\n" {
		t.Errorf("initial markdown not honored: got %q", resp.Markdown)
	}
}

// ─── handleSaveDraft ────────────────────────────────────

// TestSaveDraft_Happy — save with a valid token round-trips.
func TestSaveDraft_Happy(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "seed\n")

	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	var enter struct {
		EditToken string `json:"edit_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &enter)

	body, _ := json.Marshal(map[string]string{
		"markdown":   "new content\n",
		"edit_token": enter.EditToken,
	})
	saveRec := callDraft(s.handleSaveDraft, http.MethodPut, "/api/draft/doc", "doc", body, "alice")
	if saveRec.Code != http.StatusOK {
		t.Errorf("save status = %d, want 200: %s", saveRec.Code, saveRec.Body.String())
	}

	// The saved draft must be readable back through the store.
	fs := s.store.(*storage.FileStore)
	got, err := fs.Drafts.ReadDraft("doc", "alice")
	if err != nil {
		t.Errorf("ReadDraft: %v", err)
	}
	if got != "new content\n" {
		t.Errorf("stored draft = %q, want new content", got)
	}
}

// TestSaveDraft_WrongToken_409 — a token mismatch surfaces as 409
// (edit session superseded).
func TestSaveDraft_WrongToken_409(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "seed\n")
	_ = callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")

	body, _ := json.Marshal(map[string]string{
		"markdown":   "hacked\n",
		"edit_token": "not-a-real-token",
	})
	rec := callDraft(s.handleSaveDraft, http.MethodPut, "/api/draft/doc", "doc", body, "alice")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

// TestSaveDraft_MissingPath_400.
func TestSaveDraft_MissingPath_400(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	body, _ := json.Marshal(map[string]string{"markdown": "x", "edit_token": "y"})
	rec := callDraft(s.handleSaveDraft, http.MethodPut, "/api/draft/", "", body, "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestSaveDraft_InvalidJSON_400.
func TestSaveDraft_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	rec := callDraft(s.handleSaveDraft, http.MethodPut, "/api/draft/doc", "doc", []byte("not json"), "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── handlePublish ──────────────────────────────────────

// TestPublish_Happy — publish returns the fresh PutResult and the page
// on disk carries the new content.
func TestPublish_Happy(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "v1\n")

	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	var enter struct {
		EditToken string `json:"edit_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &enter)

	saveBody, _ := json.Marshal(map[string]string{
		"markdown":   "v2 body\n",
		"edit_token": enter.EditToken,
	})
	_ = callDraft(s.handleSaveDraft, http.MethodPut, "/api/draft/doc", "doc", saveBody, "alice")

	pubBody, _ := json.Marshal(map[string]any{
		"edit_token": enter.EditToken,
		"summary":    "second draft",
	})
	pubRec := callDraft(s.handlePublish, http.MethodPost, "/api/publish/doc", "doc", pubBody, "alice")
	if pubRec.Code != http.StatusOK {
		t.Fatalf("publish status = %d, want 200: %s", pubRec.Code, pubRec.Body.String())
	}

	// The page on disk must reflect v2.
	fs := s.store.(*storage.FileStore)
	page, err := fs.Get("doc")
	if err != nil {
		t.Fatalf("Get after publish: %v", err)
	}
	if page.Markdown != "v2 body\n" {
		t.Errorf("published body = %q, want v2 body", page.Markdown)
	}
}

// TestPublish_EditSuperseded_409 — publish with an unknown token
// surfaces as "edit session superseded".
func TestPublish_EditSuperseded_409(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "v1\n")

	pubBody, _ := json.Marshal(map[string]string{
		"edit_token": "never-issued-token",
	})
	rec := callDraft(s.handlePublish, http.MethodPost, "/api/publish/doc", "doc", pubBody, "alice")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

// TestPublish_DatabaseRowConflict — when inlineEditConflicts holds the
// page path, publish refuses with 409 + kind=database_row_conflict.
// force_publish=true skips the guard and publishes anyway.
func TestPublish_DatabaseRowConflict_And_ForceOverride(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "v1\n")

	// Enter + save so a draft exists to publish.
	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	var enter struct {
		EditToken string `json:"edit_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &enter)
	saveBody, _ := json.Marshal(map[string]string{"markdown": "v2\n", "edit_token": enter.EditToken})
	_ = callDraft(s.handleSaveDraft, http.MethodPut, "/api/draft/doc", "doc", saveBody, "alice")

	// Simulate an inline row edit for this page.
	s.inlineEditConflicts.Store("doc", "customers")

	// Publish without force → 409 with database_row_conflict + table name.
	pubBody, _ := json.Marshal(map[string]any{"edit_token": enter.EditToken})
	pubRec := callDraft(s.handlePublish, http.MethodPost, "/api/publish/doc", "doc", pubBody, "alice")
	if pubRec.Code != http.StatusConflict {
		t.Fatalf("without force: status = %d, want 409", pubRec.Code)
	}
	var body struct {
		Error string `json:"error"`
		Table string `json:"table"`
	}
	_ = json.Unmarshal(pubRec.Body.Bytes(), &body)
	if body.Error != "database_row_conflict" || body.Table != "customers" {
		t.Errorf("body = %+v, want database_row_conflict + customers", body)
	}

	// Re-add the guard (the first call deleted it as part of publish
	// pipeline cleanup) and retry with force_publish=true.
	s.inlineEditConflicts.Store("doc", "customers")
	pubBody, _ = json.Marshal(map[string]any{
		"edit_token":    enter.EditToken,
		"force_publish": true,
	})
	pubRec = callDraft(s.handlePublish, http.MethodPost, "/api/publish/doc", "doc", pubBody, "alice")
	if pubRec.Code != http.StatusOK {
		t.Errorf("with force: status = %d, want 200: %s", pubRec.Code, pubRec.Body.String())
	}
}

// TestPublish_MissingPath_400.
func TestPublish_MissingPath_400(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	body, _ := json.Marshal(map[string]string{"edit_token": "x"})
	rec := callDraft(s.handlePublish, http.MethodPost, "/api/publish/", "", body, "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestPublish_InvalidJSON_400.
func TestPublish_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	rec := callDraft(s.handlePublish, http.MethodPost, "/api/publish/doc", "doc", []byte("not json"), "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── handleDiscardDraft ─────────────────────────────────

// TestDiscardDraft_WithMatchingToken — happy path.
func TestDiscardDraft_WithMatchingToken(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "v1\n")
	rec := callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")
	var enter struct {
		EditToken string `json:"edit_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &enter)

	discardRec := callDraft(s.handleDiscardDraft, http.MethodDelete,
		"/api/draft/doc?edit_token="+enter.EditToken, "doc", nil, "alice")
	if discardRec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", discardRec.Code, discardRec.Body.String())
	}
	// Lock must be gone.
	fs := s.store.(*storage.FileStore)
	if lock := fs.Drafts.GetLock("doc"); lock.Owner != "" {
		t.Errorf("lock survived discard: %+v", lock)
	}
}

// TestDiscardDraft_WithoutTokenActiveSession_409 — no token supplied
// while an active token exists → the DiscardDraft storage refuses with
// ErrEditSuperseded, mapped to 409.
func TestDiscardDraft_WithoutTokenActiveSession_409(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "v1\n")
	_ = callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")

	rec := callDraft(s.handleDiscardDraft, http.MethodDelete, "/api/draft/doc", "doc", nil, "alice")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

// TestDiscardDraft_NotOwner_403 — bob cannot discard alice's draft.
func TestDiscardDraft_NotOwner_403(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	putPageDraft(t, s, "/doc", "v1\n")
	_ = callDraft(s.handleEnterEdit, http.MethodPost, "/api/edit/doc", "doc", nil, "alice")

	rec := callDraft(s.handleDiscardDraft, http.MethodDelete, "/api/draft/doc", "doc", nil, "bob")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// TestDiscardDraft_MissingPath_400.
func TestDiscardDraft_MissingPath_400(t *testing.T) {
	t.Parallel()
	s := newDraftsTestServer(t)
	rec := callDraft(s.handleDiscardDraft, http.MethodDelete, "/api/draft/", "", nil, "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── stripFlowMarkers pure helper ──────────────────────

// TestStripFlowMarkers removes ephemeral flow markers but keeps
// bookmarks (marked with `{#!...}`). Publish routes through this
// before writing to disk.
func TestStripFlowMarkers(t *testing.T) {
	t.Parallel()
	in := "before {#step1} middle {#!bookmark} end {#/step1}"
	out := stripFlowMarkers(in)
	if out != "before  middle {#!bookmark} end " {
		t.Errorf("stripped = %q, want %q", out, "before  middle {#!bookmark} end ")
	}
}
