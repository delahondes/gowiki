package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// IncludeIndex tracks which pages include which other pages.
// It is persisted as _includes.json in the meta directory.
type IncludeIndex struct {
	mu             sync.RWMutex
	basePath       string              // path to data/meta/ directory
	PageToIncludes map[string][]string `json:"page_to_includes"`
}

// NewIncludeIndex creates a new IncludeIndex. basePath is the meta directory.
func NewIncludeIndex(basePath string) *IncludeIndex {
	return &IncludeIndex{
		basePath:       basePath,
		PageToIncludes: make(map[string][]string),
	}
}

// Load reads the include index from disk (data/meta/_includes.json).
// If the file doesn't exist, initializes an empty map (not an error).
func (idx *IncludeIndex) Load() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	filePath := filepath.Join(idx.basePath, "_includes.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			idx.PageToIncludes = make(map[string][]string)
			return nil
		}
		return err
	}

	var stored struct {
		PageToIncludes map[string][]string `json:"page_to_includes"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	if stored.PageToIncludes == nil {
		stored.PageToIncludes = make(map[string][]string)
	}
	idx.PageToIncludes = stored.PageToIncludes
	return nil
}

// Save persists the include index to disk atomically.
func (idx *IncludeIndex) Save() error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	filePath := filepath.Join(idx.basePath, "_includes.json")

	stored := struct {
		PageToIncludes map[string][]string `json:"page_to_includes"`
	}{
		PageToIncludes: idx.PageToIncludes,
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

// GetIncluders returns the list of pages that include the given page.
// This is a reverse lookup across the PageToIncludes map.
func (idx *IncludeIndex) GetIncluders(pagePath string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var result []string
	for page, includes := range idx.PageToIncludes {
		for _, inc := range includes {
			if inc == pagePath {
				result = append(result, page)
				break
			}
		}
	}
	return result
}

// RemovePage removes a page from the include index (both as includer and as included).
func (idx *IncludeIndex) RemovePage(pagePath string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	delete(idx.PageToIncludes, pagePath)
}

// UpdatePage replaces the includes for a page. If includes is empty,
// the page entry is removed from the index.
func (idx *IncludeIndex) UpdatePage(pagePath string, includes []string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if len(includes) == 0 {
		delete(idx.PageToIncludes, pagePath)
		return
	}
	dst := make([]string, len(includes))
	copy(dst, includes)
	idx.PageToIncludes[pagePath] = dst
}

// DetectCycle checks whether setting pagePath's includes to newIncludes
// would create a cycle. Returns the cycle path and true if a cycle exists.
// This must be called BEFORE UpdatePage.
func (idx *IncludeIndex) DetectCycle(pagePath string, newIncludes []string) (cycle []string, hasCycle bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Special case: direct self-include.
	for _, inc := range newIncludes {
		if inc == pagePath {
			return []string{pagePath, pagePath}, true
		}
	}

	// For each page in newIncludes, run DFS following the existing graph.
	// If any path leads back to pagePath, we have a cycle.
	for _, start := range newIncludes {
		visited := make(map[string]bool)
		path := []string{pagePath, start}
		if idx.dfs(start, pagePath, visited, &path) {
			return path, true
		}
	}

	return nil, false
}

// dfs walks the include graph from current, looking for target.
// It uses the existing PageToIncludes for lookups.
// path tracks the traversal for error reporting.
func (idx *IncludeIndex) dfs(current, target string, visited map[string]bool, path *[]string) bool {
	if current == target {
		return true
	}
	if visited[current] {
		return false
	}
	visited[current] = true

	for _, next := range idx.PageToIncludes[current] {
		*path = append(*path, next)
		if idx.dfs(next, target, visited, path) {
			return true
		}
		*path = (*path)[:len(*path)-1]
	}

	return false
}
