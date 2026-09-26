package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gowiki/backend/internal/auth"
)

// newOAuthServerTestServer wires a Server with real OAuthServer, TokenStore,
// UserStore, and SessionStore in a t.TempDir(). Every OAuth-authorization-
// server handler under test in this file exercises this real chain — the
// metadata + DCR + authorize + token flow is what MCP clients rely on, so
// mocking the store would just test the mocks.
func newOAuthServerTestServer(t *testing.T) *Server {
	t.Helper()
	meta := t.TempDir()

	oauthSrv, err := auth.NewOAuthServer(meta)
	if err != nil {
		t.Fatalf("NewOAuthServer: %v", err)
	}
	tokens, err := auth.NewTokenStore(meta)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	users, err := auth.NewUserStore(meta)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	sessions, err := auth.NewSessionStore(filepath.Join(meta, "s"), 24*time.Hour)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	return &Server{
		oauthServer:  oauthSrv,
		tokenStore:   tokens,
		userStore:    users,
		sessionStore: sessions,
	}
}

// pkcePair returns a matching (verifier, S256 challenge) — same shape a real
// MCP client would generate before hitting /oauth/authorize.
func pkcePair(verifier string) (string, string) {
	h := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(h[:])
}

// registerClient is the canonical MCP-client shape: one loopback redirect_uri
// and public-client auth (token_endpoint_auth_method=none).
func registerClient(t *testing.T, s *Server, name, redirect string) auth.RegisteredClient {
	t.Helper()
	c, err := s.oauthServer.RegisterClient(name, []string{redirect})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	return c
}

// ─── Metadata endpoints ─────────────────────────────────

func TestAuthServerMetadata_ContainsRequiredFields(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "http://wiki.example.com/.well-known/oauth-authorization-server", nil)
	rec := httptest.NewRecorder()
	s.handleOAuthAuthServerMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v — %s", err, rec.Body.String())
	}

	// RFC 8414 required fields for MCP clients to discover the flow.
	requiredKeys := []string{
		"issuer",
		"authorization_endpoint",
		"token_endpoint",
		"registration_endpoint",
		"response_types_supported",
		"grant_types_supported",
		"code_challenge_methods_supported",
		"token_endpoint_auth_methods_supported",
		"scopes_supported",
	}
	for _, k := range requiredKeys {
		if _, ok := body[k]; !ok {
			t.Errorf("missing required key %q in metadata: %s", k, rec.Body.String())
		}
	}

	if issuer, _ := body["issuer"].(string); issuer != "http://wiki.example.com" {
		t.Errorf("issuer = %q, want http://wiki.example.com", issuer)
	}
	if got, _ := body["authorization_endpoint"].(string); got != "http://wiki.example.com/oauth/authorize" {
		t.Errorf("authorization_endpoint = %q", got)
	}
	if got, _ := body["token_endpoint"].(string); got != "http://wiki.example.com/oauth/token" {
		t.Errorf("token_endpoint = %q", got)
	}

	// Only PKCE S256, only public clients — invariants that guard the
	// spec's minimum-viable posture for MCP.
	pkce, _ := body["code_challenge_methods_supported"].([]any)
	if len(pkce) != 1 || pkce[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v, want [S256]", pkce)
	}
	auths, _ := body["token_endpoint_auth_methods_supported"].([]any)
	if len(auths) != 1 || auths[0] != "none" {
		t.Errorf("token_endpoint_auth_methods_supported = %v, want [none]", auths)
	}
}

func TestProtectedResourceMetadata(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "https://wiki.example.com/.well-known/oauth-protected-resource", nil)
	// Force scheme=https so requestOrigin picks it.
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "wiki.example.com")
	rec := httptest.NewRecorder()
	s.handleOAuthProtectedResourceMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if got, _ := body["resource"].(string); got != "https://wiki.example.com/api/mcp/v1" {
		t.Errorf("resource = %q, want https://wiki.example.com/api/mcp/v1", got)
	}
	servers, _ := body["authorization_servers"].([]any)
	if len(servers) != 1 || servers[0] != "https://wiki.example.com" {
		t.Errorf("authorization_servers = %v", servers)
	}
	methods, _ := body["bearer_methods_supported"].([]any)
	if len(methods) != 1 || methods[0] != "header" {
		t.Errorf("bearer_methods_supported = %v", methods)
	}
}

