package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gowiki/backend/internal/auth"
)

// newAuthTestServer wires the minimum a login/logout/me test needs: real
// UserStore + SessionStore in a temp dir. No config, no ACL, no tokens
// — the handlers under test don't touch those.
func newAuthTestServer(t *testing.T) *Server {
	t.Helper()
	meta := t.TempDir()
	users, err := auth.NewUserStore(meta)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	sessions, err := auth.NewSessionStore(filepath.Join(meta, "s"), 24*time.Hour)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	return &Server{
		userStore:    users,
		sessionStore: sessions,
	}
}

// seedUser puts a real user with a known password into the store.
func seedUser(t *testing.T, s *Server, username, password string, disabled bool) {
	t.Helper()
	if err := s.userStore.Create(auth.User{Username: username, DisplayName: username + " Display", Email: username + "@example"}, password); err != nil {
		t.Fatalf("Create %s: %v", username, err)
	}
	if disabled {
		yes := true
		if err := s.userStore.Update(username, auth.UserUpdate{Disabled: &yes}); err != nil {
			t.Fatalf("Update %s: %v", username, err)
		}
	}
}

func postJSON(handler http.HandlerFunc, url string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// ─── handleLogin ─────────────────────────────────────────

func TestHandleLogin_HappyPath(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	seedUser(t, s, "alice", "hunter22", false)

	rec := postJSON(s.handleLogin, "/api/auth/login",
		map[string]string{"username": "alice", "password": "hunter22"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v — %s", err, rec.Body.String())
	}
	if body["username"] != "alice" {
		t.Errorf("username = %q, want alice", body["username"])
	}

	// Cookie assertion: gowiki_session set with a non-empty value.
	setCookie := rec.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("no Set-Cookie header")
	}
	if !containsSubstring(setCookie, auth.CookieName+"=") {
		t.Errorf("Set-Cookie = %q, want to contain %q", setCookie, auth.CookieName+"=")
	}
}

func TestHandleLogin_WrongPassword_401(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	seedUser(t, s, "alice", "hunter22", false)

	rec := postJSON(s.handleLogin, "/api/auth/login",
		map[string]string{"username": "alice", "password": "WRONG"})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLogin_UnknownUser_401(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)

	rec := postJSON(s.handleLogin, "/api/auth/login",
		map[string]string{"username": "ghost", "password": "anything"})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandleLogin_DisabledUser_403(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	seedUser(t, s, "bob", "hunter22", true)

	rec := postJSON(s.handleLogin, "/api/auth/login",
		map[string]string{"username": "bob", "password": "hunter22"})

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleLogin_MissingFields_400(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)

	// Empty username.
	rec := postJSON(s.handleLogin, "/api/auth/login",
		map[string]string{"username": "", "password": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty username: status = %d, want 400", rec.Code)
	}

	// Empty password.
	rec = postJSON(s.handleLogin, "/api/auth/login",
		map[string]string{"username": "alice", "password": ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty password: status = %d, want 400", rec.Code)
	}
}

func TestHandleLogin_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── handleLogout ────────────────────────────────────────

func TestHandleLogout_ClearsCookieAndDeletesSession(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	seedUser(t, s, "alice", "hunter22", false)

	sessID := s.sessionStore.Create("alice")

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessID})
	rec := httptest.NewRecorder()
	s.handleLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// Session should be gone from the store.
	if _, ok := s.sessionStore.Get(sessID); ok {
		t.Error("session still present after logout")
	}
	// Clear cookie: Set-Cookie with MaxAge=0 or expired.
	setCookie := rec.Header().Get("Set-Cookie")
	if !containsSubstring(setCookie, "Max-Age=0") && !containsSubstring(setCookie, "1970") {
		t.Errorf("Set-Cookie did not clear cookie: %q", setCookie)
	}
}

func TestHandleLogout_NoCookie_StillOK(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	s.handleLogout(rec, req)
	// Logout without a cookie is idempotent — clear-cookie header is set
	// and the response is 200. This lets a broken tab still "log out"
	// cleanly.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ─── handleMe ───────────────────────────────────────────

func TestHandleMe_Unauthenticated_401(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandleMe_StaleCookie_401ClearsCookie(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: "does-not-exist"})
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	setCookie := rec.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Error("expected Set-Cookie clearing the stale session")
	}
}

func TestHandleMe_HappyPath(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	seedUser(t, s, "alice", "hunter22", false)
	sessID := s.sessionStore.Create("alice")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessID})
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["username"] != "alice" {
		t.Errorf("username = %v, want alice", body["username"])
	}
	if body["display_name"] != "alice Display" {
		t.Errorf("display_name = %v, want %q", body["display_name"], "alice Display")
	}
	if body["email"] != "alice@example" {
		t.Errorf("email = %v", body["email"])
	}
	// is_admin should be a bool. Alice is not admin.
	if body["is_admin"] != false {
		t.Errorf("is_admin = %v, want false", body["is_admin"])
	}
}

func TestHandleMe_AdminUserIsAdmin(t *testing.T) {
	t.Parallel()
	s := newAuthTestServer(t)
	// Manually create with admin group — Verify isn't required, just Get.
	if err := s.userStore.Create(auth.User{Username: "root", Groups: []string{"admin"}, DisplayName: "Root"}, "pw"); err != nil {
		t.Fatalf("Create root: %v", err)
	}
	sessID := s.sessionStore.Create("root")

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: sessID})
	rec := httptest.NewRecorder()
	s.handleMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["is_admin"] != true {
		t.Errorf("is_admin = %v, want true", body["is_admin"])
	}
}

// ─── helper ─────────────────────────────────────────────

func containsSubstring(haystack, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(s, substr string) int {
outer:
	for i := 0; i+len(substr) <= len(s); i++ {
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
