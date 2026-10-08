package todo

import "testing"

// pagePathAltForm is used by ListForPage to match task source_page under
// both namespace-index spellings (/foo and /foo/). Historical tasks
// were persisted under whichever form the caller passed at creation,
// so the dual-match is what lets reviewflow drift (e.g. tasks created
// in early 2026 under `/qms/dir/sop02` while the current path is
// `/qms/dir/sop02/`) be found by the current reconcilers.
func TestPagePathAltForm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"/foo", "/foo/"},
		{"/foo/", "/foo"},
		{"/foo/bar", "/foo/bar/"},
		{"/foo/bar/", "/foo/bar"},
		{"/", "/"},
		{"", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := pagePathAltForm(tc.in); got != tc.want {
				t.Errorf("pagePathAltForm(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
