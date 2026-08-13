// OAuth 2.0 authorization server for the MCP endpoint.
//
// Gowiki acts as an OAuth 2.0 authorization server so that MCP clients
// (Claude.ai, Claude Desktop's "Add connector" UI, etc.) can register
// themselves via RFC 7591 Dynamic Client Registration and obtain access
// tokens via the standard authorization-code + PKCE flow.
//
// The issued access token is a normal gwk_ API token — issued via the
// TokenStore so it flows through the same audit, expiry, and admin
// listing machinery — with the OAuth client name recorded in the token
// name field for traceability.
//
// This is a MINIMAL implementation deliberately focused on what MCP
// clients need:
//   - public clients only (no client_secret; PKCE mandatory)
//   - S256 code challenges only (plain rejected)
//   - single grant type: authorization_code
//   - no refresh tokens (issued tokens are long-lived, matching
//     existing API-token behavior)
//   - client registration is unauthenticated but rate-limit-protected
//     by the calling HTTP layer
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrRegisteredClientNotFound   = errors.New("oauth client not found")
	ErrOAuthInvalidRedirect  = errors.New("redirect_uri does not match a registered value")
	ErrOAuthCodeNotFound     = errors.New("authorization code not found or already used")
	ErrOAuthCodeExpired      = errors.New("authorization code expired")
	ErrOAuthBadPKCE          = errors.New("PKCE verifier does not match challenge")
	ErrOAuthUnsupportedPKCE  = errors.New("only S256 code_challenge_method is supported")
	ErrOAuthMissingChallenge = errors.New("code_challenge is required (PKCE mandatory)")
	ErrOAuthNoRedirectURIs   = errors.New("at least one redirect_uri is required")
)

// RegisteredClient is a registered client (typically an MCP host like Claude).
// Storage is a flat JSON file under metaRoot. Public clients only.
type RegisteredClient struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	CreatedAt    string   `json:"created_at"`
	// LastUsedAt is best-effort — helps admins spot stale registrations.
	LastUsedAt string `json:"last_used_at,omitempty"`
}

// AuthCode is a short-lived authorization code kept in memory only.
// Discarded on redemption or expiry.
type AuthCode struct {
	Code          string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Username      string
	Scope         string
	ExpiresAt     time.Time
}

// AuthCodeTTL bounds how long a code is valid — spec recommends short.
const AuthCodeTTL = 10 * time.Minute

// OAuthServer holds the client registry and in-memory code store. It is
// safe for concurrent use.
type OAuthServer struct {
	mu          sync.RWMutex
	clients     map[string]RegisteredClient
	codes       map[string]AuthCode
	clientsPath string
}

// NewOAuthServer loads (or creates) the client registry at
// metaRoot/oauth_clients.json.
func NewOAuthServer(metaRoot string) (*OAuthServer, error) {
	srv := &OAuthServer{
		clients:     map[string]RegisteredClient{},
		codes:       map[string]AuthCode{},
		clientsPath: filepath.Join(metaRoot, "oauth_clients.json"),
	}
	if err := srv.load(); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *OAuthServer) load() error {
	data, err := os.ReadFile(s.clientsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read oauth clients: %w", err)
	}
	var list []RegisteredClient
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("parse oauth clients: %w", err)
	}
	for _, c := range list {
		s.clients[c.ClientID] = c
	}
	return nil
}

func (s *OAuthServer) saveLocked() error {
	list := make([]RegisteredClient, 0, len(s.clients))
	for _, c := range s.clients {
		list = append(list, c)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal oauth clients: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.clientsPath), 0o755); err != nil {
		return err
	}
	// Atomic replace via temp file.
	tmp, err := os.CreateTemp(filepath.Dir(s.clientsPath), "oauth_clients-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.clientsPath)
}

// RegisterClient creates a new public OAuth client. redirectURIs must be
// absolute HTTPS URLs (localhost http is also accepted for developer flows).
func (s *OAuthServer) RegisterClient(clientName string, redirectURIs []string) (RegisteredClient, error) {
	if len(redirectURIs) == 0 {
		return RegisteredClient{}, ErrOAuthNoRedirectURIs
	}
	for _, u := range redirectURIs {
		if err := validateRedirectURI(u); err != nil {
			return RegisteredClient{}, err
		}
	}

	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		return RegisteredClient{}, fmt.Errorf("generate client id: %w", err)
	}
	id := "mcp_" + hex.EncodeToString(idBytes)

	name := strings.TrimSpace(clientName)
	if name == "" {
		name = "unnamed client"
	}

	client := RegisteredClient{
		ClientID:     id,
		ClientName:   name,
		RedirectURIs: append([]string(nil), redirectURIs...),
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	s.mu.Lock()
	s.clients[id] = client
	if err := s.saveLocked(); err != nil {
		delete(s.clients, id)
		s.mu.Unlock()
		return RegisteredClient{}, err
	}
	s.mu.Unlock()
	return client, nil
}

