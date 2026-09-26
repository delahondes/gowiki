package todo

import (
	"strings"
	"testing"
)

// ExtractTodoDirectives is the {todo ...} directive parser. It scans a
// markdown blob line-by-line and produces one ParsedDirective per match.
// The kvRe regex must recognise bare and single- and double-quoted values.

func TestExtractTodoDirectives_Basic(t *testing.T) {
	t.Parallel()
	md := "{todo title=\"Ship RC\" assign=alice due=2026-12-01 priority=high}\n"
	got := ExtractTodoDirectives(md)
	if len(got) != 1 {
		t.Fatalf("got %d directives, want 1", len(got))
	}
	d := got[0]
	if d.Title != "Ship RC" {
		t.Errorf("Title = %q, want %q", d.Title, "Ship RC")
	}
	if d.Assign != "alice" {
		t.Errorf("Assign = %q, want alice", d.Assign)
	}
	if d.Due != "2026-12-01" {
		t.Errorf("Due = %q, want 2026-12-01", d.Due)
	}
	if d.Priority != "high" {
		t.Errorf("Priority = %q, want high", d.Priority)
	}
	if d.NodeKey == "" {
		t.Errorf("NodeKey should be set")
	}
}

func TestExtractTodoDirectives_NoMatch(t *testing.T) {
	t.Parallel()
	// {todo ...} with no space after "todo" is not a directive per todoDirectiveRe.
	md := "regular content\n{todo}\ndone.\n"
	if got := ExtractTodoDirectives(md); len(got) != 0 {
		t.Errorf("got %d directives on non-matching input, want 0", len(got))
	}
}

func TestExtractTodoDirectives_Multiple(t *testing.T) {
	t.Parallel()
	md := "" +
		"{todo title=One assign=alice}\n" +
		"body\n" +
		"{todo title=Two assign=bob}\n"
	got := ExtractTodoDirectives(md)
	if len(got) != 2 {
		t.Fatalf("got %d directives, want 2", len(got))
	}
	if got[0].Title != "One" || got[1].Title != "Two" {
		t.Errorf("titles = %q, %q", got[0].Title, got[1].Title)
	}
	if got[0].NodeKey == got[1].NodeKey {
		t.Errorf("distinct titles should produce distinct NodeKeys")
	}
}

func TestExtractTodoDirectives_QuotedValueWithSpaces(t *testing.T) {
	t.Parallel()
	md := `{todo title="Multi word title" assign="group:admin,alice" description='also has "nested" quotes'}` + "\n"
	got := ExtractTodoDirectives(md)
	if len(got) != 1 {
		t.Fatalf("got %d directives, want 1", len(got))
	}
	if got[0].Title != "Multi word title" {
		t.Errorf("Title = %q", got[0].Title)
	}
	if got[0].Assign != "group:admin,alice" {
		t.Errorf("Assign = %q", got[0].Assign)
	}
	if !strings.Contains(got[0].Description, "nested") {
		t.Errorf("Description = %q (should preserve inner content)", got[0].Description)
	}
}

func TestExtractTodoDirectives_AllAttrs(t *testing.T) {
	t.Parallel()
	md := `{todo title=X assign=alice resolution=all due=2026-01-01 recur=weekly priority=urgent action=read:/docs tags=onboarding description=intro}` + "\n"
	got := ExtractTodoDirectives(md)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	d := got[0]
	if d.Resolution != "all" {
		t.Errorf("Resolution = %q", d.Resolution)
	}
	if d.Recur != "weekly" {
		t.Errorf("Recur = %q", d.Recur)
	}
	if d.Action != "read:/docs" {
		t.Errorf("Action = %q", d.Action)
	}
	if d.Tags != "onboarding" {
		t.Errorf("Tags = %q", d.Tags)
	}
	if d.Description != "intro" {
		t.Errorf("Description = %q", d.Description)
	}
}

func TestExtractTodoDirectives_UnknownAttrsIgnored(t *testing.T) {
	t.Parallel()
	md := `{todo title=X mystery=42 assign=alice}` + "\n"
	got := ExtractTodoDirectives(md)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	// Unknown keys are silently dropped — the ParsedDirective struct has
	// no field for them and the parser doesn't warn. Known attrs still work.
	if got[0].Title != "X" || got[0].Assign != "alice" {
		t.Errorf("known attrs lost when unknown attr present: %+v", got[0])
	}
}

