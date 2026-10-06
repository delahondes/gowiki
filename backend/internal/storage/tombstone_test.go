package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// User-reported production bug on wiki.gmt.bio/regulatory/qms/cpm/sop01/rec01:
// the page was deleted and then recreated at the same path. The recreated
// page carried both lives in its history view — v12 (the pre-delete last
// edit) + v1..vN of the new life — and the old page's reviewflow state
// resurrected with 2024-stamped deadlines on 2026-10-05 content. Root
// cause: Delete() archived a "deleted" marker but left the attic in
// place, so Put() on the recreated path appended to the old version
// numbering. The reviewflow sidecar also survived.
//
// Fix: Delete() now tombstones everything (attic + meta sidecars) into
// data/attic/<path>/@tombstones/@deleted-<ts>/ before removing live
// content. A one-shot startup sweep drains pages whose previous
// deletion predates this fix.

// -------------------------------------------------------------------------
// Attic.Tombstone + helpers
// -------------------------------------------------------------------------

func TestAttic_Tombstone_MovesLiveEntriesIntoSubdir(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())

	if err := a.Archive("/page", 1, []byte("v1"), "alice", "initial", nil); err != nil {
		t.Fatalf("Archive v1: %v", err)
	}
	if err := a.Archive("/page", 2, []byte("v2"), "alice", "deleted", nil); err != nil {
		t.Fatalf("Archive v2: %v", err)
	}

	when := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	tombDir, err := a.Tombstone("/page", when)
	if err != nil {
		t.Fatalf("Tombstone: %v", err)
	}

	// Expected shape: attic/page/@tombstones/@deleted-2026-10-05T12-00-00Z/
	if !strings.Contains(filepath.ToSlash(tombDir), "/@tombstones/@deleted-2026-10-05T12-00-00Z") {
		t.Errorf("tombDir shape unexpected: %s", tombDir)
	}

	// Live attic dir must retain only the @tombstones holder — not the
	// version files, not the index.json.
	liveEntries, _ := os.ReadDir(a.pageDir("/page"))
	for _, e := range liveEntries {
		if e.Name() != tombstonesSubdir {
			t.Errorf("live attic dir still has %q — tombstone did not move it", e.Name())
		}
	}

	// Both version files + the index must be inside the tomb.
	for _, name := range []string{"index.json", "1.md.gz", "2.md.gz"} {
		if _, err := os.Stat(filepath.Join(tombDir, name)); err != nil {
			t.Errorf("tombDir missing %s: %v", name, err)
		}
	}

	// New Archive() on the same path lands cleanly at v1 (not v3) —
	// the next life of this URL starts from scratch.
	if err := a.Archive("/page", 1, []byte("new-life-v1"), "bob", "initial", nil); err != nil {
		t.Fatalf("post-tombstone Archive: %v", err)
	}
	fresh, _ := a.ListVersions("/page")
	if len(fresh) != 1 || fresh[0].Version != 1 || fresh[0].Author != "bob" {
		t.Errorf("post-tombstone attic should contain only new-life v1; got %+v", fresh)
	}
}

func TestAttic_Tombstone_IdempotentOnEmptyAttic(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())
	// No prior archive — Tombstone must not fail.
	tombDir, err := a.Tombstone("/never-existed", time.Now())
	if err != nil {
		t.Fatalf("Tombstone on empty attic: %v", err)
	}
	// The dir may or may not exist; what matters is the method returned
	// a path without erroring.
	if tombDir == "" {
		t.Errorf("Tombstone returned empty path")
	}
}

func TestAttic_LatestEntrySummary(t *testing.T) {
	t.Parallel()
	a := NewAttic(t.TempDir())

	if got := a.LatestEntrySummary("/nope"); got != "" {
		t.Errorf("empty attic → want empty string, got %q", got)
	}

	_ = a.Archive("/p", 1, []byte("x"), "alice", "initial", nil)
	_ = a.Archive("/p", 2, []byte("y"), "alice", "edited", nil)
	_ = a.Archive("/p", 3, []byte("z"), "alice", "deleted", nil)

	if got := a.LatestEntrySummary("/p"); got != "deleted" {
		t.Errorf("LatestEntrySummary = %q, want %q", got, "deleted")
	}
}

// -------------------------------------------------------------------------
// tombstoneMetaSidecars
// -------------------------------------------------------------------------

