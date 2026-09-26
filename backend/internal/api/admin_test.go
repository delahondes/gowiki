package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/auth"
)

// newAdminTestServer builds a Server wired to real UserStore / GroupStore /
// SessionStore inside a t.TempDir(). The admin handlers touch nothing else;
// keeping the wiring minimal makes each case fail-locatable.
func newAdminTestServer(t *testing.T) *Server {
	t.Helper()
	meta := t.TempDir()
	users, err := auth.NewUserStore(meta)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	groups, err := auth.NewGroupStore(filepath.Join(meta, "g"))
	if err != nil {
		t.Fatalf("NewGroupStore: %v", err)
	}
	sessions, err := auth.NewSessionStore(filepath.Join(meta, "s"), 24*time.Hour)
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}
	return &Server{
		userStore:    users,
		groupStore:   groups,
		sessionStore: sessions,
	}
}

// callAdmin invokes a handler with a chi route context, optional URL params,
// and optional caller username in the request context.
func callAdmin(handler http.HandlerFunc, method, url string, params map[string]string, body []byte, callerUsername string) *httptest.ResponseRecorder {
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

// createUserDirect writes a user through the store without HTTP so the test
// can set up fixtures independent of the handler under test.
func createUserDirect(t *testing.T, s *Server, username string, groups []string) {
	t.Helper()
	if err := s.userStore.Create(auth.User{Username: username, Groups: groups}, "initial-pw-1234"); err != nil {
		t.Fatalf("Create user %s: %v", username, err)
	}
}

// ─── requireAdmin ────────────────────────────────────────

// TestRequireAdmin_UnauthenticatedIs401 — the middleware refuses the request
// before any handler runs when no username is in context. This is the auth
// invariant that gates every admin endpoint.
func TestRequireAdmin_UnauthenticatedIs401(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := callAdmin(s.requireAdmin(next).ServeHTTP, http.MethodGet, "/api/admin", nil, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestRequireAdmin_NonAdminIs403 — an authenticated but non-admin user gets
// 403, not 401 (the distinction matters: "who are you?" vs "you can't").
func TestRequireAdmin_NonAdminIs403(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "alice", []string{"editors"})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := callAdmin(s.requireAdmin(next).ServeHTTP, http.MethodGet, "/api/admin", nil, nil, "alice")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// TestRequireAdmin_AdminPasses — a user in the admin group flows through.
func TestRequireAdmin_AdminPasses(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "root", []string{"admin"})
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	rec := callAdmin(s.requireAdmin(next).ServeHTTP, http.MethodGet, "/api/admin", nil, nil, "root")
	if !called || rec.Code != http.StatusOK {
		t.Errorf("admin blocked: called=%v code=%d", called, rec.Code)
	}
}

// ─── handleListUsers ─────────────────────────────────────

// TestListUsers_StripsPasswordHash — the response must not carry the
// bcrypt hash. If it ever does, a compromised token / admin endpoint
// leaks every user's password digest.
func TestListUsers_StripsPasswordHash(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "alice", []string{"editors"})

	rec := callAdmin(s.handleListUsers, http.MethodGet, "/api/admin/users", nil, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// The body must not mention "password_hash" — that's the JSON tag
	// that WOULD appear if the raw User struct leaked.
	if bytes.Contains(rec.Body.Bytes(), []byte("password_hash")) {
		t.Errorf("password_hash present in response: %s", rec.Body.String())
	}
	var body struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// NewUserStore bootstraps a default admin — we care that alice is
	// among the returned users and that no hash leaked, not the exact
	// count.
	found := false
	for _, u := range body.Users {
		if u["username"] == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("alice missing from users = %+v", body.Users)
	}
}

// ─── handleCreateUser ────────────────────────────────────

// TestCreateUser_Happy — POST with valid body creates the user; 201 with
// the created username echoed back.
func TestCreateUser_Happy(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	body, _ := json.Marshal(map[string]any{
		"username": "bob",
		"password": "hunter22-hunter22",
		"email":    "bob@example.com",
		"groups":   []string{"editors"},
	})
	rec := callAdmin(s.handleCreateUser, http.MethodPost, "/api/admin/users", nil, body, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if _, err := s.userStore.Get("bob"); err != nil {
		t.Errorf("user bob not persisted: %v", err)
	}
}

// TestCreateUser_MissingUsernameOrPassword_400 — the two required fields
// each get their own 400 refusal.
func TestCreateUser_MissingUsernameOrPassword_400(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	cases := []map[string]any{
		{"password": "x"},
		{"username": "x"},
		{},
	}
	for i, c := range cases {
		body, _ := json.Marshal(c)
		rec := callAdmin(s.handleCreateUser, http.MethodPost, "/api/admin/users", nil, body, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: status = %d, want 400", i, rec.Code)
		}
	}
}

// TestCreateUser_DuplicateIs409 — creating a user that already exists
// surfaces the ErrUserExists mapping.
func TestCreateUser_DuplicateIs409(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "dup", nil)
	body, _ := json.Marshal(map[string]any{
		"username": "dup",
		"password": "another-pw-1234",
	})
	rec := callAdmin(s.handleCreateUser, http.MethodPost, "/api/admin/users", nil, body, "")
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

// TestCreateUser_InvalidJSON_400 — malformed body.
func TestCreateUser_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	rec := callAdmin(s.handleCreateUser, http.MethodPost, "/api/admin/users", nil, []byte("not json"), "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── handleUpdateUser ────────────────────────────────────

// TestUpdateUser_Happy — patch the email + display_name, verify persistence.
func TestUpdateUser_Happy(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "alice", nil)
	body, _ := json.Marshal(map[string]any{
		"email":        "new@example.com",
		"display_name": "Alice",
	})
	rec := callAdmin(s.handleUpdateUser, http.MethodPut, "/api/admin/users/alice",
		map[string]string{"username": "alice"}, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got, _ := s.userStore.Get("alice")
	if got.Email != "new@example.com" || got.DisplayName != "Alice" {
		t.Errorf("user = %+v, want email=new@example.com displayName=Alice", got)
	}
}

// TestUpdateUser_UnknownIs404.
func TestUpdateUser_UnknownIs404(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	body, _ := json.Marshal(map[string]any{"email": "x@y"})
	rec := callAdmin(s.handleUpdateUser, http.MethodPut, "/api/admin/users/nobody",
		map[string]string{"username": "nobody"}, body, "root")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestUpdateUser_DisableRevokesSessions — the security invariant. When
// admin flips Disabled true, every existing session for that user must be
// killed on the same call, not later. Otherwise a disabled user rides
// their existing cookie up to 24h.
func TestUpdateUser_DisableRevokesSessions(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "eve", nil)
	// Seed three live sessions for eve + one for a bystander.
	sid1 := s.sessionStore.Create("eve")
	sid2 := s.sessionStore.Create("eve")
	sid3 := s.sessionStore.Create("eve")
	bystander := s.sessionStore.Create("mallory")

	body, _ := json.Marshal(map[string]any{"disabled": true})
	rec := callAdmin(s.handleUpdateUser, http.MethodPut, "/api/admin/users/eve",
		map[string]string{"username": "eve"}, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	for _, sid := range []string{sid1, sid2, sid3} {
		if _, ok := s.sessionStore.Get(sid); ok {
			t.Errorf("eve's session %s survived disable", sid)
		}
	}
	if _, ok := s.sessionStore.Get(bystander); !ok {
		t.Errorf("bystander session collateral-damaged")
	}
	// And the user record itself must reflect Disabled=true.
	got, _ := s.userStore.Get("eve")
	if !got.Disabled {
		t.Errorf("Disabled = false, want true on stored user")
	}
}

// TestUpdateUser_EnableDoesNotTouchSessions — the revoke only fires when
// flipping ON. Setting Disabled=false must not touch sessions (there
// are none to kill anyway, but the code path must not misfire).
func TestUpdateUser_EnableDoesNotTouchSessions(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "alice", nil)
	sid := s.sessionStore.Create("alice")

	body, _ := json.Marshal(map[string]any{"disabled": false})
	rec := callAdmin(s.handleUpdateUser, http.MethodPut, "/api/admin/users/alice",
		map[string]string{"username": "alice"}, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if _, ok := s.sessionStore.Get(sid); !ok {
		t.Errorf("session revoked despite Disabled=false")
	}
}

// ─── handleDeleteUser ────────────────────────────────────

// TestDeleteUser_Happy — regular delete removes the user AND kills its sessions.
func TestDeleteUser_Happy(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "cathy", nil)
	sid := s.sessionStore.Create("cathy")

	rec := callAdmin(s.handleDeleteUser, http.MethodDelete, "/api/admin/users/cathy",
		map[string]string{"username": "cathy"}, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if _, err := s.userStore.Get("cathy"); err == nil {
		t.Errorf("user still present after delete")
	}
	if _, ok := s.sessionStore.Get(sid); ok {
		t.Errorf("cathy's session survived hard delete")
	}
}

// TestDeleteUser_SelfDeleteIs400 — no shooting yourself in the foot.
func TestDeleteUser_SelfDeleteIs400(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "root", []string{"admin"})
	rec := callAdmin(s.handleDeleteUser, http.MethodDelete, "/api/admin/users/root",
		map[string]string{"username": "root"}, nil, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (self-delete forbidden)", rec.Code)
	}
	if _, err := s.userStore.Get("root"); err != nil {
		t.Errorf("root got deleted despite the guard: %v", err)
	}
}

// TestDeleteUser_UnknownIs404.
func TestDeleteUser_UnknownIs404(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	rec := callAdmin(s.handleDeleteUser, http.MethodDelete, "/api/admin/users/nobody",
		map[string]string{"username": "nobody"}, nil, "root")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ─── handleSetPassword ───────────────────────────────────

// TestSetPassword_Happy — the new password verifies, the old one doesn't.
func TestSetPassword_Happy(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	if err := s.userStore.Create(auth.User{Username: "alice"}, "old-pw-1234"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"password": "new-pw-9999"})
	rec := callAdmin(s.handleSetPassword, http.MethodPut, "/api/admin/users/alice/password",
		map[string]string{"username": "alice"}, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if err := s.userStore.Verify("alice", "new-pw-9999"); err != nil {
		t.Errorf("new password rejected: %v", err)
	}
	if err := s.userStore.Verify("alice", "old-pw-1234"); err == nil {
		t.Errorf("old password still accepted")
	}
}

// TestSetPassword_MissingPassword_400.
func TestSetPassword_MissingPassword_400(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	createUserDirect(t, s, "alice", nil)
	body, _ := json.Marshal(map[string]any{})
	rec := callAdmin(s.handleSetPassword, http.MethodPut, "/api/admin/users/alice/password",
		map[string]string{"username": "alice"}, body, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── Group handlers ──────────────────────────────────────

// TestListGroups_Bootstrap — a fresh GroupStore ships two defaults (admin + editors).
func TestListGroups_Bootstrap(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)
	rec := callAdmin(s.handleListGroups, http.MethodGet, "/api/admin/groups", nil, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Groups []auth.Group `json:"groups"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	// At minimum: admin + editors from bootstrap.
	names := map[string]bool{}
	for _, g := range body.Groups {
		names[g.Name] = true
	}
	if !names["admin"] || !names["editors"] {
		t.Errorf("groups = %+v, want admin+editors", body.Groups)
	}
}

// TestCreateGroup_Happy + Duplicate + MissingName.
func TestCreateGroup_HappyDuplicateMissingName(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)

	// Happy.
	body, _ := json.Marshal(map[string]string{"name": "reviewers", "description": "cool"})
	rec := callAdmin(s.handleCreateGroup, http.MethodPost, "/api/admin/groups", nil, body, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("happy: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	// Duplicate.
	rec = callAdmin(s.handleCreateGroup, http.MethodPost, "/api/admin/groups", nil, body, "")
	if rec.Code != http.StatusConflict {
		t.Errorf("dup: status = %d, want 409", rec.Code)
	}

	// Missing name.
	body, _ = json.Marshal(map[string]string{"description": "orphan"})
	rec = callAdmin(s.handleCreateGroup, http.MethodPost, "/api/admin/groups", nil, body, "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing-name: status = %d, want 400", rec.Code)
	}
}

// TestUpdateGroup — happy + unknown + missing-name-in-path.
func TestUpdateGroup_HappyUnknownMissingPath(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)

	body, _ := json.Marshal(map[string]string{"description": "new desc"})
	rec := callAdmin(s.handleUpdateGroup, http.MethodPut, "/api/admin/groups/editors",
		map[string]string{"name": "editors"}, body, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("happy: %d, want 200", rec.Code)
	}

	rec = callAdmin(s.handleUpdateGroup, http.MethodPut, "/api/admin/groups/ghost",
		map[string]string{"name": "ghost"}, body, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown: %d, want 404", rec.Code)
	}

	rec = callAdmin(s.handleUpdateGroup, http.MethodPut, "/api/admin/groups/",
		map[string]string{"name": ""}, body, "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing name: %d, want 400", rec.Code)
	}
}

// TestDeleteGroup — happy + unknown + missing-name.
func TestDeleteGroup_HappyUnknownMissing(t *testing.T) {
	t.Parallel()
	s := newAdminTestServer(t)

	rec := callAdmin(s.handleDeleteGroup, http.MethodDelete, "/api/admin/groups/editors",
		map[string]string{"name": "editors"}, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("happy: %d, want 200", rec.Code)
	}

	rec = callAdmin(s.handleDeleteGroup, http.MethodDelete, "/api/admin/groups/ghost",
		map[string]string{"name": "ghost"}, nil, "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown: %d, want 404", rec.Code)
	}

	rec = callAdmin(s.handleDeleteGroup, http.MethodDelete, "/api/admin/groups/",
		map[string]string{"name": ""}, nil, "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing name: %d, want 400", rec.Code)
	}
}
