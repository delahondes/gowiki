package storage

import "testing"

func TestMatchPathFilters_LeadingSlashConsistency(t *testing.T) {
	// Regression: the mcpserver used to pass filter paths WITHOUT a
	// leading slash to IncludePaths, but changelog PagePath entries carry
	// one — so HasPrefix never matched and `path_prefix=/regulatory/qms/`
	// silently returned zero results. Both sides must use the canonical
	// (leading-slash) form.
	cases := []struct {
		name     string
		pagePath string
		include  []string
		exclude  []string
		want     bool
	}{
		{
			name:     "leading-slash prefix matches leading-slash page",
			pagePath: "/regulatory/qms/dir/mq02",
			include:  []string{"/regulatory/qms/"},
			want:     true,
		},
		{
			name:     "no-leading-slash prefix would silently miss (documents shape)",
			pagePath: "/regulatory/qms/dir/mq02",
			include:  []string{"regulatory/qms/"},
			want:     false,
		},
		{
			name:     "exclude leading-slash prefix wins over include",
			pagePath: "/regulatory/qms/secret",
			include:  []string{"/regulatory/"},
			exclude:  []string{"/regulatory/qms/"},
			want:     false,
		},
		{
			name:     "empty include and exclude → matches",
			pagePath: "/anywhere",
			want:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchPathFilters(tc.pagePath, tc.include, tc.exclude)
			if got != tc.want {
				t.Errorf("matchPathFilters(%q, %v, %v) = %v, want %v",
					tc.pagePath, tc.include, tc.exclude, got, tc.want)
			}
		})
	}
}
