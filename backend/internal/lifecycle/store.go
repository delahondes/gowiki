package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Store is the on-disk index of lifecycle rules extracted from page
// markdown. Persisted as one JSON file per source page, so a page-save
// hook can atomically replace all rules for that page with the newly
// parsed set (or delete the file when no directive remains).
//
// Rationale for per-page files (vs one global rules.json):
//   - Concurrent saves to different pages don't fight over one file.
//   - Rebuild on startup is a cheap walk of a small directory.
//   - Deleting a page cleans up its rules with a plain os.Remove — no
//     read-modify-write on a global map.
//
// File layout: data/meta/_lifecycle/{sha1(source_page)}.json.
type Store struct {
	mu  sync.RWMutex
	dir string
}

// NewStore prepares the directory. Call Load() to warm the in-memory
// cache; the disk is authoritative regardless.
func NewStore(metaRoot string) (*Store, error) {
	dir := filepath.Join(metaRoot, "_lifecycle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create lifecycle dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// pageFile returns the JSON path for a source page's rules. Hashing
// keeps the filesystem happy for pages whose canonical path has
// characters that would otherwise need escaping.
func (s *Store) pageFile(sourcePage string) string {
	h := sha1Hex(sourcePage)
	return filepath.Join(s.dir, h+".json")
}

// pageEntry is what a per-page file holds. Rules are stored as a plain
// slice; the source page is repeated so a raw file dump has enough
// context without a filename lookup.
type pageEntry struct {
	SourcePage string `json:"source_page"`
	Rules      []Rule `json:"rules"`
}

// SetPageRules replaces every rule attached to sourcePage with `rules`.
// Empty slice deletes the entry — used when a page save removes the
// last {lifecycle} directive from a page.
func (s *Store) SetPageRules(sourcePage string, rules []Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.pageFile(sourcePage)
	if len(rules) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove page entry: %w", err)
		}
		return nil
	}
	entry := pageEntry{SourcePage: sourcePage, Rules: rules}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal page entry: %w", err)
	}
	data = append(data, '\n')
	return writeAtomic(path, data)
}

// AllRules returns every rule known to the store, across all source
// pages, sorted by rule ID for deterministic iteration order. The slice
// is a fresh copy the caller may sort or filter further.
func (s *Store) AllRules() ([]Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Rule
	for _, ent := range entries {
		if ent.IsDir() || filepath.Ext(ent.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, ent.Name()))
		if err != nil {
			// A single corrupt file shouldn't blank the whole store —
			// scanner still runs against every other rule.
			continue
		}
		var entry pageEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		out = append(out, entry.Rules...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// RulesForSourcePage returns just the rules attached to one source page.
// Handy for the wiki-hook "recompute for this page" path, and for tests.
func (s *Store) RulesForSourcePage(sourcePage string) ([]Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(s.pageFile(sourcePage))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var entry pageEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("parse page entry: %w", err)
	}
	return entry.Rules, nil
}
