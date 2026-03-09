package comment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store handles file-based persistence of comments under data/meta/.
type Store struct {
	metaRoot string
}

func NewStore(metaRoot string) *Store {
	return &Store{metaRoot: metaRoot}
}

// statePath derives the comment file path for a page.
// /foo/bar → meta/foo/bar.comments.json
func (s *Store) statePath(pagePath string) string {
	clean := strings.TrimPrefix(pagePath, "/")
	if clean == "" {
		clean = "index"
	}
	return filepath.Join(s.metaRoot, filepath.FromSlash(clean)+".comments.json")
}

// Load reads all comments for a page. Returns empty slice if no file exists.
func (s *Store) Load(pagePath string) ([]Comment, error) {
	data, err := os.ReadFile(s.statePath(pagePath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read comments: %w", err)
	}
	var comments []Comment
	if err := json.Unmarshal(data, &comments); err != nil {
		return nil, fmt.Errorf("parse comments: %w", err)
	}
	return comments, nil
}

// Save writes comments atomically.
func (s *Store) Save(pagePath string, comments []Comment) error {
	p := s.statePath(pagePath)

	// Remove file if no comments left.
	if len(comments) == 0 {
		err := os.Remove(p)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create comment meta dir: %w", err)
	}
	data, err := json.MarshalIndent(comments, "", "  ")
	if err != nil {
		return fmt.Errorf("encode comments: %w", err)
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

// Delete removes the comment file for a page.
func (s *Store) Delete(pagePath string) error {
	err := os.Remove(s.statePath(pagePath))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Rename moves the comment sidecar file from oldPath to newPath.
func (s *Store) Rename(oldPath, newPath string) error {
	oldFile := s.statePath(oldPath)
	if _, err := os.Stat(oldFile); os.IsNotExist(err) {
		return nil // no comments to move
	}
	newFile := s.statePath(newPath)
	if err := os.MkdirAll(filepath.Dir(newFile), 0o755); err != nil {
		return fmt.Errorf("create comment meta dir for new path: %w", err)
	}
	return os.Rename(oldFile, newFile)
}
