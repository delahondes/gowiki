package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const refIndexFile = "_refindex.json"

// refIndexJSON is the on-disk serialization format for the reference index.
type refIndexJSON struct {
	PageToMedia  map[string][]string `json:"page_to_media"`
	MediaToPages map[string][]string `json:"media_to_pages"`
}

// RefIndex tracks which pages reference which media files, and vice versa.
// All public methods are safe for concurrent use.
type RefIndex struct {
	mu           sync.RWMutex
	basePath     string              // path to data/meta/ directory
	PageToMedia  map[string][]string // page path → sorted list of media paths it references
	MediaToPages map[string][]string // media path → sorted list of page paths referencing it
}

// NewRefIndex creates a new RefIndex. basePath is the meta directory (e.g., "data/meta").
func NewRefIndex(basePath string) *RefIndex {
	return &RefIndex{
		basePath:     basePath,
		PageToMedia:  make(map[string][]string),
		MediaToPages: make(map[string][]string),
	}
}

// Load reads the ref index from disk (data/meta/_refindex.json).
// If the file doesn't exist, initializes empty maps (not an error).
func (r *RefIndex) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	filePath := filepath.Join(r.basePath, refIndexFile)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.PageToMedia = make(map[string][]string)
			r.MediaToPages = make(map[string][]string)
			return nil
		}
		return err
	}

	var idx refIndexJSON
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}

	if idx.PageToMedia == nil {
		idx.PageToMedia = make(map[string][]string)
	}
	if idx.MediaToPages == nil {
		idx.MediaToPages = make(map[string][]string)
	}
	r.PageToMedia = idx.PageToMedia
	r.MediaToPages = idx.MediaToPages
	return nil
}

// Save persists the ref index to disk atomically (write temp file, rename).
func (r *RefIndex) Save() error {
	r.mu.RLock()
	idx := refIndexJSON{
		PageToMedia:  r.PageToMedia,
		MediaToPages: r.MediaToPages,
	}
	r.mu.RUnlock()

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	filePath := filepath.Join(r.basePath, refIndexFile)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filePath, data)
}

// UpdatePage replaces the media references for a page and updates the reverse map.
// oldRefs are removed, newRefs are added. Both maps stay consistent.
func (r *RefIndex) UpdatePage(pagePath string, newMediaRefs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	oldRefs := r.PageToMedia[pagePath]

	// Remove this page from the reverse map for all old refs.
	for _, media := range oldRefs {
		r.removePageFromMedia(pagePath, media)
	}

	// Set new forward refs (sorted copy).
	if len(newMediaRefs) == 0 {
		delete(r.PageToMedia, pagePath)
	} else {
		sorted := make([]string, len(newMediaRefs))
		copy(sorted, newMediaRefs)
		sort.Strings(sorted)
		r.PageToMedia[pagePath] = sorted
	}

	// Add this page to the reverse map for all new refs.
	for _, media := range newMediaRefs {
		r.addPageToMedia(pagePath, media)
	}
}

// PageToMediaSnapshot returns a copy of the media refs for a page.
// Used to capture old refs before a page update.
func (r *RefIndex) PageToMediaSnapshot(pagePath string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	refs := r.PageToMedia[pagePath]
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	copy(out, refs)
	return out
}

// GetReferencingPages returns the list of pages that reference a given media file.
func (r *RefIndex) GetReferencingPages(mediaPath string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pages := r.MediaToPages[mediaPath]
	if len(pages) == 0 {
		return nil
	}
	out := make([]string, len(pages))
	copy(out, pages)
	return out
}

// FindOrphans scans contentDir for non-.md files and returns those with zero references.
// It walks contentDir, finds all files with extensions that are NOT .md,
// and checks each against MediaToPages. Returns paths relative to contentDir.
func (r *RefIndex) FindOrphans(contentDir string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var orphans []string
	err := filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext == "" || strings.EqualFold(ext, ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(contentDir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if len(r.MediaToPages[rel]) == 0 {
			orphans = append(orphans, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(orphans)
	return orphans, nil
}

// FindNewlyOrphaned computes which media files became orphaned as a result of
// updating a page's refs from oldRefs to newRefs.
// Returns media paths that were in oldRefs but not in newRefs AND now have zero references.
func (r *RefIndex) FindNewlyOrphaned(oldRefs, newRefs []string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	newSet := make(map[string]struct{}, len(newRefs))
	for _, ref := range newRefs {
		newSet[ref] = struct{}{}
	}

	var orphaned []string
	for _, ref := range oldRefs {
		if _, kept := newSet[ref]; kept {
			continue
		}
		if len(r.MediaToPages[ref]) == 0 {
			orphaned = append(orphaned, ref)
		}
	}
	sort.Strings(orphaned)
	return orphaned
}

// removePageFromMedia removes a page from a media's reverse-map entry.
// Caller must hold r.mu for writing.
func (r *RefIndex) removePageFromMedia(pagePath, mediaPath string) {
	pages := r.MediaToPages[mediaPath]
	for i, p := range pages {
		if p == pagePath {
			r.MediaToPages[mediaPath] = append(pages[:i], pages[i+1:]...)
			break
		}
	}
	if len(r.MediaToPages[mediaPath]) == 0 {
		delete(r.MediaToPages, mediaPath)
	}
}

// addPageToMedia adds a page to a media's reverse-map entry in sorted order.
// Caller must hold r.mu for writing.
func (r *RefIndex) addPageToMedia(pagePath, mediaPath string) {
	pages := r.MediaToPages[mediaPath]
	idx := sort.SearchStrings(pages, pagePath)
	if idx < len(pages) && pages[idx] == pagePath {
		return // already present
	}
	pages = append(pages, "")
	copy(pages[idx+1:], pages[idx:])
	pages[idx] = pagePath
	r.MediaToPages[mediaPath] = pages
}
