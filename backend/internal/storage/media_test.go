package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newTestMediaStore builds an isolated MediaFileStore rooted at t.TempDir.
// VersionStore and MediaAttic are wired so version bumps + archive happen.
func newTestMediaStore(t *testing.T) *MediaFileStore {
	t.Helper()
	root := t.TempDir()
	metaRoot := t.TempDir()
	dataRoot := t.TempDir()

	s, err := NewMediaFileStore(root)
	if err != nil {
		t.Fatalf("NewMediaFileStore: %v", err)
	}
	vs := NewMediaVersionStore(metaRoot)
	if err := vs.Load(); err != nil {
		t.Fatalf("MediaVersionStore.Load: %v", err)
	}
	s.VersionStore = vs
	s.MediaAttic = NewMediaAttic(dataRoot)
	return s
}

func putBytes(t *testing.T, s *MediaFileStore, ns, name string, data []byte, overwrite bool) MediaEntry {
	t.Helper()
	e, err := s.Put(ns, name, bytes.NewReader(data), overwrite, "tester")
	if err != nil {
		t.Fatalf("Put(%s/%s): %v", ns, name, err)
	}
	return e
}

func TestMediaFileStore_PutListResolve_RoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)

	entry := putBytes(t, s, "docs", "hello.png", []byte("PNGDATA"), false)
	if entry.Path != "/docs/hello.png" {
		t.Errorf("Path = %q, want /docs/hello.png", entry.Path)
	}
	if entry.Version != 1 {
		t.Errorf("initial upload should be version 1, got %d", entry.Version)
	}

	// List should return the file.
	entries, err := s.List("docs")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "hello.png" {
		t.Fatalf("List returned %+v", entries)
	}

	// ResolvePath returns an absolute path on disk whose contents match.
	abs, err := s.ResolvePath("/docs/hello.png")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read resolved path: %v", err)
	}
	if string(got) != "PNGDATA" {
		t.Errorf("resolved content = %q, want %q", got, "PNGDATA")
	}
}

// Attachments must carry an extension — matches the Gowiki invariant
// documented in CLAUDE.md ("Do not create extension-less files under
// data/content/").
func TestMediaFileStore_Put_RefusesExtensionlessName(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)
	_, err := s.Put("", "noext", bytes.NewReader([]byte("x")), false, "tester")
	if err == nil {
		t.Fatal("expected refusal for extension-less filename")
	}
	if !strings.Contains(err.Error(), "extension") {
		t.Errorf("error should name the extension invariant, got %q", err.Error())
	}
}

func TestMediaFileStore_Put_ConflictWithoutOverwrite(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)
	putBytes(t, s, "", "one.txt", []byte("A"), false)
	_, err := s.Put("", "one.txt", bytes.NewReader([]byte("B")), false, "tester")
	if !errors.Is(err, ErrMediaConflict) {
		t.Errorf("second Put without overwrite should return ErrMediaConflict, got %v", err)
	}
}

func TestMediaFileStore_Put_OverwriteIncrementsVersionAndArchives(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)

	putBytes(t, s, "", "img.png", []byte("V1"), false)
	if v := s.VersionStore.GetVersion("img.png"); v != 1 {
		t.Fatalf("initial version = %d, want 1", v)
	}

	entry := putBytes(t, s, "", "img.png", []byte("V2"), true)
	if entry.Version != 2 {
		t.Errorf("Put(overwrite=true).Version = %d, want 2", entry.Version)
	}
	if v := s.VersionStore.GetVersion("img.png"); v != 2 {
		t.Errorf("store version = %d, want 2", v)
	}

	// The previous version's bytes should be sitting in the media attic.
	got, err := s.MediaAttic.ReadVersion("img.png", 1)
	if err != nil {
		t.Fatalf("attic ReadVersion(1): %v", err)
	}
	if string(got) != "V1" {
		t.Errorf("attic v1 content = %q, want V1", got)
	}
}

// A file that was created before version tracking existed (currentVersion=0)
// should be bootstrapped to version 1 in the attic before the overwrite
// bumps to version 2 — captured by the guard at media.go:149-153.
func TestMediaFileStore_Put_OverwriteBootstrapsUntrackedFileAsV1(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)

	// Write directly to disk to simulate a legacy pre-versioning file.
	dir, err := s.namespaceDirPath("")
	if err != nil {
		t.Fatalf("namespaceDirPath: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.txt"), []byte("OLD"), 0o644); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}
	if v := s.VersionStore.GetVersion("legacy.txt"); v != 0 {
		t.Fatalf("legacy file should start untracked, got version %d", v)
	}

	entry := putBytes(t, s, "", "legacy.txt", []byte("NEW"), true)
	if entry.Version != 2 {
		t.Errorf("after bootstrap+bump, version = %d, want 2", entry.Version)
	}
	// Attic should carry the bootstrapped v1 content.
	got, err := s.MediaAttic.ReadVersion("legacy.txt", 1)
	if err != nil {
		t.Fatalf("attic ReadVersion(1): %v", err)
	}
	if string(got) != "OLD" {
		t.Errorf("attic v1 content = %q, want OLD", got)
	}
}

