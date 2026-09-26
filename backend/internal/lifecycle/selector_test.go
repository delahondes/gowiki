package lifecycle

import "testing"

// mkRule is a shortcut for building a Rule with known ID (needed for the
// selector's compiled-regex cache).
func mkRule(id, scope string, tags, exclude []string) Rule {
	return Rule{
		ID:          id,
		ScopeRegex:  scope,
		Tags:        tags,
		ExcludeTags: exclude,
	}
}

func TestPageMatches_ScopeOnly(t *testing.T) {
	t.Parallel()
	r := mkRule("s1", "^/qms/.*", nil, nil)
	if !PageMatches(r, "/qms/soft/mq01", nil) {
		t.Error("path in scope should match")
	}
	if PageMatches(r, "/other/page", nil) {
		t.Error("path outside scope should not match")
	}
}

func TestPageMatches_TagsAreOR(t *testing.T) {
	t.Parallel()
	// Rule requires ANY of {sop, rec, tpl}.
	r := mkRule("t1", "", []string{"sop", "rec", "tpl"}, nil)
	// Page with just one of the wanted tags — matches.
	if !PageMatches(r, "/x", TagSet([]string{"sop"})) {
		t.Error("page tagged sop should match tags=sop,rec,tpl (OR)")
	}
	if !PageMatches(r, "/x", TagSet([]string{"rec", "other"})) {
		t.Error("page tagged rec+other should match — one hit is enough")
	}
	// Page with none of the wanted tags — doesn't match.
	if PageMatches(r, "/x", TagSet([]string{"other"})) {
		t.Error("page with none of the wanted tags shouldn't match")
	}
	if PageMatches(r, "/x", nil) {
		t.Error("page with no tags at all shouldn't match a tag rule")
	}
}

func TestPageMatches_ExcludeTagsIsNAND(t *testing.T) {
	t.Parallel()
	r := mkRule("e1", "^/x/.*", nil, []string{"archived", "draft"})
	if !PageMatches(r, "/x/foo", TagSet([]string{"sop"})) {
		t.Error("page not tagged with any excluded tag should match")
	}
	if PageMatches(r, "/x/foo", TagSet([]string{"sop", "archived"})) {
		t.Error("page tagged archived should be excluded")
	}
	if PageMatches(r, "/x/foo", TagSet([]string{"draft"})) {
		t.Error("page tagged draft should be excluded")
	}
}

func TestPageMatches_AllSelectorsCombined(t *testing.T) {
	t.Parallel()
	// path=/qms/.* AND tags∈{sop,rec} AND NOT tags∈{archived}
	r := mkRule("c1", "^/qms/.*", []string{"sop", "rec"}, []string{"archived"})
	// Meets all three.
	if !PageMatches(r, "/qms/one", TagSet([]string{"sop"})) {
		t.Error("all criteria met should match")
	}
	// Fails scope.
	if PageMatches(r, "/other/one", TagSet([]string{"sop"})) {
		t.Error("wrong scope should not match")
	}
	// Fails tags (has neither sop nor rec).
	if PageMatches(r, "/qms/one", TagSet([]string{"tpl"})) {
		t.Error("tags mismatch should not match")
	}
	// Fails exclude.
	if PageMatches(r, "/qms/one", TagSet([]string{"sop", "archived"})) {
		t.Error("excluded tag should override otherwise-matching selectors")
	}
}

func TestPageMatches_NoSelectors_MatchesEverything(t *testing.T) {
	t.Parallel()
	// The Parse layer would refuse this shape, but the matcher itself
	// treats missing selectors as "no constraint" — the parse layer is
	// what enforces "must have at least one positive selector."
	r := mkRule("none", "", nil, nil)
	if !PageMatches(r, "/whatever", nil) {
		t.Error("matcher with no selectors should be permissive")
	}
}
