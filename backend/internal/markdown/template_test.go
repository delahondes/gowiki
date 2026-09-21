package markdown

import (
	"strings"
	"testing"
)

func TestIsTemplatePage(t *testing.T) {
	tt := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"no directive", "# hello\nplain text", false},
		{"has marker alone", "# T\n\n{template}\n\npayload", true},
		{"marker embedded in text is ignored", "some text with {template} inline", false},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTemplatePage(tc.content); got != tc.want {
				t.Errorf("IsTemplatePage() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSplitTemplatePayload(t *testing.T) {
	content := "# Template\n\ntracking\n\n{template}\n\n{tag rec}\n\npayload here\n"
	header, payload, ok := SplitTemplatePayload(content)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !strings.Contains(header, "tracking") || strings.Contains(header, "payload") {
		t.Errorf("header wrong:\n%q", header)
	}
	if !strings.Contains(payload, "payload here") || strings.Contains(payload, "tracking") {
		t.Errorf("payload wrong:\n%q", payload)
	}
	if strings.Contains(header, "{template}") || strings.Contains(payload, "{template}") {
		t.Errorf("marker leaked into header or payload")
	}
}

func TestSplitTemplatePayload_NoMarker(t *testing.T) {
	_, _, ok := SplitTemplatePayload("just a page")
	if ok {
		t.Error("expected ok=false when no {template} marker")
	}
}

func TestSplitTemplatePayload_TrimsLeadingBlankLines(t *testing.T) {
	// A natural template has a blank line between {template} and the first
	// payload line ({tag rec} / {template-title}). The payload must NOT
	// start with that blank line, otherwise the created document opens
	// with an empty line.
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "one blank line",
			content: "{template}\n\n{tag rec}\n\npayload",
			want:    "{tag rec}\n\npayload",
		},
		{
			name:    "two blank lines",
			content: "{template}\n\n\n{tag rec}\n",
			want:    "{tag rec}\n",
		},
		{
			name:    "no blank line",
			content: "{template}\n{tag rec}\n",
			want:    "{tag rec}\n",
		},
		{
			name:    "whitespace-only lines count as blank",
			content: "{template}\n  \n\t\n{tag rec}\n",
			want:    "{tag rec}\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, payload, ok := SplitTemplatePayload(tc.content)
			if !ok {
				t.Fatal("expected ok=true")
			}
			if payload != tc.want {
				t.Errorf("payload = %q, want %q", payload, tc.want)
			}
		})
	}
}

func TestParseTemplateReviewflowArgs(t *testing.T) {
	args, ok := ParseTemplateReviewflowArgs(`some text
{template-reviewflow author=alice.laporte reviewer="Michel Laborde" validation=e.f}
after`)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if args["author"] != "alice.laporte" ||
		args["reviewer"] != "Michel Laborde" ||
		args["validation"] != "e.f" {
		t.Errorf("wrong args: %#v", args)
	}
}

func TestParseTemplateReviewflowArgs_BareIsOk(t *testing.T) {
	args, ok := ParseTemplateReviewflowArgs("hello\n{template-reviewflow}\nworld")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(args) != 0 {
		t.Errorf("expected empty args, got %#v", args)
	}
}

func TestParseReviewflowArgs(t *testing.T) {
	args := ParseReviewflowArgs("{reviewflow version=1.2 author=alice reviewer=bob validation=carol}")
	if args["version"] != "1.2" || args["author"] != "alice" ||
		args["reviewer"] != "bob" || args["validation"] != "carol" {
		t.Errorf("wrong args: %#v", args)
	}
}

func TestFormatTemplateStamp_WithVersionTag(t *testing.T) {
	s := FormatTemplateStamp(TemplateStampArgs{
		TemplatePath:        "/regulatory/qms/soft/sop01/tpl10",
		TemplateTitle:       "SOFT/SOP01/TPL10 : Verification and Validation Plan",
		TemplatePageVersion: 7,
		VersionTag:          "1.0",
	})
	want := "Created from template [SOFT/SOP01/TPL10 : Verification and Validation Plan](/regulatory/qms/soft/sop01/tpl10?v=7), version 1.0"
	if s != want {
		t.Errorf("got:\n%s\nwant:\n%s", s, want)
	}
}

func TestFormatTemplateStamp_NoReviewflow(t *testing.T) {
	s := FormatTemplateStamp(TemplateStampArgs{
		TemplatePath:        "/plain",
		TemplateTitle:       "Plain",
		TemplatePageVersion: 3,
	})
	want := "Created from template [Plain](/plain?v=3), revision 3"
	if s != want {
		t.Errorf("got:\n%s\nwant:\n%s", s, want)
	}
}

