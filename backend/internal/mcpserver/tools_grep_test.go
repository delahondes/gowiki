package mcpserver

import (
	"regexp"
	"testing"
)

// grepPages is the pure core of search_pages' pattern (grep) branch.
// Covers the shape agents care about: exact matching, line+column,
// per-page and total caps, ACL filtering, path-prefix scoping.

// Helper: build a page corpus as a map so the getMarkdown callback is
// easy to configure per test. Map keys are slash-prefixed (matches what
// Sitemap.ListAllPages returns); grepPages strips the slash before
// calling getMarkdown, so the callback un-strips to look things up.
func newCorpus(pages map[string]string) (paths []string, getMarkdown func(string) (string, error)) {
	for p := range pages {
		paths = append(paths, p)
	}
	getMarkdown = func(p string) (string, error) { return pages["/"+p], nil }
	return paths, getMarkdown
}

func TestGrepPages_LiteralMatch_LineAndColumn(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/docs/a": "first line\nhere is TOKEN in line 2\nthird line\n",
	})
	re := regexp.MustCompile(`TOKEN`)
	matches, scanned, _ := grepPages(grepScanArgs{
		pages: paths, re: re, limit: 100,
		getMarkdown: getMarkdown,
	})
	if scanned != 1 {
		t.Errorf("scanned = %d, want 1", scanned)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	m := matches[0]
	if m.Path != "/docs/a" {
		t.Errorf("Path = %q", m.Path)
	}
	if m.Line != 2 {
		t.Errorf("Line = %d, want 2", m.Line)
	}
	if m.Column != 9 { // "here is TOKEN..." → T is at byte 9 (1-indexed)
		t.Errorf("Column = %d, want 9", m.Column)
	}
	if m.Match != "TOKEN" {
		t.Errorf("Match = %q", m.Match)
	}
	if m.LineText != "here is TOKEN in line 2" {
		t.Errorf("LineText = %q", m.LineText)
	}
}

func TestGrepPages_MultipleOccurrencesOnOneLine(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/p": "foo foo foo\n",
	})
	re := regexp.MustCompile(`foo`)
	matches, _, _ := grepPages(grepScanArgs{
		pages: paths, re: re, limit: 100,
		getMarkdown: getMarkdown,
	})
	if len(matches) != 3 {
		t.Fatalf("matches = %d, want 3", len(matches))
	}
	if matches[0].Column != 1 || matches[1].Column != 5 || matches[2].Column != 9 {
		t.Errorf("columns = [%d %d %d], want [1 5 9]",
			matches[0].Column, matches[1].Column, matches[2].Column)
	}
}

func TestGrepPages_RespectsLimit(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/a": "hit\nhit\nhit\n",
		"/b": "hit\nhit\n",
	})
	re := regexp.MustCompile(`hit`)
	matches, _, _ := grepPages(grepScanArgs{
		pages: paths, re: re, limit: 2,
		getMarkdown: getMarkdown,
	})
	if len(matches) != 2 {
		t.Errorf("matches = %d, want 2 (hard cap)", len(matches))
	}
}

func TestGrepPages_MaxPerPage_CapsEachPageIndependently(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/a": "x\nx\nx\nx\n", // 4 hits
		"/b": "x\nx\n",       // 2 hits
	})
	re := regexp.MustCompile(`x`)
	matches, _, _ := grepPages(grepScanArgs{
		pages: paths, re: re, limit: 100, maxPerPage: 2,
		getMarkdown: getMarkdown,
	})
	// 2 per page, two pages → 4 total.
	if len(matches) != 4 {
		t.Fatalf("matches = %d, want 4 (2 per page × 2 pages)", len(matches))
	}
}

func TestGrepPages_ACLSkip(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/public":  "token\n",
		"/private": "token\n",
	})
	re := regexp.MustCompile(`token`)
	matches, scanned, skipped := grepPages(grepScanArgs{
		pages: paths, re: re, limit: 100,
		canView:     func(p string) bool { return p != "private" },
		getMarkdown: getMarkdown,
	})
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1 (private hidden)", len(matches))
	}
	if matches[0].Path != "/public" {
		t.Errorf("Path = %q, want /public", matches[0].Path)
	}
	if scanned != 1 {
		t.Errorf("scanned = %d, want 1", scanned)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
}

func TestGrepPages_PathPrefixScope(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/qms/sop01":  "hit\n",
		"/qms/sop02":  "hit\n",
		"/other/page": "hit\n",
	})
	re := regexp.MustCompile(`hit`)
	matches, scanned, _ := grepPages(grepScanArgs{
		pages: paths, re: re, prefix: "qms", limit: 100,
		getMarkdown: getMarkdown,
	})
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2 (prefix excludes /other)", len(matches))
	}
	if scanned != 2 {
		t.Errorf("scanned = %d, want 2", scanned)
	}
}

func TestGrepPages_Regex_StructuredDirective(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/t1": "{template-stamp}\nbody\n",
		"/t2": "no directive here\n",
		"/t3": "a line with {template-stamp foo=bar} on it\n",
	})
	// Classic agent query: find every page using the {template-stamp}
	// directive. Fuzzy full-text would miss the directive-as-literal
	// shape and would also match the word "template" elsewhere.
	re := regexp.MustCompile(`\{template-stamp(?:\s[^{}]*)?\}`)
	matches, _, _ := grepPages(grepScanArgs{
		pages: paths, re: re, limit: 100,
		getMarkdown: getMarkdown,
	})
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2 (t1 + t3)", len(matches))
	}
	// t3's match keeps its trailing args, exactly as grep -o would.
	if matches[1].Match != "{template-stamp foo=bar}" {
		t.Errorf("Match on t3 = %q, want '{template-stamp foo=bar}'", matches[1].Match)
	}
}

func TestGrepPages_CaseInsensitiveViaFlag(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/p": "Hello world\nHELLO again\n",
	})
	re := regexp.MustCompile(`(?i)hello`)
	matches, _, _ := grepPages(grepScanArgs{
		pages: paths, re: re, limit: 100,
		getMarkdown: getMarkdown,
	})
	if len(matches) != 2 {
		t.Errorf("matches = %d, want 2 (case-insensitive)", len(matches))
	}
}

func TestGrepPages_NoMatches_EmptyResult(t *testing.T) {
	t.Parallel()
	paths, getMarkdown := newCorpus(map[string]string{
		"/p": "nothing here\n",
	})
	re := regexp.MustCompile(`absent`)
	matches, scanned, _ := grepPages(grepScanArgs{
		pages: paths, re: re, limit: 100,
		getMarkdown: getMarkdown,
	})
	if len(matches) != 0 {
		t.Errorf("matches = %+v, want empty", matches)
	}
	if scanned != 1 {
		t.Errorf("scanned = %d, want 1 (page was scanned even with no hit)", scanned)
	}
	if matches == nil {
		t.Errorf("matches must be a non-nil empty slice so the JSON renders as [] not null")
	}
}
