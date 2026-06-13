// Package manual provides embedded user manual pages that are bootstrapped
// into the wiki's content directory on first start and on every binary
// upgrade.
package manual

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed *.md screenshots/*.png
var manualFS embed.FS

// markerFile records the hash of the embedded manual that was last written
// to disk. It lives directly under metaRoot (not under wiki/manual) so the
// wipe-and-regenerate cycle does not destroy its own breadcrumb.
const markerFile = ".manual_bootstrap_hash"

// Bootstrap synchronizes contentRoot/wiki/manual/ with the embedded manual.
// On the first run (or whenever the binary ships a different manual than
// the one previously written), it wipes contentRoot/wiki/manual/ and the
// matching meta directory, then writes every embedded file from scratch.
// On subsequent restarts with the same embed, it is a no-op.
//
// The manual is treated as a binary-owned artifact: renames, deletions, and
// content edits in the embedded set propagate cleanly, and any local edits
// to manual pages are overwritten on upgrade. Attic history under
// data/attic/wiki/manual/ is untouched.
//
// Returns the number of files written this call (0 if up to date).
func Bootstrap(contentRoot, metaRoot string) int {
	hash, err := embedHash()
	if err != nil {
		log.Printf("manual: failed to hash embedded manual: %v", err)
		return 0
	}
	markerPath := filepath.Join(metaRoot, markerFile)
	if existing, err := os.ReadFile(markerPath); err == nil &&
		strings.TrimSpace(string(existing)) == hash {
		return 0
	}

	contentTarget := filepath.Join(contentRoot, "wiki", "manual")
	metaTarget := filepath.Join(metaRoot, "wiki", "manual")

	_ = os.RemoveAll(contentTarget)
	_ = os.RemoveAll(metaTarget)

	if err := os.MkdirAll(contentTarget, 0o755); err != nil {
		log.Printf("manual: failed to create directory: %v", err)
		return 0
	}

	written := 0
	fs.WalkDir(manualFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		targetPath := filepath.Join(contentTarget, path)
		if dir := filepath.Dir(targetPath); dir != contentTarget {
			os.MkdirAll(dir, 0o755)
		}

		data, err := manualFS.ReadFile(path)
		if err != nil {
			log.Printf("manual: failed to read embedded %s: %v", path, err)
			return nil
		}

		if err := os.WriteFile(targetPath, data, 0o644); err != nil {
			log.Printf("manual: failed to write %s: %v", targetPath, err)
			return nil
		}

		written++
		return nil
	})

	if err := os.MkdirAll(metaRoot, 0o755); err == nil {
		_ = os.WriteFile(markerPath, []byte(hash+"\n"), 0o644)
	}

	return written
}

// embedHash returns a stable fingerprint of every file in manualFS so that
// upgrades are detected by content rather than by a manually-bumped version
// constant. Paths are sorted to avoid filesystem-order variation.
func embedHash() (string, error) {
	var paths []string
	walkErr := fs.WalkDir(manualFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, path := range paths {
		h.Write([]byte(path))
		h.Write([]byte{0})
		data, err := manualFS.ReadFile(path)
		if err != nil {
			return "", err
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
