package markdown

import (
	"testing"
)

// ExtractLinkOccurrences backs the list_broken_links MCP tool. Unlike
// ExtractPageLinks it keeps per-occurrence context — a broken-link
// report that only knows resolved paths tells you which page is dead
// but not WHERE in the source to go fix it. These cases pin the shape
// so the MCP tool can rely on it.

func TestExtractLinkOccurrences_Basic(t *testing.T) {
	t.Parallel()
	md := "See [the SOP](/qms/sop01) and [this guide](./guide) for details.\n"
	// pagePath carries a leading slash — resolve.go:ResolvePath needs it
	// so path.Dir() yields an absolute namespace. The storage layer
	// always passes the leading-slash form; mcpserver's tool wrapper
	// does the same.
	got := ExtractLinkOccurrences(md, "/qms/foo/bar")
	if len(got) != 2 {
		t.Fatalf("want 2 occurrences, got %d: %+v", len(got), got)
	}
	if got[0].Label != "the SOP" || got[0].Href != "/qms/sop01" || got[0].Resolved != "/qms/sop01" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Label != "this guide" || got[1].Href != "./guide" {
		t.Errorf("second = %+v", got[1])
	}
	// Relative resolution: ./guide from /qms/foo/bar → /qms/foo/guide.
	if got[1].Resolved != "/qms/foo/guide" {
		t.Errorf("relative resolve: got %q, want /qms/foo/guide", got[1].Resolved)
	}
	if got[0].Line != 1 || got[1].Line != 1 {
		t.Errorf("line numbers wrong: %+v", got)
	}
}

func TestExtractLinkOccurrences_DuplicateHrefReportedTwice(t *testing.T) {
	t.Parallel()
	// Same href twice (two mentions of the same page) must show up as
	// two occurrences — the broken-link report tells the human which
	// LINE to fix, so dedup would hide the second one.
	md := "First mention: [see](/x).\n\nSecond mention: [see again](/x).\n"
	got := ExtractLinkOccurrences(md, "any")
	if len(got) != 2 {
		t.Fatalf("want 2 (no dedup), got %d", len(got))
	}
	if got[0].Line != 1 || got[1].Line != 3 {
		t.Errorf("line numbers: got %d and %d, want 1 and 3", got[0].Line, got[1].Line)
	}
}

func TestExtractLinkOccurrences_SkipsExternalAndMedia(t *testing.T) {
	t.Parallel()
	md := "External [doc](https://example.com) and image ![caption](/pic.png) and attachment [file](/doc.pdf) — ignore all three.\n"
	got := ExtractLinkOccurrences(md, "x")
	if len(got) != 0 {
		t.Errorf("want 0 (external + image + non-page extension skipped), got %+v", got)
	}
}

func TestExtractLinkOccurrences_SkipsFragmentOnly(t *testing.T) {
	t.Parallel()
	// A pure fragment link stays on the same page — nothing to resolve
	// or report. Fragment-on-page links (page#heading) also don't
	// report broken because the fragment is stripped before Exists
	// (see docstring on the extractor).
	md := "[in-page](#section) and [other-page-fragment](/other#section)\n"
	got := ExtractLinkOccurrences(md, "x")
	if len(got) != 1 {
		t.Fatalf("want 1 (pure #frag skipped, fragment-on-page kept), got %+v", got)
	}
	if got[0].Resolved != "/other" {
		t.Errorf("fragment-on-page: got %q, want /other", got[0].Resolved)
	}
}

func TestExtractLinkOccurrences_SkipsInsideCodeFence(t *testing.T) {
	t.Parallel()
	// A link in a fenced code block is sample text, not a real
	// reference. Reporting it would make the tool spam dozens of
	// false positives on any page that documents the syntax.
	md := "Live link [a](/x)\n\n```\nSample: [b](/y)\n```\n\nAnother live [c](/z)\n"
	got := ExtractLinkOccurrences(md, "p")
	if len(got) != 2 {
		t.Fatalf("want 2 (fenced block skipped), got %+v", got)
	}
	if got[0].Label != "a" || got[1].Label != "c" {
		t.Errorf("wrong occurrences kept: %+v", got)
	}
}

func TestExtractLinkOccurrences_StripsMdExtension(t *testing.T) {
	t.Parallel()
	// A link written as [x](/path.md) still points at the page at
	// /path (the .md is a filesystem detail the canonical URL layer
	// hides). Must resolve to the extensionless form so Exists finds it.
	md := "[with ext](/path.md)\n"
	got := ExtractLinkOccurrences(md, "x")
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Resolved != "/path" {
		t.Errorf("want /path, got %q", got[0].Resolved)
	}
}