func TestTombstoneMetaSidecars_LeafPage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	metaDir := filepath.Join(root, "meta")
	tombDir := filepath.Join(root, "tomb")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}

	// Set up the sidecars for a leaf page at /foo:
	// meta/foo.json, meta/foo.reviewflow.json, meta/foo.comments.json,
	// plus an unrelated sibling file meta/bar.json that must stay put.
	writes := map[string]string{
		"foo.json":            `{"version":12}`,
		"foo.reviewflow.json": `{"roles":{"author":"alice"}}`,
		"foo.comments.json":   `[{"body":"hi"}]`,
		"bar.json":            `{"version":1}`,
	}
	for name, body := range writes {
		if err := os.WriteFile(filepath.Join(metaDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := tombstoneMetaSidecars(filepath.Join(metaDir, "foo.json"), tombDir); err != nil {
		t.Fatalf("tombstoneMetaSidecars: %v", err)
	}

	// Destination filenames: meta.json / reviewflow.json / comments.json.
	for name, wantBody := range map[string]string{
		"meta.json":       `{"version":12}`,
		"reviewflow.json": `{"roles":{"author":"alice"}}`,
		"comments.json":   `[{"body":"hi"}]`,
	} {
		data, err := os.ReadFile(filepath.Join(tombDir, name))
		if err != nil {
			t.Errorf("tomb/%s not written: %v", name, err)
			continue
		}
		if string(data) != wantBody {
			t.Errorf("tomb/%s body = %q, want %q", name, data, wantBody)
		}
	}

	// Sibling bar.json must still be there — the prefix match is strict.
	if _, err := os.Stat(filepath.Join(metaDir, "bar.json")); err != nil {
		t.Errorf("sibling bar.json was moved by mistake: %v", err)
	}

	// Original files are gone from meta dir.
	for _, name := range []string{"foo.json", "foo.reviewflow.json", "foo.comments.json"} {
		if _, err := os.Stat(filepath.Join(metaDir, name)); !os.IsNotExist(err) {
			t.Errorf("meta/%s should have been moved: err=%v", name, err)
		}
	}
}

func TestTombstoneMetaSidecars_NamespaceIndex(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	metaDir := filepath.Join(root, "meta", "foo")
	tombDir := filepath.Join(root, "tomb")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}

	// Namespace index: meta/foo/index.json + meta/foo/index.reviewflow.json
	// plus a sibling leaf meta/foo/other.json for a different page under
	// the same namespace that must not be touched.
	writes := map[string]string{
		"index.json":            `{"version":7}`,
		"index.reviewflow.json": `{"roles":{"author":"bob"}}`,
		"other.json":            `{"version":1}`,
	}
	for name, body := range writes {
		if err := os.WriteFile(filepath.Join(metaDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := tombstoneMetaSidecars(filepath.Join(metaDir, "index.json"), tombDir); err != nil {
		t.Fatalf("tombstoneMetaSidecars: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tombDir, "meta.json")); err != nil {
		t.Errorf("tomb/meta.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tombDir, "reviewflow.json")); err != nil {
		t.Errorf("tomb/reviewflow.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(metaDir, "other.json")); err != nil {
		t.Errorf("other.json was moved by mistake: %v", err)
	}
}

// -------------------------------------------------------------------------
// FileStore.Delete end-to-end tombstoning
// -------------------------------------------------------------------------

func TestFileStore_Delete_TombstonesAtticAndMeta(t *testing.T) {
	t.Parallel()
	s := newTestFileStore(t)

	// Write a page, bump its version a couple of times.
	if _, err := s.Put("/doc", "# Doc\n\nv1 body\n", "alice"); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	if _, err := s.Put("/doc", "# Doc\n\nv2 body\n", "alice"); err != nil {
		t.Fatalf("Put v2: %v", err)
	}

	// Simulate a reviewflow sidecar by writing one directly — the test
	// doesn't need the full reviewflow service, only the sidecar file
	// shape Delete() has to tombstone.
	rfPath := filepath.Join(s.metaRoot, "doc.reviewflow.json")
	if err := os.WriteFile(rfPath, []byte(`{"roles":{"author":"alice"}}`), 0o644); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	// Delete the page.
	if _, err := s.Delete("/doc", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Live attic dir keeps only the @tombstones holder.
	liveAttic, _ := os.ReadDir(s.Attic.pageDir("/doc"))
	for _, e := range liveAttic {
		if e.Name() != tombstonesSubdir {
			t.Errorf("live attic retained %q after Delete; want only %q", e.Name(), tombstonesSubdir)
		}
	}

	// Find the single @deleted-<ts> directory under @tombstones.
	tombRoot := filepath.Join(s.Attic.pageDir("/doc"), tombstonesSubdir)
	tombs, _ := os.ReadDir(tombRoot)
	if len(tombs) != 1 {
		t.Fatalf("expected 1 tombstone dir, got %d", len(tombs))
	}
	tombDir := filepath.Join(tombRoot, tombs[0].Name())

	// Attic version files + index must be inside the tomb. The two Puts
	// landed v1 and v2; Delete() archives at meta.Version (= 2), which
	// Archive() dedups against the existing v2 entry — so the final
	// state is just v1 and v2, both inside the tomb.
	for _, name := range []string{"index.json", "1.md.gz", "2.md.gz"} {
		if _, err := os.Stat(filepath.Join(tombDir, name)); err != nil {
			t.Errorf("tomb missing attic %s: %v", name, err)
		}
	}

	// Meta sidecars inside the tomb under canonical names.
	for _, name := range []string{"meta.json", "reviewflow.json"} {
		if _, err := os.Stat(filepath.Join(tombDir, name)); err != nil {
			t.Errorf("tomb missing meta %s: %v", name, err)
		}
	}

	// The live sidecar is gone.
	if _, err := os.Stat(rfPath); !os.IsNotExist(err) {
		t.Errorf("live reviewflow sidecar still present after delete: %v", err)
	}

	// Content file is gone.
	if _, err := os.Stat(filepath.Join(s.contentRoot, "doc.md")); !os.IsNotExist(err) {
		t.Errorf("content file still present after delete: %v", err)
	}
}

func TestFileStore_Delete_RecreatedPageStartsFresh(t *testing.T) {
	t.Parallel()
	s := newTestFileStore(t)

	// First life: v1 → v2 → deleted.
	_, _ = s.Put("/rec01", "# Rec\n\nold v1\n", "alice")
	_, _ = s.Put("/rec01", "# Rec\n\nold v2\n", "alice")
	meta := s.loadMetaForTest(t, "/rec01")
	oldVersion := meta.Version

	if _, err := s.Delete("/rec01", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Second life at the same path.
	if _, err := s.Put("/rec01", "# Rec\n\nnew v1\n", "bob"); err != nil {
		t.Fatalf("Put after delete: %v", err)
	}
	freshMeta := s.loadMetaForTest(t, "/rec01")
	// Version numbering must reset — the user-reported bug on
	// /qms/cpm/sop01/rec01 was precisely this: the recreated page
	// kept counting from the deleted page's final version. Page ID
	// is deterministic from the path (sha1(pagePath)) so it is
	// expected to be identical across lives; it's version numbering
	// and reviewflow state that need to start fresh.
	if freshMeta.Version != 1 {
		t.Errorf("recreated page version = %d, want 1 (collision with pre-delete numbering)", freshMeta.Version)
	}
	if freshMeta.Version >= oldVersion {
		t.Errorf("recreated page version %d still >= old version %d — tombstoning didn't reset numbering", freshMeta.Version, oldVersion)
	}

	// Attic for the new life has only v1.
	entries, _ := s.Attic.ListVersions("/rec01")
	if len(entries) != 1 || entries[0].Version != 1 {
		t.Errorf("recreated page attic = %+v, want single v1", entries)
	}

	// The deleted page's history is still retrievable via the
	// tombstone API — regulatory audit guarantee.
	tombs, _ := s.Attic.ListTombstones("/rec01")
	if len(tombs) != 1 {
		t.Fatalf("want 1 tombstone from first life, got %d", len(tombs))
	}
	oldV1, err := s.Attic.ReadTombstonedVersion("/rec01", tombs[0].ID, 1)
	if err != nil {
		t.Fatalf("ReadTombstonedVersion: %v", err)
	}
	if !strings.Contains(string(oldV1), "old v1") {
		t.Errorf("tombstoned v1 content = %q, want to contain 'old v1'", oldV1)
	}
}

// -------------------------------------------------------------------------
// ReconcileTombstones
// -------------------------------------------------------------------------

func TestReconcileTombstones_DrainsPreFixDrift(t *testing.T) {
	t.Parallel()
	s := newTestFileStore(t)

	// Simulate the CPM/SOP01/REC01 pre-fix state:
	//   - attic has a full history ending with summary="deleted"
	//   - content file exists at the same path (recreated page)
	//   - reviewflow sidecar for the OLD page is still at the live
	//     meta path (survived its deletion).
	// This is exactly the shape Delete() used to leave behind.
	for i, body := range []string{"v1", "v2", "v3"} {
		summary := "edit"
		if i == len(body)-1 {
			summary = "deleted"
		}
		if err := s.Attic.Archive("/rec01", int64(i+1), []byte(body), "alice", summary, nil); err != nil {
			t.Fatalf("Archive v%d: %v", i+1, err)
		}
	}
	// Make the LAST entry's summary = "deleted".
	entries, _ := s.Attic.ListVersions("/rec01")
	entries[len(entries)-1].Summary = "deleted"
	_ = s.Attic.writeIndex("/rec01", entries)

	// Recreated content file + live reviewflow sidecar from previous life.
	contentPath := filepath.Join(s.contentRoot, "rec01.md")
	if err := os.WriteFile(contentPath, []byte("# Rec\n\nnew content\n"), 0o644); err != nil {
		t.Fatalf("write content: %v", err)
	}
	rfPath := filepath.Join(s.metaRoot, "rec01.reviewflow.json")
	if err := os.MkdirAll(filepath.Dir(rfPath), 0o755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}
	if err := os.WriteFile(rfPath, []byte(`{"roles":{"author":"ghost"}}`), 0o644); err != nil {
		t.Fatalf("write stale sidecar: %v", err)
	}

	n, err := s.ReconcileTombstones()
	if err != nil {
		t.Fatalf("ReconcileTombstones: %v", err)
	}
	if n != 1 {
		t.Errorf("touched %d pages, want 1", n)
	}

	// The old attic is now under @tombstones; the live attic dir has
	// only the holder dir.
	liveAttic, _ := os.ReadDir(s.Attic.pageDir("/rec01"))
	for _, e := range liveAttic {
		if e.Name() != tombstonesSubdir {
			t.Errorf("live attic still has %q after reconcile", e.Name())
		}
	}

	// The orphaned reviewflow sidecar is tombstoned (moved out of live
	// meta tree) — the recreated page no longer inherits ghost roles.
	if _, err := os.Stat(rfPath); !os.IsNotExist(err) {
		t.Errorf("stale reviewflow sidecar still at live path: %v", err)
	}

	// The recreated content file was left alone — the user's new
	// content is intact.
	if _, err := os.Stat(contentPath); err != nil {
		t.Errorf("recreated content file was deleted by reconciler: %v", err)
	}

	// A second run finds nothing to do.
	n2, err := s.ReconcileTombstones()
	if err != nil {
		t.Fatalf("second ReconcileTombstones: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second run touched %d pages, want 0 (idempotence)", n2)
	}
}

func TestReconcileTombstones_IgnoresClean(t *testing.T) {
	t.Parallel()
	s := newTestFileStore(t)

	// A perfectly normal page with no deletion in its history — the
	// reconciler must not touch it.
	if _, err := s.Put("/clean", "# Clean\n\nbody\n", "alice"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	n, err := s.ReconcileTombstones()
	if err != nil {
		t.Fatalf("ReconcileTombstones: %v", err)
	}
	if n != 0 {
		t.Errorf("touched %d pages on a clean store, want 0", n)
	}
	// Attic for /clean has no @tombstones subdir.
	entries, _ := os.ReadDir(s.Attic.pageDir("/clean"))
	for _, e := range entries {
		if e.Name() == tombstonesSubdir {
			t.Errorf("clean page attic grew a @tombstones subdir — reconciler was too eager")
		}
	}
}

// Helper: load a page's current meta from disk, mirroring what the
// server does. Keeps the recreate-starts-fresh test readable.
func (s *FileStore) loadMetaForTest(t *testing.T, pagePath string) PageMetadata {
	t.Helper()
	page, err := s.Get(pagePath)
	if err != nil {
		t.Fatalf("Get %s: %v", pagePath, err)
	}
	return page.Meta
}
