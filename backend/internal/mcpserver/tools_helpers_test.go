package mcpserver

import (
	"context"
	"testing"

	"gowiki/backend/internal/auth"
)

// pageFolderRoot is called before a row is inserted to decide which ACL
// namespace to check — the fixed prefix of the folder pattern, before any
// @field placeholder. Getting this wrong lets an insert bypass ACL by
// hiding target segments behind an @token that resolves to something the
// caller couldn't otherwise reach.
func TestPageFolderRoot(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/regulatory", "/regulatory"},
		{"/regulatory/qms", "/regulatory/qms"},
		{"/regulatory/qms/", "/regulatory/qms"},
		{"regulatory/qms", "/regulatory/qms"},
		{"/regulatory/@field", "/regulatory"},
		{"/regulatory/qms/@field", "/regulatory/qms"},
		{"/regulatory/qms/@field/child", "/regulatory/qms"},
		{"/deviations/@year/@id", "/deviations"},
		{"@field", "/"},
		{"/", "/"},
		{"", "/"},
	}
	for _, tc := range cases {
		if got := pageFolderRoot(tc.in); got != tc.want {
			t.Errorf("pageFolderRoot(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHasArg(t *testing.T) {
	args := map[string]any{
		"present":      "value",
		"empty_string": "",
		"nil_value":    nil,
		"zero_int":     0,
		"false_bool":   false,
		"empty_object": map[string]any{},
	}
	// hasArg only reports presence — the actual value does not matter.
	// This is what lets partial updates distinguish "keep it" (absent)
	// from "clear it" (present-but-empty).
	for _, name := range []string{"present", "empty_string", "nil_value", "zero_int", "false_bool", "empty_object"} {
		if !hasArg(args, name) {
			t.Errorf("hasArg(_, %q) = false, want true", name)
		}
	}
	if hasArg(args, "missing") {
		t.Errorf("hasArg(_, %q) = true, want false", "missing")
	}
}

// ─── Deps.isAdmin / canEdit / canView ────────────────────
//
// These enforce the dual-ACL model that every MCP write depends on. A
// regression here would let an agent slip past ACLs, so pin the shapes
// with real UserStore / ACLStore.

// depsForTest builds Deps with real Auth stores in a temp dir. The
// ExtractUsername closure reads from our test context key.
func depsForTest(t *testing.T) (Deps, func(username string) context.Context) {
	t.Helper()
	meta := t.TempDir()
	users, err := auth.NewUserStore(meta)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	acl, err := auth.NewACLStore(meta)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}
	// Seed users. The default bootstrap creates an admin, but we want
	// deterministic controlled fixtures.
	_ = users.Create(auth.User{Username: "admin_user", Groups: []string{"admin"}}, "pw")
	_ = users.Create(auth.User{Username: "editor_user", Groups: []string{"editors"}}, "pw")
	_ = users.Create(auth.User{Username: "reader_user", Groups: nil}, "pw")

	type ctxKey struct{ name string }
	userKey := ctxKey{"username"}
	deps := Deps{
		UserStore: users,
		ACL:       acl,
		ExtractUsername: func(ctx context.Context) string {
			if v, ok := ctx.Value(userKey).(string); ok {
				return v
			}
			return ""
		},
	}
	mkCtx := func(username string) context.Context {
		return context.WithValue(context.Background(), userKey, username)
	}
	return deps, mkCtx
}

func TestDeps_IsAdmin(t *testing.T) {
	deps, ctxFor := depsForTest(t)
	if !deps.isAdmin(ctxFor("admin_user")) {
		t.Errorf("isAdmin(admin_user) = false, want true")
	}
	if deps.isAdmin(ctxFor("editor_user")) {
		t.Errorf("isAdmin(editor_user) = true, want false")
	}
	if deps.isAdmin(ctxFor("")) {
		t.Errorf("isAdmin(anonymous) = true, want false")
	}
	if deps.isAdmin(ctxFor("nonexistent_user")) {
		t.Errorf("isAdmin(unknown) = true, want false")
	}
}

func TestDeps_IsAdmin_NoUserStore(t *testing.T) {
	// Without a UserStore wired, isAdmin must default to false — never
	// silently grant admin.
	deps := Deps{ExtractUsername: func(context.Context) string { return "anyone" }}
	if deps.isAdmin(context.Background()) {
		t.Errorf("isAdmin returned true with nil UserStore")
	}
}

func TestDeps_CanView_DefaultInstallRefusesEveryone(t *testing.T) {
	// The MCP layer requires @ai permission on every checked path in
	// addition to the caller's own. Default bootstrap grants nothing to
	// @ai, so every canView returns false — matches the "explicit opt-in"
	// invariant of the dual-ACL design.
	deps, ctxFor := depsForTest(t)
	for _, user := range []string{"admin_user", "editor_user", ""} {
		if deps.canView(ctxFor(user), "/anything") {
			t.Errorf("canView(%q) = true with no @ai rule (default install must refuse)", user)
		}
	}
}

func TestDeps_CanView_WithAIRuleGrants(t *testing.T) {
	// Seed an @ai rule and prove canView flips true for users who also
	// have the underlying permission.
	deps, ctxFor := depsForTest(t)
	seedAIRule(t, deps.ACL, "view")

	if !deps.canView(ctxFor("admin_user"), "/anything") {
		t.Errorf("admin+@ai granted view but canView returned false")
	}
	if !deps.canView(ctxFor("editor_user"), "/anything") {
		t.Errorf("editor+@ai granted view but canView returned false")
	}
}

func TestDeps_CanEdit_RequiresBothUserAndAI(t *testing.T) {
	deps, ctxFor := depsForTest(t)
	// With no @ai rule, even the admin is refused.
	if deps.canEdit(ctxFor("admin_user"), "/x") {
		t.Errorf("admin_user allowed edit without @ai rule")
	}

	// Grant @ai edit — admin and editor both pass.
	seedAIRule(t, deps.ACL, "edit")
	if !deps.canEdit(ctxFor("admin_user"), "/x") {
		t.Errorf("admin denied edit after @ai grant")
	}
	if !deps.canEdit(ctxFor("editor_user"), "/x") {
		t.Errorf("editor denied edit after @ai grant")
	}
	// Reader still refused — @ai alone isn't enough; the caller also needs
	// the underlying group grant.
	if deps.canEdit(ctxFor("reader_user"), "/x") {
		t.Errorf("reader_user granted edit — dual ACL leaked")
	}
}

func TestDeps_CanEdit_NoACLIsPermissive(t *testing.T) {
	// When ACL is nil (embedding that hasn't wired it), canEdit returns
	// true — matches the same convention as the other ACL helpers.
	deps := Deps{ExtractUsername: func(context.Context) string { return "x" }}
	if !deps.canEdit(context.Background(), "/x") {
		t.Errorf("canEdit denied with nil ACL — production installs would refuse every MCP write")
	}
}

func TestDeps_CanDelete_RequiresAllThree(t *testing.T) {
	deps, ctxFor := depsForTest(t)
	seedAIRule(t, deps.ACL, "delete")
	// Editor lacks delete on the underlying rule (default: admin only).
	if deps.canDelete(ctxFor("editor_user"), "/x") {
		t.Errorf("editor_user granted delete — dual ACL leaked")
	}
	if !deps.canDelete(ctxFor("admin_user"), "/x") {
		t.Errorf("admin denied delete after @ai grant")
	}
}

// seedAIRule appends a rule granting @ai the given permission on all paths.
// Fails the test on any error.
func seedAIRule(t *testing.T, acl *auth.ACLStore, permission string) {
	t.Helper()
	rules := acl.List()
	rules = append(rules, auth.ACLRule{Pattern: ".*", SubjectType: "special", Subject: "@ai", Permissions: []string{permission}})
	if err := acl.Replace(rules); err != nil {
		t.Fatalf("seed @ai rule: %v", err)
	}
}

func TestDeps_EffectiveGroups_UnionsLocalAndOAuth(t *testing.T) {
	deps, _ := depsForTest(t)
	// Create a user whose local group is [beta] and oauth groups are
	// [ops, sre]. EffectiveGroups should union both.
	_ = deps.UserStore.Create(auth.User{Username: "mixed", Groups: []string{"beta"}, OAuthGroups: []string{"ops", "sre"}}, "pw")

	got := deps.effectiveGroups("mixed")
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, want := range []string{"beta", "ops", "sre"} {
		if !seen[want] {
			t.Errorf("effectiveGroups missing %q: got %v", want, got)
		}
	}
}
