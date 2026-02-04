package storage

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var ErrPageNotFound = errors.New("page not found")

type PageMetadata struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int64     `json:"version"`
}

type Page struct {
	Path     string       `json:"path"`
	Markdown string       `json:"markdown"`
	Meta     PageMetadata `json:"meta"`
}

type FileStore struct {
	rootDir string
}

func NewFileStore(rootDir string) (*FileStore, error) {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data root: %w", err)
	}
	return &FileStore{rootDir: rootDir}, nil
}

func (s *FileStore) Get(pagePath string) (Page, error) {
	normalized, err := normalizePagePath(pagePath)
	if err != nil {
		return Page{}, err
	}

	contentPath, err := s.contentPath(normalized)
	if err != nil {
		return Page{}, err
	}

	content, err := os.ReadFile(contentPath)
	if errors.Is(err, os.ErrNotExist) {
		return Page{}, ErrPageNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("read page: %w", err)
	}

	metaPath := metadataPathForContent(contentPath)
	meta, err := s.readOrInitMeta(normalized, contentPath, metaPath)
	if err != nil {
		return Page{}, err
	}

	return Page{
		Path:     normalized,
		Markdown: string(content),
		Meta:     meta,
	}, nil
}

func (s *FileStore) Put(pagePath, markdown string) (Page, error) {
	normalized, err := normalizePagePath(pagePath)
	if err != nil {
		return Page{}, err
	}

	contentPath, err := s.contentPath(normalized)
	if err != nil {
		return Page{}, err
	}
	if err := os.MkdirAll(filepath.Dir(contentPath), 0o755); err != nil {
		return Page{}, fmt.Errorf("create page directory: %w", err)
	}

	now := time.Now().UTC()
	metaPath := metadataPathForContent(contentPath)

	meta, err := s.loadMeta(metaPath)
	switch {
	case err == nil:
		meta.UpdatedAt = now
		meta.Version++
	case errors.Is(err, os.ErrNotExist):
		meta = PageMetadata{
			ID:        makePageID(normalized),
			CreatedAt: now,
			UpdatedAt: now,
			Version:   1,
		}
	default:
		return Page{}, fmt.Errorf("load metadata: %w", err)
	}

	if err := writeFileAtomic(contentPath, []byte(markdown)); err != nil {
		return Page{}, fmt.Errorf("write page: %w", err)
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return Page{}, fmt.Errorf("encode metadata: %w", err)
	}
	metaBytes = append(metaBytes, '\n')
	if err := writeFileAtomic(metaPath, metaBytes); err != nil {
		return Page{}, fmt.Errorf("write metadata: %w", err)
	}

	return Page{
		Path:     normalized,
		Markdown: markdown,
		Meta:     meta,
	}, nil
}

func (s *FileStore) readOrInitMeta(pagePath, contentPath, metaPath string) (PageMetadata, error) {
	meta, err := s.loadMeta(metaPath)
	if err == nil {
		return meta, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return PageMetadata{}, fmt.Errorf("read metadata: %w", err)
	}

	info, statErr := os.Stat(contentPath)
	if statErr != nil {
		return PageMetadata{}, fmt.Errorf("stat page file: %w", statErr)
	}
	t := info.ModTime().UTC()
	bootstrap := PageMetadata{
		ID:        makePageID(pagePath),
		CreatedAt: t,
		UpdatedAt: t,
		Version:   1,
	}
	bytes, marshalErr := json.MarshalIndent(bootstrap, "", "  ")
	if marshalErr != nil {
		return PageMetadata{}, fmt.Errorf("encode bootstrap metadata: %w", marshalErr)
	}
	bytes = append(bytes, '\n')
	if writeErr := writeFileAtomic(metaPath, bytes); writeErr != nil {
		return PageMetadata{}, fmt.Errorf("write bootstrap metadata: %w", writeErr)
	}
	return bootstrap, nil
}

func (s *FileStore) loadMeta(metaPath string) (PageMetadata, error) {
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return PageMetadata{}, err
	}
	var meta PageMetadata
	if err := json.Unmarshal(b, &meta); err != nil {
		return PageMetadata{}, fmt.Errorf("decode metadata: %w", err)
	}
	return meta, nil
}

func (s *FileStore) contentPath(pagePath string) (string, error) {
	p := filepath.Join(s.rootDir, pagePath+".md")
	rel, err := filepath.Rel(s.rootDir, p)
	if err != nil {
		return "", fmt.Errorf("build page path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid page path")
	}
	return p, nil
}

func metadataPathForContent(contentPath string) string {
	return strings.TrimSuffix(contentPath, ".md") + ".meta.json"
}

func normalizePagePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("page path cannot be empty")
	}
	cleaned := path.Clean("/" + trimmed)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("invalid page path")
	}
	return cleaned, nil
}

func writeFileAtomic(targetPath string, content []byte) error {
	dir := filepath.Dir(targetPath)
	base := filepath.Base(targetPath)
	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(content); err != nil {
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

func makePageID(pagePath string) string {
	h := sha1.Sum([]byte(pagePath))
	return hex.EncodeToString(h[:])
}
