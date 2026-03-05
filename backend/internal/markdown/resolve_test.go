package markdown

import "testing"

func TestResolvePath(t *testing.T) {
	tests := []struct {
		name     string
		pagePath string
		refPath  string
		want     string
	}{
		// Absolute paths.
		{
			name:     "absolute path kept with leading slash",
			pagePath: "/docs/intro",
			refPath:  "/foo/bar.png",
			want:     "/foo/bar.png",
		},
		{
			name:     "absolute path at root",
			pagePath: "/docs/intro",
			refPath:  "/logo.png",
			want:     "/logo.png",
		},
		{
			name:     "absolute path cleaned",
			pagePath: "/docs/intro",
			refPath:  "/foo/../bar.png",
			want:     "/bar.png",
		},
		{
			name:     "absolute path with double slash",
			pagePath: "/docs/intro",
			refPath:  "//foo/bar.png",
			want:     "/foo/bar.png",
		},

		// Relative paths with dot prefix.
		{
			name:     "dot-relative in namespace",
			pagePath: "/docs/intro",
			refPath:  "./diagram.png",
			want:     "/docs/diagram.png",
		},
		{
			name:     "dot-relative at root page",
			pagePath: "/start",
			refPath:  "./logo.png",
			want:     "/logo.png",
		},
		{
			name:     "parent-relative",
			pagePath: "/docs/intro",
			refPath:  "../logo.png",
			want:     "/logo.png",
		},
		{
			name:     "parent-relative deeply nested",
			pagePath: "/a/b/c/page",
			refPath:  "../../img.png",
			want:     "/a/img.png",
		},

		// Bare relative paths (no prefix).
		{
			name:     "bare relative in namespace",
			pagePath: "/docs/intro",
			refPath:  "foo.png",
			want:     "/docs/foo.png",
		},
		{
			name:     "bare relative at root",
			pagePath: "/start",
			refPath:  "foo.png",
			want:     "/foo.png",
		},
		{
			name:     "bare relative with subpath",
			pagePath: "/docs/intro",
			refPath:  "images/pic.png",
			want:     "/docs/images/pic.png",
		},

		// Edge cases.
		{
			name:     "empty refPath",
			pagePath: "/docs/intro",
			refPath:  "",
			want:     "",
		},
		{
			name:     "whitespace-only refPath",
			pagePath: "/docs/intro",
			refPath:  "   ",
			want:     "",
		},
		{
			name:     "escape above root returns empty",
			pagePath: "/start",
			refPath:  "../../etc/passwd",
			want:     "",
		},
		{
			name:     "absolute just slash",
			pagePath: "/start",
			refPath:  "/",
			want:     "",
		},
		{
			name:     "page path resolution for include",
			pagePath: "/docs/intro",
			refPath:  "./subpage",
			want:     "/docs/subpage",
		},
		{
			name:     "absolute page path for include",
			pagePath: "/docs/intro",
			refPath:  "/footer",
			want:     "/footer",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolvePath(tc.pagePath, tc.refPath)
			if got != tc.want {
				t.Errorf("ResolvePath(%q, %q) = %q, want %q", tc.pagePath, tc.refPath, got, tc.want)
			}
		})
	}
}
