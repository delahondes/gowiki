package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/collab"
	"gowiki/backend/internal/storage"
)

// newPresenceTestServer wires the minimum stack presence handlers use:
// a userStore (for display name resolution), a FileStore (for page +
// draft access), a session store, and the presenceHub. WebSocket upgrade
// itself is skipped — the tests exercise the HTTP guards.
func newPresenceTestServer(t *testing.T) (*Server, *storage.FileStore) {
	t.Helper()
	meta := t.TempDir()
	users, err := auth.NewUserStore(meta)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	sessions, err := auth.NewSessionStore(filepath.Join(meta, "s"), time.Hour)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	fs, err := storage.NewFileStore(t.TempDir() + "/content")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return &Server{
		userStore:    users,
		sessionStore: sessions,
		store:        fs,
		draftManager: fs.Drafts,
		presenceHub:  collab.NewHub(),
	}, fs
}

// TestPresenceWS_Unauth_401 — WebSocket upgrade needs a username in
// context; without one the endpoint refuses at HTTP layer (401).
func TestPresenceWS_Unauth_401(t *testing.T) {
	t.Parallel()
	s, _ := newPresenceTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ws/presence", nil)
	rec := httptest.NewRecorder()
	s.handlePresenceWS(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestCollabWS_MissingPath_400 — path portion after /api/ws/collab/
// must be non-empty.
func TestCollabWS_MissingPath_400(t *testing.T) {
	t.Parallel()
	s, _ := newPresenceTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ws/collab/", nil)
	req = req.WithContext(context.WithValue(req.Context(), usernameKey, "alice"))
	rec := httptest.NewRecorder()
	s.handleCollabWS(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestCollabWS_Unauth_401.
func TestCollabWS_Unauth_401(t *testing.T) {
	t.Parallel()
	s, _ := newPresenceTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/ws/collab/doc", nil)
	rec := httptest.NewRecorder()
	s.handleCollabWS(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestCollabDraftRead_NoLock_404 — no active editing session → 404.
func TestCollabDraftRead_NoLock_404(t *testing.T) {
	t.Parallel()
	s, _ := newPresenceTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/collab/draft/doc", nil)
	rec := httptest.NewRecorder()
	s.handleCollabDraftRead(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for no-lock", rec.Code)
	}
}

// TestCollabDraftRead_MissingPath_400.
func TestCollabDraftRead_MissingPath_400(t *testing.T) {
	t.Parallel()
	s, _ := newPresenceTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/collab/draft/", nil)
	rec := httptest.NewRecorder()
	s.handleCollabDraftRead(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing path", rec.Code)
	}
}

// TestCollabDraftRead_HappyPath — with an active draft, the handler
// returns the draft markdown and its owner. This is the "join
// session" contract the collab guest relies on.
func TestCollabDraftRead_HappyPath(t *testing.T) {
	t.Parallel()
	s, fs := newPresenceTestServer(t)
	// Seed a published page and enter edit mode as alice to create a lock+draft.
	if _, err := fs.Put("/doc", "published body\n", "seed"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	md, token, err := fs.Drafts.EnterEditMode("/doc", "alice", false, "published body\n")
	if err != nil {
		t.Fatalf("EnterEditMode: %v", err)
	}
	if err := fs.Drafts.SaveDraft("/doc", "alice", token, "draft body\n"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	_ = md

	req := httptest.NewRequest(http.MethodGet, "/api/collab/draft/doc", nil)
	rec := httptest.NewRecorder()
	s.handleCollabDraftRead(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Markdown string `json:"markdown"`
		Owner    string `json:"owner"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Owner != "alice" {
		t.Errorf("owner = %q, want alice", body.Owner)
	}
	if body.Markdown != "draft body\n" {
		t.Errorf("markdown = %q, want draft body", body.Markdown)
	}
}
