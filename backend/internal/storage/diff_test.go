package storage

import (
	"strings"
	"testing"
)

// countOps returns how many hunks of each op the diff produced.
func countOps(hunks []DiffHunk) map[string]int {
	out := map[string]int{"equal": 0, "insert": 0, "delete": 0}
	for _, h := range hunks {
		out[h.Op]++
	}
	return out
}

func TestDiffLines_IdenticalStringsYieldNoChangeHunks(t *testing.T) {
	t.Parallel()
	text := "alpha\nbeta\ngamma\n"
	hunks := DiffLines(text, text)
	counts := countOps(hunks)
	if counts["insert"] != 0 {
		t.Errorf("insert hunks on identical input: %d", counts["insert"])
	}
	if counts["delete"] != 0 {
		t.Errorf("delete hunks on identical input: %d", counts["delete"])
	}
	// Reconstruct: joining all equal content must yield the original text.
	var joined strings.Builder
	for _, h := range hunks {
		if h.Op == "equal" {
			joined.WriteString(h.Content)
		}
	}
	if joined.String() != text {
		t.Errorf("equal hunks did not reconstruct input\ngot:  %q\nwant: %q", joined.String(), text)
	}
}

func TestDiffLines_MiddleLineChangeProducesInsertAndDelete(t *testing.T) {
	t.Parallel()
	a := "alpha\nbeta\ngamma\n"
	b := "alpha\nBETA\ngamma\n"
	hunks := DiffLines(a, b)
	counts := countOps(hunks)
	if counts["insert"] == 0 {
		t.Errorf("expected at least one insert hunk, got %v", counts)
	}
	if counts["delete"] == 0 {
		t.Errorf("expected at least one delete hunk, got %v", counts)
	}
	// The literal changed content should appear in the corresponding op.
	sawDeleteBeta := false
	sawInsertBETA := false
	for _, h := range hunks {
		if h.Op == "delete" && strings.Contains(h.Content, "beta") {
			sawDeleteBeta = true
		}
		if h.Op == "insert" && strings.Contains(h.Content, "BETA") {
			sawInsertBETA = true
		}
	}
	if !sawDeleteBeta {
		t.Errorf("no delete hunk containing 'beta': %+v", hunks)
	}
	if !sawInsertBETA {
		t.Errorf("no insert hunk containing 'BETA': %+v", hunks)
	}
}

func TestDiffLines_WholeContentReplacement(t *testing.T) {
	t.Parallel()
	a := "old-1\nold-2\nold-3\n"
	b := "new-1\nnew-2\nnew-3\n"
	hunks := DiffLines(a, b)
	// Every content line must show up in either a delete (from a) or an
	// insert (into b), and nothing should have stayed equal.
	sawInsert, sawDelete, sawEqualContent := false, false, false
	for _, h := range hunks {
		switch h.Op {
		case "insert":
			sawInsert = true
		case "delete":
			sawDelete = true
		case "equal":
			if strings.TrimSpace(h.Content) != "" {
				sawEqualContent = true
			}
		}
	}
	if !sawDelete || !sawInsert {
		t.Errorf("expected both delete and insert; got insert=%v delete=%v", sawInsert, sawDelete)
	}
	if sawEqualContent {
		t.Errorf("no line should have been equal, but at least one was")
	}
}

func TestDiffLines_EmptyOnEitherSide(t *testing.T) {
	t.Parallel()

	// Empty → text: everything is an insert.
	hunks := DiffLines("", "alpha\nbeta\n")
	c := countOps(hunks)
	if c["delete"] != 0 {
		t.Errorf("empty→text produced delete hunks: %v", c)
	}
	if c["insert"] == 0 {
		t.Errorf("empty→text produced no insert hunks: %v", c)
	}

	// Text → empty: everything is a delete.
	hunks = DiffLines("alpha\nbeta\n", "")
	c = countOps(hunks)
	if c["insert"] != 0 {
		t.Errorf("text→empty produced insert hunks: %v", c)
	}
	if c["delete"] == 0 {
		t.Errorf("text→empty produced no delete hunks: %v", c)
	}

	// Empty → empty: no hunks at all (splitKeepNewlines drops empty).
	hunks = DiffLines("", "")
	if len(hunks) != 0 {
		t.Errorf("empty→empty produced %d hunks, want 0: %+v", len(hunks), hunks)
	}
}

// Trailing-newline handling is where line-based diffs classically get
// confused. Pin the current behaviour: adding a final newline is a
// change that surfaces in the diff (not a silent equality).
func TestDiffLines_TrailingNewlineHandling(t *testing.T) {
	t.Parallel()
	a := "alpha\nbeta"   // no trailing newline
	b := "alpha\nbeta\n" // trailing newline added
	hunks := DiffLines(a, b)
	// Some diff must have happened — either an insert or a delete or
	// both around the last line. If neither fires, we've silently
	// merged the two into "equal" which would hide a real change.
	c := countOps(hunks)
	if c["insert"] == 0 && c["delete"] == 0 {
		t.Errorf("trailing-newline change produced no diff at all: %+v", hunks)
	}
}

// splitKeepNewlines is exercised implicitly by DiffLines above, but it
// has one interesting shape worth pinning directly: a text that ends
// with a newline should NOT emit a trailing empty-line item.
func TestSplitKeepNewlines_NoTrailingEmpty(t *testing.T) {
	t.Parallel()
	got := splitKeepNewlines("alpha\nbeta\n")
	if len(got) != 2 {
		t.Fatalf("splitKeepNewlines returned %d items, want 2: %#v", len(got), got)
	}
	if got[0] != "alpha\n" || got[1] != "beta\n" {
		t.Errorf("unexpected split: %#v", got)
	}

	// Trailing content without newline is kept verbatim.
	got = splitKeepNewlines("alpha\nbeta")
	if len(got) != 2 || got[1] != "beta" {
		t.Errorf("no-trailing-newline split unexpected: %#v", got)
	}

	// Empty string returns nil.
	if got := splitKeepNewlines(""); got != nil {
		t.Errorf("empty input should return nil, got %#v", got)
	}
}
