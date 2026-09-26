package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"gowiki/backend/internal/config"
)

// newConfigTestServer wires just the config store. The two handlers
// (Get, Update) touch nothing else that isn't guarded by a nil-check;
// initOAuthClient returns cleanly when Azure isn't configured, and
// reinitAIProvider is a no-op when AI is disabled.
func newConfigTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	store, err := config.Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	// Set a non-empty DataDir + a well-known Site.Title so that any
	// Update round-trips through validate() successfully by default.
	cfg := store.Get()
	cfg.DataDir = dir
	cfg.Site.Title = "Test Wiki"
	if err := store.Update(cfg); err != nil {
		t.Fatalf("config seed: %v", err)
	}
	return &Server{configStore: store}
}

// TestHandleGetConfig_ReturnsCurrent — the raw Config struct is
// returned as-is. Assert on Site.Title so the caller reads what the
// store holds.
func TestHandleGetConfig_ReturnsCurrent(t *testing.T) {
	t.Parallel()
	s := newConfigTestServer(t)
	rec := callAdmin(s.handleGetConfig, http.MethodGet, "/api/admin/config", nil, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got config.Config
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v — %s", err, rec.Body.String())
	}
	if got.Site.Title != "Test Wiki" {
		t.Errorf("Site.Title = %q, want Test Wiki", got.Site.Title)
	}
}

// TestHandleUpdateConfig_HappyPath — a valid replacement updates the
// live store and returns the freshly-persisted config. Use Site.Title
// as the round-trip witness.
func TestHandleUpdateConfig_HappyPath(t *testing.T) {
	t.Parallel()
	s := newConfigTestServer(t)
	newCfg := s.configStore.Get()
	newCfg.Site.Title = "Renamed"

	body, _ := json.Marshal(newCfg)
	rec := callAdmin(s.handleUpdateConfig, http.MethodPut, "/api/admin/config", nil, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if s.configStore.Get().Site.Title != "Renamed" {
		t.Errorf("store.Site.Title = %q, want Renamed", s.configStore.Get().Site.Title)
	}
}

// TestHandleUpdateConfig_PreservesOperationalFields — DataDir /
// Server.Addr / Server.TLSDomain / Server.WebDir are managed at
// startup (not by the admin UI). If a caller omits them, the handler
// must preserve the current values rather than zeroing them out —
// otherwise a UI save would break the running server.
func TestHandleUpdateConfig_PreservesOperationalFields(t *testing.T) {
	t.Parallel()
	s := newConfigTestServer(t)
	// Seed operational fields directly on the store so we know what
	// the pre-update snapshot looks like.
	cfg := s.configStore.Get()
	cfg.Server.Addr = ":9090"
	cfg.Server.TLSDomain = "example.com"
	cfg.Server.WebDir = "/opt/wiki/dist"
	if err := s.configStore.Update(cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// PUT a payload that leaves those fields empty.
	pruned := s.configStore.Get()
	pruned.DataDir = ""
	pruned.Server.Addr = ""
	pruned.Server.TLSDomain = ""
	pruned.Server.WebDir = ""
	pruned.Site.Title = "Still valid"
	body, _ := json.Marshal(pruned)

	rec := callAdmin(s.handleUpdateConfig, http.MethodPut, "/api/admin/config", nil, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	after := s.configStore.Get()
	if after.Server.Addr != ":9090" {
		t.Errorf("Server.Addr = %q, want :9090 (was zeroed by handler)", after.Server.Addr)
	}
	if after.Server.TLSDomain != "example.com" {
		t.Errorf("Server.TLSDomain = %q, want example.com", after.Server.TLSDomain)
	}
	if after.Server.WebDir != "/opt/wiki/dist" {
		t.Errorf("Server.WebDir = %q, want /opt/wiki/dist", after.Server.WebDir)
	}
	if after.DataDir == "" {
		t.Errorf("DataDir zeroed — handler must preserve it when caller omits it")
	}
}

// TestHandleUpdateConfig_InvalidJSON_400.
func TestHandleUpdateConfig_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newConfigTestServer(t)
	rec := callAdmin(s.handleUpdateConfig, http.MethodPut, "/api/admin/config", nil, []byte("{not-json"), "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleUpdateConfig_ValidationRejected_400 — the store's
// validate() refuses empty Site.Title; the handler must translate
// that into 400. This guards against the "handler blindly forwards
// validate errors as 500" regression.
func TestHandleUpdateConfig_ValidationRejected_400(t *testing.T) {
	t.Parallel()
	s := newConfigTestServer(t)
	invalid := s.configStore.Get()
	invalid.Site.Title = "" // validate() refuses this
	body, _ := json.Marshal(invalid)
	rec := callAdmin(s.handleUpdateConfig, http.MethodPut, "/api/admin/config", nil, body, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
