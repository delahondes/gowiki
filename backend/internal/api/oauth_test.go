package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gowiki/backend/internal/config"
)

// The oauth.go client-side handlers are painful to exercise end-to-end
// because they depend on OIDC discovery against Azure AD (or another
// live IdP). Tests here focus on:
//   1. handleAuthProviders — pure config reader, no external deps.
//   2. handleOAuthLogin / handleOAuthCallback — the "not configured"
//      guard branches, which fire when no OAuth client is initialised.
//   3. Pure helpers: sanitizeUsername, requestOrigin.
// End-to-end flows through a mocked IdP are a follow-up requiring a
// full OIDC discovery + token endpoint test double.

func newOAuthTestServer(t *testing.T, provider, clientID string) *Server {
	t.Helper()
	path := t.TempDir() + "/config.yaml"
	// Load creates a default config file if missing.
	store, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := store.Get()
	cfg.Auth.OAuth.Provider = provider
	cfg.Auth.OAuth.ClientID = clientID
	if err := store.Update(cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}
	return &Server{configStore: store}
}

// ─── handleAuthProviders ─────────────────────────────────

func TestHandleAuthProviders_NoProvider_LocalOnly(t *testing.T) {
	t.Parallel()
	s := newOAuthTestServer(t, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/providers", nil)
	rec := httptest.NewRecorder()
	s.handleAuthProviders(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["local"] != true {
		t.Errorf("local = %v, want true", body["local"])
	}
	// providers is always a JSON array (never null), even when empty —
	// UI code does .length on it without a null guard.
	providers, ok := body["providers"].([]any)
	if !ok {
		t.Fatalf("providers not an array: %T (%v)", body["providers"], body["providers"])
	}
	if len(providers) != 0 {
		t.Errorf("providers len = %d, want 0", len(providers))
	}
}

func TestHandleAuthProviders_AzureWithClientID_ListsAzure(t *testing.T) {
	t.Parallel()
	s := newOAuthTestServer(t, "azure", "abc-client-id")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/providers", nil)
	rec := httptest.NewRecorder()
	s.handleAuthProviders(rec, req)

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	providers, _ := body["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("providers len = %d, want 1: %v", len(providers), providers)
	}
	p, _ := providers[0].(map[string]any)
	if p["name"] != "azure" {
		t.Errorf("provider name = %v", p["name"])
	}
	if p["label"] != "Microsoft 365" {
		t.Errorf("provider label = %v", p["label"])
	}
}

func TestHandleAuthProviders_AzureWithoutClientID_NotListed(t *testing.T) {
	t.Parallel()
	// Provider name set but client_id blank — the code hides the option
	// so users don't try to sign in against an incomplete config.
	s := newOAuthTestServer(t, "azure", "")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/providers", nil)
	rec := httptest.NewRecorder()
	s.handleAuthProviders(rec, req)

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	providers, _ := body["providers"].([]any)
	if len(providers) != 0 {
		t.Errorf("providers len = %d, want 0 (incomplete config must not surface)", len(providers))
	}
}

// ─── handleOAuthLogin — not-configured guard ─────────────

func TestHandleOAuthLogin_NotConfigured_503(t *testing.T) {
	t.Parallel()
	// No provider, no client — oauthClient is nil AND initOAuthClient
	// will fail because provider is not "azure".
	s := newOAuthTestServer(t, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oauth/login", nil)
	rec := httptest.NewRecorder()
	s.handleOAuthLogin(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

// ─── handleOAuthCallback — not-configured guard ──────────

func TestHandleOAuthCallback_NotConfigured_503(t *testing.T) {
	t.Parallel()
	s := newOAuthTestServer(t, "", "")
	// oauthClient is nil, so we hit the top guard even before parsing params.
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/oauth/callback?code=x&state=y", nil)
	rec := httptest.NewRecorder()
	s.handleOAuthCallback(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// ─── sanitizeUsername ────────────────────────────────────

func TestSanitizeUsername(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"alice", "alice"},
		{"Alice.Smith", "alice.smith"}, // lowercased, dot preserved
		{"first_last", "first_last"},
		{"a-b", "a-b"},
		{"àéîõü", "user"},                         // all-non-ASCII → fallback
		{"", "user"},                              // empty → fallback
		{"alice@example.com", "aliceexample.com"}, // @ stripped, dot kept
		{"has spaces", "hasspaces"},               // whitespace stripped
		{"abc!@#$def", "abcdef"},                  // punctuation stripped
		{"digits123", "digits123"},
	}
	for _, tc := range cases {
		if got := sanitizeUsername(tc.in); got != tc.want {
			t.Errorf("sanitizeUsername(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ─── requestOrigin ───────────────────────────────────────

func TestRequestOrigin_XForwardedHost(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://backend:8080/api/x", nil)
	req.Header.Set("X-Forwarded-Host", "wiki.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	got := requestOrigin(req)
	if got != "https://wiki.example.com" {
		t.Errorf("got %q, want https://wiki.example.com", got)
	}
}

func TestRequestOrigin_XForwardedHost_DefaultsToHTTP(t *testing.T) {
	t.Parallel()
	// If X-Forwarded-Proto is missing, code defaults to http.
	req := httptest.NewRequest(http.MethodGet, "http://backend:8080/api/x", nil)
	req.Header.Set("X-Forwarded-Host", "wiki.example.com")

	got := requestOrigin(req)
	if got != "http://wiki.example.com" {
		t.Errorf("got %q, want http://wiki.example.com", got)
	}
}

func TestRequestOrigin_OriginHeader(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://backend:8080/api/x", nil)
	req.Header.Set("Origin", "https://wiki.gmt.bio")

	got := requestOrigin(req)
	if got != "https://wiki.gmt.bio" {
		t.Errorf("got %q, want https://wiki.gmt.bio", got)
	}
}

func TestRequestOrigin_OriginNull_FallsThrough(t *testing.T) {
	t.Parallel()
	// Some sandboxed browsers send Origin: null. Treat as absent and
	// fall through to the next signal.
	req := httptest.NewRequest(http.MethodGet, "http://backend.host/api/x", nil)
	req.Header.Set("Origin", "null")

	got := requestOrigin(req)
	if got != "http://backend.host" {
		t.Errorf("got %q, want http://backend.host (from r.Host)", got)
	}
}

func TestRequestOrigin_Referer(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://backend/api/x", nil)
	req.Header.Set("Referer", "https://wiki.example.com/page/subpage")

	got := requestOrigin(req)
	if got != "https://wiki.example.com" {
		t.Errorf("got %q, want https://wiki.example.com", got)
	}
}

func TestRequestOrigin_FallbackToHost(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "http://only.host:9090/api/x", nil)
	// No proxy / origin / referer headers — bare r.Host.
	got := requestOrigin(req)
	if got != "http://only.host:9090" {
		t.Errorf("got %q, want http://only.host:9090", got)
	}
}
