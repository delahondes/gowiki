package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/config"
)

// newTokensTestServer builds a Server with TokenStore + a config Store
// carrying a MaxTokensPerUser cap. The AIAPI on/off gate lives in the
// middleware (tryBearerAuth), not in these CRUD handlers — the handlers
// operate on session-authenticated callers who receive the plaintext
// once at creation and never again.
func newTokensTestServer(t *testing.T, maxTokensPerUser int) *Server {
	t.Helper()
	meta := t.TempDir()
	tokens, err := auth.NewTokenStore(meta)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	cfgPath := meta + "/config.yaml"
	cfgStore, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := cfgStore.Get()
	cfg.AIAPI.MaxTokensPerUser = maxTokensPerUser
	if err := cfgStore.Update(cfg); err != nil {
		t.Fatalf("cfg.Update: %v", err)
	}
	return &Server{
		tokenStore:  tokens,
		configStore: cfgStore,
	}
}

// callTokens invokes a handler with optional body, URL params, and caller.
func callTokens(handler http.HandlerFunc, method, url string, params map[string]string, body []byte, callerUsername string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, url, reader)
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if callerUsername != "" {
		ctx = context.WithValue(ctx, usernameKey, callerUsername)
	}
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// ─── handleCreateToken ────────────────────────────────────

// TestCreateToken_ReturnsPlaintextExactlyOnce — the plaintext token
// appears in the CREATE response and NEVER again. This is the single
// most important invariant of the token store: after this response,
// only the caller has the string, and only the bcrypt hash lives on
// disk.
func TestCreateToken_ReturnsPlaintextExactlyOnce(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	body, _ := json.Marshal(map[string]string{"name": "laptop"})

	rec := callTokens(s.handleCreateToken, http.MethodPost, "/api/tokens", nil, body, "alice")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(created.Token, "gwk_") {
		t.Errorf("plaintext = %q, expected gwk_ prefix", created.Token)
	}
	if created.Name != "laptop" || created.ID == "" {
		t.Errorf("unexpected shape: %+v", created)
	}

	// Now list — the plaintext must NOT appear anywhere in the response
	// (neither in a `token` field nor as a substring of any value).
	listRec := callTokens(s.handleListTokens, http.MethodGet, "/api/tokens", nil, nil, "alice")
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRec.Code)
	}
	if strings.Contains(listRec.Body.String(), created.Token) {
		t.Errorf("plaintext leaked into list response: %s", listRec.Body.String())
	}
	// A non-empty `token_hash` in the response would be the bcrypt digest
	// leaking. The field can appear as the empty string (List strips it
	// but the field's JSON tag is not `omitempty`), which is harmless.
	var listBody struct {
		Tokens []struct {
			TokenHash string `json:"token_hash"`
		} `json:"tokens"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &listBody)
	for _, tok := range listBody.Tokens {
		if tok.TokenHash != "" {
			t.Errorf("token_hash carries a value in list: %q", tok.TokenHash)
		}
	}
}

// TestCreateToken_MissingName_400.
func TestCreateToken_MissingName_400(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	body, _ := json.Marshal(map[string]string{})
	rec := callTokens(s.handleCreateToken, http.MethodPost, "/api/tokens", nil, body, "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestCreateToken_InvalidJSON_400.
func TestCreateToken_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	rec := callTokens(s.handleCreateToken, http.MethodPost, "/api/tokens", nil, []byte("not json"), "alice")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestCreateToken_ExceedsMaxPerUser_409 — cap = 2, third create refuses
// with 409. Other users' tokens don't count against alice's cap.
func TestCreateToken_ExceedsMaxPerUser_409(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 2)

	mkBody := func(name string) []byte {
		b, _ := json.Marshal(map[string]string{"name": name})
		return b
	}

	// Alice creates 2 (cap).
	for _, n := range []string{"one", "two"} {
		rec := callTokens(s.handleCreateToken, http.MethodPost, "/api/tokens", nil, mkBody(n), "alice")
		if rec.Code != http.StatusCreated {
			t.Fatalf("alice %s: %d, want 201", n, rec.Code)
		}
	}

	// 3rd → 409.
	rec := callTokens(s.handleCreateToken, http.MethodPost, "/api/tokens", nil, mkBody("three"), "alice")
	if rec.Code != http.StatusConflict {
		t.Errorf("alice third: %d, want 409", rec.Code)
	}

	// Bob is unaffected — his cap is independent.
	rec = callTokens(s.handleCreateToken, http.MethodPost, "/api/tokens", nil, mkBody("bob-one"), "bob")
	if rec.Code != http.StatusCreated {
		t.Errorf("bob first: %d, want 201 (per-user cap should be independent)", rec.Code)
	}
}

// ─── handleListTokens ─────────────────────────────────────

// TestListTokens_OnlyOwnTokens — alice sees hers, not bob's.
func TestListTokens_OnlyOwnTokens(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	_, _, _ = s.tokenStore.Create("alice", "a1", 5)
	_, _, _ = s.tokenStore.Create("alice", "a2", 5)
	_, _, _ = s.tokenStore.Create("bob", "b1", 5)

	rec := callTokens(s.handleListTokens, http.MethodGet, "/api/tokens", nil, nil, "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Tokens []auth.APIToken `json:"tokens"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Tokens) != 2 {
		t.Errorf("got %d tokens for alice, want 2 (bob's must not leak)", len(body.Tokens))
	}
	for _, tok := range body.Tokens {
		if tok.User != "alice" {
			t.Errorf("wrong user in list: %+v", tok)
		}
		if tok.TokenHash != "" {
			t.Errorf("hash leaked in list: %+v", tok)
		}
	}
}