func TestExtractLinkOccurrences_PreservesLabelWithMarkup(t *testing.T) {
	t.Parallel()
	// Labels can carry inline markup (bold, code, escapes). The MCP
	// tool shows the label verbatim; we don't try to render it.
	md := "[**bold** label with `code`](/x) and [esc\\[ape\\]](/y)\n"
	got := ExtractLinkOccurrences(md, "p")
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Label != "**bold** label with `code`" {
		t.Errorf("label 0 = %q", got[0].Label)
	}
	if got[1].Label != "esc\\[ape\\]" {
		t.Errorf("label 1 = %q", got[1].Label)
	}
}

func TestExtractLinkOccurrences_EmptyLabel(t *testing.T) {
	t.Parallel()
	// Empty-label links [](/path) are common in Gowiki — the title is
	// pulled from the target page at render time. Still a real link,
	// still worth reporting as broken.
	md := "[](/x)\n"
	got := ExtractLinkOccurrences(md, "p")
	if len(got) != 1 {
		t.Fatalf("want 1, got %+v", got)
	}
	if got[0].Label != "" {
		t.Errorf("want empty label, got %q", got[0].Label)
	}
	if got[0].Resolved != "/x" {
		t.Errorf("resolved = %q", got[0].Resolved)
	}
}

// SlugifyHeading must agree with the frontend compiler/slugify.ts
// byte-for-byte — a slug mismatch would make the broken-link scan
// report "dead" anchors that render fine in the browser (and the
// opposite). Cases below mirror the frontend's rule set.

