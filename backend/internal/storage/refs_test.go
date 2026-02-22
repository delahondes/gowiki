package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestUpdatePage_AddRefs(t *testing.T) {
	r := NewRefIndex(t.TempDir())

	r.UpdatePage("index", []string{"images/logo.png", "images/banner.png"})

	assertStringSlice(t, r.PageToMedia["index"], []string{"images/banner.png", "images/logo.png"})
	assertStringSlice(t, r.MediaToPages["images/logo.png"], []string{"index"})
	assertStringSlice(t, r.MediaToPages["images/banner.png"], []string{"index"})
}

func TestUpdatePage_ReplaceRefs(t *testing.T) {
	r := NewRefIndex(t.TempDir())

	r.UpdatePage("index", []string{"images/old.png", "images/keep.png"})
	r.UpdatePage("index", []string{"images/new.png", "images/keep.png"})

	assertStringSlice(t, r.PageToMedia["index"], []string{"images/keep.png", "images/new.png"})
	assertStringSlice(t, r.MediaToPages["images/keep.png"], []string{"index"})
	assertStringSlice(t, r.MediaToPages["images/new.png"], []string{"index"})
	if _, ok := r.MediaToPages["images/old.png"]; ok {
		t.Error("old.png should have been removed from reverse map")
	}
}

func TestUpdatePage_RemoveAllRefs(t *testing.T) {
	r := NewRefIndex(t.TempDir())

	r.UpdatePage("index", []string{"images/logo.png"})
	r.UpdatePage("index", nil)

	if _, ok := r.PageToMedia["index"]; ok {
		t.Error("page should be removed from forward map when refs are empty")
	}
	if _, ok := r.MediaToPages["images/logo.png"]; ok {
		t.Error("media should be removed from reverse map when no pages reference it")
	}
}

func TestReverseMapConsistency_MultiplePages(t *testing.T) {
	r := NewRefIndex(t.TempDir())

	r.UpdatePage("page1", []string{"shared.png", "only1.png"})
	r.UpdatePage("page2", []string{"shared.png", "only2.png"})

	assertStringSlice(t, r.MediaToPages["shared.png"], []string{"page1", "page2"})
	assertStringSlice(t, r.MediaToPages["only1.png"], []string{"page1"})
	assertStringSlice(t, r.MediaToPages["only2.png"], []string{"page2"})

	// Remove page1's refs
	r.UpdatePage("page1", nil)
	assertStringSlice(t, r.MediaToPages["shared.png"], []string{"page2"})
	if _, ok := r.MediaToPages["only1.png"]; ok {
		t.Error("only1.png should have been removed")
	}
}

func TestGetReferencingPages(t *testing.T) {
	r := NewRefIndex(t.TempDir())

	pages := r.GetReferencingPages("nonexistent.png")
	if pages != nil {
		t.Errorf("expected nil for nonexistent media, got %v", pages)
	}

	r.UpdatePage("page1", []string{"shared.png"})
	r.UpdatePage("page2", []string{"shared.png"})

	pages = r.GetReferencingPages("shared.png")
	assertStringSlice(t, pages, []string{"page1", "page2"})

	// Verify returned slice is a copy
	pages[0] = "mutated"
	original := r.GetReferencingPages("shared.png")
	if original[0] == "mutated" {
		t.Error("GetReferencingPages should return a copy")
	}
}

func TestFindOrphans(t *testing.T) {
	contentDir := t.TempDir()

	// Create some files
	mkFile(t, contentDir, "page.md", "# hello")
	mkFile(t, contentDir, "images/logo.png", "png-data")
	mkFile(t, contentDir, "images/orphan.jpg", "jpg-data")
	mkFile(t, contentDir, "docs/diagram.svg", "svg-data")

	r := NewRefIndex(t.TempDir())
	r.UpdatePage("index", []string{"images/logo.png"})

	orphans, err := r.FindOrphans(contentDir)
	if err != nil {
		t.Fatal(err)
	}

	assertStringSlice(t, orphans, []string{"docs/diagram.svg", "images/orphan.jpg"})
}

func TestFindOrphans_NoOrphans(t *testing.T) {
	contentDir := t.TempDir()

	mkFile(t, contentDir, "images/logo.png", "png-data")
	mkFile(t, contentDir, "page.md", "# hello")

	r := NewRefIndex(t.TempDir())
	r.UpdatePage("index", []string{"images/logo.png"})

	orphans, err := r.FindOrphans(contentDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Errorf("expected no orphans, got %v", orphans)
	}
}

