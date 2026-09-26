package todo

import (
	"testing"
)

// Pure helpers on model.go: IsGroupTarget, CleanTargetName, SplitTargets,
// TargetContains, ResolveAllMembers, BuildTargetsArray, Patch.IsEmpty,
// Recurrence.IsZero, WikiAction.IsZero.

func TestIsGroupTarget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"group:admin", true},
		{"admin", false},
		{"", false},
		{"group:", true},    // prefix alone matches — pin observed behaviour
		{"@group:x", false}, // leading @ is not stripped by IsGroupTarget itself
	}
	for _, tc := range cases {
		if got := IsGroupTarget(tc.in); got != tc.want {
			t.Errorf("IsGroupTarget(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCleanTargetName(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"group:admin", "admin"},
		{"admin", "admin"},
		{"", ""},
		{"group:", ""},
	}
	for _, tc := range cases {
		if got := CleanTargetName(tc.in); got != tc.want {
			t.Errorf("CleanTargetName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitTargets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"alice", []string{"alice"}},
		{"alice,bob", []string{"alice", "bob"}},
		{"alice, bob , charlie", []string{"alice", "bob", "charlie"}}, // whitespace trimmed
		{"a,,b", []string{"a", "b"}},                                  // empty parts dropped
		{"group:admin,alice", []string{"group:admin", "alice"}},
	}
	for _, tc := range cases {
		got := SplitTargets(tc.in)
		if !equalSlices(got, tc.want) {
			t.Errorf("SplitTargets(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestTargetContains(t *testing.T) {
	t.Parallel()
	// TargetContains matches BOTH the raw target and the cleaned name.
	if !TargetContains("group:admin,alice", "alice") {
		t.Errorf("should find alice in comma-list")
	}
	if !TargetContains("group:admin", "admin") {
		t.Errorf("should match cleaned group name")
	}
	if !TargetContains("group:admin", "group:admin") {
		t.Errorf("should match raw prefixed target")
	}
	if TargetContains("alice,bob", "charlie") {
		t.Errorf("should not match absent value")
	}
	if TargetContains("", "alice") {
		t.Errorf("empty target should not contain anything")
	}
}

// stubResolver implements GroupResolver by looking up a static map.
type stubResolver struct {
	groups map[string][]string
}

func (s stubResolver) GroupMembers(name string) []string {
	return s.groups[name]
}

func TestResolveAllMembers_ExplicitGroupPrefix(t *testing.T) {
	t.Parallel()
	r := stubResolver{groups: map[string][]string{
		"admin":   {"alice", "bob"},
		"editors": {"bob", "carol"},
	}}
	got := ResolveAllMembers("group:admin,group:editors", r)
	want := []string{"alice", "bob", "carol"} // dedup preserved
	if !equalSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveAllMembers_BareIsAutoDetected(t *testing.T) {
	t.Parallel()
	r := stubResolver{groups: map[string][]string{
		"managers": {"mgr1", "mgr2"},
	}}
	// "managers" is auto-detected as a group (resolver returns members).
	// "alice" is not in the resolver so it's kept as a direct user.
	got := ResolveAllMembers("managers,alice", r)
	want := []string{"mgr1", "mgr2", "alice"}
	if !equalSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveAllMembers_Empty(t *testing.T) {
	t.Parallel()
	r := stubResolver{}
	if got := ResolveAllMembers("", r); got != nil {
		t.Errorf("empty target should return nil, got %v", got)
	}
}

func TestBuildTargetsArray(t *testing.T) {
	t.Parallel()
	got := BuildTargetsArray("alice", []string{"editors", "admin"})
	// User first, then each group in both bare and group:-prefixed form.
	want := []string{"alice", "editors", "group:editors", "admin", "group:admin"}
	if !equalSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildTargetsArray_NoGroups(t *testing.T) {
	t.Parallel()
	got := BuildTargetsArray("alice", nil)
	if len(got) != 1 || got[0] != "alice" {
		t.Errorf("got %v, want [alice]", got)
	}
}

func TestRecurrence_IsZero(t *testing.T) {
	t.Parallel()
	if !(Recurrence{}).IsZero() {
		t.Errorf("empty Recurrence should be zero")
	}
	if (Recurrence{Type: "delay", Days: 3}).IsZero() {
		t.Errorf("populated Recurrence should not be zero")
	}
}

func TestWikiAction_IsZero(t *testing.T) {
	t.Parallel()
	if !(WikiAction{}).IsZero() {
		t.Errorf("empty WikiAction should be zero")
	}
	if (WikiAction{Type: "read", Page: "/x"}).IsZero() {
		t.Errorf("populated WikiAction should not be zero")
	}
}

func TestPatch_IsEmpty(t *testing.T) {
	t.Parallel()
	if !(Patch{}).IsEmpty() {
		t.Errorf("empty Patch should be empty")
	}
	title := "new"
	if (Patch{Title: &title}).IsEmpty() {
		t.Errorf("Patch with Title should not be empty")
	}
	status := StatusDone
	if (Patch{Status: &status}).IsEmpty() {
		t.Errorf("Patch with Status should not be empty")
	}
}

// equalSlices is a small helper used across model tests.
func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
