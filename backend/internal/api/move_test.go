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

// newMoveTestServer wires a real FileStore so the mover interface is
// available. Move touches attic + refs + rename semantics that are
// exercised via the FileStore's actual code paths.
func newMoveTestServer(t *testing.T) (*Server, *storage.FileStore) {
	t.Helper()
	fs, err := storage.NewFileStore(t.TempDir() + "/content")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return &Server{store: fs}, fs
}

func seedMovePage(t *testing.T, fs *storage.FileStore, pagePath, content string) {
	t.Helper()
	if _, err := fs.Put(pagePath, content, "seed"); err != nil {
		t.Fatalf("Put %s: %v", pagePath, err)
	}
}

func callMove(handler http.HandlerFunc, pagePath string, body any, callerUsername string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/move/"+pagePath, bytes.NewReader(raw))
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

// TestMove_HappyPath — a plain rename returns 200 and the underlying
// page moves.
func TestMove_HappyPath(t *testing.T) {
	t.Parallel()
	s, fs := newMoveTestServer(t)
	seedMovePage(t, fs, "/old", "content")
	rec := callMove(s.handleMovePage, "old", map[string]any{"to": "/new"}, "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fs.Exists("old") {
		t.Errorf("old page still exists after move")
	}
	if !fs.Exists("new") {
		t.Errorf("new page missing after move")
	}
}

// TestMove_MissingPath_400.
func TestMove_MissingPath_400(t *testing.T) {
	t.Parallel()
	s, _ := newMoveTestServer(t)
	rec := callMove(s.handleMovePage, "", map[string]any{"to": "/new"}, "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestMove_InvalidJSON_400.
func TestMove_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s, fs := newMoveTestServer(t)
	seedMovePage(t, fs, "/old", "content")
	req := httptest.NewRequest(http.MethodPost, "/api/move/old", bytes.NewReader([]byte("{invalid")))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "old")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	s.handleMovePage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestMove_MutuallyExclusiveFlags_400 — the handler validates exactly
// one of `to`, `to_namespace_index`, `to_regular_page`.
func TestMove_MutuallyExclusiveFlags_400(t *testing.T) {
	t.Parallel()
	s, fs := newMoveTestServer(t)
	seedMovePage(t, fs, "/old", "x")
	// Both flags at once.
	rec := callMove(s.handleMovePage, "old", map[string]any{
		"to":                 "/new",
		"to_namespace_index": true,
	}, "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (two flags set)", rec.Code)
	}
	// None set.
	rec2 := callMove(s.handleMovePage, "old", map[string]any{}, "alice")
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (no flag set)", rec2.Code)
	}
}

// TestMove_MissingSource_404 — moving a page that doesn't exist maps
// to 404 via handleMoveError's ErrPageNotFound branch.
func TestMove_MissingSource_404(t *testing.T) {
	t.Parallel()
	s, _ := newMoveTestServer(t)
	rec := callMove(s.handleMovePage, "nowhere", map[string]any{"to": "/new"}, "alice")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for missing source", rec.Code)
	}
}

// TestMove_DestinationExists_409.
func TestMove_DestinationExists_409(t *testing.T) {
	t.Parallel()
	s, fs := newMoveTestServer(t)
	seedMovePage(t, fs, "/old", "content")
	seedMovePage(t, fs, "/new", "already here")
	rec := callMove(s.handleMovePage, "old", map[string]any{"to": "/new"}, "alice")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 for destination-exists", rec.Code)
	}
}

// TestMove_DryRun_ReturnsPreview — dry_run + to returns a preview,
// original stays in place.
func TestMove_DryRun_ReturnsPreview(t *testing.T) {
	t.Parallel()
	s, fs := newMoveTestServer(t)
	seedMovePage(t, fs, "/old", "content")
	rec := callMove(s.handleMovePage, "old", map[string]any{"to": "/new", "dry_run": true}, "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("dry_run status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !fs.Exists("old") || fs.Exists("new") {
		t.Errorf("dry_run mutated the store (old exists=%v, new exists=%v)", fs.Exists("old"), fs.Exists("new"))
	}
}
