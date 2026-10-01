package mcpserver

import (
	"strings"
	"testing"
)

// buildBrokenLinksList is the pure core of the list_broken_links MCP tool.
// It stands in for a hand-rolled loop over the sitemap that would
// otherwise need Store, Sitemap and ACL to be plumbed through just to
// test "did the ACL skip the right pages" or "did the Exists memo
// actually dedup". The handler runs the result through the JSON
// envelope and does nothing else.

// fakeCorpus is a tiny in-memory wiki: pageMarkdown[path] = source
// text; existingTargets lists which pages the link checker treats as
// present (lets a test say "every target exists" vs "only /real
// exists"). acl, when non-nil, hides pages from the viewer.
type fakeCorpus struct {
	pageMarkdown    map[string]string
	existingTargets map[string]bool
	acl             map[string]bool // true = visible, false/missing = hidden
}

func (c fakeCorpus) allPaths() []string {
	paths := make([]string, 0, len(c.pageMarkdown))
	for p := range c.pageMarkdown {
		paths = append(paths, p)
	}
	return paths
}

func (c fakeCorpus) canView(p string) bool {
	if c.acl == nil {
		return true
	}
	return c.acl[p]
}

func (c fakeCorpus) readMarkdown(p string) (string, bool) {
	md, ok := c.pageMarkdown[strings.TrimPrefix(p, "/")]
	return md, ok
}

func (c fakeCorpus) exists(p string) bool {
	return c.existingTargets[strings.TrimPrefix(p, "/")]
}

// Baseline: scan returns one row per dead-link occurrence, with the
// resolved path, label, and line number. Pages with no broken links
// don't contribute rows.
func TestBuildBrokenLinksList_HappyPath(t *testing.T) {
	t.Parallel()
	c := fakeCorpus{
		pageMarkdown: map[string]string{
			"a/page1": "Links to [real](/real) and [missing](/qms/ghost).\n",
			"a/page2": "[dead](/dead)\n",
			"a/clean": "No links here, just prose.\n",
		},
		existingTargets: map[string]bool{"real": true},
	}
	result := buildBrokenLinksList(brokenLinksScanArgs{
		pages:        []string{"/a/page1", "/a/page2", "/a/clean"},
		prefix:       "a",
		limit:        100,
		canView:      c.canView,
		readMarkdown: c.readMarkdown,
		exists:       c.exists,
	})
	if len(result.Broken) != 2 {
		t.Fatalf("want 2 broken rows, got %d: %+v", len(result.Broken), result.Broken)
	}
	if result.Scanned != 3 {
		t.Errorf("scanned = %d, want 3", result.Scanned)
	}
	if result.PagesWithBroken != 2 {
		t.Errorf("pagesWithBroken = %d, want 2 (page1 and page2, not clean)", result.PagesWithBroken)
	}
	if result.TruncatedAtLimit {
		t.Error("truncated flag spuriously set under limit")
	}
	// Row shape: label and resolved path survive, page gets a leading slash.
	first := result.Broken[0]
	if first.Page != "/a/page1" || first.Resolved != "/qms/ghost" || first.Label != "missing" {
		t.Errorf("row shape wrong: %+v", first)
	}
}

// Multiple broken links on the same page bump broken-rows but
// pages_with_broken counts the PAGE once. Pins the counter semantics
// so the "how many SOPs have holes?" question answers correctly even
// when a single doc has ten dead links.
func TestBuildBrokenLinksList_PagesWithBrokenCountsOnce(t *testing.T) {
	t.Parallel()
	c := fakeCorpus{
		pageMarkdown: map[string]string{
			"doc": "[one](/a) [two](/b) [three](/c)\n",
		},
	}
	result := buildBrokenLinksList(brokenLinksScanArgs{
		pages: []string{"/doc"}, prefix: "", limit: 100,
		canView: c.canView, readMarkdown: c.readMarkdown, exists: c.exists,
	})
	if len(result.Broken) != 3 {
		t.Fatalf("want 3 broken rows, got %d", len(result.Broken))
	}
	if result.PagesWithBroken != 1 {
		t.Errorf("pagesWithBroken = %d, want 1 (one page, three broken refs)", result.PagesWithBroken)
	}
}

// ACL-hidden pages count against skipped_access, NOT scanned — so a
// caller can distinguish "no broken links in this subtree" from
// "you can't see the subtree".
func TestBuildBrokenLinksList_ACLSkip(t *testing.T) {
	t.Parallel()
	c := fakeCorpus{
		pageMarkdown: map[string]string{
			"a/visible": "[dead](/missing)\n",
			"a/hidden":  "[alsobroken](/also-missing)\n",
		},
		acl: map[string]bool{"a/visible": true, "a/hidden": false},
	}
	result := buildBrokenLinksList(brokenLinksScanArgs{
		pages: []string{"/a/visible", "/a/hidden"}, prefix: "a", limit: 100,
		canView: c.canView, readMarkdown: c.readMarkdown, exists: c.exists,
	})
	if len(result.Broken) != 1 || result.Broken[0].Page != "/a/visible" {
		t.Fatalf("want 1 broken row from /a/visible, got %+v", result.Broken)
	}
	if result.Scanned != 1 {
		t.Errorf("scanned = %d, want 1 (hidden page must not count)", result.Scanned)
	}
	if result.SkippedAccess != 1 {
		t.Errorf("skippedAccess = %d, want 1", result.SkippedAccess)
	}
}

