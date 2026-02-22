package markdown

import (
	"strings"
	"testing"
)

func TestExtractMediaRefs(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		pagePath string
		want     []string
	}{
		{
			name:     "image reference",
			content:  "![alt text](./diagram.png)",
			pagePath: "docs/intro",
			want:     []string{"docs/diagram.png"},
		},
		{
			name:     "image with title",
			content:  `![alt](./pic.jpg "A picture")`,
			pagePath: "docs/intro",
			want:     []string{"docs/pic.jpg"},
		},
		{
			name:     "link to file with extension",
			content:  "[download](./report.pdf)",
			pagePath: "docs/intro",
			want:     []string{"docs/report.pdf"},
		},
		{
			name:     "link with title",
			content:  `[download](./report.pdf "Report")`,
			pagePath: "docs/intro",
			want:     []string{"docs/report.pdf"},
		},
		{
			name:     "link to .md is page link not media",
			content:  "[see also](./other.md)",
			pagePath: "docs/intro",
			want:     nil,
		},
		{
			name:     "link without extension is not media",
			content:  "[other page](./subpage)",
			pagePath: "docs/intro",
			want:     nil,
		},
		{
			name:     "absolute media ref",
			content:  "![logo](/images/logo.png)",
			pagePath: "docs/intro",
			want:     []string{"images/logo.png"},
		},
		{
			name:     "external URL skipped",
			content:  "![ext](https://example.com/pic.png)",
			pagePath: "docs/intro",
			want:     nil,
		},
		{
			name:     "http URL skipped",
			content:  "![ext](http://example.com/pic.png)",
			pagePath: "docs/intro",
			want:     nil,
		},
		{
			name:     "multiple refs deduplicated and sorted",
			content:  "![a](./b.png)\n![c](./a.png)\n![d](./b.png)",
			pagePath: "docs/intro",
			want:     []string{"docs/a.png", "docs/b.png"},
		},
		{
			name: "code block content skipped",
			content: "```\n![fake](./not-real.png)\n```\n![real](./real.png)",
			pagePath: "docs/intro",
			want:     []string{"docs/real.png"},
		},
		{
			name: "code block with language specifier skipped",
			content: "```markdown\n![fake](./not-real.png)\n```\n![real](./real.png)",
			pagePath: "docs/intro",
			want:     []string{"docs/real.png"},
		},
		{
			name:     "empty content",
			content:  "",
			pagePath: "docs/intro",
			want:     nil,
		},
		{
			name:     "no matches",
			content:  "Just some regular text\nwith no refs.",
			pagePath: "docs/intro",
			want:     nil,
		},
		{
			name:     "mixed images and links",
			content:  "![img](./photo.jpg)\n[doc](./file.pdf)\n[page](./other)",
			pagePath: "docs/intro",
			want:     []string{"docs/file.pdf", "docs/photo.jpg"},
		},
		{
			name:     "parent relative ref",
			content:  "![logo](../images/logo.svg)",
			pagePath: "docs/guides/intro",
			want:     []string{"docs/images/logo.svg"},
		},
		{
			name:     "image at start of line and link at start of line",
			content:  "![a](./x.png)\n[b](./y.zip)",
			pagePath: "page",
			want:     []string{"x.png", "y.zip"},
		},
		{
			name:     "link preceded by text on same line",
			content:  "See [this file](./doc.pdf) for details",
			pagePath: "page",
			want:     []string{"doc.pdf"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractMediaRefs(tc.content, tc.pagePath)
			if !sliceEqual(got, tc.want) {
				t.Errorf("ExtractMediaRefs() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractMediaRefs_LargeCodeBlock(t *testing.T) {
	var b strings.Builder
	b.WriteString("```\n")
	for i := 0; i < 100; i++ {
		b.WriteString("![fake](./fake.png)\n")
	}
	b.WriteString("```\n")
	b.WriteString("![real](./real.png)\n")

	got := ExtractMediaRefs(b.String(), "docs/page")
	want := []string{"docs/real.png"}
	if !sliceEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
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
