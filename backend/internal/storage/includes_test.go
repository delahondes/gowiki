package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIncludeIndex_SelfInclude(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())

	cycle, hasCycle := idx.DetectCycle("A", []string{"A"})
	if !hasCycle {
		t.Fatal("expected cycle for self-include")
	}
	expectCycle(t, cycle, []string{"A", "A"})
}

func TestIncludeIndex_SimpleCycle(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())
	// B already includes A
	idx.UpdatePage("B", []string{"A"})

	// Now A wants to include B → cycle: A → B → A
	cycle, hasCycle := idx.DetectCycle("A", []string{"B"})
	if !hasCycle {
		t.Fatal("expected cycle")
	}
	expectCycle(t, cycle, []string{"A", "B", "A"})
}

func TestIncludeIndex_TransitiveCycle(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())
	// B includes C, C includes A
	idx.UpdatePage("B", []string{"C"})
	idx.UpdatePage("C", []string{"A"})

	// Now A wants to include B → cycle: A → B → C → A
	cycle, hasCycle := idx.DetectCycle("A", []string{"B"})
	if !hasCycle {
		t.Fatal("expected cycle")
	}
	expectCycle(t, cycle, []string{"A", "B", "C", "A"})
}

func TestIncludeIndex_DeepChainNoCycle(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())
	idx.UpdatePage("B", []string{"C"})
	idx.UpdatePage("C", []string{"D"})

	// A includes B → no cycle (A→B→C→D, D doesn't include A)
	cycle, hasCycle := idx.DetectCycle("A", []string{"B"})
	if hasCycle {
		t.Fatalf("unexpected cycle: %v", cycle)
	}
}

func TestIncludeIndex_DiamondNoCycle(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())
	// A→B, A→C, B→D, C→D — diamond shape, not a cycle
	idx.UpdatePage("B", []string{"D"})
	idx.UpdatePage("C", []string{"D"})

	cycle, hasCycle := idx.DetectCycle("A", []string{"B", "C"})
	if hasCycle {
		t.Fatalf("unexpected cycle in diamond: %v", cycle)
	}
}

func TestIncludeIndex_MultipleIncludesOneCycle(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())
	// B includes A, D is a leaf
	idx.UpdatePage("B", []string{"A"})
	idx.UpdatePage("D", []string{})

	// A wants to include both C and B. C is fine but B creates a cycle.
	cycle, hasCycle := idx.DetectCycle("A", []string{"C", "B"})
	if !hasCycle {
		t.Fatal("expected cycle through B")
	}
	// The cycle should go through B
	expectCycle(t, cycle, []string{"A", "B", "A"})
}

func TestIncludeIndex_EmptyIncludes(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())
	idx.UpdatePage("B", []string{"A"})

	cycle, hasCycle := idx.DetectCycle("A", []string{})
	if hasCycle {
		t.Fatalf("unexpected cycle with empty includes: %v", cycle)
	}
}

func TestIncludeIndex_UpdatePageAndDetect(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())

	// Initially no cycle: A includes B
	cycle, hasCycle := idx.DetectCycle("A", []string{"B"})
	if hasCycle {
		t.Fatalf("unexpected cycle: %v", cycle)
	}
	idx.UpdatePage("A", []string{"B"})

	// Now B wants to include A → cycle
	cycle, hasCycle = idx.DetectCycle("B", []string{"A"})
	if !hasCycle {
		t.Fatal("expected cycle after update")
	}
	expectCycle(t, cycle, []string{"B", "A", "B"})
}

func TestIncludeIndex_UpdatePageRemovesEntry(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())
	idx.UpdatePage("A", []string{"B"})

	if _, ok := idx.PageToIncludes["A"]; !ok {
		t.Fatal("expected A to be in the index")
	}

	idx.UpdatePage("A", []string{})
	if _, ok := idx.PageToIncludes["A"]; ok {
		t.Fatal("expected A to be removed from the index")
	}
}

func TestIncludeIndex_LoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Create and populate an index, then save.
	idx := NewIncludeIndex(dir)
	idx.UpdatePage("index", []string{"sidebar", "footer"})
	idx.UpdatePage("docs/intro", []string{"docs/shared-header"})

	if err := idx.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify the file exists.
	filePath := filepath.Join(dir, "_includes.json")
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	// Load into a new index and compare.
	idx2 := NewIncludeIndex(dir)
	if err := idx2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(idx2.PageToIncludes) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(idx2.PageToIncludes))
	}
	expectSlice(t, idx2.PageToIncludes["index"], []string{"sidebar", "footer"})
	expectSlice(t, idx2.PageToIncludes["docs/intro"], []string{"docs/shared-header"})
}

func TestIncludeIndex_LoadMissingFile(t *testing.T) {
	dir := t.TempDir()
	idx := NewIncludeIndex(dir)
	if err := idx.Load(); err != nil {
		t.Fatalf("load missing file should not error: %v", err)
	}
	if len(idx.PageToIncludes) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(idx.PageToIncludes))
	}
}

func TestIncludeIndex_ThreadSafety(t *testing.T) {
	idx := NewIncludeIndex(t.TempDir())

	var wg sync.WaitGroup
	// Concurrent updates
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			page := "page" + string(rune('A'+n%26))
			idx.UpdatePage(page, []string{"target"})
		}(i)
	}

	// Concurrent cycle detections
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			page := "page" + string(rune('A'+n%26))
			idx.DetectCycle(page, []string{"other"})
		}(i)
	}

	wg.Wait()

	// Just verify no panics or data races occurred.
	// The index should have some entries.
	if len(idx.PageToIncludes) == 0 {
		t.Fatal("expected some entries after concurrent updates")
	}
}

func TestIncludeIndex_ExistingCycleDefensive(t *testing.T) {
	// If the existing index already has a cycle (B→C→B),
	// DFS should not loop infinitely when exploring from A.
	idx := NewIncludeIndex(t.TempDir())
	idx.UpdatePage("B", []string{"C"})
	idx.UpdatePage("C", []string{"B"})

	// A includes B — no cycle involving A, even though B↔C form a cycle.
	cycle, hasCycle := idx.DetectCycle("A", []string{"B"})
	if hasCycle {
		t.Fatalf("unexpected cycle: %v — the existing B↔C cycle should not affect A", cycle)
	}
}

// --- helpers ---

func expectCycle(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("cycle length: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cycle[%d]: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func expectSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice length: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}
