package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
)

// LinkIndex is a small filesystem-backed reverse-index (page → links,
// plus a scanned reverse lookup for backlinks). The tests below pin
// the invariants the rest of the app relies on: Save/Load round-trip,
// UpdatePage replaces (not accumulates), empty updates evict, backlinks
// sort deterministically, and concurrent updates hold under -race.

func newTestLinkIndex(t *testing.T) *LinkIndex {
	t.Helper()
	return NewLinkIndex(t.TempDir())
}

func TestLinkIndex_Empty_LoadOnMissingFile(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	// _links.json does not exist yet — Load must be a no-op, not an error.
	if err := idx.Load(); err != nil {
		t.Fatalf("Load on empty dir: %v", err)
	}
	if len(idx.PageToLinks) != 0 {
		t.Errorf("expected empty PageToLinks, got %v", idx.PageToLinks)
	}
}

func TestLinkIndex_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	idx := NewLinkIndex(dir)
	idx.UpdatePage("/a", []string{"/b", "/c"})
	idx.UpdatePage("/b", []string{"/c"})
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fresh := NewLinkIndex(dir)
	if err := fresh.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(fresh.PageToLinks, idx.PageToLinks) {
		t.Errorf("round-trip mismatch:\n orig = %v\n load = %v", idx.PageToLinks, fresh.PageToLinks)
	}
}

func TestLinkIndex_UpdatePageReplacesNotAccumulates(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	idx.UpdatePage("/a", []string{"/x", "/y"})
	idx.UpdatePage("/a", []string{"/z"})
	got := idx.PageToLinks["/a"]
	if !reflect.DeepEqual(got, []string{"/z"}) {
		t.Errorf("expected re-Update to REPLACE, got %v", got)
	}
}

func TestLinkIndex_UpdatePageEmptyEvicts(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	idx.UpdatePage("/a", []string{"/b"})
	idx.UpdatePage("/a", nil)
	if _, ok := idx.PageToLinks["/a"]; ok {
		t.Errorf("expected empty-links update to remove the page entry")
	}
	idx.UpdatePage("/b", []string{"/c"})
	idx.UpdatePage("/b", []string{})
	if _, ok := idx.PageToLinks["/b"]; ok {
		t.Errorf("expected empty-slice update to remove the page entry")
	}
}

func TestLinkIndex_UpdatePageStoresCopy(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	original := []string{"/b", "/c"}
	idx.UpdatePage("/a", original)
	// Mutating the caller's slice must not mutate the stored links,
	// otherwise a caller who reuses a scratch buffer can corrupt the index.
	original[0] = "/oops"
	if idx.PageToLinks["/a"][0] != "/b" {
		t.Errorf("UpdatePage aliased caller slice: got %v", idx.PageToLinks["/a"])
	}
}

func TestLinkIndex_RemovePage(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	idx.UpdatePage("/a", []string{"/x"})
	idx.UpdatePage("/b", []string{"/y"})
	idx.RemovePage("/a")
	if _, ok := idx.PageToLinks["/a"]; ok {
		t.Errorf("expected /a to be removed")
	}
	if _, ok := idx.PageToLinks["/b"]; !ok {
		t.Errorf("RemovePage evicted an unrelated entry")
	}
}

func TestLinkIndex_GetBacklinksBasic(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	idx.UpdatePage("/a", []string{"/target", "/other"})
	idx.UpdatePage("/b", []string{"/target"})
	idx.UpdatePage("/c", []string{"/other"})

	got := idx.GetBacklinks("/target")
	want := []string{"/a", "/b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetBacklinks(/target) = %v, want %v", got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("backlinks not sorted: %v", got)
	}
}

func TestLinkIndex_GetBacklinksUnknownReturnsEmpty(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	idx.UpdatePage("/a", []string{"/x"})
	got := idx.GetBacklinks("/nowhere")
	if len(got) != 0 {
		t.Errorf("GetBacklinks(unknown) = %v, want empty", got)
	}
}

func TestLinkIndex_GetBacklinksDeduplicatesPerPage(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	// A single page listing the same target twice must count as one
	// backlink from that page, not two. The implementation `break`s
	// out of the inner loop on first match — pin that behaviour.
	idx.UpdatePage("/a", []string{"/target", "/target", "/target"})
	got := idx.GetBacklinks("/target")
	if !reflect.DeepEqual(got, []string{"/a"}) {
		t.Errorf("GetBacklinks with duplicate targets = %v, want [/a]", got)
	}
}

func TestLinkIndex_SaveCreatesParentDir(t *testing.T) {
	t.Parallel()
	// basePath does not exist yet — Save must MkdirAll it.
	base := filepath.Join(t.TempDir(), "does", "not", "exist")
	idx := NewLinkIndex(base)
	idx.UpdatePage("/a", []string{"/b"})
	if err := idx.Save(); err != nil {
		t.Fatalf("Save into missing dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "_links.json")); err != nil {
		t.Errorf("_links.json not created: %v", err)
	}
}

func TestLinkIndex_LoadCorruptFileErrors(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "_links.json"), []byte("{ not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	idx := NewLinkIndex(base)
	if err := idx.Load(); err == nil {
		t.Errorf("expected Load to fail on corrupt JSON")
	}
}

func TestLinkIndex_LoadNullMapCoercesToEmpty(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	// A file with an explicit null map used to trip callers that
	// assumed the map was non-nil. Load must coerce.
	if err := os.WriteFile(filepath.Join(base, "_links.json"), []byte(`{"page_to_links": null}`), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	idx := NewLinkIndex(base)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx.PageToLinks == nil {
		t.Errorf("PageToLinks should be non-nil after Load with null map")
	}
	// Writing should not panic.
	idx.UpdatePage("/a", []string{"/b"})
}

func TestLinkIndex_Concurrent_UpdateAndReadUnderRace(t *testing.T) {
	t.Parallel()
	idx := newTestLinkIndex(t)
	// Seed a shared target so GetBacklinks has work to do.
	for i := 0; i < 4; i++ {
		idx.UpdatePage(pageID(i), []string{"/target"})
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					idx.UpdatePage(pageID(id), []string{"/target", "/other"})
					_ = idx.GetBacklinks("/target")
				}
			}
		}(i)
	}
	// Let the goroutines churn a bit.
	for i := 0; i < 50; i++ {
		idx.RemovePage(pageID(i % 4))
		idx.UpdatePage(pageID(i%4), []string{"/target"})
	}
	close(stop)
	wg.Wait()
}

func pageID(i int) string {
	return "/p" + string(rune('0'+i))
}
