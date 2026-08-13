// HTTP handlers exposing gowiki as an OAuth 2.0 authorization server.
// See backend/internal/auth/oauthserver.go for the underlying storage and
// PKCE mechanics. This file is the outward-facing HTTP contract:
//
//	GET  /.well-known/oauth-authorization-server  — RFC 8414 metadata
//	GET  /.well-known/oauth-protected-resource    — RFC 9728 metadata
//	POST /oauth/register                          — RFC 7591 DCR
//	GET  /oauth/authorize                         — consent screen (or redirect to login)
//	POST /oauth/authorize/decision                — consent form target
//	POST /oauth/token                             — authorization_code → access_token
//
// Access tokens issued here are ordinary gwk_ API tokens created via the
// existing TokenStore, so they show up under Admin → Tokens and can be
// revoked from there.
package api

import (
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/auth"
)

// registerOAuthServerRoutes wires the OAuth authorization-server endpoints
// onto the given chi router. All endpoints are public (no auth middleware);
// each handler enforces its own rules (session lookup on /authorize,
// PKCE + client match on /token, etc).
func (s *Server) registerOAuthServerRoutes(r chi.Router) {
	if s.oauthServer == nil {
		return
	}
	r.Get("/.well-known/oauth-authorization-server", s.handleOAuthAuthServerMetadata)
	r.Get("/.well-known/oauth-protected-resource", s.handleOAuthProtectedResourceMetadata)
	r.Post("/oauth/register", s.handleOAuthRegister)
	r.Get("/oauth/authorize", s.handleOAuthAuthorize)
	r.Post("/oauth/authorize/decision", s.handleOAuthAuthorizeDecision)
	r.Post("/oauth/login", s.handleOAuthLoginForm)
	r.Post("/oauth/token", s.handleOAuthToken)
}

// ── Metadata endpoints ─────────────────────────────────────────

func (s *Server) handleOAuthAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	origin := requestOrigin(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                origin,
		"authorization_endpoint":                origin + "/oauth/authorize",
		"token_endpoint":                        origin + "/oauth/token",
		"registration_endpoint":                 origin + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"mcp"},
	})
}

func (s *Server) handleOAuthProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	origin := requestOrigin(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 origin + "/api/mcp/v1",
		"authorization_servers":    []string{origin},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{"mcp"},
	})
}

// ── Dynamic Client Registration (RFC 7591) ─────────────────────

type dcrRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	var req dcrRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata", "malformed JSON body")
		return
	}

	if len(req.RedirectURIs) == 0 {
		oauthJSONError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	// We only issue public clients — reject any other auth method explicitly
	// so well-behaved clients fall back to `none` (which is what MCP expects).
	if req.TokenEndpointAuthMethod != "" && req.TokenEndpointAuthMethod != "none" {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata",
			"only token_endpoint_auth_method=none (public client) is supported")
		return
	}

	client, err := s.oauthServer.RegisterClient(req.ClientName, req.RedirectURIs)
	if err != nil {
		if errors.Is(err, auth.ErrOAuthInvalidRedirect) {
			oauthJSONError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
		oauthJSONError(w, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ClientID,
		"client_id_issued_at":        time.Now().Unix(),
		"client_name":                client.ClientName,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
	})
}

// ── Authorization endpoint ─────────────────────────────────────

type authorizeParams struct {
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
}

