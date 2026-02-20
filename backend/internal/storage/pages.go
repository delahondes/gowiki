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
var ErrNamespaceConflict = errors.New("namespace conflict: a directory exists at this path")

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
	contentRoot string
	metaRoot    string
}

func NewFileStore(contentRoot string) (*FileStore, error) {
	content := filepath.Clean(contentRoot)
	meta := filepath.Join(filepath.Dir(content), "meta")
	if err := os.MkdirAll(content, 0o755); err != nil {
		return nil, fmt.Errorf("create content root: %w", err)
	}
	if err := os.MkdirAll(meta, 0o755); err != nil {
		return nil, fmt.Errorf("create meta root: %w", err)
	}
	return &FileStore{contentRoot: content, metaRoot: meta}, nil
}

func (s *FileStore) Get(pagePath string) (Page, error) {
	normalized, err := normalizePagePath(pagePath)
	if err != nil {
		return Page{}, err
	}

	contentPath, _, err := s.resolveExistingContentPath(normalized)
	if errors.Is(err, os.ErrNotExist) {
		return Page{}, ErrPageNotFound
	}
	if err != nil {
		return Page{}, err
	}

	content, err := os.ReadFile(contentPath)
	if err != nil {
		return Page{}, fmt.Errorf("read page: %w", err)
	}

	metaPath, err := s.metadataPathForContent(contentPath)
	if err != nil {
		return Page{}, err
	}
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

	contentPath, _, err := s.resolveWritableContentPath(normalized)
	if err != nil {
		return Page{}, err
	}

	// Namespace constraint: if the resolved content path is a non-index page
	// file (e.g. ns.md), check that a directory ns/ does not already exist.
	if !strings.HasSuffix(contentPath, string(filepath.Separator)+"index.md") {
		dirPath := strings.TrimSuffix(contentPath, ".md")
		if info, statErr := os.Stat(dirPath); statErr == nil && info.IsDir() {
			return Page{}, ErrNamespaceConflict
		}
	}

	if err := os.MkdirAll(filepath.Dir(contentPath), 0o755); err != nil {
		return Page{}, fmt.Errorf("create page directory: %w", err)
	}

	metaPath, err := s.metadataPathForContent(contentPath)
	if err != nil {
		return Page{}, err
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return Page{}, fmt.Errorf("create metadata directory: %w", err)
	}

	now := time.Now().UTC()
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
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return PageMetadata{}, fmt.Errorf("create metadata directory: %w", err)
	}
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

func (s *FileStore) resolveExistingContentPath(pagePath string) (string, bool, error) {
	pageFile, err := s.contentPagePath(pagePath)
	if err != nil {
		return "", false, err
	}
	indexFile, err := s.contentNamespaceIndexPath(pagePath)
	if err != nil {
		return "", false, err
	}

	pageExists := fileExists(pageFile)
	indexExists := fileExists(indexFile)
	if pageExists && indexExists {
		return "", false, fmt.Errorf("ambiguous page path %q: both page and namespace index exist", pagePath)
	}
	if indexExists {
		return indexFile, true, nil
	}
	if pageExists {
		return pageFile, false, nil
	}
	return "", false, os.ErrNotExist
}

func (s *FileStore) resolveWritableContentPath(pagePath string) (string, bool, error) {
	pageFile, err := s.contentPagePath(pagePath)
	if err != nil {
		return "", false, err
	}
	indexFile, err := s.contentNamespaceIndexPath(pagePath)
	if err != nil {
		return "", false, err
	}

	pageExists := fileExists(pageFile)
	indexExists := fileExists(indexFile)
	if pageExists && indexExists {
		return "", false, fmt.Errorf("ambiguous page path %q: both page and namespace index exist", pagePath)
	}
	if indexExists {
		return indexFile, true, nil
	}
	if pageExists {
		return pageFile, false, nil
	}

	// When the namespace directory already exists, the logical page path is the namespace index.
	indexDir := strings.TrimSuffix(indexFile, string(filepath.Separator)+"index.md")
	if info, err := os.Stat(indexDir); err == nil && info.IsDir() {
		return indexFile, true, nil
	}

	return pageFile, false, nil
}

func (s *FileStore) contentPagePath(pagePath string) (string, error) {
	return s.safeJoinContent(pagePath + ".md")
}

func (s *FileStore) contentNamespaceIndexPath(pagePath string) (string, error) {
	return s.safeJoinContent(filepath.ToSlash(filepath.Join(pagePath, "index.md")))
}

func (s *FileStore) metadataPathForContent(contentPath string) (string, error) {
	rel, err := filepath.Rel(s.contentRoot, contentPath)
	if err != nil {
		return "", fmt.Errorf("derive metadata path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid content path")
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasSuffix(rel, ".md") {
		return "", fmt.Errorf("invalid page content extension")
	}
	metaRel := strings.TrimSuffix(rel, ".md") + ".json"
	metaPath := filepath.Join(s.metaRoot, filepath.FromSlash(metaRel))
	check, err := filepath.Rel(s.metaRoot, metaPath)
	if err != nil {
		return "", fmt.Errorf("build metadata path: %w", err)
	}
	if strings.HasPrefix(check, "..") {
		return "", fmt.Errorf("invalid metadata path")
	}
	return metaPath, nil
}

func (s *FileStore) safeJoinContent(rel string) (string, error) {
	p := filepath.Join(s.contentRoot, filepath.FromSlash(rel))
	r, err := filepath.Rel(s.contentRoot, p)
	if err != nil {
		return "", fmt.Errorf("build page path: %w", err)
	}
	if strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("invalid page path")
	}
	return p, nil
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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
