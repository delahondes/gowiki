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
	"sync"
	"time"
)

// MediaVersionStore tracks the current version number of each media file.
// Persisted as data/meta/_mediaversions.json.
type MediaVersionStore struct {
	mu       sync.RWMutex
	versions map[string]int64 // media path -> current version
	path     string           // path to _mediaversions.json
}

// NewMediaVersionStore creates a new MediaVersionStore.
// metaRoot is the data/meta/ directory.
func NewMediaVersionStore(metaRoot string) *MediaVersionStore {
	return &MediaVersionStore{
		versions: make(map[string]int64),
		path:     filepath.Join(metaRoot, "_mediaversions.json"),
	}
}

// Load reads the version map from disk. If the file doesn't exist, initializes empty.
func (s *MediaVersionStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.versions = make(map[string]int64)
			return nil
		}
		return fmt.Errorf("read media versions: %w", err)
	}

	var v map[string]int64
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("parse media versions: %w", err)
	}
	if v == nil {
		v = make(map[string]int64)
	}
	s.versions = v
	return nil
}

// GetVersion returns the current version for a media path (0 if not tracked).
func (s *MediaVersionStore) GetVersion(mediaPath string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.versions[mediaPath]
}

// IncrementVersion increments the version for a media path, persists, and returns the new version.
func (s *MediaVersionStore) IncrementVersion(mediaPath string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.versions[mediaPath]++
	newVersion := s.versions[mediaPath]

	if err := s.saveLocked(); err != nil {
		return newVersion, fmt.Errorf("save media versions: %w", err)
	}
	return newVersion, nil
}

// SetVersion sets the version for a media path (used for first upload) and persists.
func (s *MediaVersionStore) SetVersion(mediaPath string, version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.versions[mediaPath] = version
	return s.saveLocked()
}

// GetAllVersions returns a copy of the full version map.
func (s *MediaVersionStore) GetAllVersions() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]int64, len(s.versions))
	for k, v := range s.versions {
		out[k] = v
	}
	return out
}

// GetVersionsForPaths returns a map of media path -> current version for the given paths.
func (s *MediaVersionStore) GetVersionsForPaths(paths []string) map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]int64, len(paths))
	for _, p := range paths {
		if v, ok := s.versions[p]; ok {
			out[p] = v
		}
	}
	return out
}

// saveLocked persists the version map. Caller must hold s.mu for writing.
func (s *MediaVersionStore) saveLocked() error {
	data, err := json.MarshalIndent(s.versions, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(s.path, data)
}

// MediaAtticEntry describes one archived version of a media file.
type MediaAtticEntry struct {
	Version   int64  `json:"version"`
	Timestamp string `json:"timestamp"`
	Author    string `json:"author"`
	MD5       string `json:"md5"`
	Size      int64  `json:"size"`
}

// MediaAttic manages the version archive for media files under data/attic/media/.
type MediaAttic struct {
	root string // e.g. data/attic/media
}

// NewMediaAttic creates a new MediaAttic.
// dataDir is the data/ directory.
func NewMediaAttic(dataDir string) *MediaAttic {
	return &MediaAttic{root: filepath.Join(dataDir, "attic", "media")}
}

func (a *MediaAttic) mediaDir(mediaPath string) string {
	return filepath.Join(a.root, filepath.FromSlash(mediaPath))
}

func (a *MediaAttic) indexPath(mediaPath string) string {
	return filepath.Join(a.mediaDir(mediaPath), "index.json")
}

func (a *MediaAttic) versionFile(mediaPath string, version int64) string {
	return filepath.Join(a.mediaDir(mediaPath), fmt.Sprintf("%d.gz", version))
}

// Archive stores a version of a media file as gzipped content and updates the index.
func (a *MediaAttic) Archive(mediaPath string, version int64, content []byte, author string) error {
	dir := a.mediaDir(mediaPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create media attic dir: %w", err)
	}

	// Skip if already archived.
	vPath := a.versionFile(mediaPath, version)
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
		return fmt.Errorf("write media attic version: %w", err)
	}

	// Update index.
	entries, _ := a.readIndex(mediaPath)
	entries = append(entries, MediaAtticEntry{
		Version:   version,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Author:    author,
		MD5:       md5sum(content),
		Size:      int64(len(content)),
	})
	return a.writeIndex(mediaPath, entries)
}

// ReadVersion returns the content of a specific media version.
func (a *MediaAttic) ReadVersion(mediaPath string, version int64) ([]byte, error) {
	vPath := a.versionFile(mediaPath, version)
	data, err := os.ReadFile(vPath)
	if err != nil {
		return nil, fmt.Errorf("read media attic version: %w", err)
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

// ListVersions returns all archived versions for a media file, sorted by version number.
func (a *MediaAttic) ListVersions(mediaPath string) ([]MediaAtticEntry, error) {
	entries, err := a.readIndex(mediaPath)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Version < entries[j].Version
	})
	return entries, nil
}

func (a *MediaAttic) readIndex(mediaPath string) ([]MediaAtticEntry, error) {
	data, err := os.ReadFile(a.indexPath(mediaPath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read media attic index: %w", err)
	}
	var entries []MediaAtticEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse media attic index: %w", err)
	}
	return entries, nil
}

func (a *MediaAttic) writeIndex(mediaPath string, entries []MediaAtticEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode media attic index: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(a.indexPath(mediaPath), data)
}
