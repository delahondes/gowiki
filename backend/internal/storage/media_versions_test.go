package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newVersionStore(t *testing.T) *MediaVersionStore {
	t.Helper()
	vs := NewMediaVersionStore(t.TempDir())
	if err := vs.Load(); err != nil {
		t.Fatalf("Load fresh store: %v", err)
	}
	return vs
}

func TestMediaVersionStore_FreshStore_ReturnsZero(t *testing.T) {
	t.Parallel()
	vs := newVersionStore(t)
	if v := vs.GetVersion("nothing/here.png"); v != 0 {
		t.Errorf("GetVersion on unknown media = %d, want 0", v)
	}
}

func TestMediaVersionStore_SetVersion_Persists(t *testing.T) {
	t.Parallel()
	metaRoot := t.TempDir()
	vs := NewMediaVersionStore(metaRoot)
	if err := vs.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := vs.SetVersion("docs/a.png", 3); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}

	// Fresh store reloading the same path sees the same value.
	vs2 := NewMediaVersionStore(metaRoot)
	if err := vs2.Load(); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if v := vs2.GetVersion("docs/a.png"); v != 3 {
		t.Errorf("reloaded version = %d, want 3", v)
	}
}

func TestMediaVersionStore_IncrementVersion(t *testing.T) {
	t.Parallel()
	vs := newVersionStore(t)

	// First increment on unknown key goes from 0 → 1 (map default).
	v, err := vs.IncrementVersion("img.png")
	if err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if v != 1 {
		t.Errorf("first Increment returned %d, want 1", v)
	}
	// Subsequent increment climbs.
	v, err = vs.IncrementVersion("img.png")
	if err != nil {
		t.Fatalf("Increment #2: %v", err)
	}
	if v != 2 {
		t.Errorf("second Increment returned %d, want 2", v)
	}
	if got := vs.GetVersion("img.png"); got != 2 {
		t.Errorf("GetVersion after Increment = %d, want 2", got)
	}
}

func TestMediaVersionStore_GetAllVersions_ReturnsCopy(t *testing.T) {
	t.Parallel()
	vs := newVersionStore(t)
	_ = vs.SetVersion("a.png", 1)
	_ = vs.SetVersion("b.png", 2)

	got := vs.GetAllVersions()
	if got["a.png"] != 1 || got["b.png"] != 2 {
		t.Fatalf("GetAllVersions = %v", got)
	}

	// Mutating the returned map must NOT affect the store — otherwise a
	// caller could silently rewrite everyone else's versions.
	got["a.png"] = 99
	got["hostile"] = 42
	if v := vs.GetVersion("a.png"); v != 1 {
		t.Errorf("store mutated via returned map: version = %d, want 1", v)
	}
	if v := vs.GetVersion("hostile"); v != 0 {
		t.Errorf("store mutated via returned map: hostile appeared at %d", v)
	}
}

func TestMediaVersionStore_GetVersionsForPaths(t *testing.T) {
	t.Parallel()
	vs := newVersionStore(t)
	_ = vs.SetVersion("a.png", 1)
	_ = vs.SetVersion("b.png", 2)
	_ = vs.SetVersion("c.png", 3)

	got := vs.GetVersionsForPaths([]string{"a.png", "c.png", "unknown.png"})
	if len(got) != 2 {
		t.Fatalf("expected 2 entries (skipping unknown), got %v", got)
	}
	if got["a.png"] != 1 || got["c.png"] != 3 {
		t.Errorf("filtered map wrong: %v", got)
	}
	if _, ok := got["unknown.png"]; ok {
		t.Errorf("unknown path leaked into result")
	}
}

func TestMediaVersionStore_RenamePath(t *testing.T) {
	t.Parallel()
	vs := newVersionStore(t)
	_ = vs.SetVersion("old.png", 5)

	if err := vs.RenamePath("old.png", "new.png"); err != nil {
		t.Fatalf("RenamePath: %v", err)
	}
	if v := vs.GetVersion("old.png"); v != 0 {
		t.Errorf("old path still tracked after rename: %d", v)
	}
	if v := vs.GetVersion("new.png"); v != 5 {
		t.Errorf("new path version = %d, want 5", v)
	}
}

func TestMediaVersionStore_RenamePath_NoOpOnUnknown(t *testing.T) {
	t.Parallel()
	vs := newVersionStore(t)
	if err := vs.RenamePath("no-such.png", "irrelevant.png"); err != nil {
		t.Errorf("RenamePath of unknown key returned %v, want nil (no-op)", err)
	}
	if v := vs.GetVersion("irrelevant.png"); v != 0 {
		t.Errorf("rename of unknown source created target at %d", v)
	}
}

