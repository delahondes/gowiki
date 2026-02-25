package storage

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AtticEntry describes one archived version of a page.
type AtticEntry struct {
	Version   int64            `json:"version"`
	Timestamp string           `json:"timestamp"`
	Author    string           `json:"author"`
	MD5       string           `json:"md5"`
	Summary   string           `json:"summary"`
	MediaRefs map[string]int64 `json:"media_refs,omitempty"`
}

// Attic manages the version archive under data/attic/.
type Attic struct {
	root string // e.g. data/attic
}

func NewAttic(dataDir string) *Attic {
	root := filepath.Join(dataDir, "attic")
	return &Attic{root: root}
}

func (a *Attic) pageDir(pagePath string) string {
	return filepath.Join(a.root, filepath.FromSlash(pagePath))
}

func (a *Attic) indexPath(pagePath string) string {
	return filepath.Join(a.pageDir(pagePath), "index.json")
}

func (a *Attic) versionFile(pagePath string, version int64) string {
	return filepath.Join(a.pageDir(pagePath), fmt.Sprintf("%d.md.gz", version))
}

// Archive stores a version of a page as a gzipped markdown file and updates the per-page index.
// If the version file already exists, it is a no-op (dedup).
// mediaRefs is optional: if non-nil, it records the media path -> media version at time of archive.
func (a *Attic) Archive(pagePath string, version int64, content []byte, author, summary string, mediaRefs map[string]int64) error {
	dir := a.pageDir(pagePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create attic dir: %w", err)
	}

	// Skip if this version is already archived.
	vPath := a.versionFile(pagePath, version)
	if _, err := os.Stat(vPath); err == nil {
		return nil
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(content); err != nil {
		gz.Close()
		return fmt.Errorf("gzip write: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}
	if err := writeFileAtomic(vPath, buf.Bytes()); err != nil {
		return fmt.Errorf("write attic version: %w", err)
	}

	// Update index.
	entries, _ := a.readIndex(pagePath) // ignore error on first write
	entry := AtticEntry{
		Version:   version,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Author:    author,
		MD5:       md5sum(content),
		Summary:   summary,
	}
	if len(mediaRefs) > 0 {
		entry.MediaRefs = mediaRefs
	}
	entries = append(entries, entry)
	return a.writeIndex(pagePath, entries)
}

// ReadVersion returns the markdown content of a specific version.
func (a *Attic) ReadVersion(pagePath string, version int64) ([]byte, error) {
	vPath := a.versionFile(pagePath, version)
	data, err := os.ReadFile(vPath)
	if err != nil {
		return nil, fmt.Errorf("read attic version: %w", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()
	content, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("gzip read: %w", err)
	}
	return content, nil
}

// ListVersions returns all archived versions for a page, sorted by version number.
func (a *Attic) ListVersions(pagePath string) ([]AtticEntry, error) {
	entries, err := a.readIndex(pagePath)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Version < entries[j].Version
	})
	return entries, nil
}

// GetEntry returns the attic entry for a specific version, or nil if not found.
func (a *Attic) GetEntry(pagePath string, version int64) (*AtticEntry, error) {
	entries, err := a.readIndex(pagePath)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Version == version {
			return &entries[i], nil
		}
	}
	return nil, nil
}

func (a *Attic) readIndex(pagePath string) ([]AtticEntry, error) {
	data, err := os.ReadFile(a.indexPath(pagePath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read attic index: %w", err)
	}
	var entries []AtticEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse attic index: %w", err)
	}
	return entries, nil
}

func (a *Attic) writeIndex(pagePath string, entries []AtticEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode attic index: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(a.indexPath(pagePath), data)
}

// MigrateExistingPages creates version 1 attic entries for all existing pages
// that don't already have attic records. Called on startup.
func (a *Attic) MigrateExistingPages(contentRoot, metaRoot string, changelog *Changelog) error {
	return filepath.Walk(contentRoot, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(absPath, ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(contentRoot, absPath)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		pagePath := strings.TrimSuffix(rel, ".md")
		pagePath = strings.TrimSuffix(pagePath, "/index")

		// Skip if attic already has entries.
		existing, _ := a.readIndex(pagePath)
		if len(existing) > 0 {
			return nil
		}

		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return readErr
		}

		// Read existing metadata to get the version number.
		metaRel := strings.TrimSuffix(rel, ".md") + ".json"
		metaPath := filepath.Join(metaRoot, filepath.FromSlash(metaRel))
		var version int64 = 1
		var author string = "system"
		if metaData, err := os.ReadFile(metaPath); err == nil {
			var meta PageMetadata
			if json.Unmarshal(metaData, &meta) == nil && meta.Version > 0 {
				version = meta.Version
			}
		}

		if err := a.Archive(pagePath, version, content, author, "migration: initial archive", nil); err != nil {
			return fmt.Errorf("migrate %s: %w", pagePath, err)
		}

		if changelog != nil {
			changelog.Append(pagePath, version, author, "migration: initial archive", "migrate")
		}

		return nil
	})
}
