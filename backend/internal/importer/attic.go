package importer

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gowiki/backend/internal/storage"
)

// atticVersion represents a single DokuWiki attic entry (one version of one page).
type atticVersion struct {
	Timestamp int64
	Author    string
	Summary   string
	FilePath  string // absolute path to the .txt.gz file
}

// pageHistory groups all attic versions for one page.
type pageHistory struct {
	PageID   string          // DokuWiki page ID (e.g. "pipeline/profile")
	Versions []atticVersion  // sorted by timestamp ascending
}

// reAtticFile matches DokuWiki attic filenames: pagename.TIMESTAMP.txt.gz
var reAtticFile = regexp.MustCompile(`^(.+)\.(\d+)\.txt\.gz$`)

// ImportAttic reads the DokuWiki attic directory, converts each version,
// and writes into the Gowiki attic under destDir.
// It also updates page metadata to reflect the correct version count.
func ImportAttic(opts Options) error {
	dataDir := opts.SrcDataDir()
	atticDir := filepath.Join(dataDir, "attic")
	metaDir := filepath.Join(dataDir, "meta")
	pagesDir := filepath.Join(dataDir, "pages")
	destAtticDir := filepath.Join(opts.DestDir, "attic")
	destMetaDir := filepath.Join(opts.DestDir, "meta")

	if _, err := os.Stat(atticDir); os.IsNotExist(err) {
		log.Println("No attic directory found, skipping version import.")
		return nil
	}

	log.Println("Importing DokuWiki version history (attic)...")

	// Phase 1: Scan attic directory, group by page.
	histories := make(map[string]*pageHistory) // pageID -> history

	err := filepath.Walk(atticDir, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(atticDir, absPath)
		relPath = filepath.ToSlash(relPath)

		dir := filepath.ToSlash(filepath.Dir(relPath))
		base := filepath.Base(relPath)

		m := reAtticFile.FindStringSubmatch(base)
		if m == nil {
			return nil // skip non-matching files
		}

		pageName := m[1]
		timestamp, _ := strconv.ParseInt(m[2], 10, 64)

		// Build page ID: dir/pagename (or just pagename if at root)
		var pageID string
		if dir == "." {
			pageID = pageName
		} else {
			pageID = dir + "/" + pageName
		}

		h, ok := histories[pageID]
		if !ok {
			h = &pageHistory{PageID: pageID}
			histories[pageID] = h
		}
		h.Versions = append(h.Versions, atticVersion{
			Timestamp: timestamp,
			FilePath:  absPath,
		})

		return nil
	})
	if err != nil {
		return fmt.Errorf("walk attic: %w", err)
	}

	// Phase 2: Load changelog data for author info.
	changelogs := loadChangelogs(metaDir)

	// Phase 3: Sort versions and assign authors from changelogs.
	for pageID, h := range histories {
		sort.Slice(h.Versions, func(i, j int) bool {
			return h.Versions[i].Timestamp < h.Versions[j].Timestamp
		})

		cl := changelogs[pageID]
		for i := range h.Versions {
			ts := h.Versions[i].Timestamp
			if entry, ok := cl[ts]; ok {
				h.Versions[i].Author = entry.author
				h.Versions[i].Summary = entry.summary
			}
		}
	}

	// Phase 4: Convert and write each version.
	// First, clear any existing attic entries that the startup migration created,
	// so we can replace them with the proper DokuWiki history.
	if !opts.DryRun {
		for pageID := range histories {
			gowikiPagePath := dokuPageIDToGowikiPath(pageID)
			dir := filepath.Join(destAtticDir, filepath.FromSlash(gowikiPagePath))
			if _, err := os.Stat(dir); err == nil {
				os.RemoveAll(dir)
			}
		}
	}

	totalVersions := 0
	totalPages := 0

	for pageID, h := range histories {
		if opts.DryRun {
			totalPages++
			totalVersions += len(h.Versions)
			continue
		}

		// Determine the Gowiki page path.
		gowikiPagePath := dokuPageIDToGowikiPath(pageID)

		// Source relPath for conversion (used to derive namespace context).
		srcRelPath := pageID + ".txt"

		for i, v := range h.Versions {
			versionNum := int64(i + 1) // 1-based

			// Read and decompress the attic file.
			content, err := readGzipFile(v.FilePath)
			if err != nil {
				if opts.Verbose {
					log.Printf("  skip attic %s v%d: %v", pageID, versionNum, err)
				}
				continue
			}

			// Convert DokuWiki content to Gowiki markdown.
			result := ConvertPage(string(content), srcRelPath, pagesDir)
			markdown := result.Markdown

			// Write to Gowiki attic.
			author := v.Author
			if author == "" {
				author = "unknown"
			}
			ts := time.Unix(v.Timestamp, 0).UTC()

			err = writeAtticVersion(destAtticDir, gowikiPagePath, versionNum, []byte(markdown), author, v.Summary, ts)
			if err != nil {
				if opts.Verbose {
					log.Printf("  error writing attic %s v%d: %v", gowikiPagePath, versionNum, err)
				}
				continue
			}
			totalVersions++
		}

		// Archive the current page content as the latest version.
		newVersion := int64(len(h.Versions)) + 1
		currentContent := readCurrentPageContent(opts.DestDir, gowikiPagePath)
		if currentContent != nil {
			// Use the last changelog entry's author, or fall back to metadata.
			lastAuthor := "system"
			if len(h.Versions) > 0 && h.Versions[len(h.Versions)-1].Author != "" {
				lastAuthor = h.Versions[len(h.Versions)-1].Author
			}
			writeAtticVersion(destAtticDir, gowikiPagePath, newVersion, currentContent, lastAuthor, "imported current version", time.Now().UTC())
			totalVersions++
		}

		// Update page metadata version to match.
		updatePageMetaVersion(destMetaDir, gowikiPagePath, newVersion)

		totalPages++

		if opts.Verbose {
			log.Printf("  %s: %d versions imported", gowikiPagePath, len(h.Versions))
		}
	}

	log.Printf("Attic import complete: %d versions for %d pages.", totalVersions, totalPages)
	return nil
}