func TestParseRecur_Presets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want Recurrence
	}{
		{"daily", Recurrence{Type: "calendar", Every: 1, Unit: "day"}},
		{"weekly", Recurrence{Type: "calendar", Every: 1, Unit: "week"}},
		{"monthly", Recurrence{Type: "calendar", Every: 1, Unit: "month"}},
		{"yearly", Recurrence{Type: "calendar", Every: 1, Unit: "year"}},
		{"DAILY", Recurrence{Type: "calendar", Every: 1, Unit: "day"}}, // case-insensitive
	}
	for _, tc := range cases {
		got := parseRecur(tc.in)
		if got != tc.want {
			t.Errorf("parseRecur(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseRecur_DelayAndCounted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want Recurrence
	}{
		{"3d", Recurrence{Type: "delay", Days: 3}},
		{"14d", Recurrence{Type: "delay", Days: 14}},
		{"2weeks", Recurrence{Type: "calendar", Every: 2, Unit: "week"}},
		{"6months", Recurrence{Type: "calendar", Every: 6, Unit: "month"}},
		{"5years", Recurrence{Type: "calendar", Every: 5, Unit: "year"}},
		{"10days", Recurrence{Type: "calendar", Every: 10, Unit: "day"}},
		{"", Recurrence{}},
		{"bogus", Recurrence{}},
		{"0d", Recurrence{}}, // parseInt("0")=0 which is > 0 false — rejected
	}
	for _, tc := range cases {
		got := parseRecur(tc.in)
		if got != tc.want {
			t.Errorf("parseRecur(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestParseAction_Shapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want WikiAction
	}{
		{"read:/docs/policy", WikiAction{Type: "read", Page: "/docs/policy"}},
		{"edit:/docs/index", WikiAction{Type: "edit", Page: "/docs/index"}},
		{"create:/reports/.*", WikiAction{Type: "create", Pattern: "/reports/.*"}},
		{"set_meta:/x:schema:field:value", WikiAction{Type: "set_meta", Page: "/x", Schema: "schema", Field: "field", Value: "value"}},
		{"", WikiAction{}},
		{"noparts", WikiAction{}},                  // no colon
		{"unknown:/foo", WikiAction{}},             // unknown action type
		{"set_meta:/x:schema:field", WikiAction{}}, // too few colons
		{"read:/docs/trailing/", WikiAction{Type: "read", Page: "/docs/trailing"}}, // trailing slash trimmed
	}
	for _, tc := range cases {
		got := parseAction(tc.in)
		if got != tc.want {
			t.Errorf("parseAction(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestResolveActionPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sourcePage, actionPath, want string
	}{
		{"/docs/team", "/absolute", "/absolute"},
		{"/docs/team", ".", "/docs/team"},
		{"docs/team", ".", "/docs/team"}, // source-page missing leading slash
		{"/docs/team", "../sibling", "/sibling"},
		{"/docs/team/deep", "up", "/docs/team/up"},
		{"/docs/team", "", ""},
	}
	for _, tc := range cases {
		got := resolveActionPath(tc.sourcePage, tc.actionPath)
		if got != tc.want {
			t.Errorf("resolveActionPath(%q, %q) = %q, want %q",
				tc.sourcePage, tc.actionPath, got, tc.want)
		}
	}
}

func TestComputeNodeKey_StableAndDistinct(t *testing.T) {
	t.Parallel()
	a := computeNodeKey("/docs/team", "Ship", "alice")
	b := computeNodeKey("/docs/team", "Ship", "alice")
	if a != b {
		t.Errorf("computeNodeKey not stable across calls: %q vs %q", a, b)
	}
	c := computeNodeKey("/docs/team", "Ship", "bob")
	if a == c {
		t.Errorf("different assignee should produce different NodeKey")
	}
	d := computeNodeKey("/docs/other", "Ship", "alice")
	if a == d {
		t.Errorf("different page should produce different NodeKey")
	}
}
