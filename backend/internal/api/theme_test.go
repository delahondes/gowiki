package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/config"
)

// newThemeTestServer wires a userStore + a temp configStore. The theme
// overrides handler needs configStore.Get(); the preferences handlers
// need userStore.Get / UpdateThemePreference.
func newThemeTestServer(t *testing.T, overrides map[string]string) *Server {
	t.Helper()
	meta := t.TempDir()
	users, err := auth.NewUserStore(meta)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	cfgStore, err := config.Load(filepath.Join(meta, "config.yaml"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if overrides != nil {
		cfg := cfgStore.Get()
		cfg.Themes.PaletteOverrides = overrides
		if err := cfgStore.Update(cfg); err != nil {
			t.Fatalf("configStore.Update: %v", err)
		}
	}
	return &Server{userStore: users, configStore: cfgStore}
}

// TestThemeOverridesCSS_EmptyReturnsBlankCSS — no overrides configured →
// the endpoint responds 200 with an empty body (the frontend still
// includes the stylesheet, so the response must be servable).
func TestThemeOverridesCSS_EmptyReturnsBlankCSS(t *testing.T) {
	t.Parallel()
	s := newThemeTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/theme/overrides.css", nil)
	rec := httptest.NewRecorder()
	s.handleThemeOverridesCSS(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body should be empty when no overrides; got %q", rec.Body.String())
	}
}

// TestThemeOverridesCSS_WritesLightScopedRule — valid overrides emit a
// light-scoped selector with each mapped variable.
func TestThemeOverridesCSS_WritesLightScopedRule(t *testing.T) {
	t.Parallel()
	s := newThemeTestServer(t, map[string]string{"primary-fg": "#ff0000"})
	req := httptest.NewRequest(http.MethodGet, "/api/theme/overrides.css", nil)
	rec := httptest.NewRecorder()
	s.handleThemeOverridesCSS(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `html:not([data-theme="dark"])`) {
		t.Errorf("body missing light-mode scope selector: %q", body)
	}
	if !strings.Contains(body, "--gw-color-primary-fg: #ff0000;") {
		t.Errorf("body missing expected CSS variable: %q", body)
	}
}

// TestThemeOverridesCSS_RejectsInjection — a key or value that doesn't
// pass the palette regex is skipped, keeping arbitrary CSS out of the
// response body.
func TestThemeOverridesCSS_RejectsInjection(t *testing.T) {
	t.Parallel()
	s := newThemeTestServer(t, map[string]string{
		"good":                        "#123",
		"BAD_KEY":                     "#456",
		"evil":                        "red; } body { display: none",
		"underscore_is_normalized_ok": "#abc",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/theme/overrides.css", nil)
	rec := httptest.NewRecorder()
	s.handleThemeOverridesCSS(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "--gw-color-good: #123;") {
		t.Errorf("expected --gw-color-good in body: %q", body)
	}
	if strings.Contains(body, "BAD_KEY") {
		t.Errorf("uppercase key should have been rejected: %q", body)
	}
	if strings.Contains(body, "display: none") || strings.Contains(body, "body {") {
		t.Errorf("evil value should have been rejected: %q", body)
	}
	// Underscore-in-key is normalized to hyphen on write.
	if !strings.Contains(body, "--gw-color-underscore-is-normalized-ok: #abc;") {
		t.Errorf("expected underscore→hyphen normalization: %q", body)
	}
}

// TestGetMePreferences_Unauth_401.
func TestGetMePreferences_Unauth_401(t *testing.T) {
	t.Parallel()
	s := newThemeTestServer(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me/preferences", nil)
	rec := httptest.NewRecorder()
	s.handleGetMePreferences(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestPutMePreferences_HappyPath — sets the preference; a subsequent
// GET returns it; cookie is set.
func TestPutMePreferences_HappyPath(t *testing.T) {
	t.Parallel()
	s := newThemeTestServer(t, nil)
	if err := s.userStore.Create(auth.User{Username: "alice"}, "initial-pw-1234"); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"theme_preference": "dark"})
	req := httptest.NewRequest(http.MethodPut, "/api/auth/me/preferences", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), usernameKey, "alice")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	s.handlePutMePreferences(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// Cookie mirror for the no-flash boot.
	cookieSet := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "gowiki_theme" && c.Value == "dark" {
			cookieSet = true
		}
	}
	if !cookieSet {
		t.Errorf("expected gowiki_theme cookie set to 'dark'")
	}

	// Round-trip via Get.
	getReq := httptest.NewRequest(http.MethodGet, "/api/auth/me/preferences", nil)
	getReq = getReq.WithContext(context.WithValue(getReq.Context(), usernameKey, "alice"))
	getRec := httptest.NewRecorder()
	s.handleGetMePreferences(getRec, getReq)
	var pref struct {
		ThemePreference string `json:"theme_preference"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &pref); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pref.ThemePreference != "dark" {
		t.Errorf("theme_preference = %q, want dark", pref.ThemePreference)
	}
}

// TestPutMePreferences_InvalidValue_400 — only "", light, dark, auto
// are accepted.
func TestPutMePreferences_InvalidValue_400(t *testing.T) {
	t.Parallel()
	s := newThemeTestServer(t, nil)
	if err := s.userStore.Create(auth.User{Username: "alice"}, "initial-pw-1234"); err != nil {
		t.Fatalf("Create user: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"theme_preference": "purple"})
	req := httptest.NewRequest(http.MethodPut, "/api/auth/me/preferences", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), usernameKey, "alice"))
	rec := httptest.NewRecorder()
	s.handlePutMePreferences(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid theme value", rec.Code)
	}
}
