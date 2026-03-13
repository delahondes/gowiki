package markdown

import (
	"testing"
)

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "heading markers removed",
			input:    "# Title\n## Subtitle\n### Section",
			expected: "Title\nSubtitle\nSection",
		},
		{
			name:     "bold markers removed",
			input:    "This is **bold** text",
			expected: "This is bold text",
		},
		{
			name:     "italic markers removed",
			input:    "This is *italic* text",
			expected: "This is italic text",
		},
		{
			name:     "underline markers removed",
			input:    "This is _underlined_ text",
			expected: "This is underlined text",
		},
		{
			name:     "strikethrough markers removed",
			input:    "This is ~~deleted~~ text",
			expected: "This is deleted text",
		},
		{
			name:     "inline code markers removed",
			input:    "Use `fmt.Println` here",
			expected: "Use fmt.Println here",
		},
		{
			name:     "link syntax stripped keeping text",
			input:    "See [the docs](https://example.com) for more",
			expected: "See the docs for more",
		},
		{
			name:     "image syntax stripped keeping alt",
			input:    "Here is ![a photo](image.png) inline",
			expected: "Here is a photo inline",
		},
		{
			name:     "code fences removed but content kept",
			input:    "Before\n```go\nfmt.Println(\"hello\")\n```\nAfter",
			expected: "Before\nfmt.Println(\"hello\")\nAfter",
		},
		{
			name:     "directive lines removed",
			input:    "{include path=footer}\nSome text\n{blockquote class=note}",
			expected: "Some text",
		},
		{
			name:     "unordered list markers stripped",
			input:    "- First item\n- Second item\n  - Nested",
			expected: "First item\nSecond item\n  Nested",
		},
		{
			name:     "ordered list markers stripped",
			input:    "1. First\n2. Second\n3. Third",
			expected: "First\nSecond\nThird",
		},
		{
			name:     "table separator rows removed",
			input:    "| Name | Value |\n|---|---|\n| A | B |",
			expected: "Name   Value  \n  A   B",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "combined formatting",
			input:    "# Welcome\n\nThis is **bold** and *italic* with [a link](url).\n\n```\ncode\n```\n\n{include path=sidebar}\n\nEnd.",
			expected: "Welcome\n\nThis is bold and italic with a link.\n\ncode\n\nEnd.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMarkdown(tt.input)
			if got != tt.expected {
				t.Errorf("StripMarkdown():\n  got:  %q\n  want: %q", got, tt.expected)
			}
		})
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple h1",
			input:    "# My Page Title\n\nSome content",
			expected: "My Page Title",
		},
		{
			name:     "h1 with bold",
			input:    "# **Bold** Title\n\nContent",
			expected: "Bold Title",
		},
		{
			name:     "no h1",
			input:    "## Subtitle only\n\nNo h1 here",
			expected: "",
		},
		{
			name:     "h1 inside code block ignored",
			input:    "```\n# Not a title\n```\n# Real Title",
			expected: "Real Title",
		},
		{
			name:     "first h1 wins",
			input:    "# First\n# Second",
			expected: "First",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "h1 with inline code",
			input:    "# The `main` Function",
			expected: "The main Function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTitle(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractTitle():\n  got:  %q\n  want: %q", got, tt.expected)
			}
		})
	}
}
