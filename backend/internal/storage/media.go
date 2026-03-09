package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrMediaConflict = errors.New("media already exists")

type MediaEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Kind      string    `json:"kind"` // file | folder
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int64     `json:"version,omitempty"` // current media version (0 if untracked)
}

type MediaFileStore struct {
	rootDir       string
	VersionStore  *MediaVersionStore
	MediaAttic    *MediaAttic
}

func NewMediaFileStore(rootDir string) (*MediaFileStore, error) {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create media root: %w", err)
	}
	return &MediaFileStore{rootDir: rootDir}, nil
}

func (s *MediaFileStore) List(namespacePath string) ([]MediaEntry, error) {
	ns, err := normalizeMediaNamespacePath(namespacePath)
	if err != nil {
		return nil, err
	}

	dirPath, err := s.namespaceDirPath(ns)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dirPath)
	if errors.Is(err, os.ErrNotExist) {
		return []MediaEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read media namespace: %w", err)
	}

	out := make([]MediaEntry, 0, len(entries))
	for _, entry := range entries {
		info, statErr := entry.Info()
		if statErr != nil {
			return nil, fmt.Errorf("read media entry info: %w", statErr)
		}
		kind := "file"
		size := info.Size()
		if entry.IsDir() {
			kind = "folder"
			size = 0
		}
		name := entry.Name()
		entryPath := name
		if ns != "" {
			entryPath = ns + "/" + name
		}
		var ver int64
		if kind == "file" && s.VersionStore != nil {
			ver = s.VersionStore.GetVersion(entryPath)
		}
		out = append(out, MediaEntry{
			Name:      name,
			Path:      "/" + entryPath,
			Kind:      kind,
			Size:      size,
			UpdatedAt: info.ModTime().UTC(),
			Version:   ver,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "folder"
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	return out, nil
}

func (s *MediaFileStore) Put(namespacePath, fileName string, content io.Reader, overwrite bool, author string) (MediaEntry, error) {
	ns, err := normalizeMediaNamespacePath(namespacePath)
	if err != nil {
		return MediaEntry{}, err
	}

	safeName, err := normalizeMediaFileName(fileName)
	if err != nil {
		return MediaEntry{}, err
	}

	dirPath, err := s.namespaceDirPath(ns)
	if err != nil {
		return MediaEntry{}, err
	}
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return MediaEntry{}, fmt.Errorf("create media namespace: %w", err)
	}

	targetPath := filepath.Join(dirPath, safeName)

	relPath := safeName
	if ns != "" {
		relPath = ns + "/" + safeName
	}

	if !overwrite {
		if _, statErr := os.Stat(targetPath); statErr == nil {
			return MediaEntry{}, ErrMediaConflict
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return MediaEntry{}, fmt.Errorf("check media conflict: %w", statErr)
		}
	} else {
		// Archive existing file before overwriting, if it exists.
		if oldContent, readErr := os.ReadFile(targetPath); readErr == nil && s.VersionStore != nil && s.MediaAttic != nil {
			currentVersion := s.VersionStore.GetVersion(relPath)
			if currentVersion == 0 {
				// File exists but was never version-tracked — bootstrap as version 1.
				_ = s.VersionStore.SetVersion(relPath, 1)
				currentVersion = 1
			}
			_ = s.MediaAttic.Archive(relPath, currentVersion, oldContent, author)
		}
	}

	if err := writeReaderAtomic(targetPath, content); err != nil {
		return MediaEntry{}, fmt.Errorf("write media file: %w", err)
	}

	// Track version: increment on overwrite, set to 1 on new upload.
	if s.VersionStore != nil {
		currentVersion := s.VersionStore.GetVersion(relPath)
		if currentVersion == 0 {
			// New file — set version 1.
			_ = s.VersionStore.SetVersion(relPath, 1)
		} else {
			// Overwrite — increment.
			_, _ = s.VersionStore.IncrementVersion(relPath)
		}
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return MediaEntry{}, fmt.Errorf("stat media file: %w", err)
	}

	var ver int64
	if s.VersionStore != nil {
		ver = s.VersionStore.GetVersion(relPath)
	}
	return MediaEntry{
		Name:      safeName,
		Path:      "/" + relPath,
		Kind:      "file",
		Size:      info.Size(),
		UpdatedAt: info.ModTime().UTC(),
		Version:   ver,
	}, nil
}

func (s *MediaFileStore) Delete(mediaPath string) error {
	normalized, err := normalizeMediaPath(mediaPath)
	if err != nil {
		return err
	}

	targetPath := filepath.Join(s.rootDir, filepath.FromSlash(normalized))
	rel, err := filepath.Rel(s.rootDir, targetPath)
	if err != nil {
		return fmt.Errorf("build media path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("invalid media path")
	}

	if err := os.Remove(targetPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return fmt.Errorf("delete media file: %w", err)
	}

	return nil
}

func (s *MediaFileStore) ResolvePath(mediaPath string) (string, error) {
	normalized, err := normalizeMediaPath(mediaPath)
	if err != nil {
		return "", err
	}

	targetPath := filepath.Join(s.rootDir, filepath.FromSlash(normalized))
	rel, err := filepath.Rel(s.rootDir, targetPath)
	if err != nil {
		return "", fmt.Errorf("build media path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid media path")
	}
	return targetPath, nil
}

func (s *MediaFileStore) namespaceDirPath(namespacePath string) (string, error) {
	candidate := s.rootDir
	if namespacePath != "" {
		candidate = filepath.Join(s.rootDir, filepath.FromSlash(namespacePath))
	}
	rel, err := filepath.Rel(s.rootDir, candidate)
	if err != nil {
		return "", fmt.Errorf("build namespace path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid namespace path")
	}
	return candidate, nil
}

func normalizeMediaNamespacePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	cleaned := path.Clean("/" + trimmed)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("invalid namespace path")
	}
	return cleaned, nil
}

func normalizeMediaPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("media path cannot be empty")
	}
	cleaned := path.Clean("/" + trimmed)
	if cleaned == "." || cleaned == "/" {
		return "", fmt.Errorf("invalid media path")
	}
	return cleaned, nil
}

func normalizeMediaFileName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	name = filepath.Base(name)
	name = path.Base(name)
	if name == "." || name == "" {
		return "", fmt.Errorf("invalid file name")
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') {
		return "", fmt.Errorf("invalid file name")
	}
	if path.Ext(name) == "" {
		return "", fmt.Errorf("attachments must have a file extension")
	}
	return name, nil
}

func writeReaderAtomic(targetPath string, content io.Reader) error {
	dir := filepath.Dir(targetPath)
	base := filepath.Base(targetPath)
	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, targetPath)
}
