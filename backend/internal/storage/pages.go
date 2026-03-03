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

	"gowiki/backend/internal/markdown"
)

var ErrPageNotFound = errors.New("page not found")
var ErrNamespaceConflict = errors.New("namespace conflict: a directory exists at this path")
var ErrPageHasLock = errors.New("page is locked by a draft")

// CircularIncludeError is returned when saving a page would create a cycle
// in the include graph.
type CircularIncludeError struct {
	Cycle []string // the cycle path, e.g. ["a", "b", "a"]
}

func (e *CircularIncludeError) Error() string {
	return fmt.Sprintf("circular include detected: %s", strings.Join(e.Cycle, " → "))
}

// PutResult is the result of a page write, including side-effect information.
type PutResult struct {
	Page          Page     `json:"page"`
	OrphanedMedia []string `json:"orphaned_media,omitempty"`
}

// DeleteResult is returned by Delete with side-effect information.
type DeleteResult struct {
	OrphanedMedia []string `json:"orphaned_media,omitempty"`
	IncludedBy    []string `json:"included_by,omitempty"`
}

type PageMetadata struct {
	ID        string           `json:"id"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Version   int64            `json:"version"`
	Author    string           `json:"author,omitempty"`
	MediaRefs map[string]int64 `json:"media_refs,omitempty"`
}

type Page struct {
	Path             string       `json:"path"`
	Markdown         string       `json:"markdown"`
	Meta             PageMetadata `json:"meta"`
	IsNamespaceIndex bool         `json:"-"` // not serialized directly; API handler includes it
}

// DatabaseSyncer is an optional hook for syncing page content to the database.
type DatabaseSyncer interface {
	SyncPageRows(pagePath, markdown string)
	RemovePageRows(pagePath string)
}

type FileStore struct {
	contentRoot       string
	metaRoot          string
	dataDir           string
	RefIndex          *RefIndex
	IncludeIndex      *IncludeIndex
	TagIndex          *TagIndex
	SearchIndex       *SearchIndex
	Attic             *Attic
	Changelog         *Changelog
	Drafts            *DraftStore
	MediaVersionStore *MediaVersionStore
	DatabaseSync      DatabaseSyncer
}

func NewFileStore(contentRoot string) (*FileStore, error) {
	content := filepath.Clean(contentRoot)
	dataDir := filepath.Dir(content)
	meta := filepath.Join(dataDir, "meta")
	if err := os.MkdirAll(content, 0o755); err != nil {
		return nil, fmt.Errorf("create content root: %w", err)
	}
	if err := os.MkdirAll(meta, 0o755); err != nil {
		return nil, fmt.Errorf("create meta root: %w", err)
	}
	return &FileStore{
		contentRoot:  content,
		metaRoot:     meta,
		dataDir:      dataDir,
		RefIndex:     NewRefIndex(meta),
		IncludeIndex: NewIncludeIndex(meta),
		Attic:        NewAttic(dataDir),
		Changelog:    NewChangelog(dataDir),
		Drafts:       NewDraftStore(dataDir, meta),
	}, nil
}

func (s *FileStore) Get(pagePath string) (Page, error) {
	normalized, err := normalizePagePath(pagePath)
	if err != nil {
		return Page{}, err
	}

	contentPath, isIndex, err := s.resolveExistingContentPath(normalized)
	if errors.Is(err, os.ErrNotExist) {
		return Page{}, ErrPageNotFound
	}
	if err != nil {
		return Page{}, err
	}

	// If the resolved content path is an index.md, it is a namespace index
	// regardless of how the path was resolved (e.g. "test/index" resolves
	// via contentPagePath but is still a namespace index file).
	if strings.HasSuffix(contentPath, string(filepath.Separator)+"index.md") {
		isIndex = true
	}

	// Normalize path: strip trailing /index so the returned path is canonical.
	if isIndex && strings.HasSuffix(normalized, "/index") {
		normalized = strings.TrimSuffix(normalized, "/index")
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
		Path:             normalized,
		Markdown:         string(content),
		Meta:             meta,
		IsNamespaceIndex: isIndex,
	}, nil
}

// CheckNamespaceConflict checks whether creating or writing a page at the given
// path would violate namespace constraints, without performing any write.
// Returns ErrNamespaceConflict if the path is forbidden, nil otherwise.
func (s *FileStore) CheckNamespaceConflict(pagePath string) error {
	normalized, err := normalizePagePath(pagePath)
	if err != nil {
		return err
	}

	contentPath, _, err := s.resolveWritableContentPath(normalized)
	if err != nil {
		return err
	}

	return s.checkNamespaceConstraints(contentPath)
}

// checkNamespaceConstraints performs both forward and reverse namespace checks
// on a resolved content path.
func (s *FileStore) checkNamespaceConstraints(contentPath string) error {
	// Forward: if the resolved content path is a non-index page file (e.g. ns.md),
	// check that a directory ns/ does not already exist.
	if !strings.HasSuffix(contentPath, string(filepath.Separator)+"index.md") {
		dirPath := strings.TrimSuffix(contentPath, ".md")
		if info, statErr := os.Stat(dirPath); statErr == nil && info.IsDir() {
			return ErrNamespaceConflict
		}
	}

	// Reverse: creating ns/child.md is forbidden if ns.md exists.
	// Walk up from the content file's parent directory toward contentRoot, checking
	// that no .md page file conflicts with any directory that must exist.
	dir := filepath.Dir(contentPath)
	for dir != s.contentRoot && len(dir) > len(s.contentRoot) {
		conflictFile := dir + ".md"
		if info, statErr := os.Stat(conflictFile); statErr == nil && !info.IsDir() {
			return ErrNamespaceConflict
		}
		dir = filepath.Dir(dir)
	}

	return nil
}

func (s *FileStore) Put(pagePath, markdownContent, author string) (PutResult, error) {
	normalized, err := normalizePagePath(pagePath)
	if err != nil {
		return PutResult{}, err
	}

	contentPath, _, err := s.resolveWritableContentPath(normalized)
	if err != nil {
		return PutResult{}, err
	}

	if err := s.checkNamespaceConstraints(contentPath); err != nil {
		return PutResult{}, err
	}

	// --- Include cycle detection (before writing) ---
	newIncludes := markdown.ExtractIncludes(markdownContent, normalized)
	if cycle, hasCycle := s.IncludeIndex.DetectCycle(normalized, newIncludes); hasCycle {
		return PutResult{}, &CircularIncludeError{Cycle: cycle}
	}

	// --- Extract media refs ---
	newMediaRefs := markdown.ExtractMediaRefs(markdownContent, normalized)
	oldMediaRefs := s.RefIndex.PageToMediaSnapshot(normalized)

	// --- Write page and metadata ---
	if err := os.MkdirAll(filepath.Dir(contentPath), 0o755); err != nil {
		return PutResult{}, fmt.Errorf("create page directory: %w", err)
	}

	metaPath, err := s.metadataPathForContent(contentPath)
	if err != nil {
		return PutResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return PutResult{}, fmt.Errorf("create metadata directory: %w", err)
	}

	// Read current content for MD5 dedup and archiving.
	oldContent, _ := os.ReadFile(contentPath)

	// If content is identical to current published version, skip creating a new version.
	// Media version changes are reflected in the URL (?v=N), so MD5 dedup is sufficient.
	if len(oldContent) > 0 && md5sum(oldContent) == md5sum([]byte(markdownContent)) {
		meta, metaErr := s.loadMeta(metaPath)
		if metaErr == nil {
			return PutResult{
				Page: Page{Path: normalized, Markdown: markdownContent, Meta: meta},
			}, nil
		}
	}

	now := time.Now().UTC()
	meta, err := s.loadMeta(metaPath)
	switch {
	case err == nil:
		// Archive the current published content before overwriting.
		// Use the media_refs frozen in the old metadata (not the version store),
		// so the archive reflects media as they were when this version was published.
		if len(oldContent) > 0 && s.Attic != nil {
			_ = s.Attic.Archive(normalized, meta.Version, oldContent, meta.Author, "", meta.MediaRefs)
		}
		meta.UpdatedAt = now
		meta.Version++
		meta.Author = author
	case errors.Is(err, os.ErrNotExist):
		meta = PageMetadata{
			ID:        makePageID(normalized),
			CreatedAt: now,
			UpdatedAt: now,
			Version:   1,
			Author:    author,
		}
	default:
		return PutResult{}, fmt.Errorf("load metadata: %w", err)
	}

	// Clear MediaRefs — version info is now tracked in the markdown URL (?v=N).
	meta.MediaRefs = nil

	if err := writeFileAtomic(contentPath, []byte(markdownContent)); err != nil {
		return PutResult{}, fmt.Errorf("write page: %w", err)
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return PutResult{}, fmt.Errorf("encode metadata: %w", err)
	}
	metaBytes = append(metaBytes, '\n')
	if err := writeFileAtomic(metaPath, metaBytes); err != nil {
		return PutResult{}, fmt.Errorf("write metadata: %w", err)
	}

	// Archive the new version (media versions are in the markdown URLs now).
	if s.Attic != nil {
		_ = s.Attic.Archive(normalized, meta.Version, []byte(markdownContent), author, "", nil)
	}

	// Append to global changelog.
	if s.Changelog != nil {
		s.Changelog.Append(normalized, meta.Version, author, "", "edit")
	}

	// --- Update indexes ---
	s.RefIndex.UpdatePage(normalized, newMediaRefs)
	s.IncludeIndex.UpdatePage(normalized, newIncludes)

	// --- Update tag index ---
	if s.TagIndex != nil {
		tags := markdown.ExtractTags(markdownContent)
		title := markdown.ExtractTitle(markdownContent)
		s.TagIndex.UpdatePage(normalized, tags, title)
	}

	// --- Update search index ---
	if s.SearchIndex != nil {
		title := markdown.ExtractTitle(markdownContent)
		plaintext := markdown.StripMarkdown(markdownContent)
		_ = s.SearchIndex.IndexPage(normalized, title, plaintext)
	}

	// Persist indexes (best effort — indexes are rebuilt on startup anyway).
	_ = s.RefIndex.Save()
	_ = s.IncludeIndex.Save()
	if s.TagIndex != nil {
		_ = s.TagIndex.Save()
	}

	// --- Sync database rows if configured ---
	if s.DatabaseSync != nil {
		s.DatabaseSync.SyncPageRows(normalized, markdownContent)
	}

	// --- Compute newly orphaned media ---
	orphaned := s.RefIndex.FindNewlyOrphaned(oldMediaRefs, newMediaRefs)

	return PutResult{
		Page: Page{
			Path:     normalized,
			Markdown: markdownContent,
			Meta:     meta,
		},
		OrphanedMedia: orphaned,
	}, nil
}

// Delete removes a page, archiving its content first. It returns information
// about orphaned media and pages that included the deleted page.
func (s *FileStore) Delete(pagePath, author string) (DeleteResult, error) {
	normalized, err := normalizePagePath(pagePath)
	if err != nil {
		return DeleteResult{}, err
	}

	contentPath, _, err := s.resolveExistingContentPath(normalized)
	if errors.Is(err, os.ErrNotExist) {
		return DeleteResult{}, ErrPageNotFound
	}
	if err != nil {
		return DeleteResult{}, err
	}

	// Check for draft lock — cannot delete a page being edited.
	if s.Drafts != nil {
		lock := s.Drafts.GetLock(normalized)
		if lock.Owner != "" {
			return DeleteResult{}, fmt.Errorf("%w: locked by %s", ErrPageHasLock, lock.Owner)
		}
	}

	// Read current content for archiving.
	content, err := os.ReadFile(contentPath)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("read page for archiving: %w", err)
	}

	// Load metadata for version number.
	metaPath, err := s.metadataPathForContent(contentPath)
	if err != nil {
		return DeleteResult{}, err
	}
	meta, err := s.readOrInitMeta(normalized, contentPath, metaPath)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("load metadata: %w", err)
	}

	// Archive current version with summary "deleted", preserving frozen media refs.
	if s.Attic != nil {
		if archiveErr := s.Attic.Archive(normalized, meta.Version, content, author, "deleted", meta.MediaRefs); archiveErr != nil {
			return DeleteResult{}, fmt.Errorf("archive before delete: %w", archiveErr)
		}
	}

	// Append to global changelog with type "delete".
	if s.Changelog != nil {
		s.Changelog.Append(normalized, meta.Version, author, "deleted", "delete")
	}

	// Remove the .md file.
	if err := os.Remove(contentPath); err != nil {
		return DeleteResult{}, fmt.Errorf("remove page file: %w", err)
	}

	// Remove the metadata .json file (best effort).
	os.Remove(metaPath)

	// Clean up empty parent directories for content and meta paths.
	cleanEmptyParents(filepath.Dir(contentPath), s.contentRoot)
	cleanEmptyParents(filepath.Dir(metaPath), s.metaRoot)

	// Sync database: remove rows for deleted page.
	if s.DatabaseSync != nil {
		s.DatabaseSync.RemovePageRows(normalized)
	}

	// Snapshot old media refs before removing from index.
	oldMediaRefs := s.RefIndex.PageToMediaSnapshot(normalized)

	// Update RefIndex: remove all refs from this page.
	s.RefIndex.RemovePage(normalized)

	// Update IncludeIndex: find pages that include this page (for warning), then remove.
	includedBy := s.IncludeIndex.GetIncluders(normalized)
	s.IncludeIndex.RemovePage(normalized)

	// Update TagIndex: remove this page.
	if s.TagIndex != nil {
		s.TagIndex.RemovePage(normalized)
	}

	// Update SearchIndex: remove this page.
	if s.SearchIndex != nil {
		_ = s.SearchIndex.DeletePage(normalized)
	}

	// Persist indexes (best effort).
	_ = s.RefIndex.Save()
	_ = s.IncludeIndex.Save()
	if s.TagIndex != nil {
		_ = s.TagIndex.Save()
	}

	// Compute newly orphaned media: all old refs that now have zero references.
	orphaned := s.RefIndex.FindNewlyOrphaned(oldMediaRefs, nil)

	return DeleteResult{
		OrphanedMedia: orphaned,
		IncludedBy:    includedBy,
	}, nil
}

// cleanEmptyParents removes empty directories from dir up to (but not including) stopAt.
func cleanEmptyParents(dir, stopAt string) {
	for dir != stopAt && dir != "." && dir != "/" {
		if err := os.Remove(dir); err != nil {
			break // directory not empty or other error
		}
		dir = filepath.Dir(dir)
	}
}

// FindOrphans returns media files under contentRoot with zero page references.
func (s *FileStore) FindOrphans() ([]string, error) {
	return s.RefIndex.FindOrphans(s.contentRoot)
}

// GetReferencingPages returns the list of pages that reference a given media file.
func (s *FileStore) GetReferencingPages(mediaPath string) []string {
	return s.RefIndex.GetReferencingPages(mediaPath)
}

// RebuildIndexes walks all .md pages under contentRoot, extracts media refs
// and include directives, and rebuilds both RefIndex and IncludeIndex from scratch.
// Called on server startup.
func (s *FileStore) RebuildIndexes() error {
	refIdx := NewRefIndex(s.metaRoot)
	incIdx := NewIncludeIndex(s.metaRoot)
	tagIdx := NewTagIndex(s.metaRoot)

	err := filepath.Walk(s.contentRoot, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(absPath, ".md") {
			return nil
		}

		rel, relErr := filepath.Rel(s.contentRoot, absPath)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		// Derive the logical page path from the file path.
		pagePath := strings.TrimSuffix(rel, ".md")
		pagePath = strings.TrimSuffix(pagePath, "/index")

		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return readErr
		}

		contentStr := string(content)
		mediaRefs := markdown.ExtractMediaRefs(contentStr, pagePath)
		refIdx.UpdatePage(pagePath, mediaRefs)

		includes := markdown.ExtractIncludes(contentStr, pagePath)
		incIdx.UpdatePage(pagePath, includes)

		tags := markdown.ExtractTags(contentStr)
		title := markdown.ExtractTitle(contentStr)
		tagIdx.UpdatePage(pagePath, tags, title)

		return nil
	})
	if err != nil {
		return fmt.Errorf("rebuild indexes: %w", err)
	}

	s.RefIndex = refIdx
	s.IncludeIndex = incIdx
	s.TagIndex = tagIdx

	if err := s.RefIndex.Save(); err != nil {
		return fmt.Errorf("save ref index: %w", err)
	}
	if err := s.IncludeIndex.Save(); err != nil {
		return fmt.Errorf("save include index: %w", err)
	}
	if err := s.TagIndex.Save(); err != nil {
		return fmt.Errorf("save tag index: %w", err)
	}

	// Rebuild search index if available.
	if s.SearchIndex != nil {
		if err := s.SearchIndex.RebuildFromDir(s.contentRoot); err != nil {
			return fmt.Errorf("rebuild search index: %w", err)
		}
	}

	return nil
}

// PageEntry is a summary for sitemap / listing purposes.
type PageEntry struct {
	Path             string `json:"path"`
	Title            string `json:"title"`
	IsNamespaceIndex bool   `json:"is_namespace_index"`
}

// ListAllPages walks content/ and returns all pages with titles.
func (s *FileStore) ListAllPages() ([]PageEntry, error) {
	var pages []PageEntry

	err := filepath.Walk(s.contentRoot, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(absPath, ".md") {
			return nil
		}

		rel, relErr := filepath.Rel(s.contentRoot, absPath)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		isNsIndex := strings.HasSuffix(rel, "/index.md")
		pagePath := strings.TrimSuffix(rel, ".md")
		pagePath = strings.TrimSuffix(pagePath, "/index")

		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return readErr
		}

		contentStr := string(content)
		title := markdown.ExtractTitle(contentStr)
		title = markdown.ResolveTemplateVars(title, contentStr)
		pages = append(pages, PageEntry{Path: pagePath, Title: title, IsNamespaceIndex: isNsIndex})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pages, nil
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

// logoExtensions is the ordered list of file extensions to check for a site logo.
var logoExtensions = []string{".png", ".svg", ".jpg", ".jpeg", ".gif", ".webp"}

// ResolveLogo scans the content root for a logo file (logo.{png,svg,jpg,jpeg,gif,webp}).
// Returns the relative path of the first match, or empty string if none found.
func (s *FileStore) ResolveLogo() (string, error) {
	for _, ext := range logoExtensions {
		name := "logo" + ext
		fp := filepath.Join(s.contentRoot, name)
		if fileExists(fp) {
			return name, nil
		}
	}
	return "", nil
}
