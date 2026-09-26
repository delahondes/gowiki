package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

// Archive → ListVersions → ReadVersion → GetEntry: the core round-trip.
func TestAttic_Archive_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := NewAttic(dir)

	if err := a.Archive("/hello", 1, []byte("first"), "alice", "initial", nil); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	entries, err := a.ListVersions("/hello")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(entries) != 1 || entries[0].Version != 1 || entries[0].Author != "alice" || entries[0].Summary != "initial" {
		t.Errorf("unexpected entries: %+v", entries)
	}
	if entries[0].MD5 == "" || entries[0].Timestamp == "" {
		t.Errorf("expected timestamp + md5 to be populated: %+v", entries[0])
	}

	content, err := a.ReadVersion("/hello", 1)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if !bytes.Equal(content, []byte("first")) {
		t.Errorf("ReadVersion = %q, want %q", content, "first")
	}

	entry, err := a.GetEntry("/hello", 1)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if entry == nil || entry.Version != 1 || entry.Summary != "initial" {
		t.Errorf("GetEntry = %+v", entry)
	}
}

// Archive persists mediaRefs into the entry.
func TestAttic_Archive_MediaRefs(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	refs := map[string]int64{"/img/foo.png": 3, "/attach/doc.pdf": 1}
	if err := a.Archive("/page", 1, []byte("body"), "bob", "with refs", refs); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	entry, _ := a.GetEntry("/page", 1)
	if entry == nil {
		t.Fatal("no entry")
	}
	if len(entry.MediaRefs) != 2 || entry.MediaRefs["/img/foo.png"] != 3 || entry.MediaRefs["/attach/doc.pdf"] != 1 {
		t.Errorf("MediaRefs = %+v", entry.MediaRefs)
	}
}

// Archive is idempotent for the same version (no duplicate index entries,
// no re-write). This matters because a retry after a partial failure must
// not corrupt the index.
func TestAttic_Archive_Idempotent_SameVersion(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	if err := a.Archive("/p", 1, []byte("v1"), "a", "s", nil); err != nil {
		t.Fatalf("first Archive: %v", err)
	}
	if err := a.Archive("/p", 1, []byte("v1-changed"), "a", "s", nil); err != nil {
		t.Fatalf("retry Archive: %v", err)
	}
	entries, _ := a.ListVersions("/p")
	if len(entries) != 1 {
		t.Errorf("index has %d entries, want 1 (dedup)", len(entries))
	}
	c, _ := a.ReadVersion("/p", 1)
	if !bytes.Equal(c, []byte("v1")) {
		t.Errorf("first-write content overwritten: got %q", c)
	}
}

// Multiple archives of the same page produce sorted-by-version output.
func TestAttic_ListVersions_SortedAscending(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	// Archive in reverse order to prove sort is applied on read.
	for _, v := range []int64{3, 1, 2} {
		if err := a.Archive("/p", v, []byte("v"), "a", "", nil); err != nil {
			t.Fatalf("Archive v%d: %v", v, err)
		}
	}
	entries, _ := a.ListVersions("/p")
	if len(entries) != 3 {
		t.Fatalf("got %d entries", len(entries))
	}
	for i, want := range []int64{1, 2, 3} {
		if entries[i].Version != want {
			t.Errorf("entries[%d].Version = %d, want %d", i, entries[i].Version, want)
		}
	}
}

// ListVersions on an unknown page returns (nil, nil), not an error.
func TestAttic_ListVersions_UnknownPage(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	entries, err := a.ListVersions("/nope")
	if err != nil {
		t.Errorf("expected nil error for unknown page, got %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got %v", entries)
	}
}

// ReadVersion with an unknown version returns a wrapped error carrying
// the underlying os.ErrNotExist so callers can distinguish "no such
// version" from real IO failures.
func TestAttic_ReadVersion_Unknown_Returns404Shape(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	_ = a.Archive("/p", 1, []byte("body"), "a", "", nil)
	_, err := a.ReadVersion("/p", 42)
	if err == nil {
		t.Fatal("expected error for unknown version")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected wrapped os.ErrNotExist, got %v", err)
	}
}