func TestFindNewlyOrphaned(t *testing.T) {
	r := NewRefIndex(t.TempDir())

	// page1 and page2 both reference shared.png; page1 also references only1.png
	r.UpdatePage("page1", []string{"shared.png", "only1.png"})
	r.UpdatePage("page2", []string{"shared.png"})

	// Simulate page1 dropping all refs (after UpdatePage has already been called).
	// At this point, the forward/reverse maps reflect the state AFTER the update.
	// We need to test FindNewlyOrphaned against the current state of the reverse map.

	// First, update page1 to have no refs.
	oldRefs := []string{"shared.png", "only1.png"}
	newRefs := []string(nil)
	r.UpdatePage("page1", newRefs)

	orphaned := r.FindNewlyOrphaned(oldRefs, newRefs)
	// shared.png still referenced by page2, so only only1.png is newly orphaned.
	assertStringSlice(t, orphaned, []string{"only1.png"})
}

func TestFindNewlyOrphaned_AllKept(t *testing.T) {
	r := NewRefIndex(t.TempDir())
	r.UpdatePage("page1", []string{"a.png", "b.png"})

	orphaned := r.FindNewlyOrphaned([]string{"a.png", "b.png"}, []string{"a.png", "b.png"})
	if len(orphaned) != 0 {
		t.Errorf("expected no orphans when refs unchanged, got %v", orphaned)
	}
}

func TestFindNewlyOrphaned_RefsAdded(t *testing.T) {
	r := NewRefIndex(t.TempDir())
	r.UpdatePage("page1", []string{"a.png", "b.png"})

	orphaned := r.FindNewlyOrphaned([]string{"a.png"}, []string{"a.png", "b.png"})
	if len(orphaned) != 0 {
		t.Errorf("expected no orphans when only adding refs, got %v", orphaned)
	}
}

func TestLoadSave_RoundTrip(t *testing.T) {
	metaDir := t.TempDir()

	r1 := NewRefIndex(metaDir)
	r1.UpdatePage("index", []string{"images/logo.png", "screenshots/demo.png"})
	r1.UpdatePage("docs/intro", []string{"docs/diagram.svg"})

	if err := r1.Save(); err != nil {
		t.Fatal(err)
	}

	r2 := NewRefIndex(metaDir)
	if err := r2.Load(); err != nil {
		t.Fatal(err)
	}

	assertStringSlice(t, r2.PageToMedia["index"], []string{"images/logo.png", "screenshots/demo.png"})
	assertStringSlice(t, r2.PageToMedia["docs/intro"], []string{"docs/diagram.svg"})
	assertStringSlice(t, r2.MediaToPages["images/logo.png"], []string{"index"})
	assertStringSlice(t, r2.MediaToPages["screenshots/demo.png"], []string{"index"})
	assertStringSlice(t, r2.MediaToPages["docs/diagram.svg"], []string{"docs/intro"})
}

func TestLoad_MissingFile(t *testing.T) {
	r := NewRefIndex(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load should succeed on missing file, got: %v", err)
	}
	if len(r.PageToMedia) != 0 || len(r.MediaToPages) != 0 {
		t.Error("maps should be empty after loading missing file")
	}
}

func TestLoad_CorruptFile(t *testing.T) {
	metaDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(metaDir, refIndexFile), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRefIndex(metaDir)
	if err := r.Load(); err == nil {
		t.Fatal("Load should fail on corrupt JSON")
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRefIndex(t.TempDir())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			page := "page" + string(rune('A'+n%26))
			r.UpdatePage(page, []string{"shared.png", "media" + string(rune('0'+n%10)) + ".png"})
			r.GetReferencingPages("shared.png")
		}(i)
	}
	wg.Wait()

	// Verify no panics or data corruption — shared.png should have references.
	pages := r.GetReferencingPages("shared.png")
	if len(pages) == 0 {
		t.Error("shared.png should have at least some referencing pages")
	}
}

// helpers

func mkFile(t *testing.T, base, relPath, content string) {
	t.Helper()
	full := filepath.Join(base, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("slice length mismatch: got %v, want %v", got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("slice[%d]: got %q, want %q (full: got %v, want %v)", i, got[i], want[i], got, want)
			return
		}
	}
}
