// tag-attic-validated stamps attic entries for validated reviewflow versions
// with plugin_meta["reviewflow"] so the history UI shows them as validated.
//
// Usage:
//
//	go run ./cmd/tag-attic-validated -data /opt/gowiki/data [-dry-run]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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

	tagged := 0
	for _, rfPath := range rfFiles {
		rel, err := filepath.Rel(metaRoot, rfPath)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		pagePath := storage.CanonicalPath(strings.TrimSuffix(rel, ".reviewflow.json"))

		st, err := rfStore.Load(pagePath)
		if err != nil || st == nil || len(st.VersionHistory) == 0 {
			continue
		}

		for _, vr := range st.VersionHistory {
			// Check if already tagged.
			entry, err := attic.GetEntry(pagePath, vr.PageVersion)
			if err != nil || entry == nil {
				log.Printf("SKIP %s v%d: attic entry not found", pagePath, vr.PageVersion)
				continue
			}
			if entry.PluginMeta != nil {
				if _, exists := entry.PluginMeta["reviewflow"]; exists {
					continue // already tagged
				}
			}

			meta := reviewflow.AtticMeta{
				VersionTag:  vr.VersionTag,
				ConfirmedBy: vr.ConfirmedBy,
				IsValidated: true,
			}
			data, _ := json.Marshal(meta)

			if *dryRun {
				fmt.Printf("DRY-RUN %s v%d: would tag as validated %s\n", pagePath, vr.PageVersion, vr.VersionTag)
				continue
			}

			if err := attic.UpdateEntryMeta(pagePath, vr.PageVersion, "reviewflow", data); err != nil {
				log.Printf("ERROR %s v%d: %v", pagePath, vr.PageVersion, err)
				continue
			}
			log.Printf("OK %s v%d: tagged as validated %s", pagePath, vr.PageVersion, vr.VersionTag)
			tagged++
		}
	}

	log.Printf("Done. Tagged %d attic entries.", tagged)
}