// GetEntry on an unknown version returns (nil, nil).
func TestAttic_GetEntry_Unknown_ReturnsNilNil(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	_ = a.Archive("/p", 1, []byte("body"), "a", "", nil)
	entry, err := a.GetEntry("/p", 42)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil entry, got %+v", entry)
	}
}

// UpdateEntryMeta inserts a plugin_meta key and returns via GetEntry.
func TestAttic_UpdateEntryMeta_Insert(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	_ = a.Archive("/p", 1, []byte("body"), "a", "", nil)

	payload := json.RawMessage(`{"validated":true}`)
	if err := a.UpdateEntryMeta("/p", 1, "reviewflow", payload); err != nil {
		t.Fatalf("UpdateEntryMeta: %v", err)
	}
	entry, _ := a.GetEntry("/p", 1)
	if entry == nil {
		t.Fatal("entry missing")
	}
	got, ok := entry.PluginMeta["reviewflow"]
	if !ok {
		t.Fatalf("reviewflow key absent: %+v", entry.PluginMeta)
	}
	// json.RawMessage is stored verbatim by MarshalJSON but MarshalIndent
	// pretty-prints the whole tree, so on re-read the bytes carry the
	// pretty-printed layout. Compare parsed structure, not raw bytes.
	var gotParsed, wantParsed any
	if err := json.Unmarshal(got, &gotParsed); err != nil {
		t.Fatalf("parse got: %v", err)
	}
	_ = json.Unmarshal(payload, &wantParsed)
	if !reflect.DeepEqual(gotParsed, wantParsed) {
		t.Errorf("payload mismatch: got=%v want=%v", gotParsed, wantParsed)
	}
}

// UpdateEntryMeta replaces an existing key's value.
func TestAttic_UpdateEntryMeta_Overwrite(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	_ = a.Archive("/p", 1, []byte("body"), "a", "", nil)

	_ = a.UpdateEntryMeta("/p", 1, "k", json.RawMessage(`{"v":1}`))
	if err := a.UpdateEntryMeta("/p", 1, "k", json.RawMessage(`{"v":2}`)); err != nil {
		t.Fatalf("second UpdateEntryMeta: %v", err)
	}
	entry, _ := a.GetEntry("/p", 1)
	var got map[string]int
	if err := json.Unmarshal(entry.PluginMeta["k"], &got); err != nil {
		t.Fatalf("parse overwritten meta: %v", err)
	}
	if got["v"] != 2 {
		t.Errorf("meta not overwritten: got v=%d, want 2 (raw: %s)", got["v"], entry.PluginMeta["k"])
	}
}

// UpdateEntryMeta on an unknown version returns an error.
func TestAttic_UpdateEntryMeta_UnknownVersion(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	_ = a.Archive("/p", 1, []byte("body"), "a", "", nil)
	if err := a.UpdateEntryMeta("/p", 42, "k", json.RawMessage(`{}`)); err == nil {
		t.Error("expected error for unknown version")
	}
}

// RenamePage moves the whole attic history under the new path and clears
// the old path's index.
func TestAttic_RenamePage(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	_ = a.Archive("/old/name", 1, []byte("v1"), "a", "", nil)
	_ = a.Archive("/old/name", 2, []byte("v2"), "a", "", nil)

	if err := a.RenamePage("/old/name", "/new/dest"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}

	// Old path is empty.
	oldEntries, _ := a.ListVersions("/old/name")
	if len(oldEntries) != 0 {
		t.Errorf("old path still has %d entries", len(oldEntries))
	}

	// New path has all versions with content intact.
	newEntries, _ := a.ListVersions("/new/dest")
	if len(newEntries) != 2 {
		t.Fatalf("new path has %d entries, want 2", len(newEntries))
	}
	c1, err := a.ReadVersion("/new/dest", 1)
	if err != nil || !bytes.Equal(c1, []byte("v1")) {
		t.Errorf("ReadVersion v1 = %q, %v", c1, err)
	}
	c2, err := a.ReadVersion("/new/dest", 2)
	if err != nil || !bytes.Equal(c2, []byte("v2")) {
		t.Errorf("ReadVersion v2 = %q, %v", c2, err)
	}
}

