package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReconcileTombstones finds page paths that were deleted before the
// tombstone-on-delete fix shipped (so the previous life's attic was
// left in place and the recreated page's new history collides with the
// old numbering), and tombstones them retroactively. Also handles the
// crash case where Delete() archived the "deleted" marker but failed
// before running the tombstone + content-removal steps.
//
// Detection: for every page attic dir whose latest index entry has
// summary == "deleted", move it into a @tombstones subdir iff a live
// companion file still exists for the same path — either a .md under
// contentRoot or a .reviewflow.json under metaRoot. Both of those are
// signals that the "previous life ended in deletion" state didn't
// actually end: the content was recreated, or the sidecar survived
// the deletion, or both.
//
// Idempotent. Returns the number of pages tombstoned.
func (s *FileStore) ReconcileTombstones() (int, error) {
	if s.Attic == nil {
		return 0, nil
	}
	root := s.Attic.root
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("stat attic root: %w", err)
	}
	if !info.IsDir() {
		return 0, nil
	}

	var touched int
	walkErr := filepath.Walk(root, func(abspath string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if fi.Name() != "index.json" {
			return nil
		}
		// Skip anything already inside a @tombstones subtree.
		rel, relErr := filepath.Rel(root, abspath)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if strings.Contains(relSlash, "/"+tombstonesSubdir+"/") || strings.HasPrefix(relSlash, tombstonesSubdir+"/") {
			return nil
		}

		pagePath := "/" + strings.TrimSuffix(relSlash, "/index.json")
		if pagePath == "/" {
			// Root page attic sits at data/attic/index.json — skip the
			// root for now; the reconciler is for namespaced pages
			// whose previous life collided with a recreation.
			return nil
		}

		if s.Attic.LatestEntrySummary(pagePath) != "deleted" {
			return nil
		}

		if !s.hasLiveCompanion(pagePath) {
			return nil
		}

		if _, tombErr := s.tombstoneDrift(pagePath); tombErr != nil {
			return fmt.Errorf("tombstone drift for %s: %w", pagePath, tombErr)
		}
		touched++
		return nil
	})
	if walkErr != nil {
		return touched, walkErr
	}
	return touched, nil
}

// hasLiveCompanion reports whether the file tree still carries state
// for pagePath — content file or reviewflow sidecar. Either is enough
// to prove the "previous life ended with a delete" claim is stale.
func (s *FileStore) hasLiveCompanion(pagePath string) bool {
	trimmed := strings.TrimPrefix(pagePath, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return false
	}
	leafContent := filepath.Join(s.contentRoot, filepath.FromSlash(trimmed)+".md")
	indexContent := filepath.Join(s.contentRoot, filepath.FromSlash(trimmed), "index.md")
	leafRf := filepath.Join(s.metaRoot, filepath.FromSlash(trimmed)+".reviewflow.json")
	indexRf := filepath.Join(s.metaRoot, filepath.FromSlash(trimmed), "index.reviewflow.json")
	leafMeta := filepath.Join(s.metaRoot, filepath.FromSlash(trimmed)+".json")
	indexMeta := filepath.Join(s.metaRoot, filepath.FromSlash(trimmed), "index.json")
	for _, p := range []string{leafContent, indexContent, leafRf, indexRf, leafMeta, indexMeta} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// tombstoneDrift runs the move half of Delete() for a path whose
// previous Delete() predates the tombstone fix. It calls Attic.Tombstone
// to move the attic entries, then moves EVERY surviving meta file for
// the same path into the same tombstone dir.
//
// The content file is deliberately left in place: the recreated page's
// content is what the user wants to keep. All meta — including the
// recreated page's own meta.json, if the user already saved it once —
// goes to the tombstone. The next Put on that path then regenerates a
// fresh ID + Version=1 via readOrInitMeta. That's the right default:
// after a delete, "a different page lives here now" is the whole
// point, so inheriting the dead page's reviewflow panel, comments or
// version counter would be the bug, not the fix.
//
// Both the leaf shape (meta/foo.json + .reviewflow.json etc.) and the
// index shape (meta/foo/index.json + .reviewflow.json etc.) are moved
// — if both exist on disk the architecture is already broken, but
// tombstoning either shape independently keeps the reconciler simple.
func (s *FileStore) tombstoneDrift(pagePath string) (string, error) {
	now := time.Now()
	tombDir, err := s.Attic.Tombstone(pagePath, now)
	if err != nil {
		return "", err
	}

	trimmed := strings.TrimPrefix(pagePath, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return tombDir, nil
	}

	leafMeta := filepath.Join(s.metaRoot, filepath.FromSlash(trimmed)+".json")
	indexMeta := filepath.Join(s.metaRoot, filepath.FromSlash(trimmed), "index.json")

	// tombstoneMetaSidecars uses metaPath to derive the prefix for
	// matching sibling sidecars. Even when the base file (foo.json or
	// index.json) doesn't exist on disk, call it anyway — the function
	// will still scan metaDir and move any `prefix.X.json` sidecars
	// that are present. The base file's existence is incidental.
	for _, metaPath := range []string{leafMeta, indexMeta} {
		if err := tombstoneMetaSidecars(metaPath, tombDir); err != nil {
			return tombDir, err
		}
	}
	return tombDir, nil
}
