package lifecycle

import (
	"strings"
	"testing"
	"time"
)

// happy-path minimum: a rule with scope + when + title + assign parses
// cleanly and yields a stable ID.
func TestParse_Happy(t *testing.T) {
	t.Parallel()
	src := `{lifecycle scope="^/qms/.*" when=stale:30m title="Review {{path}}" assign=alice}` + "\n"
	rules, errs := Parse("/admin/policies", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	if len(rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(rules))
	}
	r := rules[0]
	if r.ScopeRegex != "^/qms/.*" || r.Condition.Kind != "stale" {
		t.Errorf("scope/condition wrong: %+v", r)
	}
	if r.Condition.Duration != 30*30*24*time.Hour {
		t.Errorf("30m should be 30*30 days, got %v", r.Condition.Duration)
	}
	if r.SourcePage != "/admin/policies" {
		t.Errorf("SourcePage lost: %q", r.SourcePage)
	}
	if r.ID == "" {
		t.Errorf("ID must be populated")
	}
}

// Same directive body → same ID across parses. Deterministic hashing is
// what makes the scanner's node_keys stable.
func TestParse_IDIsDeterministic(t *testing.T) {
	t.Parallel()
	src := `{lifecycle scope="^/x/.*" when=stale:1y title=t assign=u}` + "\n"
	a, _ := Parse("/p", src)
	b, _ := Parse("/p", src)
	if len(a) != 1 || len(b) != 1 || a[0].ID != b[0].ID {
		t.Errorf("IDs should match across parses: %q vs %q", a[0].ID, b[0].ID)
	}
}

// Same body on a DIFFERENT source page → different ID. Prevents todo
// fanout collisions when a template stamp copies a rule to a new page.
func TestParse_IDDependsOnSourcePage(t *testing.T) {
	t.Parallel()
	src := `{lifecycle scope="^/x/.*" when=stale:1y title=t assign=u}` + "\n"
	a, _ := Parse("/p1", src)
	b, _ := Parse("/p2", src)
	if a[0].ID == b[0].ID {
		t.Errorf("same body on different source should yield different IDs")
	}
}

func TestParse_TagsAndExclude(t *testing.T) {
	t.Parallel()
	src := `{lifecycle tags="sop,rec,tpl" exclude_tags="archived,draft" when=stale:30d title=t assign=u}` + "\n"
	rules, errs := Parse("/x", src)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	r := rules[0]
	// splitCSV + sort — order should be deterministic.
	if strings.Join(r.Tags, ",") != "rec,sop,tpl" {
		t.Errorf("Tags sorted wrong: %v", r.Tags)
	}
	if strings.Join(r.ExcludeTags, ",") != "archived,draft" {
		t.Errorf("ExcludeTags sorted wrong: %v", r.ExcludeTags)
	}
}

func TestParse_RefusesUnknownAttribute(t *testing.T) {
	t.Parallel()
	// `scop=` typo — should be a hard error, otherwise the rule fires
	// against the whole wiki because the scope was silently dropped.
	src := `{lifecycle scop="/x/.*" when=stale:30d title=t assign=u}` + "\n"
	rules, errs := Parse("/x", src)
	if len(rules) != 0 || len(errs) == 0 {
		t.Errorf("typo should be refused, got rules=%v errs=%v", rules, errs)
	}
}

func TestParse_RequiresPositiveSelector(t *testing.T) {
	t.Parallel()
	// Only exclude_tags → refused (would nag the whole wiki).
	src := `{lifecycle exclude_tags="archived" when=stale:30d title=t assign=u}` + "\n"
	rules, errs := Parse("/x", src)
	if len(rules) != 0 || len(errs) == 0 {
		t.Errorf("exclude_tags-only rule should be refused")
	}
}

func TestParse_RequiresConditionAndTemplate(t *testing.T) {
	t.Parallel()
	cases := []string{
		// missing when=
		`{lifecycle scope="/x/.*" title=t assign=u}`,
		// missing title=
		`{lifecycle scope="/x/.*" when=stale:30d assign=u}`,
		// missing assign=
		`{lifecycle scope="/x/.*" when=stale:30d title=t}`,
	}
	for _, src := range cases {
		rules, errs := Parse("/x", src+"\n")
		if len(rules) != 0 || len(errs) == 0 {
			t.Errorf("expected refusal for %q, got rules=%v errs=%v", src, rules, errs)
		}
	}
}