func TestSlugifyHeading_Basic(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Hello":                  "hello",
		"Hello World":            "hello-world",
		"  leading & trailing  ": "leading-trailing",
		"UPPER case Mix":         "upper-case-mix",
		"foo--bar___baz":         "foo-bar-baz",
	}
	for in, want := range cases {
		if got := SlugifyHeading(in); got != want {
			t.Errorf("SlugifyHeading(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugifyHeading_NonAscii(t *testing.T) {
	t.Parallel()
	// Non-ASCII characters are separators, NOT folded to their ASCII
	// equivalent — matches the frontend's `[^a-z0-9]+` rule exactly.
	// A heading "Café Alpha" slugifies to "caf-alpha" (the é is a
	// hyphen), not "cafe-alpha". A past divergence here is exactly
	// the kind of silent bug this test exists to prevent.
	cases := map[string]string{
		"Café Alpha":   "caf-alpha",
		"éè à":         "heading", // all non-alnum after lowering → empty → default "heading"
		"1. Objectifs": "1-objectifs",
	}
	for in, want := range cases {
		if got := SlugifyHeading(in); got != want {
			t.Errorf("SlugifyHeading(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugifyHeading_EmptyFallback(t *testing.T) {
	t.Parallel()
	// An all-punctuation heading collapses to empty, which the
	// frontend falls back to the literal "heading" sentinel for so a
	// fragment URL still works. Same fallback here.
	if got := SlugifyHeading("***"); got != "heading" {
		t.Errorf("empty-after-strip: got %q, want %q", got, "heading")
	}
	if got := SlugifyHeading(""); got != "heading" {
		t.Errorf("empty input: got %q, want %q", got, "heading")
	}
}

// ExtractHeadingSlugs mirrors the frontend's "same slug twice →
// -1, -2, …" dedup rule. Must agree exactly so a fragment that
// resolves in the browser also resolves here.

func TestExtractHeadingSlugs_ATXAllLevels(t *testing.T) {
	t.Parallel()
	md := `# One

## Two

### Three

#### Four

##### Five

###### Six
`
	slugs := ExtractHeadingSlugs(md)
	for _, want := range []string{"one", "two", "three", "four", "five", "six"} {
		if _, ok := slugs[want]; !ok {
			t.Errorf("missing slug %q in %+v", want, slugs)
		}
	}
}

func TestExtractHeadingSlugs_DuplicateSuffix(t *testing.T) {
	t.Parallel()
	// Three headings sharing the same base — pin the -1, -2 suffixing
	// so the server resolves #scope, #scope-1, #scope-2 the same
	// three positions the browser does. The frontend's duplicate
	// counter starts at 0 for the first and emits -N for subsequent
	// occurrences, so the third match ends in -2 (not -3).
	md := `## Scope

## Scope

## Scope
`
	slugs := ExtractHeadingSlugs(md)
	for _, want := range []string{"scope", "scope-1", "scope-2"} {
		if _, ok := slugs[want]; !ok {
			t.Errorf("missing %q in %+v", want, slugs)
		}
	}
	if _, bad := slugs["scope-3"]; bad {
		t.Errorf("unexpected scope-3 in %+v", slugs)
	}
}

func TestExtractHeadingSlugs_SkipsFencedCode(t *testing.T) {
	t.Parallel()
	// A `#` line in a fenced block is a shell comment / code sample,
	// not a wiki heading. Reporting #commented-example as a real
	// anchor would make the broken-link checker falsely accept dead
	// fragments that happen to match code comments.
	md := "# Real heading\n\n```\n# commented example\n## not a heading\n```\n\n# Another\n"
	slugs := ExtractHeadingSlugs(md)
	for _, want := range []string{"real-heading", "another"} {
		if _, ok := slugs[want]; !ok {
			t.Errorf("missing %q in %+v", want, slugs)
		}
	}
	for _, bad := range []string{"commented-example", "not-a-heading"} {
		if _, ok := slugs[bad]; ok {
			t.Errorf("code-block %q leaked into slug set %+v", bad, slugs)
		}
	}
}

func TestExtractHeadingSlugs_StripsInlineMarkup(t *testing.T) {
	t.Parallel()
	// A heading like `## **Bold** text` slugifies on the rendered
	// text content "Bold text", not on the raw source. The frontend
	// extracts textContent from the DOM; we strip the markup before
	// slugifying to match.
	md := "## **Bold** text with `code`\n\n## [link label](http://x)\n"
	slugs := ExtractHeadingSlugs(md)
	// For the second heading only the LABEL slugifies — the URL is
	// not visible on the rendered page, so including it in the slug
	// would diverge from the browser.
	for _, want := range []string{"bold-text-with-code", "link-label"} {
		if _, ok := slugs[want]; !ok {
			t.Errorf("missing %q in %+v", want, slugs)
		}
	}
}

// Numbered-heading prefix (dialect's "## 1. Title" syntax) must be
// stripped before slugifying — the frontend's gowiki_numbered_heading
// parse rule removes "N. " from the heading text at parse time, so
// the browser renders a heading whose textContent (and anchor slug)
// doesn't include the number. A link to `#documentation-effort`
// into a `## 1. Documentation effort` heading used to be reported
// as a dead fragment; this pins the fix.
func TestExtractHeadingSlugs_StripsNumberedPrefix(t *testing.T) {
	t.Parallel()
	md := `# 1. Overview

## 1. Documentation effort

### 3. Nested

## 10. Double-digit still counts
`
	slugs := ExtractHeadingSlugs(md)
	for _, want := range []string{"overview", "documentation-effort", "nested", "double-digit-still-counts"} {
		if _, ok := slugs[want]; !ok {
			t.Errorf("missing %q in %+v", want, slugs)
		}
	}
	// None of the prefix-including slugs should leak in — a divergence
	// here would silently miss real anchors or report false positives
	// on scans of a QMS that uses numbered headings heavily.
	for _, bad := range []string{"1-overview", "1-documentation-effort", "3-nested", "10-double-digit-still-counts"} {
		if _, ok := slugs[bad]; ok {
			t.Errorf("numbered prefix leaked into slug %q: %+v", bad, slugs)
		}
	}
}

func TestExtractHeadingSlugs_IgnoresSettextAndIndented(t *testing.T) {
	t.Parallel()
	// Dialect rejects setext headings and indented `#`. The slug
	// extractor must ignore them too — otherwise a reader could
	// link to #setext-style-heading and the scan would say it's
	// fine, but the actual anchor wouldn't exist on the rendered page.
	md := `Setext heading
==============

   # indented-looks-like-a-heading

# real one
`
	slugs := ExtractHeadingSlugs(md)
	if _, ok := slugs["real-one"]; !ok {
		t.Errorf("missing real-one in %+v", slugs)
	}
	if _, bad := slugs["setext-heading"]; bad {
		t.Errorf("setext leaked: %+v", slugs)
	}
	if _, bad := slugs["indented-looks-like-a-heading"]; bad {
		t.Errorf("indented leaked: %+v", slugs)
	}
}