func TestResolveTemplatePayload(t *testing.T) {
	payload := `{tag rec}

{template-title}
# Verification and validation plan - ==[Name of the Medical Device]==

## 1. Follow-up and approval

### 1. Authors and reviewers

{template-reviewflow}

### 1. Change history

{template-stamp}

| Version | Date |
| --- | --- |
| 1.0 |  |
`
	got := ResolveTemplatePayload(payload, TemplateResolveOpts{
		Stamp: TemplateStampArgs{
			TemplatePath:        "/regulatory/qms/soft/sop01/tpl10",
			TemplateTitle:       "SOFT/SOP01/TPL10 : Verification and Validation Plan",
			TemplatePageVersion: 7,
			VersionTag:          "1.0",
		},
		Title: "VVP/SOFT02 : Verification and Validation Plan",
		ReviewflowArgs: map[string]string{
			"version":    "1.0",
			"author":     "raynald.delahondes",
			"reviewer":   "michel.laborde",
			"validation": "etienne.formstecher",
		},
	})

	// The pattern heading is replaced with the user's title, keeping the "#".
	if !strings.Contains(got, "\n# VVP/SOFT02 : Verification and Validation Plan\n") {
		t.Errorf("title not substituted:\n%s", got)
	}
	// The {template-title} marker is gone.
	if strings.Contains(got, "{template-title}") {
		t.Errorf("template-title marker leaked into output")
	}
	// {template-reviewflow} → {reviewflow …}
	if !strings.Contains(got, "{reviewflow version=1.0 author=raynald.delahondes reviewer=michel.laborde validation=etienne.formstecher}") {
		t.Errorf("reviewflow not resolved:\n%s", got)
	}
	// {template-stamp} → sentence.
	if !strings.Contains(got, "Created from template [SOFT/SOP01/TPL10 : Verification and Validation Plan](/regulatory/qms/soft/sop01/tpl10?v=7), version 1.0") {
		t.Errorf("stamp not resolved:\n%s", got)
	}
	// The {tag rec} block that opens the payload survives untouched.
	if !strings.HasPrefix(strings.TrimSpace(got), "{tag rec}") {
		t.Errorf("{tag rec} lost or moved:\n%s", got)
	}
}

func TestResolveTemplatePayload_NoTitle(t *testing.T) {
	// Row-bound-page path: no title override, no reviewflow args. Stamp
	// still gets resolved. {template-title} and {template-reviewflow}
	// markers get dropped but the pattern heading below {template-title}
	// stays as-is (because the row's own heading comes from elsewhere).
	payload := `{template-title}
# Pattern

{template-reviewflow}

{template-stamp}
`
	got := ResolveTemplatePayload(payload, TemplateResolveOpts{
		Stamp: TemplateStampArgs{
			TemplatePath:        "/tpl",
			TemplateTitle:       "Tpl",
			TemplatePageVersion: 1,
		},
	})
	if strings.Contains(got, "{template-title}") ||
		strings.Contains(got, "{template-reviewflow}") ||
		strings.Contains(got, "{template-stamp}") {
		t.Errorf("directives leaked into output:\n%s", got)
	}
	if !strings.Contains(got, "# Pattern") {
		t.Errorf("pattern heading dropped:\n%s", got)
	}
	if !strings.Contains(got, "Created from template [Tpl](/tpl?v=1)") {
		t.Errorf("stamp missing:\n%s", got)
	}
	// No {reviewflow} directive emitted.
	if strings.Contains(got, "{reviewflow") {
		t.Errorf("unexpected {reviewflow} in row-bound-page output:\n%s", got)
	}
}

func TestMergeReviewflowArgs(t *testing.T) {
	// Precedence: overrides > template-reviewflow > templateOwn. Missing
	// version defaults to "1.0".
	got := MergeReviewflowArgs(
		map[string]string{"reviewer": "override.r"},
		map[string]string{"author": "trf.a", "reviewer": "trf.r"},
		map[string]string{"author": "own.a", "reviewer": "own.r", "validation": "own.v", "version": "2.5"},
	)
	if got["author"] != "trf.a" || got["reviewer"] != "override.r" ||
		got["validation"] != "own.v" || got["version"] != "1.0" {
		t.Errorf("merge wrong: %#v", got)
	}
}

func TestExtractTemplateTitlePattern(t *testing.T) {
	payload := `{template-title}
# Pattern - ==[X]==

body`
	got := ExtractTemplateTitlePattern(payload)
	want := "Pattern - ==[X]=="
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
