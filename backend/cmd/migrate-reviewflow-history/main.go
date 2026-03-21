// migrate-reviewflow-history scans attic entries for pages with reviewflow
// directives and populates version_history in the reviewflow state files.
//
// For each reviewflow page, it reads every attic version's markdown, parses
// the {reviewflow} directive to extract the version tag, identifies when
// version tags changed, and records the last entry for each completed
// version tag as a validated version in version_history.
//
// Usage:
//
//	go run ./cmd/migrate-reviewflow-history -data /opt/gowiki/data [-dry-run]
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gowiki/backend/internal/reviewflow"
	"gowiki/backend/internal/storage"
)

func main() {
	dataDir := flag.String("data", "data", "path to data directory")
	dryRun := flag.Bool("dry-run", false, "print changes without writing")
	flag.Parse()

	metaRoot := filepath.Join(*dataDir, "meta")
	attic := storage.NewAttic(*dataDir)
	rfStore := reviewflow.NewStore(metaRoot)

	// Find all .reviewflow.json files.
	var rfFiles []string
	filepath.Walk(metaRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".reviewflow.json") {
			rfFiles = append(rfFiles, path)
		}
		return nil
	})

	log.Printf("Found %d reviewflow state files", len(rfFiles))

	updated := 0
	for _, rfPath := range rfFiles {
		// Derive page path from meta file path.
		rel, err := filepath.Rel(metaRoot, rfPath)
		if err != nil {
			log.Printf("SKIP %s: %v", rfPath, err)
			continue
		}
		rel = filepath.ToSlash(rel)
		pagePath := storage.CanonicalPath(strings.TrimSuffix(rel, ".reviewflow.json"))

		// Load current state.
		st, err := rfStore.Load(pagePath)
		if err != nil || st == nil {
			log.Printf("SKIP %s: load error or nil state", pagePath)
			continue
		}

		// Skip if version_history is already populated.
		if len(st.VersionHistory) > 0 {
			log.Printf("SKIP %s: already has %d version history entries", pagePath, len(st.VersionHistory))
			continue
		}

		// Load attic entries.
		entries, err := attic.ListVersions(pagePath)
		if err != nil || len(entries) == 0 {
			log.Printf("SKIP %s: no attic entries", pagePath)
			continue
		}

		// Walk through entries in order, parsing the version tag from each.
		type taggedEntry struct {
			version    int64
			tag        string
			timestamp  string
			author     string
		}
		var tagged []taggedEntry
		for _, e := range entries {
			content, err := attic.ReadVersion(pagePath, e.Version)
			if err != nil {
				continue
			}
			_, versionTag, found := reviewflow.ParseDirective(string(content))
			if !found {
				// No directive in this version — record empty tag.
				tagged = append(tagged, taggedEntry{
					version:   e.Version,
					tag:       "",
					timestamp: e.Timestamp,
					author:    e.Author,
				})
				continue
			}
			tagged = append(tagged, taggedEntry{
				version:   e.Version,
				tag:       versionTag,
				timestamp: e.Timestamp,
				author:    e.Author,
			})
		}

		if len(tagged) == 0 {
			log.Printf("SKIP %s: no parseable entries", pagePath)
			continue
		}

		// Identify completed version tags: walk forward, when the tag changes
		// from X to Y, the previous entry is the "validated" version for X.
		var history []reviewflow.VersionRecord
		for i := 1; i < len(tagged); i++ {
			prev := tagged[i-1]
			curr := tagged[i]
			if prev.tag != "" && prev.tag != curr.tag {
				// Tag changed: prev is the last version with that tag.
				ts, _ := time.Parse(time.RFC3339, prev.timestamp)
				history = append(history, reviewflow.VersionRecord{
					PageVersion: prev.version,
					Timestamp:   ts,
					VersionTag:  prev.tag,
					ConfirmedBy: make(map[string]string), // unknown from history
				})
			}
		}

		if len(history) == 0 {
			log.Printf("SKIP %s: no version tag transitions found (current tag: %q, %d entries)",
				pagePath, st.VersionTag, len(tagged))
			continue
		}

		// Set validated_page_version to the last history entry's page version.
		latestValidated := history[len(history)-1].PageVersion

		if *dryRun {
			fmt.Printf("DRY-RUN %s: would add %d version history entries\n", pagePath, len(history))
			for _, h := range history {
				fmt.Printf("  tag=%s  page_version=%d  timestamp=%s\n",
					h.VersionTag, h.PageVersion, h.Timestamp.Format(time.RFC3339))
			}
			continue
		}

		st.VersionHistory = history
		st.ValidatedVersion = latestValidated
		if err := rfStore.Save(pagePath, st); err != nil {
			log.Printf("ERROR %s: save failed: %v", pagePath, err)
			continue
		}
		log.Printf("OK %s: added %d version history entries (latest validated: v%d tag=%s)",
			pagePath, len(history), latestValidated, history[len(history)-1].VersionTag)
		updated++
	}

	log.Printf("Done. Updated %d/%d reviewflow states.", updated, len(rfFiles))
}
