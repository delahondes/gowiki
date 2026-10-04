package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// User-reported: edit_page refused /regulatory/qms/qara/sop14 (lock
// held) but allowed /regulatory/qms/qara/sop14/ (same page). The two
// URL forms wrote to different lock files on disk — "foo/bar.lock.json"
// vs "foo/bar/.lock.json". Pins that the DraftStore collapses both
// forms onto one lock when a FileStore-backed resolver is wired in.

// Shared harness: a real FileStore so DraftStore gets its SetLockKeyResolver
// hook. The draft store on its own has no way to tell a namespace index
// from a leaf; the resolver carries that knowledge.
func newLockKeyFileStore(t *testing.T) *FileStore {
	t.Helper()
	root := t.TempDir()
	fs, err := NewFileStore(filepath.Join(root, "content"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return fs
}

func TestLockKey_NamespaceIndex_BothURLFormsShareLock(t *testing.T) {
	fs := newLockKeyFileStore(t)
	// Seed a namespace index file so resolveExistingContentPath can tell
	// us it IS a namespace index.
	if _, err := fs.Put("qara/sop14", "# ns index\n", "seed"); err != nil {
		t.Fatalf("Put leaf (will be converted): %v", err)
	}
	if _, err := fs.ConvertToNamespaceIndex("qara/sop14", "seed"); err != nil {
		t.Fatalf("ConvertToNamespaceIndex: %v", err)
	}

	// Alice takes the lock through the trailing-slash form.
	if _, _, err := fs.Drafts.EnterEditMode("/qara/sop14/", "alice", false, "# ns index\n"); err != nil {
		t.Fatalf("EnterEditMode via /qara/sop14/: %v", err)
	}

	// Bob tries the no-slash form — must see Alice's lock, not an
	// unlocked second file.
	_, _, err := fs.Drafts.EnterEditMode("/qara/sop14", "bob", false, "# ns index\n")
	if err == nil {
		t.Fatalf("EnterEditMode via /qara/sop14 as bob: want ErrPageLocked, got nil")
	}
	// Same holds for the owner-check path — Alice from the no-slash
	// form must recognise HER OWN lock (otherwise ErrEditSuperseded).
	if lock := fs.Drafts.GetLock("/qara/sop14"); lock.Owner != "alice" {
		t.Errorf("GetLock via no-slash form: owner = %q, want alice", lock.Owner)
	}
	if lock := fs.Drafts.GetLock("qara/sop14/"); lock.Owner != "alice" {
		t.Errorf("GetLock via leading-slash-free form with trailing /: owner = %q, want alice", lock.Owner)
	}
}

func TestLockKey_LeafPage_LockFileNextToContent(t *testing.T) {
	fs := newLockKeyFileStore(t)
	if _, err := fs.Put("docs/plain", "# plain\n", "seed"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, _, err := fs.Drafts.EnterEditMode("/docs/plain", "alice", false, "# plain\n"); err != nil {
		t.Fatalf("EnterEditMode: %v", err)
	}

	// Lock file should mirror the content tree: docs/plain.lock.json
	// alongside docs/plain.md, NOT docs/plain/.lock.json.
	if _, err := os.Stat(filepath.Join(fs.metaRoot, "docs", "plain.lock.json")); err != nil {
		t.Errorf("expected lock file docs/plain.lock.json; got: %v", err)
	}
	// The accidental trailing-slash form must find the same lock.
	if lock := fs.Drafts.GetLock("/docs/plain/"); lock.Owner != "alice" {
		t.Errorf("trailing-slash form on a leaf page: owner = %q, want alice", lock.Owner)
	}
}

func TestLockKey_NamespaceIndex_LockFileInsideNamespaceDir(t *testing.T) {
	fs := newLockKeyFileStore(t)
	// Create a namespace index at docs/guide/index.md.
	if _, err := fs.Put("docs/guide", "# g\n", "seed"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := fs.ConvertToNamespaceIndex("docs/guide", "seed"); err != nil {
		t.Fatalf("ConvertToNamespaceIndex: %v", err)
	}

	if _, _, err := fs.Drafts.EnterEditMode("/docs/guide/", "alice", false, "# g\n"); err != nil {
		t.Fatalf("EnterEditMode: %v", err)
	}

	// Lock file at metaRoot/docs/guide/index.lock.json (mirrors
	// content/docs/guide/index.md) — not docs/guide.lock.json which
	// would be a leaf-page lock, and not docs/guide/.lock.json which
	// was the buggy "hidden file" form.
	if _, err := os.Stat(filepath.Join(fs.metaRoot, "docs", "guide", "index.lock.json")); err != nil {
		t.Errorf("expected lock file docs/guide/index.lock.json; got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fs.metaRoot, "docs", "guide.lock.json")); err == nil {
		t.Errorf("docs/guide.lock.json must NOT exist — that path is for a leaf page")
	}
}

func TestLockKey_NewNamespaceIndex_TrailingSlashRouting(t *testing.T) {
	// For a page that doesn't exist yet, the trailing slash is the
	// only signal of "I'm about to create a namespace index". The
	// resolver must honor it so the lock file lands in the right
	// place from the first save.
	fs := newLockKeyFileStore(t)

	if _, _, err := fs.Drafts.EnterEditMode("/new/ns/", "alice", false, ""); err != nil {
		t.Fatalf("EnterEditMode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fs.metaRoot, "new", "ns", "index.lock.json")); err != nil {
		t.Errorf("expected lock file new/ns/index.lock.json for new namespace index; got: %v", err)
	}
}