func TestMediaFileStore_Delete_HappyPathAndNotExist(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)

	putBytes(t, s, "docs", "gone.png", []byte("x"), false)
	if err := s.Delete("/docs/gone.png"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// File is really gone.
	if _, err := os.Stat(filepath.Join(s.rootDir, "docs", "gone.png")); !os.IsNotExist(err) {
		t.Errorf("file still present after Delete: %v", err)
	}
	// Second delete surfaces os.ErrNotExist.
	if err := s.Delete("/docs/gone.png"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("delete of missing file returned %v, want os.ErrNotExist", err)
	}
}

func TestMediaFileStore_List_NamespaceIsolationAndSorting(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)
	putBytes(t, s, "docs", "b.png", []byte("."), false)
	putBytes(t, s, "docs", "a.png", []byte("."), false)
	putBytes(t, s, "docs/sub", "in-sub.png", []byte("."), false)
	putBytes(t, s, "other", "should-not-list.png", []byte("."), false)

	entries, err := s.List("docs")
	if err != nil {
		t.Fatalf("List(docs): %v", err)
	}

	// Folders first, then files alphabetically (case-insensitive).
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Kind+":"+e.Name)
	}
	want := []string{"folder:sub", "file:a.png", "file:b.png"}
	if len(names) != len(want) {
		t.Fatalf("List(docs) names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("List(docs) sort mismatch at %d: got %v, want %v", i, names, want)
		}
	}
}

func TestMediaFileStore_List_UnknownNamespaceReturnsEmptyNotError(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)
	entries, err := s.List("no/such/place")
	if err != nil {
		t.Fatalf("List of missing namespace should be no-error, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty listing, got %+v", entries)
	}
}

// The store guards against namespace paths that would create a directory
// where a .md page file already exists (e.g. /docs/index/ if /docs/index.md
// is a page). The guard fires only during Put.
func TestMediaFileStore_Put_RefusesNamespaceThatShadowsPage(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)

	// Seed a page file at the root: /index.md.
	if err := os.WriteFile(filepath.Join(s.rootDir, "index.md"), []byte("# hi"), 0o644); err != nil {
		t.Fatalf("seed page: %v", err)
	}
	_, err := s.Put("index", "img.png", bytes.NewReader([]byte("x")), false, "tester")
	if err == nil {
		t.Fatal("expected refusal when namespace shadows an existing page")
	}
	if !strings.Contains(err.Error(), "conflicts with existing page") {
		t.Errorf("error should name the conflict, got %q", err.Error())
	}
}

func TestMediaFileStore_ResolvePath_RejectsInvalidPaths(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)

	// Empty and root-only inputs are outright rejected by
	// normalizeMediaPath. Both are meaningless as file identifiers.
	for _, c := range []string{"", "/"} {
		if _, err := s.ResolvePath(c); err == nil {
			t.Errorf("ResolvePath(%q) should fail, returned ok", c)
		}
	}
}

func TestMediaFileStore_ResolvePath_TraversalStaysWithinRoot(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)

	// path.Clean normalizes traversal (`..`) into a canonical path
	// rooted at "/". The result is a path INSIDE rootDir, not an
	// escape — this is the security guarantee the guard provides.
	// Verify the resolved absolute path is always contained.
	for _, c := range []string{"../etc/passwd", "docs/../../../../root/.ssh/id"} {
		abs, err := s.ResolvePath(c)
		if err != nil {
			continue // rejecting is also acceptable
		}
		if !strings.HasPrefix(abs, s.rootDir) {
			t.Errorf("ResolvePath(%q) escaped rootDir: %q outside %q", c, abs, s.rootDir)
		}
	}
}

// Concurrent Puts to distinct files must not race. Uses -race when the
// go test invocation carries it; the per-file work is independent so
// success is straightforward, but this pins the invariant.
func TestMediaFileStore_ConcurrentPutsOnDifferentFiles(t *testing.T) {
	t.Parallel()
	s := newTestMediaStore(t)

	var wg sync.WaitGroup
	const N = 8
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := "file-" + itoaMed(id) + ".txt"
			if _, err := s.Put("bulk", name, bytes.NewReader([]byte("x")), false, "worker"); err != nil {
				t.Errorf("Put(%s): %v", name, err)
			}
		}(i)
	}
	wg.Wait()

	entries, err := s.List("bulk")
	if err != nil {
		t.Fatalf("List after concurrent Puts: %v", err)
	}
	if len(entries) != N {
		t.Errorf("saw %d entries after %d concurrent Puts, want %d", len(entries), N, N)
	}
}

// tiny local helper — pulled out of the way to avoid shadowing anything.
func itoaMed(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