func parseAuthorizeParams(r *http.Request) (authorizeParams, error) {
	q := r.URL.Query()
	p := authorizeParams{
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		State:               q.Get("state"),
		Scope:               q.Get("scope"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
	}
	if q.Get("response_type") != "code" {
		return p, errors.New("response_type must be code")
	}
	if p.ClientID == "" || p.RedirectURI == "" {
		return p, errors.New("client_id and redirect_uri are required")
	}
	if p.CodeChallenge == "" {
		return p, errors.New("code_challenge is required (PKCE mandatory)")
	}
	if p.CodeChallengeMethod == "" {
		p.CodeChallengeMethod = "S256"
	}
	if p.CodeChallengeMethod != "S256" {
		return p, errors.New("only S256 code_challenge_method is supported")
	}
	return p, nil
}

func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	params, err := parseAuthorizeParams(r)
	if err != nil {
		serveOAuthError(w, err.Error())
		return
	}
	client, cerr := s.oauthServer.LookupClient(params.ClientID)
	if cerr != nil {
		serveOAuthError(w, "unknown client_id")
		return
	}
	if !client.MatchesRedirect(params.RedirectURI) {
		// Per RFC 6749 § 3.1.2.4 we must NOT redirect to an unregistered URI.
		serveOAuthError(w, "redirect_uri does not match a registered value")
		return
	}

	// Check session cookie. Show an inline login form (self-contained
	// standalone HTML page, not the SPA) when unauthenticated — the OAuth
	// flow needs a deterministic redirect target the SPA can't provide.
	username := s.usernameFromSession(r)
	if username == "" {
		serveLoginPage(w, loginContext{
			ClientName:          client.ClientName,
			ClientID:            client.ClientID,
			RedirectURI:         params.RedirectURI,
			State:               params.State,
			Scope:               params.Scope,
			CodeChallenge:       params.CodeChallenge,
			CodeChallengeMethod: params.CodeChallengeMethod,
		})
		return
	}

	serveConsentPage(w, consentContext{
		ClientName:          client.ClientName,
		ClientID:            client.ClientID,
		RedirectURI:         params.RedirectURI,
		State:               params.State,
		Scope:               params.Scope,
		CodeChallenge:       params.CodeChallenge,
		CodeChallengeMethod: params.CodeChallengeMethod,
		Username:            username,
	})
}

