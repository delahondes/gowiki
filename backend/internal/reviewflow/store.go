package reviewflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store handles file-based persistence of reviewflow state under data/meta/.
type Store struct {
	metaRoot string
}

func NewStore(metaRoot string) *Store {
	return &Store{metaRoot: metaRoot}
}

// statePath derives the reviewflow state file path for a page.
// It checks both possible locations (leaf and namespace index) to handle
// pages regardless of whether the path has a trailing slash.
func (s *Store) statePath(pagePath string) string {
	clean := strings.TrimPrefix(pagePath, "/")
	clean = strings.TrimRight(clean, "/")
	if clean == "" {
		clean = "index"
	}

	// Primary: meta/{path}.reviewflow.json (leaf page)
	leafPath := filepath.Join(s.metaRoot, filepath.FromSlash(clean)+".reviewflow.json")
	// Alternative: meta/{path}/index.reviewflow.json (namespace index)
	indexPath := filepath.Join(s.metaRoot, filepath.FromSlash(clean), "index.reviewflow.json")

	// If the leaf path exists, use it.
	if _, err := os.Stat(leafPath); err == nil {
		return leafPath
	}
	// If the index path exists, use it.
	if _, err := os.Stat(indexPath); err == nil {
		return indexPath
	}

	// Neither exists — determine the correct path by checking the content directory.
	// If content/{path}/index.md exists, it's a namespace index.
	contentRoot := strings.Replace(s.metaRoot, "meta", "content", 1)
	indexMdPath := filepath.Join(contentRoot, filepath.FromSlash(clean), "index.md")
	if _, err := os.Stat(indexMdPath); err == nil {
		return indexPath
	}

	return leafPath
}

// Load reads the reviewflow state for a page. Returns nil, nil if no state exists.
func (s *Store) Load(pagePath string) (*State, error) {
	data, err := os.ReadFile(s.statePath(pagePath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read reviewflow state: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse reviewflow state: %w", err)
	}
	return &st, nil
}

// Save writes the reviewflow state atomically.
func (s *Store) Save(pagePath string, st *State) error {
	p := s.statePath(pagePath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create reviewflow meta dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode reviewflow state: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: temp file + rename.
	tmp, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

// Delete removes the reviewflow state file for a page.
func (s *Store) Delete(pagePath string) error {
	err := os.Remove(s.statePath(pagePath))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
