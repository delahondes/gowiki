package storage

import "testing"

// CanonicalPath is a tiny function called from ~every canonical-path
// decision in the app. Pin every rule listed in
// specs/canonical_page_names.md.
func TestCanonicalPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"root storage index → /", "index", "/"},
		{"empty string → /", "", "/"},
		{"root with leading slash", "/", "/"},
		{"leading slash + index", "/index", "/"},
		{"leaf page adds slash", "page", "/page"},
		{"nested leaf unchanged", "docs/guide", "/docs/guide"},
		{"leaf page with leading slash", "/page", "/page"},
		{"namespace index storage → canonical", "docs/index", "/docs/"},
		{"deep namespace index", "a/b/index", "/a/b/"},
		{"namespace already canonical (trailing slash)", "docs/", "/docs/"},
		{"deep leaf with leading slash", "/a/b/c", "/a/b/c"},
		{"single-segment index-suffix false-positive is a leaf", "myindex", "/myindex"},
		// "index" only strips when it's a full trailing segment, so "docs/myindex"
		// stays a leaf. The rule is `HasSuffix("/index")`, which requires the
		// slash — pinning this so a future refactor doesn't broaden the match.
		{"index as substring of a filename is not stripped", "docs/myindex", "/docs/myindex"},
		{"deep namespace with intermediate index-like segment", "index/child", "/index/child"},
		{"namespace named 'index' at leaf", "docs/index/index", "/docs/index/"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CanonicalPath(tc.in); got != tc.want {
				t.Errorf("CanonicalPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
