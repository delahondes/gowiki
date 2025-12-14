package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrPageNotFound = errors.New("page not found")
	ErrPageExists   = errors.New("page already exists")
)

type Page struct {
	Path     string
	Title    string
	Content  string
	Created  time.Time
	Modified time.Time
}

type Storage interface {
	// GetPage retrieves a page by path
	GetPage(path string) (*Page, error)

	// SavePage saves a page (creates or updates)
	SavePage(path, content string) error

	// PageExists checks if a page exists
	PageExists(path string) bool

	// ListPages lists all pages
	ListPages() ([]*Page, error)
}

type FileStorage struct {
	RootPath string
}

func NewFileStorage(root string) *FileStorage {
	return &FileStorage{RootPath: root}
}

func (s *FileStorage) getFilePath(path string) string {
	// Clean path and ensure it's safe
	cleanPath := strings.Trim(path, "/")
	if cleanPath == "" {
		cleanPath = "start"
	}

	// Replace colons with path separators (Dokuwiki style)
	filePath := strings.ReplaceAll(cleanPath, ":", "/")

	// Ensure it's within our root
	fullPath := filepath.Join(s.RootPath, "pages", filePath+".txt")

	// Prevent directory traversal
	if !strings.HasPrefix(fullPath, filepath.Clean(s.RootPath)) {
		return ""
	}

	return fullPath
}

func (s *FileStorage) PageExists(path string) bool {
	filePath := s.getFilePath(path)
	if filePath == "" {
		return false
	}

	_, err := os.Stat(filePath)
	return !errors.Is(err, fs.ErrNotExist)
}

func (s *FileStorage) GetPage(path string) (*Page, error) {
	filePath := s.getFilePath(path)
	if filePath == "" {
		return nil, ErrPageNotFound
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrPageNotFound
		}
		return nil, err
	}

	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	return &Page{
		Path:     path,
		Title:    filepath.Base(path),
		Content:  string(content),
		Created:  stat.ModTime(),
		Modified: stat.ModTime(),
	}, nil
}

func (s *FileStorage) SavePage(path, content string) error {
	filePath := s.getFilePath(path)
	if filePath == "" {
		return errors.New("invalid page path")
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	// Write content
	return os.WriteFile(filePath, []byte(content), 0644)
}

func (s *FileStorage) ListPages() ([]*Page, error) {
	pagesDir := filepath.Join(s.RootPath, "pages")

	var pages []*Page
	err := filepath.WalkDir(pagesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".txt" {
			return nil
		}

		// Convert file path back to wiki path
		relPath, err := filepath.Rel(pagesDir, path)
		if err != nil {
			return err
		}

		wikiPath := strings.TrimSuffix(relPath, ".txt")
		wikiPath = strings.ReplaceAll(wikiPath, "/", ":")

		stat, err := d.Info()
		if err != nil {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		pages = append(pages, &Page{
			Path:     wikiPath,
			Title:    filepath.Base(wikiPath),
			Content:  string(content),
			Created:  stat.ModTime(),
			Modified: stat.ModTime(),
		})

		return nil
	})

	return pages, err
}
