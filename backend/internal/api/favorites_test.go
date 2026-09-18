package api

import "testing"

func TestNormalizeFavoritePath(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/foo/bar", "/foo/bar"},
		{"foo/bar", "/foo/bar"},
		{"/foo/", "/foo/"},
		{"/foo/index", "/foo/"},
		{"//foo//bar", "/foo/bar"},
		{"/", "/"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := normalizeFavoritePath(tc.in); got != tc.want {
			t.Errorf("normalizeFavoritePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