// LookupClient returns the client with the given id, or ErrRegisteredClientNotFound.
func (s *OAuthServer) LookupClient(clientID string) (RegisteredClient, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clients[clientID]
	if !ok {
		return RegisteredClient{}, ErrRegisteredClientNotFound
	}
	return c, nil
}

// ListClients returns a copy of every registered client, for admin listing.
func (s *OAuthServer) ListClients() []RegisteredClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RegisteredClient, 0, len(s.clients))
	for _, c := range s.clients {
		out = append(out, c)
	}
	return out
}

// DeleteClient removes a client registration (admin only). Existing access
// tokens issued to that client are NOT revoked — the admin must revoke them
// separately from the tokens UI.
func (s *OAuthServer) DeleteClient(clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.clients[clientID]; !ok {
		return ErrRegisteredClientNotFound
	}
	delete(s.clients, clientID)
	return s.saveLocked()
}

// MatchesRedirect reports whether the given uri is one of the client's
// registered redirect_uris. Exact match.
func (c RegisteredClient) MatchesRedirect(uri string) bool {
	for _, r := range c.RedirectURIs {
		if r == uri {
			return true
		}
	}
	return false
}

// IssueCode stores a one-shot authorization code and returns its opaque
// value. The code is bound to (client_id, redirect_uri, code_challenge,
// username) and expires after AuthCodeTTL.
func (s *OAuthServer) IssueCode(clientID, redirectURI, codeChallenge, username, scope string) (string, error) {
	if codeChallenge == "" {
		return "", ErrOAuthMissingChallenge
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	code := "oac_" + base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[code] = AuthCode{
		Code:          code,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
		Username:      username,
		Scope:         scope,
		ExpiresAt:     time.Now().Add(AuthCodeTTL),
	}
	return code, nil
}

// RedeemCode consumes a code. Verifies PKCE, redirect_uri, and client_id.
// The code is removed from the store on success OR any failure — codes are
// strictly single-use, and a failed exchange should not permit a retry.
func (s *OAuthServer) RedeemCode(code, clientID, redirectURI, codeVerifier string) (AuthCode, error) {
	s.mu.Lock()
	c, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	s.mu.Unlock()

	if !ok {
		return AuthCode{}, ErrOAuthCodeNotFound
	}
	if time.Now().After(c.ExpiresAt) {
		return AuthCode{}, ErrOAuthCodeExpired
	}
	if c.ClientID != clientID {
		return AuthCode{}, ErrOAuthCodeNotFound
	}
	if c.RedirectURI != redirectURI {
		return AuthCode{}, ErrOAuthInvalidRedirect
	}
	if !verifyPKCES256(codeVerifier, c.CodeChallenge) {
		return AuthCode{}, ErrOAuthBadPKCE
	}
	return c, nil
}

// TouchClient bumps a client's LastUsedAt timestamp. Best-effort; errors are
// swallowed since this is telemetry-only.
func (s *OAuthServer) TouchClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[clientID]
	if !ok {
		return
	}
	c.LastUsedAt = time.Now().UTC().Format(time.RFC3339)
	s.clients[clientID] = c
	_ = s.saveLocked()
}

// GCCodes deletes expired codes. Call periodically from a goroutine.
func (s *OAuthServer) GCCodes() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for code, c := range s.codes {
		if now.After(c.ExpiresAt) {
			delete(s.codes, code)
		}
	}
}

// verifyPKCES256 checks that BASE64URL(SHA256(verifier)) == challenge.
func verifyPKCES256(verifier, challenge string) bool {
	if verifier == "" {
		return false
	}
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])
	return expected == challenge
}

// validateRedirectURI rejects obviously bad URIs. HTTPS is required except
// for loopback (developer flows / local MCP hosts).
func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrOAuthInvalidRedirect, raw)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("%w: scheme must be https (or http for loopback)", ErrOAuthInvalidRedirect)
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "[::1]" {
			return fmt.Errorf("%w: http redirect_uri only allowed for loopback", ErrOAuthInvalidRedirect)
		}
	}
	if u.Fragment != "" {
		return fmt.Errorf("%w: redirect_uri must not contain a fragment", ErrOAuthInvalidRedirect)
	}
	return nil
}
