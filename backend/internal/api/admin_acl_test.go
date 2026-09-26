package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"gowiki/backend/internal/auth"
)

// newACLTestServer wires just the ACL store — the two handlers touch
// nothing else. Using a real store (not a mock) means the tests
// exercise the actual validation + persistence path.
func newACLTestServer(t *testing.T) *Server {
	t.Helper()
	meta := t.TempDir()
	store, err := auth.NewACLStore(filepath.Join(meta, "acl"))
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}
	return &Server{aclStore: store}
}

// TestHandleListACL_ReturnsBootstrapRules — a fresh store bootstraps
// with three default rules; the handler must surface them. Assert on
// the count + the admin rule specifically so a change to the default
// set trips the test.
func TestHandleListACL_ReturnsBootstrapRules(t *testing.T) {
	t.Parallel()
	s := newACLTestServer(t)
	rec := callAdmin(s.handleListACL, http.MethodGet, "/api/admin/acl", nil, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Rules []auth.ACLRule `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v — %s", err, rec.Body.String())
	}
	if len(body.Rules) != 3 {
		t.Errorf("expected 3 default rules, got %d — bootstrap defaults changed?", len(body.Rules))
	}
	seenAdmin := false
	for _, r := range body.Rules {
		if r.SubjectType == "group" && r.Subject == "admin" {
			seenAdmin = true
			// admin gets view+edit+delete on every page.
			if len(r.Permissions) != 3 {
				t.Errorf("admin default permissions = %v, want 3 entries", r.Permissions)
			}
		}
	}
	if !seenAdmin {
		t.Errorf("no default admin rule found in bootstrap: %+v", body.Rules)
	}
}

// TestHandleReplaceACL_HappyPath — a valid rule set replaces the
// defaults; the response echoes the new set (sorted by specificity).
func TestHandleReplaceACL_HappyPath(t *testing.T) {
	t.Parallel()
	s := newACLTestServer(t)
	newRules := []auth.ACLRule{
		{Pattern: ".*", SubjectType: "group", Subject: "admin", Permissions: []string{"view", "edit", "delete"}},
		{Pattern: "/docs/.*", SubjectType: "user", Subject: "alice", Permissions: []string{"view"}},
	}
	body, _ := json.Marshal(map[string]any{"rules": newRules})
	rec := callAdmin(s.handleReplaceACL, http.MethodPut, "/api/admin/acl", nil, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	// After replace, List should return exactly the new rules.
	listed := s.aclStore.List()
	if len(listed) != 2 {
		t.Errorf("expected 2 rules after replace, got %d", len(listed))
	}
}

// TestHandleReplaceACL_InvalidJSON_400 — malformed body is a client
// error, not a server error.
func TestHandleReplaceACL_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := newACLTestServer(t)
	rec := callAdmin(s.handleReplaceACL, http.MethodPut, "/api/admin/acl", nil, []byte("{not-json"), "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleReplaceACL_InvalidPermission_400 — validateRules refuses
// unknown permission strings; the handler translates that into a 400.
func TestHandleReplaceACL_InvalidPermission_400(t *testing.T) {
	t.Parallel()
	s := newACLTestServer(t)
	rules := []auth.ACLRule{{
		Pattern: ".*", SubjectType: "group", Subject: "admin",
		Permissions: []string{"view", "smash"}, // "smash" is not in ValidPermissions
	}}
	body, _ := json.Marshal(map[string]any{"rules": rules})
	rec := callAdmin(s.handleReplaceACL, http.MethodPut, "/api/admin/acl", nil, body, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleReplaceACL_InvalidSubjectType_400 — subject_type must be
// one of user/group/special.
func TestHandleReplaceACL_InvalidSubjectType_400(t *testing.T) {
	t.Parallel()
	s := newACLTestServer(t)
	rules := []auth.ACLRule{{
		Pattern: ".*", SubjectType: "clan", Subject: "admin",
		Permissions: []string{"view"},
	}}
	body, _ := json.Marshal(map[string]any{"rules": rules})
	rec := callAdmin(s.handleReplaceACL, http.MethodPut, "/api/admin/acl", nil, body, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleReplaceACL_InvalidPattern_400 — a malformed regex pattern
// is rejected. The handler validates via validateRules which compiles
// the pattern.
func TestHandleReplaceACL_InvalidPattern_400(t *testing.T) {
	t.Parallel()
	s := newACLTestServer(t)
	rules := []auth.ACLRule{{
		Pattern: "([unclosed", SubjectType: "group", Subject: "admin",
		Permissions: []string{"view"},
	}}
	body, _ := json.Marshal(map[string]any{"rules": rules})
	rec := callAdmin(s.handleReplaceACL, http.MethodPut, "/api/admin/acl", nil, body, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleReplaceACL_DuplicateRules_400 — the store rejects two rules
// with the same (pattern, subject) since they'd co-fire ambiguously.
// The handler surfaces this as 400 rather than 500.
func TestHandleReplaceACL_DuplicateRules_400(t *testing.T) {
	t.Parallel()
	s := newACLTestServer(t)
	rules := []auth.ACLRule{
		{Pattern: ".*", SubjectType: "group", Subject: "admin", Permissions: []string{"view"}},
		{Pattern: ".*", SubjectType: "group", Subject: "admin", Permissions: []string{"edit"}},
	}
	body, _ := json.Marshal(map[string]any{"rules": rules})
	rec := callAdmin(s.handleReplaceACL, http.MethodPut, "/api/admin/acl", nil, body, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
