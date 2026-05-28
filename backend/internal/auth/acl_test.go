package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewACLStore_Bootstrap(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	rules := store.List()
	if len(rules) != 3 {
		t.Fatalf("expected 3 bootstrap rules, got %d", len(rules))
	}

	// Verify the bootstrap file was created.
	data, err := os.ReadFile(filepath.Join(dir, "acl.json"))
	if err != nil {
		t.Fatalf("read acl.json: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("acl.json is empty")
	}
}

func TestNewACLStore_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	content := `[{"pattern":"wiki/.*","subject_type":"group","subject":"editors","permissions":["view","edit"]}]`
	if err := os.WriteFile(filepath.Join(dir, "acl.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}
	rules := store.List()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Pattern != "wiki/.*" {
		t.Fatalf("unexpected pattern: %s", rules[0].Pattern)
	}
}

func TestNewACLStore_TolerantOfDuplicatesOnLoad(t *testing.T) {
	dir := t.TempDir()
	// A duplicate (pattern, subject) on disk must not brick startup: it is
	// deduped, keeping the first occurrence. (Replace rejects it; load forgives.)
	content := `[
		{"pattern":"/dataset/.*","subject_type":"group","subject":"bioit","permissions":["view","edit","delete"]},
		{"pattern":"/dataset/.*","subject_type":"group","subject":"bioit","permissions":["view","edit","delete"]}
	]`
	if err := os.WriteFile(filepath.Join(dir, "acl.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore should tolerate a duplicate on disk, got: %v", err)
	}
	if rules := store.List(); len(rules) != 1 {
		t.Fatalf("expected 1 rule after dedupe, got %d", len(rules))
	}
}

func TestCheckAIPermission_IgnoresAllBaseline(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Mirrors the production shape that broke AI access to /regulatory:
	// a broad @ai grant, plus an @all deny on a more specific namespace.
	err = store.Replace([]ACLRule{
		{Pattern: "/.*", SubjectType: "special", Subject: "@ai", Permissions: []string{"view", "edit"}},
		{Pattern: "/regulatory/.*", SubjectType: "special", Subject: "@all", Permissions: []string{}},
		{Pattern: "/bioit/.*", SubjectType: "special", Subject: "@ai", Permissions: []string{}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// The @all deny on /regulatory must NOT touch the AI axis, even though it is
	// a longer (more specific) pattern than the /.* @ai grant.
	if !store.CheckAIPermission("/regulatory/qms/soft/sop01/tpl02", "view") {
		t.Error("@all deny must not strip AI view on /regulatory")
	}
	// Sibling pages under the same namespace must evaluate identically.
	if !store.CheckAIPermission("/regulatory/qms/soft/sop01/tpl04", "view") {
		t.Error("sibling page must evaluate the same as its neighbor")
	}

	// An @ai-specific deny still restricts the AI — that's the supported way.
	if store.CheckAIPermission("/bioit/pipeline", "view") {
		t.Error("@ai deny on /bioit must still block the AI")
	}

	// No @ai delete grant anywhere → AI cannot delete (deny by default).
	if store.CheckAIPermission("/regulatory/qms/soft/sop01/tpl02", "delete") {
		t.Error("AI should have no delete without an @ai delete grant")
	}

	// Regular CheckPermission for @all is unchanged: anonymous denied on /regulatory.
	if store.CheckPermission("", nil, "/regulatory/qms/soft/sop01/tpl02", "view") {
		t.Error("@all deny should still block anonymous human view")
	}
}

func TestNewACLStore_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	// Write invalid JSON.
	if err := os.WriteFile(filepath.Join(dir, "acl.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewACLStore(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewACLStore_InvalidRulesInFile(t *testing.T) {
	dir := t.TempDir()
	// Valid JSON but invalid subject_type.
	content := `[{"pattern":".*","subject_type":"bogus","subject":"x","permissions":["view"]}]`
	if err := os.WriteFile(filepath.Join(dir, "acl.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewACLStore(dir)
	if err == nil {
		t.Fatal("expected error for invalid subject_type")
	}
}

func TestACLStore_Replace(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	newRules := []ACLRule{
		{Pattern: "public/.*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
		{Pattern: ".*", SubjectType: "special", Subject: "@authenticated", Permissions: []string{"view", "edit"}},
	}
	if err := store.Replace(newRules); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	rules := store.List()
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	// Reload from disk to verify persistence.
	store2, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	rules2 := store2.List()
	if len(rules2) != 2 {
		t.Fatalf("persisted rules: expected 2, got %d", len(rules2))
	}
}

func TestACLStore_Replace_ValidationErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	tests := []struct {
		name  string
		rules []ACLRule
	}{
		{
			name:  "invalid regexp",
			rules: []ACLRule{{Pattern: "[invalid", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}}},
		},
		{
			name:  "invalid subject_type",
			rules: []ACLRule{{Pattern: ".*", SubjectType: "role", Subject: "admin", Permissions: []string{"view"}}},
		},
		{
			name:  "invalid special subject",
			rules: []ACLRule{{Pattern: ".*", SubjectType: "special", Subject: "@nobody", Permissions: []string{"view"}}},
		},
		{
			name:  "invalid permission",
			rules: []ACLRule{{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{"fly"}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := store.Replace(tc.rules); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	// Verify original rules are unchanged after failed Replace attempts.
	rules := store.List()
	if len(rules) != 3 {
		t.Fatalf("expected original 3 rules after failed replace, got %d", len(rules))
	}
}

func TestCheckPermission_AllowAll(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}
	// Bootstrap rules: @all can view, editors can view+edit, admin can view+edit+delete.

	// Unauthenticated user can view.
	if !store.CheckPermission("", nil, "some/page", "view") {
		t.Error("@all should allow view for unauthenticated")
	}

	// Unauthenticated user cannot edit.
	if store.CheckPermission("", nil, "some/page", "edit") {
		t.Error("@all should not allow edit for unauthenticated")
	}
}

func TestCheckPermission_GroupMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// User in "editors" group should be able to edit.
	if !store.CheckPermission("alice", []string{"editors"}, "any/path", "edit") {
		t.Error("editors group should allow edit")
	}

	// User in "editors" group should be able to view.
	if !store.CheckPermission("alice", []string{"editors"}, "any/path", "view") {
		t.Error("editors group should allow view")
	}

	// User in "editors" group should not be able to delete.
	if store.CheckPermission("alice", []string{"editors"}, "any/path", "delete") {
		t.Error("editors group should not allow delete")
	}
}

func TestCheckPermission_AdminGroup(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// User in "admin" group should be able to do everything.
	for _, action := range []string{"view", "edit", "delete"} {
		if !store.CheckPermission("root", []string{"admin"}, "any/path", action) {
			t.Errorf("admin group should allow %s", action)
		}
	}
}

func TestCheckPermission_SpecificPattern(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Replace with rules where a specific pattern overrides the general one.
	err = store.Replace([]ACLRule{
		{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
		{Pattern: "secret/.*", SubjectType: "group", Subject: "admin", Permissions: []string{"view", "edit", "delete"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Unauthenticated can view public pages.
	if !store.CheckPermission("", nil, "public/page", "view") {
		t.Error("@all should allow view for public pages")
	}

	// Unauthenticated can also view secret pages because @all matches them via ".*".
	// The spec says: collect matching rules (path + subject), then pick most specific.
	// For an unauthenticated user, only @all at ".*" matches. The admin rule at "secret/.*"
	// does not match because the user is not in the admin group. So the most specific
	// matching rule is @all at ".*", which grants "view".
	if !store.CheckPermission("", nil, "secret/page", "view") {
		t.Error("@all should still allow view for secret pages (no matching deny rule)")
	}

	// To truly restrict secret pages, you must NOT have @all at ".*" or add
	// a more specific @all deny. Test that pattern:
	err = store.Replace([]ACLRule{
		{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
		{Pattern: "secret/.*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
		{Pattern: "secret/.*", SubjectType: "group", Subject: "admin", Permissions: []string{"view", "edit", "delete"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Now the most specific level for "secret/page" is "secret/.*" (len 9).
	// At that level, both @all and admin group match. Union gives view + edit + delete.
	// Unauthenticated user (matched by @all) gets "view" from the @all rule at "secret/.*".
	if !store.CheckPermission("", nil, "secret/page", "view") {
		t.Error("@all at secret/.* should grant view")
	}
	// Unauthenticated should NOT get edit (only admin group has it at this level).
	if store.CheckPermission("", nil, "secret/page", "edit") {
		t.Error("unauthenticated should not get edit even at specific level")
	}

	// Admin can view and edit secret pages.
	if !store.CheckPermission("root", []string{"admin"}, "secret/page", "view") {
		t.Error("admin should be able to view secret pages")
	}
	if !store.CheckPermission("root", []string{"admin"}, "secret/page", "edit") {
		t.Error("admin should be able to edit secret pages")
	}
}

func TestCheckPermission_UserMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	err = store.Replace([]ACLRule{
		{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
		{Pattern: "user/alice/.*", SubjectType: "user", Subject: "alice", Permissions: []string{"view", "edit"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Alice can edit her own pages.
	if !store.CheckPermission("alice", nil, "user/alice/notes", "edit") {
		t.Error("alice should be able to edit her pages")
	}

	// Bob cannot edit alice's pages.
	if store.CheckPermission("bob", nil, "user/alice/notes", "edit") {
		t.Error("bob should not be able to edit alice's pages")
	}

	// Bob can still view public pages.
	if !store.CheckPermission("bob", nil, "public/page", "view") {
		t.Error("bob should be able to view public pages via @all")
	}
}

func TestCheckPermission_AuthenticatedSpecial(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	err = store.Replace([]ACLRule{
		{Pattern: ".*", SubjectType: "special", Subject: "@authenticated", Permissions: []string{"view", "edit"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Authenticated user can view.
	if !store.CheckPermission("alice", nil, "any/page", "view") {
		t.Error("@authenticated should match logged-in user for view")
	}

	// Authenticated user can edit.
	if !store.CheckPermission("alice", nil, "any/page", "edit") {
		t.Error("@authenticated should match logged-in user for edit")
	}

	// Unauthenticated user cannot view.
	if store.CheckPermission("", nil, "any/page", "view") {
		t.Error("@authenticated should not match unauthenticated user")
	}
}

func TestCheckPermission_NoRulesMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Replace with a narrow rule that won't match.
	err = store.Replace([]ACLRule{
		{Pattern: "specific/page", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// A different path should be denied.
	if store.CheckPermission("", nil, "other/page", "view") {
		t.Error("should deny when no rules match")
	}
}

func TestCheckPermission_UnionAtSameSpecificity(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Two rules with the same pattern length that both match.
	err = store.Replace([]ACLRule{
		{Pattern: "docs/.*", SubjectType: "group", Subject: "readers", Permissions: []string{"view"}},
		{Pattern: "docs/.*", SubjectType: "group", Subject: "writers", Permissions: []string{"edit"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// A user in both groups should get the union: view + edit.
	if !store.CheckPermission("alice", []string{"readers", "writers"}, "docs/guide", "view") {
		t.Error("should have view from readers group")
	}
	if !store.CheckPermission("alice", []string{"readers", "writers"}, "docs/guide", "edit") {
		t.Error("should have edit from writers group")
	}
}

func TestCheckPermission_PatternAnchoring(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	err = store.Replace([]ACLRule{
		{Pattern: "secret", SubjectType: "group", Subject: "admin", Permissions: []string{"view"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// "secret" should match exactly "secret" but not "secret/sub".
	if !store.CheckPermission("root", []string{"admin"}, "secret", "view") {
		t.Error("exact pattern should match exact path")
	}
	if store.CheckPermission("root", []string{"admin"}, "secret/sub", "view") {
		t.Error("exact pattern should not match sub-path")
	}
	if store.CheckPermission("root", []string{"admin"}, "notsecret", "view") {
		t.Error("exact pattern should not match different prefix")
	}
}

func TestValidateRules(t *testing.T) {
	// Valid rule set.
	valid := []ACLRule{
		{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
		{Pattern: "admin/.*", SubjectType: "group", Subject: "admin", Permissions: []string{"view", "edit", "delete"}},
		{Pattern: "user/alice/.*", SubjectType: "user", Subject: "alice", Permissions: []string{"view", "edit"}},
		{Pattern: ".*", SubjectType: "special", Subject: "@authenticated", Permissions: []string{"view", "edit"}},
	}
	if err := validateRules(valid); err != nil {
		t.Fatalf("expected valid rules to pass: %v", err)
	}
}

func TestCheckPermission_SelfRules(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Simulate DokuWiki %USER% rules: each user can edit pages under their own namespace.
	err = store.Replace([]ACLRule{
		{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
		{Pattern: "staff/@self/.*", SubjectType: "special", Subject: "@self", Permissions: []string{"view", "edit", "delete"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Alice can edit her own pages.
	if !store.CheckPermission("alice", nil, "staff/alice/notes", "edit") {
		t.Error("alice should be able to edit staff/alice/notes via @self")
	}
	if !store.CheckPermission("alice", nil, "staff/alice/notes", "delete") {
		t.Error("alice should be able to delete staff/alice/notes via @self")
	}

	// Alice cannot edit bob's pages.
	if store.CheckPermission("alice", nil, "staff/bob/notes", "edit") {
		t.Error("alice should not be able to edit staff/bob/notes")
	}

	// Bob can edit his own pages.
	if !store.CheckPermission("bob", nil, "staff/bob/notes", "edit") {
		t.Error("bob should be able to edit staff/bob/notes via @self")
	}

	// Unauthenticated user cannot match @self rules.
	if store.CheckPermission("", nil, "staff/alice/notes", "edit") {
		t.Error("unauthenticated should not match @self rules")
	}

	// Everyone can view via @all.
	if !store.CheckPermission("", nil, "public/page", "view") {
		t.Error("@all should allow view for public pages")
	}
}

func TestCheckPermission_SelfRulesWithWildcard(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Pattern like DokuWiki's %USER%-* (user-prefixed pages).
	err = store.Replace([]ACLRule{
		{Pattern: "interviews/@self-.*", SubjectType: "special", Subject: "@self", Permissions: []string{"view", "edit"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Alice can edit her interview pages.
	if !store.CheckPermission("alice", nil, "interviews/alice-2024", "edit") {
		t.Error("alice should be able to edit interviews/alice-2024 via @self")
	}

	// Alice cannot edit bob's interview pages.
	if store.CheckPermission("alice", nil, "interviews/bob-2024", "edit") {
		t.Error("alice should not be able to edit interviews/bob-2024")
	}
}

func TestCheckPermission_SelfRulesRegexEscaping(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// A username with regex metacharacters should be escaped.
	err = store.Replace([]ACLRule{
		{Pattern: "staff/@self/.*", SubjectType: "special", Subject: "@self", Permissions: []string{"view", "edit"}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// User "a.b" should only match literally, not "axb".
	if !store.CheckPermission("a.b", nil, "staff/a.b/notes", "edit") {
		t.Error("a.b should match staff/a.b/notes")
	}
	if store.CheckPermission("a.b", nil, "staff/axb/notes", "edit") {
		t.Error("a.b should not match staff/axb/notes (dot should be escaped)")
	}
}

func TestCheckPermission_LeadingSlashRequired(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Patterns must use leading "/" to match paths (which always have leading "/").
	err = store.Replace([]ACLRule{
		{Pattern: "/regulatory/.*", SubjectType: "group", Subject: "regulatory", Permissions: []string{"view", "edit", "delete"}},
		{Pattern: "/wiki/.*", SubjectType: "group", Subject: "docs", Permissions: []string{"view", "edit"}},
		{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Path with leading slash matches pattern with leading slash.
	if !store.CheckPermission("alice", []string{"regulatory"}, "/regulatory/qms/dir/mq01", "edit") {
		t.Error("path /regulatory/... should match pattern /regulatory/.*")
	}

	if !store.CheckPermission("bob", []string{"docs"}, "/wiki/manual", "edit") {
		t.Error("path /wiki/... should match pattern /wiki/.*")
	}

	// Pattern WITHOUT leading slash does NOT match paths with leading slash.
	err = store.Replace([]ACLRule{
		{Pattern: "regulatory/.*", SubjectType: "group", Subject: "regulatory", Permissions: []string{"view", "edit"}},
		{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if store.CheckPermission("alice", []string{"regulatory"}, "/regulatory/qms/dir/mq01", "edit") {
		t.Error("pattern without leading / should NOT match path with leading /")
	}
}

func TestValidateRules_SelfPattern(t *testing.T) {
	// @self rules with @self in pattern should pass validation.
	rules := []ACLRule{
		{Pattern: "staff/@self/.*", SubjectType: "special", Subject: "@self", Permissions: []string{"view"}},
	}
	if err := validateRules(rules); err != nil {
		t.Fatalf("expected @self rule to be valid: %v", err)
	}
}

func TestList_ReturnsCopy(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	rules := store.List()
	// Mutate the returned slice.
	rules[0].Pattern = "modified"

	// Original should be unaffected.
	original := store.List()
	if original[0].Pattern == "modified" {
		t.Error("List should return a copy, not a reference to internal state")
	}
}

func TestValidateRules_RejectsDuplicateSubjectPattern(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Two rules with the same pattern and the same subject but contradictory
	// permissions — the exact shape that silently granted anonymous view on
	// regulatory pages. Replace must reject it.
	err = store.Replace([]ACLRule{
		{Pattern: "regulatory/.*", SubjectType: "special", Subject: "@all", Permissions: []string{}},
		{Pattern: "regulatory/.*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
	})
	if err == nil {
		t.Fatal("expected Replace to reject duplicate (pattern, subject)")
	}

	// Same pattern but different subjects is fine — that's intended layering.
	err = store.Replace([]ACLRule{
		{Pattern: "regulatory/.*", SubjectType: "special", Subject: "@all", Permissions: []string{}},
		{Pattern: "regulatory/.*", SubjectType: "group", Subject: "editors", Permissions: []string{"view", "edit"}},
	})
	if err != nil {
		t.Fatalf("same pattern with distinct subjects should be allowed: %v", err)
	}
}

func TestReplace_SortsRulesForReadability(t *testing.T) {
	dir := t.TempDir()
	store, err := NewACLStore(dir)
	if err != nil {
		t.Fatalf("NewACLStore: %v", err)
	}

	// Deliberately interleaved namespaces with the catch-all in the middle.
	err = store.Replace([]ACLRule{
		{Pattern: "regulatory/qms/.*", SubjectType: "group", Subject: "editors", Permissions: []string{"view"}},
		{Pattern: "bioit/.*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
		{Pattern: ".*", SubjectType: "group", Subject: "admin", Permissions: []string{"view", "edit", "delete"}},
		{Pattern: "regulatory/.*", SubjectType: "special", Subject: "@all", Permissions: []string{}},
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	got := store.List()
	want := []string{"bioit/.*", "regulatory/.*", "regulatory/qms/.*", ".*"}
	if len(got) != len(want) {
		t.Fatalf("got %d rules, want %d", len(got), len(want))
	}
	for i, p := range want {
		if got[i].Pattern != p {
			t.Errorf("rule %d: got pattern %q, want %q", i, got[i].Pattern, p)
		}
	}
}

func TestLiteralPrefix(t *testing.T) {
	cases := map[string]string{
		"regulatory/.*":        "regulatory/",
		"regulatory/qms/sop01": "regulatory/qms/sop01",
		".*":                   "",
		"":                     "",
		"a|b":                  "a",
	}
	for pattern, want := range cases {
		if got := literalPrefix(pattern); got != want {
			t.Errorf("literalPrefix(%q) = %q, want %q", pattern, got, want)
		}
	}
}