// RenamePage is a no-op when the source has no history (idempotent).
func TestAttic_RenamePage_NoHistory(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	if err := a.RenamePage("/absent", "/dest"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// MigrateExistingPages seeds a v1 entry for a page that has content on
// disk but no attic index yet. Uses the meta file's Version if present.
func TestAttic_MigrateExistingPages_SeedsV1(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	contentRoot := filepath.Join(root, "content")
	metaRoot := filepath.Join(root, "meta")
	if err := os.MkdirAll(filepath.Join(contentRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(metaRoot, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentRoot, "docs", "guide.md"), []byte("guide content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Meta with Version=5 — migration should honor it rather than always writing v1.
	meta := PageMetadata{Version: 5}
	metaData, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(metaRoot, "docs", "guide.json"), metaData, 0o644); err != nil {
		t.Fatal(err)
	}

	// attic root is separate from content — mirror the FileStore layout by
	// putting attic under `root`.
	a := NewAttic(root)
	if err := a.MigrateExistingPages(contentRoot, metaRoot, nil); err != nil {
		t.Fatalf("MigrateExistingPages: %v", err)
	}

	entries, _ := a.ListVersions("/docs/guide")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Version != 5 {
		t.Errorf("migrated version = %d, want 5 (from meta)", entries[0].Version)
	}
	if entries[0].Author != "system" {
		t.Errorf("author = %q, want system", entries[0].Author)
	}
	body, _ := a.ReadVersion("/docs/guide", 5)
	if !bytes.Equal(body, []byte("guide content")) {
		t.Errorf("body = %q", body)
	}
}

// MigrateExistingPages is idempotent — a page with an existing attic
// entry is left alone even if migration runs again.
func TestAttic_MigrateExistingPages_SkipsExistingHistory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	contentRoot := filepath.Join(root, "content")
	metaRoot := filepath.Join(root, "meta")
	_ = os.MkdirAll(contentRoot, 0o755)
	_ = os.MkdirAll(metaRoot, 0o755)
	_ = os.WriteFile(filepath.Join(contentRoot, "p.md"), []byte("v1"), 0o644)

	a := NewAttic(root)
	// Pre-seed history at v2.
	_ = a.Archive("/p", 2, []byte("existing v2"), "human", "manual", nil)

	if err := a.MigrateExistingPages(contentRoot, metaRoot, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	entries, _ := a.ListVersions("/p")
	if len(entries) != 1 || entries[0].Version != 2 {
		t.Errorf("expected only pre-seeded v2, got %+v", entries)
	}
}

// Concurrent Archive on distinct pages doesn't race under -race.
// (Each page gets its own index file; the archive uses atomic rename.)
func TestAttic_Concurrent_Archive_DistinctPages(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			page := "/p" + string(rune('a'+i))
			for v := int64(1); v <= 5; v++ {
				if err := a.Archive(page, v, []byte("body"), "a", "", nil); err != nil {
					t.Errorf("Archive %s v%d: %v", page, v, err)
				}
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 8; i++ {
		page := "/p" + string(rune('a'+i))
		entries, _ := a.ListVersions(page)
		if len(entries) != 5 {
			t.Errorf("%s has %d entries, want 5", page, len(entries))
		}
	}
}

// Archive content survives byte-for-byte across the gzip round-trip,
// including binary bytes.
func TestAttic_Archive_BinarySafeContent(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	body := []byte{0x00, 0xff, 0x7f, 0x80, 'a', '\n', 0x00}
	if err := a.Archive("/p", 1, body, "a", "", nil); err != nil {
		t.Fatal(err)
	}
	got, _ := a.ReadVersion("/p", 1)
	if !bytes.Equal(got, body) {
		t.Errorf("content differs after gzip round-trip: %v vs %v", got, body)
	}
}
