// Package manual provides embedded user manual pages that are bootstrapped
// into the wiki's content directory on first start.
package manual

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

//go:embed *.md screenshots/*.png
var manualFS embed.FS

// Bootstrap writes the embedded manual pages and screenshots to contentRoot/wiki/manual/.
// It only writes files that don't already exist — it never overwrites
// user-edited content. Returns the number of files written.
func Bootstrap(contentRoot string) int {
	targetDir := filepath.Join(contentRoot, "wiki", "manual")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		log.Printf("manual: failed to create directory: %v", err)
		return 0
	}

	written := 0
	fs.WalkDir(manualFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		targetPath := filepath.Join(targetDir, path)

		// Don't overwrite existing files.
		if _, err := os.Stat(targetPath); err == nil {
			return nil
		}

		// Ensure subdirectories exist (for screenshots/).
		if dir := filepath.Dir(targetPath); dir != targetDir {
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

	return written
}