// handleOAuthLoginForm authenticates a form POST from the standalone login
// page, sets a session cookie on success, then redirects the user back to
// /oauth/authorize with the original OAuth params so the consent screen
// renders next.
func (s *Server) handleOAuthLoginForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		serveOAuthError(w, "invalid form")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	ctx := loginContext{
		ClientID:            r.FormValue("client_id"),
		RedirectURI:         r.FormValue("redirect_uri"),
		State:               r.FormValue("state"),
		Scope:               r.FormValue("scope"),
		CodeChallenge:       r.FormValue("code_challenge"),
		CodeChallengeMethod: r.FormValue("code_challenge_method"),
	}
	// Re-fetch client name for the re-rendered page if credentials are bad.
	if client, err := s.oauthServer.LookupClient(ctx.ClientID); err == nil {
		ctx.ClientName = client.ClientName
	}

	if username == "" || password == "" {
		ctx.Error = "Username and password are required."
		serveLoginPage(w, ctx)
		return
	}

	if verr := s.userStore.Verify(username, password); verr != nil {
		if errors.Is(verr, auth.ErrUserDisabled) {
			ctx.Error = "This account is disabled."
		} else {
			ctx.Error = "Invalid credentials."
		}
		serveLoginPage(w, ctx)
		return
	}

	s.userStore.UpdateLastLogin(username)
	sessionID := s.sessionStore.Create(username)
	s.sessionStore.SetSessionCookie(w, r, sessionID)

	// Bounce to /oauth/authorize with the original params so the flow
	// resumes at the consent screen.
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", ctx.ClientID)
	q.Set("redirect_uri", ctx.RedirectURI)
	q.Set("state", ctx.State)
	q.Set("scope", ctx.Scope)
	q.Set("code_challenge", ctx.CodeChallenge)
	q.Set("code_challenge_method", ctx.CodeChallengeMethod)
	http.Redirect(w, r, "/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// handleOAuthAuthorizeDecision receives the consent form POST. Approve →
// issue code + redirect to client. Deny → redirect to client with an error
// per RFC 6749 § 4.1.2.1.
func (s *Server) handleOAuthAuthorizeDecision(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		serveOAuthError(w, "invalid form")
		return
	}
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	scope := r.FormValue("scope")
	codeChallenge := r.FormValue("code_challenge")
	codeChallengeMethod := r.FormValue("code_challenge_method")
	decision := r.FormValue("decision")

	client, err := s.oauthServer.LookupClient(clientID)
	if err != nil || !client.MatchesRedirect(redirectURI) {
		serveOAuthError(w, "invalid client or redirect_uri")
		return
	}
	if codeChallengeMethod != "S256" {
		serveOAuthError(w, "unsupported code_challenge_method")
		return
	}

	username := s.usernameFromSession(r)
	if username == "" {
		// Session evaporated between GET and POST.
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	redirect, _ := url.Parse(redirectURI)
	q := redirect.Query()

	if decision != "approve" {
		q.Set("error", "access_denied")
		q.Set("error_description", "The user denied the request")
		if state != "" {
			q.Set("state", state)
		}
		redirect.RawQuery = q.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
		return
	}

	code, err := s.oauthServer.IssueCode(clientID, redirectURI, codeChallenge, username, scope)
	if err != nil {
		serveOAuthError(w, "failed to issue authorization code: "+err.Error())
		return
	}

	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	redirect.RawQuery = q.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

// ── Token endpoint ─────────────────────────────────────────────

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthJSONError(w, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		oauthJSONError(w, http.StatusBadRequest, "unsupported_grant_type",
			"only authorization_code is supported")
		return
	}
	code := r.PostForm.Get("code")
	clientID := r.PostForm.Get("client_id")
	redirectURI := r.PostForm.Get("redirect_uri")
	verifier := r.PostForm.Get("code_verifier")

	if code == "" || clientID == "" || redirectURI == "" || verifier == "" {
		oauthJSONError(w, http.StatusBadRequest, "invalid_request",
			"code, client_id, redirect_uri, and code_verifier are all required")
		return
	}

	authCode, err := s.oauthServer.RedeemCode(code, clientID, redirectURI, verifier)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrOAuthCodeNotFound), errors.Is(err, auth.ErrOAuthCodeExpired):
			oauthJSONError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		case errors.Is(err, auth.ErrOAuthBadPKCE):
			oauthJSONError(w, http.StatusBadRequest, "invalid_grant",
				"PKCE verifier does not match challenge")
		case errors.Is(err, auth.ErrOAuthInvalidRedirect):
			oauthJSONError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		default:
			oauthJSONError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		}
		return
	}

	client, err := s.oauthServer.LookupClient(clientID)
	if err != nil {
		oauthJSONError(w, http.StatusBadRequest, "invalid_client", "unknown client_id")
		return
	}

	// Reuse the existing API-token store so admins see these under
	// Admin → Tokens and can revoke them. maxPerUser=0 disables the per-user
	// quota — quota is enforced via OAuth client registrations rather than
	// by counting tokens.
	tokenName := "MCP OAuth: " + client.ClientName
	_, plaintext, terr := s.tokenStore.Create(authCode.Username, tokenName, 0)
	if terr != nil {
		oauthJSONError(w, http.StatusInternalServerError, "server_error",
			"failed to issue access token: "+terr.Error())
		return
	}

	s.oauthServer.TouchClient(clientID)

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": plaintext,
		"token_type":   "Bearer",
		"scope":        authCode.Scope,
		// No expires_in — gwk_ tokens don't currently carry an expiry. When
		// token expiry is added to the store, surface it here.
	})
}

// ── helpers ────────────────────────────────────────────────────

// usernameFromSession reads the gowiki session cookie and returns the
// authenticated username, or "" if unauthenticated / invalid.
func (s *Server) usernameFromSession(r *http.Request) string {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		return ""
	}
	sess, ok := s.sessionStore.Get(cookie.Value)
	if !ok {
		return ""
	}
	return sess.Username
}

func oauthJSONError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":             code,
		"error_description": description,
	})
}

// consentContext feeds the consent-page template.
type consentContext struct {
	ClientName          string
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
	Username            string
}

var consentTemplate = template.Must(template.New("consent").Parse(consentHTML))
var loginTemplate = template.Must(template.New("login").Parse(loginHTML))

func serveConsentPage(w http.ResponseWriter, ctx consentContext) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := consentTemplate.Execute(w, ctx); err != nil {
		log.Printf("oauth consent template execute: %v", err)
	}
}

// loginContext feeds the standalone login form served when /oauth/authorize
// is hit without a valid session cookie.
type loginContext struct {
	ClientName          string
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string
	Error               string
}