// TestListTokens_EmptyReturnsArray — not `null`, per the store's
// contract; the JSON client depends on the array shape.
func TestListTokens_EmptyReturnsArray(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	rec := callTokens(s.handleListTokens, http.MethodGet, "/api/tokens", nil, nil, "empty")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Tokens []auth.APIToken `json:"tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Tokens == nil {
		t.Errorf("tokens == nil (want []); body=%s", rec.Body.String())
	}
}

// ─── handleDeleteToken ────────────────────────────────────

// TestDeleteToken_Happy.
func TestDeleteToken_Happy(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	tok, _, _ := s.tokenStore.Create("alice", "hers", 5)

	rec := callTokens(s.handleDeleteToken, http.MethodDelete, "/api/tokens/"+tok.ID,
		map[string]string{"id": tok.ID}, nil, "alice")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(s.tokenStore.ListForUser("alice")) != 0 {
		t.Errorf("token not removed")
	}
}

// TestDeleteToken_RefusesOtherUsersToken — bob cannot delete alice's
// token. The store returns ErrTokenNotFound to hide the token's
// existence from bob (any other response would be a token-enumeration
// oracle).
func TestDeleteToken_RefusesOtherUsersToken(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	tok, _, _ := s.tokenStore.Create("alice", "hers", 5)

	rec := callTokens(s.handleDeleteToken, http.MethodDelete, "/api/tokens/"+tok.ID,
		map[string]string{"id": tok.ID}, nil, "bob")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (bob must not learn alice has that token)", rec.Code)
	}
	// Alice's token must survive.
	if len(s.tokenStore.ListForUser("alice")) != 1 {
		t.Errorf("bob managed to delete alice's token")
	}
}

// TestDeleteToken_Unknown_404.
func TestDeleteToken_Unknown_404(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	rec := callTokens(s.handleDeleteToken, http.MethodDelete, "/api/tokens/tok_deadbeef",
		map[string]string{"id": "tok_deadbeef"}, nil, "alice")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ─── handleAdminListTokens / handleAdminDeleteToken ────────

// TestAdminListTokens_AcrossUsers — admin sees every user's tokens.
func TestAdminListTokens_AcrossUsers(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	_, _, _ = s.tokenStore.Create("alice", "a1", 5)
	_, _, _ = s.tokenStore.Create("bob", "b1", 5)

	rec := callTokens(s.handleAdminListTokens, http.MethodGet, "/api/admin/tokens", nil, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Tokens []auth.APIToken `json:"tokens"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Tokens) != 2 {
		t.Errorf("got %d tokens, want 2 (alice + bob)", len(body.Tokens))
	}
	for _, tok := range body.Tokens {
		if tok.TokenHash != "" {
			t.Errorf("hash leaked in admin list: %+v", tok)
		}
	}
}

// TestAdminDeleteToken_CrossUser — the admin endpoint deletes anyone's
// token (unlike the user endpoint that scopes to the caller).
func TestAdminDeleteToken_CrossUser(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	tok, _, _ := s.tokenStore.Create("alice", "hers", 5)

	rec := callTokens(s.handleAdminDeleteToken, http.MethodDelete, "/api/admin/tokens/"+tok.ID,
		map[string]string{"id": tok.ID}, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(s.tokenStore.ListForUser("alice")) != 0 {
		t.Errorf("token not removed by admin delete")
	}
}

// TestAdminDeleteToken_Unknown_404.
func TestAdminDeleteToken_Unknown_404(t *testing.T) {
	t.Parallel()
	s := newTokensTestServer(t, 5)
	rec := callTokens(s.handleAdminDeleteToken, http.MethodDelete, "/api/admin/tokens/tok_deadbeef",
		map[string]string{"id": "tok_deadbeef"}, nil, "root")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
