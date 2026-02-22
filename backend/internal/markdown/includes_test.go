package markdown

import "testing"

func TestExtractIncludes(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		pagePath string
		want     []string
	}{
		{
			name:     "absolute include",
			content:  "{include path=/footer}",
			pagePath: "docs/intro",
			want:     []string{"footer"},
		},
		{
			name:     "relative include with dot",
			content:  "{include path=./subpage}",
			pagePath: "docs/intro",
			want:     []string{"docs/subpage"},
		},
		{
			name:     "bare relative include",
			content:  "{include path=sidebar}",
			pagePath: "docs/intro",
			want:     []string{"docs/sidebar"},
		},
		{
			name:     "quoted include double quotes",
			content:  `{include path="./nav"}`,
			pagePath: "docs/intro",
			want:     []string{"docs/nav"},
		},
		{
			name:     "quoted include single quotes",
			content:  `{include path='./nav'}`,
			pagePath: "docs/intro",
			want:     []string{"docs/nav"},
		},
		{
			name:     "include with leading whitespace",
			content:  "  {include path=/footer}",
			pagePath: "docs/intro",
			want:     []string{"footer"},
		},
		{
			name:     "include with trailing whitespace",
			content:  "{include path=/footer}  ",
			pagePath: "docs/intro",
			want:     []string{"footer"},
		},
		{
			name:     "include with extra space before closing brace",
			content:  "{include path=/footer }",
			pagePath: "docs/intro",
			want:     []string{"footer"},
		},
		{
			name:     "multiple includes deduplicated and sorted",
			content:  "{include path=/z}\n{include path=/a}\n{include path=/z}",
			pagePath: "page",
			want:     []string{"a", "z"},
		},
		{
			name: "include inside code block skipped",
			content: "```\n{include path=/secret}\n```\n{include path=/footer}",
			pagePath: "page",
			want:     []string{"footer"},
		},
		{
			name:     "include not on its own line ignored",
			content:  "text {include path=/footer} more text",
			pagePath: "page",
			want:     nil,
		},
		{
			name:     "empty content",
			content:  "",
			pagePath: "page",
			want:     nil,
		},
		{
			name:     "no includes",
			content:  "Just regular text\nnothing special",
			pagePath: "page",
			want:     nil,
		},
		{
			name:     "parent relative include",
			content:  "{include path=../shared/nav}",
			pagePath: "docs/guides/intro",
			want:     []string{"docs/shared/nav"},
		},
		{
			name:     "include at root page",
			content:  "{include path=./sidebar}",
			pagePath: "start",
			want:     []string{"sidebar"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractIncludes(tc.content, tc.pagePath)
			if !sliceEqual(got, tc.want) {
				t.Errorf("ExtractIncludes() = %v, want %v", got, tc.want)
			}
		})
	}
}
