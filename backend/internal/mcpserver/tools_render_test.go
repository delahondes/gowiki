package mcpserver

import (
	"strings"
	"testing"
)

func TestHtmlToStructuredText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // substrings that must appear in the output
		gone []string // substrings that must NOT appear
	}{
		{
			name: "headings and paragraphs",
			in:   `<h1>Title</h1><p>Hello <strong>world</strong></p>`,
			want: []string{"# Title", "Hello **world**"},
		},
		{
			name: "links resolved to markdown form",
			in:   `<p>Visit <a href="/foo">the page</a></p>`,
			want: []string{"[the page](/foo)"},
		},
		{
			name: "bare link with matching label collapses to url",
			in:   `<p><a href="/foo">/foo</a></p>`,
			want: []string{"/foo"},
			gone: []string{"[/foo](/foo)"},
		},
		{
			name: "unordered list",
			in:   `<ul><li>alpha</li><li>beta</li></ul>`,
			want: []string{"- alpha", "- beta"},
		},
		{
			name: "ordered list numbered",
			in:   `<ol><li>first</li><li>second</li></ol>`,
			want: []string{"1. first", "2. second"},
		},
		{
			name: "table with thead becomes pipe-table",
			in: `<table><thead><tr><th>A</th><th>B</th></tr></thead>` +
				`<tbody><tr><td>1</td><td>2</td></tr><tr><td>3</td><td>4</td></tr></tbody></table>`,
			want: []string{"| A | B |", "| --- | --- |", "| 1 | 2 |", "| 3 | 4 |"},
		},
		{
			name: "script and style dropped",
			in:   `<p>keep</p><script>alert(1)</script><style>body{color:red}</style>`,
			want: []string{"keep"},
			gone: []string{"alert(1)", "color:red"},
		},
		{
			name: "hidden element dropped",
			in:   `<p>visible</p><div hidden>secret</div>`,
			want: []string{"visible"},
			gone: []string{"secret"},
		},
		{
			name: "image with alt",
			in:   `<img alt="Diagram" src="/img/d.png">`,
			want: []string{"![Diagram](/img/d.png)"},
		},
		{
			name: "inline code",
			in:   `<p>Try <code>foo()</code>.</p>`,
			want: []string{"`foo()`"},
		},
		{
			name: "blockquote",
			in:   `<blockquote><p>quoted</p></blockquote>`,
			want: []string{"> quoted"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := htmlToStructuredText(tc.in)
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("missing %q in output:\n%s", s, got)
				}
			}
			for _, s := range tc.gone {
				if strings.Contains(got, s) {
					t.Errorf("unexpected %q in output:\n%s", s, got)
				}
			}
		})
	}
}

func TestHtmlToStructuredText_Empty(t *testing.T) {
	if got := htmlToStructuredText(""); got != "" {
		t.Errorf("empty input → %q, want empty", got)
	}
}