// The existsCache saves one lookup per unique target even when the
// same target appears many times — pin the behaviour via call-count
// so a future refactor can't silently reintroduce the N²-queries
// pattern on a corpus where every page points at the same central SOP.
func TestBuildBrokenLinksList_ExistsCacheDedupsLookups(t *testing.T) {
	t.Parallel()
	c := fakeCorpus{
		pageMarkdown: map[string]string{
			"p1": "[a](/central) [b](/central)\n", // same target twice on the same page
			"p2": "[c](/central)\n",                // same target on a different page
			"p3": "[d](/other)\n",
		},
	}
	calls := make(map[string]int)
	existsCountingSpy := func(p string) bool {
		calls[strings.TrimPrefix(p, "/")]++
		return false
	}
	buildBrokenLinksList(brokenLinksScanArgs{
		pages: []string{"/p1", "/p2", "/p3"}, prefix: "", limit: 100,
		canView: c.canView, readMarkdown: c.readMarkdown, exists: existsCountingSpy,
	})
	if calls["central"] != 1 {
		t.Errorf("Exists(/central) called %d times, want 1 (cache missed)", calls["central"])
	}
	if calls["other"] != 1 {
		t.Errorf("Exists(/other) called %d times, want 1", calls["other"])
	}
}

// The limit is a hard cap. Once hit, further occurrences and further
// pages are skipped entirely — cheap. truncated_at_limit is set so
// callers know they got a partial answer and should paginate (today
// via a tighter prefix; a cursor-style continuation isn't modelled yet).
func TestBuildBrokenLinksList_LimitTruncates(t *testing.T) {
	t.Parallel()
	c := fakeCorpus{
		pageMarkdown: map[string]string{
			"p1": "[a](/x1)\n",
			"p2": "[b](/x2)\n",
			"p3": "[c](/x3)\n",
			"p4": "[d](/x4)\n",
			"p5": "[e](/x5)\n",
		},
	}
	result := buildBrokenLinksList(brokenLinksScanArgs{
		pages: []string{"/p1", "/p2", "/p3", "/p4", "/p5"}, prefix: "", limit: 2,
		canView: c.canView, readMarkdown: c.readMarkdown, exists: c.exists,
	})
	if len(result.Broken) != 2 {
		t.Errorf("want 2 broken rows (hard cap), got %d", len(result.Broken))
	}
	if !result.TruncatedAtLimit {
		t.Error("truncated_at_limit must be true when limit was hit")
	}
	// The scan short-circuits BETWEEN pages too: once the cap is
	// reached, remaining pages are not even opened. Pin this so a
	// future refactor doesn't quietly make the tool O(all-pages)
	// per call when limit=1.
	if result.Scanned > 2 {
		t.Errorf("scanned = %d, want ≤ 2 (short-circuit after limit)", result.Scanned)
	}
}

// Empty prefix scans everything. Each row carries the full path so
// the caller can tell which subtree the hit lives in.
func TestBuildBrokenLinksList_EmptyPrefixMatchesAll(t *testing.T) {
	t.Parallel()
	c := fakeCorpus{
		pageMarkdown: map[string]string{
			"a/sub/page": "[dead](/gone)\n",
			"b/other":    "[alsodead](/alsogone)\n",
		},
	}
	result := buildBrokenLinksList(brokenLinksScanArgs{
		pages: []string{"/a/sub/page", "/b/other"}, prefix: "", limit: 100,
		canView: c.canView, readMarkdown: c.readMarkdown, exists: c.exists,
	})
	if len(result.Broken) != 2 {
		t.Fatalf("empty prefix: want 2, got %d", len(result.Broken))
	}
}

// readMarkdown returning ok=false (e.g. page was deleted mid-scan)
// drops the page quietly rather than aborting the whole scan. The
// page still counts as "scanned" — the operator knows the scan passed
// over it even if nothing landed in the broken set.
func TestBuildBrokenLinksList_ReadFailureDoesNotAbort(t *testing.T) {
	t.Parallel()
	c := fakeCorpus{
		pageMarkdown: map[string]string{
			"p1": "[dead1](/a)\n",
			// p2 is in the sitemap but not in pageMarkdown — simulates a
			// page the Store couldn't load.
			"p3": "[dead3](/c)\n",
		},
	}
	result := buildBrokenLinksList(brokenLinksScanArgs{
		pages: []string{"/p1", "/p2", "/p3"}, prefix: "", limit: 100,
		canView: c.canView, readMarkdown: c.readMarkdown, exists: c.exists,
	})
	if len(result.Broken) != 2 {
		t.Errorf("want 2 broken (p1 and p3; p2 silently dropped), got %d", len(result.Broken))
	}
	if result.Scanned != 3 {
		t.Errorf("scanned = %d, want 3 (p2 still counts as scanned)", result.Scanned)
	}
}
