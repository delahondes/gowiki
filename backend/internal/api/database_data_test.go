package api

import "testing"

func TestResolvePageFolder(t *testing.T) {
	tests := []struct {
		name       string
		pageFolder string
		rowID      int
		fields     map[string]any
		want       string
	}{
		{
			name:       "no token → id",
			pageFolder: "/regulatory/qms/soft/server",
			rowID:      24,
			fields:     map[string]any{"server_name": "littlebrother2"},
			want:       "/regulatory/qms/soft/server/24",
		},
		{
			name:       "@id token",
			pageFolder: "/x/@id",
			rowID:      7,
			fields:     nil,
			want:       "/x/7",
		},
		{
			name:       "@field with value",
			pageFolder: "/regulatory/qms/soft/server/@server_name",
			rowID:      24,
			fields:     map[string]any{"server_name": "littlebrother2"},
			want:       "/regulatory/qms/soft/server/littlebrother2",
		},
		{
			name:       "@field with empty string → id fallback",
			pageFolder: "/x/@server_name",
			rowID:      42,
			fields:     map[string]any{"server_name": ""},
			want:       "/x/42",
		},
		{
			name:       "@field with nil → id fallback",
			pageFolder: "/x/@server_name",
			rowID:      42,
			fields:     map[string]any{"server_name": nil},
			want:       "/x/42",
		},
		{
			name:       "@field missing → id fallback",
			pageFolder: "/x/@server_name",
			rowID:      42,
			fields:     map[string]any{"other": "y"},
			want:       "/x/42",
		},
		{
			name:       "underscore is preserved",
			pageFolder: "/x/@name",
			rowID:      5,
			fields:     map[string]any{"name": "backup_v2"},
			want:       "/x/backup_v2",
		},
		{
			name:       "hyphen is preserved",
			pageFolder: "/x/@name",
			rowID:      5,
			fields:     map[string]any{"name": "alpha-nodex"},
			want:       "/x/alpha-nodex",
		},
		{
			name:       "uppercase is lowercased",
			pageFolder: "/x/@name",
			rowID:      5,
			fields:     map[string]any{"name": "MyServer"},
			want:       "/x/myserver",
		},
		{
			name:       "spaces and punctuation collapse to hyphen",
			pageFolder: "/x/@name",
			rowID:      5,
			fields:     map[string]any{"name": "My Fancy Server!"},
			want:       "/x/my-fancy-server",
		},
		{
			name:       "value that slugifies to empty → id fallback",
			pageFolder: "/x/@name",
			rowID:      99,
			fields:     map[string]any{"name": "---"},
			want:       "/x/99",
		},
		{
			name:       "trailing slash on folder is stripped",
			pageFolder: "/x/",
			rowID:      3,
			fields:     nil,
			want:       "/x/3",
		},
		{
			name:       "folder missing leading slash gets one",
			pageFolder: "x",
			rowID:      3,
			fields:     nil,
			want:       "/x/3",
		},
		{
			name:       "multiple tokens",
			pageFolder: "/@category/@server_name",
			rowID:      10,
			fields:     map[string]any{"category": "Office", "server_name": "forge"},
			want:       "/office/forge",
		},
		{
			name:       "numeric field value",
			pageFolder: "/x/@n",
			rowID:      1,
			fields:     map[string]any{"n": 42},
			want:       "/x/42",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePageFolder(tc.pageFolder, tc.rowID, tc.fields)
			if got != tc.want {
				t.Fatalf("resolvePageFolder(%q, %d, %v) = %q, want %q",
					tc.pageFolder, tc.rowID, tc.fields, got, tc.want)
			}
		})
	}
}
