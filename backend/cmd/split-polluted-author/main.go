// split-polluted-author retroactively splits author fields of the form
// "username | summary" that were written by the pre-fix MCP write path.
// The pollution lived in two places:
//
//   - page metadata (data/meta/.../page.json): PageMetadata.Author
//   - per-page attic index (data/attic/.../index.json): AtticEntry.Author
//
// For each entry whose Author contains " | ", the part before the pipe
// stays in Author; the part after moves into Summary. Summary is only
// overwritten when it is currently empty — a non-empty Summary wins over
// the suffix (the suffix is almost certainly the same string that was
// also written separately in later rc.3 paths).
//
// Usage:
//
//	go run ./cmd/split-polluted-author -data /opt/gowiki/data [-dry-run]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gowiki/backend/internal/storage"
)

const sep = " | "

// splitAuthor returns the split author + a flag indicating whether the
// input was actually polluted. If so, the suffix is the recovered summary.
func splitAuthor(raw string) (author, summarySuffix string, polluted bool) {
	idx := strings.Index(raw, sep)
	if idx < 0 {
		return raw, "", false
	}
	return strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+len(sep):]), true
}

type counters struct {
	metaScanned, metaFixed   int
	atticScanned, atticFixed int
}

func fixMeta(root string, dryRun bool, c *counters) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var meta storage.PageMetadata
		if err := json.Unmarshal(data, &meta); err != nil {
			// Not a PageMetadata file (could be a plugin's own json).
			return nil
		}
		if meta.ID == "" && meta.Version == 0 && meta.Author == "" {
			// Doesn't look like a page meta file.
			return nil
		}
		c.metaScanned++
		cleanAuthor, suffix, polluted := splitAuthor(meta.Author)
		if !polluted {
			return nil
		}
		meta.Author = cleanAuthor
		// PageMetadata has no Summary field, so the suffix just drops.
		// The current-page meta is a snapshot; the recoverable summary
		// is already (or will be) in the attic entry for that version.
		c.metaFixed++
		if dryRun {
			log.Printf("[dry-run] meta %s: author %q + lost suffix %q", path, cleanAuthor, suffix)
			return nil
		}
		out, err := json.MarshalIndent(&meta, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}
		out = append(out, '\n')
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		return nil
	})
}

func fixAttic(root string, dryRun bool, c *counters) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Base(path) != "index.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var entries []storage.AtticEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			// Index file is corrupt or not an attic index — skip, don't crash.
			log.Printf("skip %s: %v", path, err)
			return nil
		}
		changed := false
		for i := range entries {
			c.atticScanned++
			cleanAuthor, suffix, polluted := splitAuthor(entries[i].Author)
			if !polluted {
				continue
			}
			entries[i].Author = cleanAuthor
			if entries[i].Summary == "" {
				entries[i].Summary = suffix
			}
			c.atticFixed++
			changed = true
		}
		if !changed {
			return nil
		}
		if dryRun {
			log.Printf("[dry-run] attic %s: %d entries fixed", path, c.atticFixed)
			return nil
		}
		out, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s: %w", path, err)
		}
		out = append(out, '\n')
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		return nil
	})
}

func main() {
	dataDir := flag.String("data", "./data", "data directory root (contains meta/ and attic/)")
	dryRun := flag.Bool("dry-run", false, "scan and report, don't rewrite anything")
	flag.Parse()

	metaRoot := filepath.Join(*dataDir, "meta")
	atticRoot := filepath.Join(*dataDir, "attic")

	c := &counters{}

	if _, err := os.Stat(metaRoot); err == nil {
		log.Printf("scanning %s", metaRoot)
		if err := fixMeta(metaRoot, *dryRun, c); err != nil {
			log.Fatalf("meta scan: %v", err)
		}
	} else {
		log.Printf("skipping meta (missing %s)", metaRoot)
	}

	if _, err := os.Stat(atticRoot); err == nil {
		log.Printf("scanning %s", atticRoot)
		if err := fixAttic(atticRoot, *dryRun, c); err != nil {
			log.Fatalf("attic scan: %v", err)
		}
	} else {
		log.Printf("skipping attic (missing %s)", atticRoot)
	}

	mode := "fixed"
	if *dryRun {
		mode = "would fix"
	}
	fmt.Printf("meta: %d scanned, %d %s\n", c.metaScanned, c.metaFixed, mode)
	fmt.Printf("attic: %d scanned, %d %s\n", c.atticScanned, c.atticFixed, mode)
}
