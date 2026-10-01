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