// changelogEntry represents one line from a .changes file.
type changelogEntry struct {
	author  string
	summary string
}

// loadChangelogs reads all .changes files from the meta directory.
// Returns map[pageID]map[timestamp]changelogEntry.
func loadChangelogs(metaDir string) map[string]map[int64]changelogEntry {
	result := make(map[string]map[int64]changelogEntry)

	filepath.Walk(metaDir, func(absPath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(absPath, ".changes") {
			return nil
		}

		relPath, _ := filepath.Rel(metaDir, absPath)
		relPath = filepath.ToSlash(relPath)
		// .changes path mirrors page path: "ns/page.changes" -> page ID "ns/page"
		pageID := strings.TrimSuffix(relPath, ".changes")

		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil
		}

		entries := make(map[int64]changelogEntry)
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) < 5 {
				continue
			}
			ts, _ := strconv.ParseInt(fields[0], 10, 64)
			author := fields[4]
			summary := ""
			if len(fields) > 5 {
				summary = fields[5]
			}
			entries[ts] = changelogEntry{author: author, summary: summary}
		}

		result[pageID] = entries
		return nil
	})

	return result
}

// dokuPageIDToGowikiPath converts a DokuWiki page ID to a Gowiki page path.
// e.g. "pipeline/profile" -> "/pipeline/profile"
//      "start" -> "/"
//      "ns/start" -> "/ns"
func dokuPageIDToGowikiPath(pageID string) string {
	// Replace start with index equivalent
	parts := strings.Split(pageID, "/")
	if parts[len(parts)-1] == "start" {
		parts[len(parts)-1] = "index"
	}

	p := "/" + strings.Join(parts, "/")

	// Simplify /path/index to /path
	p = storage.CanonicalPath(p)

	return p
}

// readGzipFile reads and decompresses a gzipped file.
func readGzipFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	return io.ReadAll(gz)
}

// writeAtticVersion writes a single version to the Gowiki attic.
// It creates the gzipped content file and updates the index.json.
func writeAtticVersion(atticRoot, pagePath string, version int64, content []byte, author, summary string, timestamp time.Time) error {
	dir := filepath.Join(atticRoot, filepath.FromSlash(pagePath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create attic dir: %w", err)
	}

	// Write gzipped content.
	vPath := filepath.Join(dir, fmt.Sprintf("%d.md.gz", version))
	// Skip if already exists (idempotent).
	if _, err := os.Stat(vPath); err == nil {
		return nil
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(content); err != nil {
		gz.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := os.WriteFile(vPath, buf.Bytes(), 0644); err != nil {
		return err
	}

	// Update index.json.
	indexPath := filepath.Join(dir, "index.json")
	var entries []atticIndexEntry
	if data, err := os.ReadFile(indexPath); err == nil {
		json.Unmarshal(data, &entries)
	}

	// Check for duplicate version.
	for _, e := range entries {
		if e.Version == version {
			return nil // already exists
		}
	}

	h := md5.Sum(content)
	entries = append(entries, atticIndexEntry{
		Version:   version,
		Timestamp: timestamp.Format(time.RFC3339),
		Author:    author,
		MD5:       hex.EncodeToString(h[:]),
		Summary:   summary,
	})

	// Sort by version.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Version < entries[j].Version
	})

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(indexPath, data, 0644)
}

// atticIndexEntry matches the Gowiki AtticEntry format.
type atticIndexEntry struct {
	Version   int64             `json:"version"`
	Timestamp string            `json:"timestamp"`
	Author    string            `json:"author"`
	MD5       string            `json:"md5"`
	Summary   string            `json:"summary"`
	MediaRefs map[string]int64  `json:"media_refs,omitempty"`
}

// readCurrentPageContent reads the current (already-converted) page content from data/content/.
func readCurrentPageContent(destDir, pagePath string) []byte {
	relPath := strings.TrimPrefix(pagePath, "/")
	if relPath == "" {
		relPath = "index"
	}
	// Try relPath.md first.
	contentFile := filepath.Join(destDir, "content", filepath.FromSlash(relPath)+".md")
	if data, err := os.ReadFile(contentFile); err == nil {
		return data
	}
	// Try relPath/index.md (namespace index pages).
	contentFile = filepath.Join(destDir, "content", filepath.FromSlash(relPath), "index.md")
	if data, err := os.ReadFile(contentFile); err == nil {
		return data
	}
	return nil
}

// updatePageMetaVersion updates the version field in a page's metadata JSON.
func updatePageMetaVersion(metaRoot, pagePath string, newVersion int64) {
	// Determine the meta file path.
	relPath := strings.TrimPrefix(pagePath, "/")
	if relPath == "" || relPath == "/" {
		relPath = "index"
	}
	// Handle namespace pages: /ns -> ns/index
	metaFile := filepath.Join(metaRoot, filepath.FromSlash(relPath)+".json")
	// Also try as index page: /ns -> ns/index.json
	if _, err := os.Stat(metaFile); os.IsNotExist(err) {
		metaFile = filepath.Join(metaRoot, filepath.FromSlash(relPath), "index.json")
	}

	data, err := os.ReadFile(metaFile)
	if err != nil {
		return // page meta doesn't exist, skip
	}

	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		return
	}

	meta["version"] = newVersion

	out, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(metaFile, out, 0644)
}
