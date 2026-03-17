package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// TagIndex tracks which pages have which tags and vice versa.
// Persisted as _tags.json in the meta directory.
type TagIndex struct {
	mu         sync.RWMutex
	basePath   string
	PageToTags map[string][]string `json:"page_to_tags"`
	TagToPages map[string][]string `json:"tag_to_pages"`
	PageTitles map[string]string   `json:"page_titles"`
}

// NewTagIndex creates a new empty TagIndex. basePath is the meta directory.
func NewTagIndex(basePath string) *TagIndex {
	return &TagIndex{
		basePath:   basePath,
		PageToTags: make(map[string][]string),
		TagToPages: make(map[string][]string),
		PageTitles: make(map[string]string),
	}
}

// Load reads the tag index from disk.
func (idx *TagIndex) Load() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	filePath := filepath.Join(idx.basePath, "_tags.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			idx.PageToTags = make(map[string][]string)
			idx.TagToPages = make(map[string][]string)
			idx.PageTitles = make(map[string]string)
			return nil
		}
		return err
	}

	var stored struct {
		PageToTags map[string][]string `json:"page_to_tags"`
		TagToPages map[string][]string `json:"tag_to_pages"`
		PageTitles map[string]string   `json:"page_titles"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	if stored.PageToTags == nil {
		stored.PageToTags = make(map[string][]string)
	}
	if stored.TagToPages == nil {
		stored.TagToPages = make(map[string][]string)
	}
	if stored.PageTitles == nil {
		stored.PageTitles = make(map[string]string)
	}
	idx.PageToTags = stored.PageToTags
	idx.TagToPages = stored.TagToPages
	idx.PageTitles = stored.PageTitles
	return nil
}

// Save persists the tag index to disk atomically.
func (idx *TagIndex) Save() error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	filePath := filepath.Join(idx.basePath, "_tags.json")
	stored := struct {
		PageToTags map[string][]string `json:"page_to_tags"`
		TagToPages map[string][]string `json:"tag_to_pages"`
		PageTitles map[string]string   `json:"page_titles"`
	}{
		PageToTags: idx.PageToTags,
		TagToPages: idx.TagToPages,
		PageTitles: idx.PageTitles,
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(filePath, data)
}

// Clear resets the tag index to empty, keeping the same object.
func (idx *TagIndex) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.PageToTags = make(map[string][]string)
	idx.TagToPages = make(map[string][]string)
	idx.PageTitles = make(map[string]string)
}

// UpdatePage replaces the tags for a page and updates reverse maps.
func (idx *TagIndex) UpdatePage(pagePath string, tags []string, title string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Remove old tag→page references.
	oldTags := idx.PageToTags[pagePath]
	for _, t := range oldTags {
		idx.removePageFromTag(t, pagePath)
	}

	if len(tags) == 0 {
		delete(idx.PageToTags, pagePath)
	} else {
		dst := make([]string, len(tags))
		copy(dst, tags)
		idx.PageToTags[pagePath] = dst
		for _, t := range tags {
			idx.addPageToTag(t, pagePath)
		}
	}

	if title != "" {
		idx.PageTitles[pagePath] = title
	} else {
		delete(idx.PageTitles, pagePath)
	}
}

// RemovePage removes a page from the tag index entirely.
func (idx *TagIndex) RemovePage(pagePath string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	oldTags := idx.PageToTags[pagePath]
	for _, t := range oldTags {
		idx.removePageFromTag(t, pagePath)
	}
	delete(idx.PageToTags, pagePath)
	delete(idx.PageTitles, pagePath)
}

// GetTagsForPage returns the tags associated with a page path.
func (idx *TagIndex) GetTagsForPage(pagePath string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	tags := idx.PageToTags[pagePath]
	if tags == nil {
		return []string{}
	}
	result := make([]string, len(tags))
	copy(result, tags)
	return result
}

// GetPagesForTag returns all pages that have the given tag, optionally
// filtered by a path prefix and excluding pages that have any of the
// specified tags. Results are sorted by path.
func (idx *TagIndex) GetPagesForTag(tag, pathPrefix string, excludeTags []string) []PageEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	pages := idx.TagToPages[tag]
	var result []PageEntry
	for _, p := range pages {
		if pathPrefix != "" && !hasPathPrefix(p, pathPrefix) {
			continue
		}
		if len(excludeTags) > 0 {
			pageTags := idx.PageToTags[p]
			if hasAnyTag(pageTags, excludeTags) {
				continue
			}
		}
		title := idx.PageTitles[p]
		if title == "" {
			title = p
		}
		result = append(result, PageEntry{Path: p, Title: title})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result
}

// hasAnyTag returns true if pageTags contains any of the tags in check.
func hasAnyTag(pageTags, check []string) bool {
	for _, c := range check {
		for _, t := range pageTags {
			if t == c {
				return true
			}
		}
	}
	return false
}

func hasPathPrefix(pagePath, prefix string) bool {
	clean := prefix
	// Strip trailing slash.
	for len(clean) > 0 && clean[len(clean)-1] == '/' {
		clean = clean[:len(clean)-1]
	}
	// Ensure leading slash for consistency with /-prefixed paths.
	if len(clean) > 0 && clean[0] != '/' {
		clean = "/" + clean
	}
	if clean == "" || clean == "/" {
		return true // empty prefix matches everything
	}
	if pagePath == clean {
		return true
	}
	return len(pagePath) > len(clean) && pagePath[:len(clean)] == clean && pagePath[len(clean)] == '/'
}

func (idx *TagIndex) removePageFromTag(tag, pagePath string) {
	pages := idx.TagToPages[tag]
	for i, p := range pages {
		if p == pagePath {
			idx.TagToPages[tag] = append(pages[:i], pages[i+1:]...)
			if len(idx.TagToPages[tag]) == 0 {
				delete(idx.TagToPages, tag)
			}
			return
		}
	}
}

func (idx *TagIndex) addPageToTag(tag, pagePath string) {
	pages := idx.TagToPages[tag]
	for _, p := range pages {
		if p == pagePath {
			return
		}
	}
	idx.TagToPages[tag] = append(pages, pagePath)
}