func serveLoginPage(w http.ResponseWriter, ctx loginContext) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := loginTemplate.Execute(w, ctx); err != nil {
		log.Printf("oauth login template execute: %v", err)
	}
}

const consentHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Authorize {{.ClientName}}</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
         background: #f6f7fb; margin: 0; padding: 48px 16px; color: #222; }
  .card { max-width: 460px; margin: 0 auto; background: #fff; border-radius: 12px;
          box-shadow: 0 8px 24px rgba(0,0,0,0.08); padding: 32px; }
  h1 { font-size: 20px; margin: 0 0 12px; }
  p { line-height: 1.5; margin: 8px 0; color: #444; }
  .client { font-weight: 600; }
  .user { color: #2563eb; }
  .buttons { display: flex; gap: 12px; margin-top: 24px; }
  button { flex: 1; padding: 10px 16px; border-radius: 8px; border: 1px solid transparent;
           font-size: 14px; font-weight: 500; cursor: pointer; }
  .approve { background: #2563eb; color: #fff; }
  .approve:hover { background: #1d4ed8; }
  .deny { background: #fff; color: #444; border-color: #d1d5db; }
  .deny:hover { background: #f3f4f6; }
  .fine { color: #666; font-size: 13px; margin-top: 16px; }
</style>
</head>
<body>
<div class="card">
  <h1>Authorize <span class="client">{{.ClientName}}</span></h1>
  <p><span class="client">{{.ClientName}}</span> is requesting access to this
     wiki as <span class="user">{{.Username}}</span>.</p>
  <p>If you approve, an access token will be issued that grants the caller
     the same view and edit permissions you have. You can revoke it at any
     time under <strong>Admin → Tokens</strong>.</p>
  <form method="post" action="/oauth/authorize/decision">
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="state" value="{{.State}}">
    <input type="hidden" name="scope" value="{{.Scope}}">
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
    <div class="buttons">
      <button type="submit" name="decision" value="deny" class="deny">Deny</button>
      <button type="submit" name="decision" value="approve" class="approve">Approve</button>
    </div>
  </form>
  <p class="fine">You are signed in as <strong>{{.Username}}</strong>.
     Approving delegates only your own permissions — nothing more.</p>
</div>
</body>
</html>`

const loginHTML = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Sign in to authorize {{.ClientName}}</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
         background: #f6f7fb; margin: 0; padding: 48px 16px; color: #222; }
  .card { max-width: 380px; margin: 0 auto; background: #fff; border-radius: 12px;
          box-shadow: 0 8px 24px rgba(0,0,0,0.08); padding: 32px; }
  h1 { font-size: 20px; margin: 0 0 6px; }
  .sub { color: #666; margin: 0 0 20px; font-size: 14px; }
  label { display: block; font-size: 13px; color: #444; margin: 12px 0 4px; }
  input { width: 100%; padding: 8px 10px; box-sizing: border-box;
          border: 1px solid #d1d5db; border-radius: 6px; font-size: 14px; }
  input:focus { outline: none; border-color: #2563eb; box-shadow: 0 0 0 3px rgba(37,99,235,0.15); }
  .error { background: #fee2e2; color: #991b1b; padding: 8px 12px; border-radius: 6px;
           margin: 12px 0; font-size: 13px; }
  button { width: 100%; margin-top: 20px; padding: 10px 16px; border-radius: 8px; border: none;
           background: #2563eb; color: #fff; font-size: 14px; font-weight: 500; cursor: pointer; }
  button:hover { background: #1d4ed8; }
</style>
</head>
<body>
<div class="card">
  <h1>Sign in</h1>
  <p class="sub">to authorize <strong>{{.ClientName}}</strong></p>
  {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
  <form method="post" action="/oauth/login">
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="state" value="{{.State}}">
    <input type="hidden" name="scope" value="{{.Scope}}">
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
    <label for="username">Username</label>
    <input id="username" name="username" type="text" autocomplete="username" autofocus required>
    <label for="password">Password</label>
    <input id="password" name="password" type="password" autocomplete="current-password" required>
    <button type="submit">Sign in</button>
  </form>
</div>
</body>
</html>`