// ─── DCR (RFC 7591) ─────────────────────────────────────

func TestOAuthRegister_HappyPath(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"client_name":                "Claude Desktop",
		"redirect_uris":              []string{"http://127.0.0.1:8890/callback"},
		"token_endpoint_auth_method": "none",
	})
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOAuthRegister(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if got, _ := resp["client_id"].(string); !strings.HasPrefix(got, "mcp_") {
		t.Errorf("client_id = %q, want mcp_-prefixed", got)
	}
	if resp["token_endpoint_auth_method"] != "none" {
		t.Errorf("token_endpoint_auth_method = %v, want none", resp["token_endpoint_auth_method"])
	}
}

func TestOAuthRegister_MalformedJSON_400(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleOAuthRegister(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "invalid_client_metadata") {
		t.Errorf("error code not surfaced in body: %s", body)
	}
}

func TestOAuthRegister_MissingRedirectURIs_400(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	body, _ := json.Marshal(map[string]any{"client_name": "no-redirects"})
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.handleOAuthRegister(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "invalid_redirect_uri") {
		t.Errorf("wrong error code: %s", body)
	}
}

func TestOAuthRegister_RejectsClientSecretAuth(t *testing.T) {
	t.Parallel()
	// The server only issues public clients — a request asking for a
	// client_secret must be refused so MCP clients fall back to
	// token_endpoint_auth_method=none per spec.
	s := newOAuthServerTestServer(t)
	body, _ := json.Marshal(map[string]any{
		"client_name":                "confidential-client",
		"redirect_uris":              []string{"https://x.example/cb"},
		"token_endpoint_auth_method": "client_secret_basic",
	})
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.handleOAuthRegister(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestOAuthRegister_InvalidRedirectURI_400(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	body, _ := json.Marshal(map[string]any{
		"client_name":   "bad-redirect",
		"redirect_uris": []string{"http://not-loopback.example/cb"}, // http requires loopback
	})
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	s.handleOAuthRegister(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "invalid_redirect_uri") {
		t.Errorf("wrong error code: %s", body)
	}
}

// ─── /oauth/authorize ────────────────────────────────────

func TestAuthorize_MissingResponseType_ServesError(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	// No response_type => parseAuthorizeParams returns an error.
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=x&redirect_uri=y", nil)
	rec := httptest.NewRecorder()
	s.handleOAuthAuthorize(rec, req)
	// serveOAuthError renders a 403 HTML page. Deliberate — any misuse of
	// the endpoint should not silently succeed.
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAuthorize_UnknownClientID_ServesError(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	_, challenge := pkcePair("some-verifier-of-sufficient-length-1234")
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {"mcp_never_registered"},
		"redirect_uri":          {"http://127.0.0.1:9999/cb"},
		"state":                 {"st"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	s.handleOAuthAuthorize(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAuthorize_RedirectURIMismatch_ServesError(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	_, challenge := pkcePair("verifier-of-sufficient-length-12345")

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/DIFFERENT"}, // not registered
		"state":                 {"st"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	s.handleOAuthAuthorize(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAuthorize_NoSession_ServesLoginPage(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	_, challenge := pkcePair("verifier-of-sufficient-length-12345")

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/cb"},
		"state":                 {"st"},
		"scope":                 {"mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	s.handleOAuthAuthorize(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (login page)", rec.Code)
	}
	body := rec.Body.String()
	// Login page has a <form action="/oauth/login"> and prompts for
	// username. Consent page instead would have Approve/Deny buttons and
	// action="/oauth/authorize/decision".
	if !strings.Contains(body, `action="/oauth/login"`) {
		t.Errorf("expected login-form action, got: %s", body[:min(400, len(body))])
	}
	if !strings.Contains(body, "Claude") {
		t.Errorf("client name not embedded in login page")
	}
}

func TestAuthorize_WithSession_ServesConsentPage(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude Desktop", "http://127.0.0.1:8890/cb")
	// Seed a user and a session.
	if err := s.userStore.Create(auth.User{Username: "alice"}, "pw"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	sessID := s.sessionStore.Create("alice")

	_, challenge := pkcePair("verifier-of-sufficient-length-12345")
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/cb"},
		"state":                 {"st"},
		"scope":                 {"mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessID})
	rec := httptest.NewRecorder()
	s.handleOAuthAuthorize(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (consent page)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `action="/oauth/authorize/decision"`) {
		t.Errorf("expected consent-form action, got: %s", body[:min(400, len(body))])
	}
	if !strings.Contains(body, "alice") {
		t.Errorf("logged-in username not shown on consent page")
	}
	if !strings.Contains(body, "Claude Desktop") {
		t.Errorf("client name not shown on consent page")
	}
}

// ─── /oauth/authorize/decision ───────────────────────────

func TestAuthorizeDecision_Deny_RedirectsWithAccessDenied(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	if err := s.userStore.Create(auth.User{Username: "alice"}, "pw"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	sessID := s.sessionStore.Create("alice")

	_, challenge := pkcePair("verifier-of-sufficient-length-12345")
	form := url.Values{
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/cb"},
		"state":                 {"random-state"},
		"scope":                 {"mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"decision":              {"deny"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize/decision",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessID})
	rec := httptest.NewRecorder()
	s.handleOAuthAuthorizeDecision(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=access_denied") {
		t.Errorf("Location = %q, want error=access_denied", loc)
	}
	if !strings.Contains(loc, "state=random-state") {
		t.Errorf("state not preserved in redirect: %q", loc)
	}
}

func TestAuthorizeDecision_Approve_IssuesCodeAndRedirects(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	if err := s.userStore.Create(auth.User{Username: "alice"}, "pw"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	sessID := s.sessionStore.Create("alice")

	verifier, challenge := pkcePair("verifier-of-sufficient-length-12345")
	_ = verifier
	form := url.Values{
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/cb"},
		"state":                 {"random-state"},
		"scope":                 {"mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"decision":              {"approve"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize/decision",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessID})
	rec := httptest.NewRecorder()
	s.handleOAuthAuthorizeDecision(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "code=oac_") {
		t.Errorf("Location = %q, want code=oac_...", loc)
	}
	if !strings.Contains(loc, "state=random-state") {
		t.Errorf("state not preserved: %q", loc)
	}
}

func TestAuthorizeDecision_UnsupportedPKCEMethod_ServesError(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	if err := s.userStore.Create(auth.User{Username: "alice"}, "pw"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	sessID := s.sessionStore.Create("alice")

	form := url.Values{
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/cb"},
		"state":                 {"st"},
		"scope":                 {"mcp"},
		"code_challenge":        {"anything"},
		"code_challenge_method": {"plain"}, // forbidden
		"decision":              {"approve"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize/decision",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessID})
	rec := httptest.NewRecorder()
	s.handleOAuthAuthorizeDecision(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// ─── /oauth/token ────────────────────────────────────────

func TestToken_FullFlow_ExchangeCodeForAccessToken(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	if err := s.userStore.Create(auth.User{Username: "alice"}, "pw"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	verifier, challenge := pkcePair("verifier-of-sufficient-length-12345")

	// Skip the HTTP consent step — IssueCode directly.
	code, err := s.oauthServer.IssueCode(client.ClientID, "http://127.0.0.1:8890/cb", challenge, "alice", "mcp")
	if err != nil {
		t.Fatalf("IssueCode: %v", err)
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {client.ClientID},
		"redirect_uri":  {"http://127.0.0.1:8890/cb"},
		"code_verifier": {verifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleOAuthToken(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	tok, _ := resp["access_token"].(string)
	if !strings.HasPrefix(tok, "gwk_") {
		t.Errorf("access_token = %q, want gwk_-prefixed", tok)
	}
	if resp["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", resp["token_type"])
	}
	if resp["scope"] != "mcp" {
		t.Errorf("scope = %v, want mcp", resp["scope"])
	}

	// The issued token verifies against the TokenStore under alice.
	verified, verr := s.tokenStore.Verify(tok)
	if verr != nil {
		t.Fatalf("issued token failed Verify: %v", verr)
	}
	if verified.User != "alice" {
		t.Errorf("verified token user = %q, want alice", verified.User)
	}
}

func TestToken_UnsupportedGrantType_400(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	form := url.Values{"grant_type": {"password"}}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleOAuthToken(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unsupported_grant_type") {
		t.Errorf("wrong error code: %s", rec.Body.String())
	}
}

func TestToken_MissingRequiredParams_400(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	form := url.Values{
		"grant_type": {"authorization_code"},
		// missing code, client_id, redirect_uri, code_verifier
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleOAuthToken(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Errorf("wrong error code: %s", rec.Body.String())
	}
}

func TestToken_BadPKCEVerifier_InvalidGrant(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	if err := s.userStore.Create(auth.User{Username: "alice"}, "pw"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	_, challenge := pkcePair("verifier-of-sufficient-length-12345")
	code, _ := s.oauthServer.IssueCode(client.ClientID, "http://127.0.0.1:8890/cb", challenge, "alice", "mcp")

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {client.ClientID},
		"redirect_uri":  {"http://127.0.0.1:8890/cb"},
		"code_verifier": {"THIS_DOES_NOT_MATCH_THE_CHALLENGE_1234567"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleOAuthToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("wrong error code: %s", rec.Body.String())
	}
}

func TestToken_UnknownCode_InvalidGrant(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	verifier, _ := pkcePair("verifier-of-sufficient-length-12345")
	_ = verifier

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"oac_never_existed"},
		"client_id":     {client.ClientID},
		"redirect_uri":  {"http://127.0.0.1:8890/cb"},
		"code_verifier": {"verifier-of-sufficient-length-12345"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleOAuthToken(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("wrong error code: %s", rec.Body.String())
	}
}

// ─── /oauth/login (login form target) ────────────────────

func TestOAuthLoginForm_BadCredentials_ShowsError(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	if err := s.userStore.Create(auth.User{Username: "alice"}, "correct"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	form := url.Values{
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/cb"},
		"state":                 {"st"},
		"scope":                 {"mcp"},
		"code_challenge":        {"challenge"},
		"code_challenge_method": {"S256"},
		"username":              {"alice"},
		"password":              {"WRONG"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleOAuthLoginForm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered login page)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Invalid credentials") {
		t.Errorf("expected error message, got: %s", body[:min(400, len(body))])
	}
}

func TestOAuthLoginForm_GoodCredentials_RedirectsToAuthorize(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	if err := s.userStore.Create(auth.User{Username: "alice"}, "correct"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}
	form := url.Values{
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/cb"},
		"state":                 {"st"},
		"scope":                 {"mcp"},
		"code_challenge":        {"challenge"},
		"code_challenge_method": {"S256"},
		"username":              {"alice"},
		"password":              {"correct"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleOAuthLoginForm(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/oauth/authorize?") {
		t.Errorf("Location = %q, want /oauth/authorize?...", loc)
	}
	// Session cookie was set.
	if rec.Header().Get("Set-Cookie") == "" {
		t.Errorf("expected Set-Cookie after successful login")
	}
}

func TestOAuthLoginForm_MissingCredentials_ShowsError(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	client := registerClient(t, s, "Claude", "http://127.0.0.1:8890/cb")
	form := url.Values{
		"client_id":             {client.ClientID},
		"redirect_uri":          {"http://127.0.0.1:8890/cb"},
		"state":                 {"st"},
		"code_challenge":        {"challenge"},
		"code_challenge_method": {"S256"},
		// no username / password
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleOAuthLoginForm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (re-rendered login page)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Username and password are required") {
		t.Errorf("expected error message: %s", rec.Body.String())
	}
}

// ─── oauthJSONError shape ────────────────────────────────

func TestOAuthJSONError_Shape(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	oauthJSONError(rec, http.StatusBadRequest, "invalid_request", "missing code")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["error"] != "invalid_request" {
		t.Errorf("error = %q", body["error"])
	}
	if body["error_description"] != "missing code" {
		t.Errorf("error_description = %q", body["error_description"])
	}
}

// ─── usernameFromSession helper ──────────────────────────

func TestUsernameFromSession_NoCookie_Empty(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := s.usernameFromSession(req); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestUsernameFromSession_ValidCookie_ReturnsUsername(t *testing.T) {
	t.Parallel()
	s := newOAuthServerTestServer(t)
	sessID := s.sessionStore.Create("alice")
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessID})
	if got := s.usernameFromSession(req); got != "alice" {
		t.Errorf("got %q, want alice", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
