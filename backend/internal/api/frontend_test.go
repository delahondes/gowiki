package api

import (
	"testing"
)

// canServeAsAttachmentPath is a pure helper — the frontend fallback
// only serves paths that have an extension AND aren't the app entry
// point. Everything else falls through to the SPA (index.html).
func TestCanServeAsAttachmentPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"index.html", false},         // reserved app entry point
		{"page", false},               // no extension → not an attachment
		{"docs/guide", false},         // no extension
		{"docs/", false},              // no basename
		{"image.png", true},           // extension → serve as media
		{"docs/attachment.pdf", true}, // nested with extension
		{"path/with.dots/in-it/file.txt", true},
		{"trailing.", true}, // trailing "." IS an extension per path.Ext — the pass-through accepts it
	}
	for _, c := range cases {
		if got := canServeAsAttachmentPath(c.in); got != c.want {
			t.Errorf("canServeAsAttachmentPath(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