func TestMediaVersionStore_ConcurrentIncrements(t *testing.T) {
	t.Parallel()
	vs := newVersionStore(t)

	const N = 32
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := vs.IncrementVersion("shared.png"); err != nil {
				t.Errorf("Increment: %v", err)
			}
		}()
	}
	wg.Wait()

	// All increments must be observed — no lost updates.
	if v := vs.GetVersion("shared.png"); v != N {
		t.Errorf("after %d concurrent increments, version = %d, want %d", N, v, N)
	}
}

func TestMediaVersionStore_Load_MissingFileIsOk(t *testing.T) {
	t.Parallel()
	vs := NewMediaVersionStore(t.TempDir())
	if err := vs.Load(); err != nil {
		t.Errorf("Load on missing file should return nil, got %v", err)
	}
	if v := vs.GetVersion("anything.png"); v != 0 {
		t.Errorf("post-empty-Load GetVersion = %d, want 0", v)
	}
}

func TestMediaVersionStore_Load_CorruptFileReturnsError(t *testing.T) {
	t.Parallel()
	metaRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(metaRoot, "_mediaversions.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	vs := NewMediaVersionStore(metaRoot)
	if err := vs.Load(); err == nil {
		t.Fatal("Load on garbage JSON should return an error, got nil")
	}
}

// -----------------------------------------------------------------------
// MediaAttic
// -----------------------------------------------------------------------

func newAttic(t *testing.T) *MediaAttic {
	t.Helper()
	return NewMediaAttic(t.TempDir())
}

func TestMediaAttic_ArchiveRead_RoundTrip(t *testing.T) {
	t.Parallel()
	a := newAttic(t)
	content := bytes.Repeat([]byte{0x00, 0x11, 0x22, 0x33}, 1024) // 4 KiB binary

	if err := a.Archive("docs/img.png", 1, content, "alice"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, err := a.ReadVersion("docs/img.png", 1)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("ReadVersion content mismatch: len(got)=%d len(want)=%d", len(got), len(content))
	}
}

func TestMediaAttic_ListVersions_SortedAscending(t *testing.T) {
	t.Parallel()
	a := newAttic(t)
	// Archive out of order to prove the sort actually runs.
	_ = a.Archive("x.png", 3, []byte("v3"), "u")
	_ = a.Archive("x.png", 1, []byte("v1"), "u")
	_ = a.Archive("x.png", 2, []byte("v2"), "u")

	entries, err := a.ListVersions("x.png")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("ListVersions returned %d, want 3", len(entries))
	}
	for i, want := range []int64{1, 2, 3} {
		if entries[i].Version != want {
			t.Errorf("entries[%d].Version = %d, want %d", i, entries[i].Version, want)
		}
	}
}

func TestMediaAttic_Archive_IdempotentOnSameVersion(t *testing.T) {
	t.Parallel()
	a := newAttic(t)
	if err := a.Archive("y.png", 1, []byte("first"), "u"); err != nil {
		t.Fatal(err)
	}
	// Re-archiving the same (path, version) is a no-op — the guard at
	// media_versions.go:182-185 skips if the version file already exists.
	if err := a.Archive("y.png", 1, []byte("second-attempt"), "u"); err != nil {
		t.Fatalf("re-archive returned %v, want nil (no-op)", err)
	}
	// Content stays what was archived first — the guard fires BEFORE the
	// new bytes are written, and before the index is appended. So the
	// index still has exactly one entry and its content is "first".
	entries, err := a.ListVersions("y.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("ListVersions after idempotent re-archive = %d entries, want 1", len(entries))
	}
	got, err := a.ReadVersion("y.png", 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Errorf("archived content = %q, want %q (idempotent guard should protect the earlier bytes)", got, "first")
	}
}

func TestMediaAttic_ReadVersion_UnknownFails(t *testing.T) {
	t.Parallel()
	a := newAttic(t)
	if _, err := a.ReadVersion("no/such.png", 7); err == nil {
		t.Error("ReadVersion of missing (path, version) should return an error")
	}
}

func TestMediaAttic_ListVersions_NoIndexReturnsEmpty(t *testing.T) {
	t.Parallel()
	a := newAttic(t)
	entries, err := a.ListVersions("no/such.png")
	if err != nil {
		t.Errorf("ListVersions on missing index returned %v, want nil", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got %v", entries)
	}
}

func TestMediaAttic_ArchiveMetadata_MD5AndSizeRecorded(t *testing.T) {
	t.Parallel()
	a := newAttic(t)
	content := []byte("hello, attic")
	if err := a.Archive("meta.txt", 1, content, "bob"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	entries, err := a.ListVersions("meta.txt")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Author != "bob" {
		t.Errorf("Author = %q, want bob", e.Author)
	}
	if e.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", e.Size, len(content))
	}
	if e.MD5 == "" {
		t.Errorf("MD5 not recorded")
	}
	if e.Timestamp == "" {
		t.Errorf("Timestamp not recorded")
	}
}
