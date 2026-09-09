package mcpserver

import (
	"strings"
	"testing"

	"gowiki/backend/internal/storage"
)

func TestApplyEdits(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		edits    []editSpec
		want     string
		wantErr  string // substring to expect in the error; empty = no error
	}{
		{
			name:    "single unique match — replaces once",
			content: "hello world",
			edits:   []editSpec{{Old: "world", New: "there"}},
			want:    "hello there",
		},
		{
			name:    "sequential edits apply in order",
			content: "foo bar",
			edits: []editSpec{
				{Old: "foo", New: "baz"},
				{Old: "baz", New: "qux"},
			},
			want: "qux bar",
		},
		{
			name:    "zero matches — refused",
			content: "hello world",
			edits:   []editSpec{{Old: "missing", New: "x"}},
			wantErr: "not found",
		},
		{
			name:    "two matches without replace_all — refused",
			content: "aa aa",
			edits:   []editSpec{{Old: "aa", New: "b"}},
			wantErr: "appears 2 times",
		},
		{
			name:    "two matches with replace_all — replaces both",
			content: "aa aa",
			edits:   []editSpec{{Old: "aa", New: "b", ReplaceAll: true}},
			want:    "b b",
		},
		{
			name:    "empty old — refused",
			content: "hello",
			edits:   []editSpec{{Old: "", New: "x"}},
			wantErr: "must be non-empty",
		},
		{
			name:    "multi-line anchor works",
			content: "line1\nline2\nline3",
			edits:   []editSpec{{Old: "line1\nline2", New: "merged"}},
			want:    "merged\nline3",
		},
		{
			name:    "second edit fails — first edit not applied (atomicity)",
			content: "foo bar",
			edits: []editSpec{
				{Old: "foo", New: "baz"}, // would succeed if applied
				{Old: "missing", New: "x"}, // will fail
			},
			wantErr: "edit 1",
		},
		{
			name:    "delete via empty new",
			content: "keep DROPME still",
			edits:   []editSpec{{Old: "DROPME ", New: ""}},
			want:    "keep still",
		},
		{
			name:    "second edit sees first edit's output",
			content: "aaa",
			edits: []editSpec{
				{Old: "aaa", New: "bbb"},
				{Old: "bbb", New: "ccc"},
			},
			want: "ccc",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := applyEdits(tc.content, tc.edits)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (result=%q)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrimHunksToContext(t *testing.T) {
	// Build a hunk list that mirrors what DiffLines produces: a mix of
	// equal/insert/delete, one line per hunk.
	mk := func(op, s string) storage.DiffHunk { return storage.DiffHunk{Op: op, Content: s} }
	full := []storage.DiffHunk{
		mk("equal", "0"),
		mk("equal", "1"),
		mk("equal", "2"),
		mk("equal", "3"),
		mk("insert", "changed A"),
		mk("delete", "changed B"),
		mk("equal", "4"),
		mk("equal", "5"),
		mk("equal", "6"),
		mk("equal", "7"),
		mk("equal", "8"),
		mk("insert", "changed C"),
		mk("equal", "9"),
		mk("equal", "10"),
		mk("equal", "11"),
	}

	tests := []struct {
		name        string
		context     int
		wantOmitted int
		wantOps     string // joined op letters: e=equal, i=insert, d=delete
	}{
		{
			name:        "context=0 → full diff returned, 0 omitted",
			context:     0,
			wantOmitted: 0,
			wantOps:     "eeeeidee eeeieee", // just the whole thing (spaces added below)
		},
		{
			name:        "context=1 → change hunks + 1 line each side",
			context:     1,
			wantOmitted: 8, // 11 equal total, 3 kept (idx 3, 6, 12)
			wantOps:     "eideeie",
		},
		{
			name:        "context=2 → change hunks + 2 lines each side",
			context:     2,
			wantOmitted: 4, // 12 equals in input; 8 kept (idx 2,3,6,7,9,10,12,13); 4 omitted
			wantOps:     "eeideeeeiee",
		},
		{
			name:        "context large enough to bridge → full diff",
			context:     20,
			wantOmitted: 0,
			wantOps:     "eeeeidee eeeieee",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, omitted := trimHunksToContext(full, tc.context)
			if omitted != tc.wantOmitted {
				t.Fatalf("omitted: got %d, want %d (result=%v)", omitted, tc.wantOmitted, opsOf(out))
			}
			gotOps := opsOf(out)
			want := strings.ReplaceAll(tc.wantOps, " ", "")
			if gotOps != want {
				t.Fatalf("ops: got %q, want %q", gotOps, want)
			}
		})
	}
}

func opsOf(hunks []storage.DiffHunk) string {
	var b strings.Builder
	for _, h := range hunks {
		switch h.Op {
		case "equal":
			b.WriteByte('e')
		case "insert":
			b.WriteByte('i')
		case "delete":
			b.WriteByte('d')
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// Atomicity: when edit 1 succeeds validation but edit 2 fails, the entire
// call must be refused — no partial application should reach the caller.
// The applyEdits function is pure, so atomicity here means "return an
// unaltered result on any error". The MCP tool wrapper is what guarantees
// no store.Put on error; this test just verifies applyEdits never returns
// a partially-applied buffer.
func TestApplyEdits_AtomicityOnFailure(t *testing.T) {
	got, _, err := applyEdits("foo bar", []editSpec{
		{Old: "foo", New: "baz"},
		{Old: "not-found", New: "x"},
	})
	if err == nil {
		t.Fatalf("expected error, got nil (result=%q)", got)
	}
	if got != "" {
		t.Fatalf("expected empty result on error, got %q", got)
	}
}