func TestParse_InvalidRegex(t *testing.T) {
	t.Parallel()
	// Unbalanced regex — parse-time error, not scanner-time surprise.
	src := `{lifecycle scope="[unbalanced" when=stale:30d title=t assign=u}` + "\n"
	rules, errs := Parse("/x", src)
	if len(rules) != 0 || len(errs) == 0 {
		t.Errorf("invalid regex should be refused")
	}
}

func TestParse_DurationUnits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		unit string
		want time.Duration
	}{
		{"1d", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"1m", 30 * 24 * time.Hour},
		{"30m", 30 * 30 * 24 * time.Hour},
		{"1y", 365 * 24 * time.Hour},
		{"2y", 2 * 365 * 24 * time.Hour},
	}
	for _, tc := range cases {
		src := `{lifecycle scope="/x/.*" when=stale:` + tc.unit + ` title=t assign=u}` + "\n"
		rules, errs := Parse("/x", src)
		if len(errs) != 0 || len(rules) != 1 {
			t.Errorf("unit %q errored: %v", tc.unit, errs)
			continue
		}
		if rules[0].Condition.Duration != tc.want {
			t.Errorf("unit %q → duration %v, want %v", tc.unit, rules[0].Condition.Duration, tc.want)
		}
	}
	// Bad units are refused. "s" (seconds) is deliberately NOT accepted
	// so nobody sets `stale:30s` and floods the store.
	bad := []string{"30s", "1h", "abcd", ""}
	for _, s := range bad {
		src := `{lifecycle scope="/x/.*" when=stale:` + s + ` title=t assign=u}` + "\n"
		rules, errs := Parse("/x", src)
		if len(rules) != 0 || len(errs) == 0 {
			t.Errorf("bad duration %q should be refused", s)
		}
	}
}

func TestParse_MultipleRulesOnSamePage(t *testing.T) {
	t.Parallel()
	src := `# some page
{lifecycle scope="/a/.*" when=stale:1y title=a assign=u}
{lifecycle scope="/b/.*" when=stale:2y title=b assign=v}
` + "\n"
	rules, errs := Parse("/p", src)
	if len(errs) != 0 || len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d (errs=%v)", len(rules), errs)
	}
	if rules[0].ID == rules[1].ID {
		t.Errorf("distinct rules should have distinct IDs")
	}
}

// A reviewflow_overdue rule is alert-only: parse must accept it
// without title= or assign=, and no duration payload is allowed.
func TestParse_ReviewflowOverdue(t *testing.T) {
	t.Parallel()
	src := `{lifecycle scope="^/qms/.*" when=reviewflow_overdue}` + "\n"
	rules, errs := Parse("/admin/qms", src)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(rules))
	}
	r := rules[0]
	if r.Condition.Kind != "reviewflow_overdue" {
		t.Errorf("kind = %q, want reviewflow_overdue", r.Condition.Kind)
	}
	if r.Condition.Duration != 0 {
		t.Errorf("reviewflow_overdue must carry no duration, got %v", r.Condition.Duration)
	}
	if !r.Condition.IsAlertOnly() {
		t.Errorf("reviewflow_overdue must be alert-only")
	}
}

// Any payload on when=reviewflow_overdue is refused — the reviewflow
// deadlines already own the "how long is too long" question, and
// silently accepting a duration would confuse readers into thinking
// the lifecycle rule owns it.
func TestParse_ReviewflowOverdueRejectsPayload(t *testing.T) {
	t.Parallel()
	src := `{lifecycle scope="^/qms/.*" when=reviewflow_overdue:30d}` + "\n"
	rules, errs := Parse("/admin/qms", src)
	if len(rules) != 0 || len(errs) == 0 {
		t.Errorf("payload on reviewflow_overdue must be rejected")
	}
}

// title=/assign= remain OPTIONAL for reviewflow_overdue since it's
// alert-only — that was the whole point of the kind. Regression guard
// against re-adding a blanket "title required" check.
func TestParse_ReviewflowOverdueNoTitleOK(t *testing.T) {
	t.Parallel()
	src := `{lifecycle tags=sop when=reviewflow_overdue}` + "\n"
	rules, errs := Parse("/admin/qms", src)
	if len(rules) != 1 || len(errs) != 0 {
		t.Fatalf("expected 1 rule with no error, got rules=%d errs=%v", len(rules), errs)
	}
}
