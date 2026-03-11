package storage

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Changelog manages the append-only global changes log.
type Changelog struct {
	mu   sync.Mutex
	path string
}

func NewChangelog(dataDir string) *Changelog {
	return &Changelog{path: filepath.Join(dataDir, "changes.log")}
}

// Append writes a line to the changes log.
// Format: timestamp\tpage\tversion\tauthor\tsummary\ttype
func (c *Changelog) Append(pagePath string, version int64, author, summary, changeType string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	line := fmt.Sprintf("%s\t%s\t%d\t%s\t%s\t%s\n",
		time.Now().UTC().Format(time.RFC3339),
		pagePath,
		version,
		author,
		summary,
		changeType,
	)

	f, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return // best effort
	}
	defer f.Close()
	f.WriteString(line)
}

// ChangeEntry represents a single parsed line from the changes log.
type ChangeEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	PagePath   string    `json:"page"`
	Version    int64     `json:"version"`
	Author     string    `json:"author"`
	Summary    string    `json:"summary"`
	ChangeType string    `json:"type"`
}

// ReadOptions controls filtering for Read().
type ReadOptions struct {
	Count        int
	IncludePaths []string
	ExcludePaths []string
	Types        []string
	Users        []string
}

// Read returns the most recent changes from the log, filtered by opts.
// Only the most recent change per page is returned (deduplication).
func (c *Changelog) Read(opts ReadOptions) ([]ChangeEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := opts.Count
	if count <= 0 {
		count = 10
	}
	if count > 100 {
		count = 100
	}

	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ChangeEntry{}, nil
		}
		return nil, err
	}

	// Parse all lines into entries.
	var allEntries []ChangeEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 6 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			continue
		}
		ver, _ := strconv.ParseInt(parts[2], 10, 64)
		allEntries = append(allEntries, ChangeEntry{
			Timestamp:  ts,
			PagePath:   parts[1],
			Version:    ver,
			Author:     parts[3],
			Summary:    parts[4],
			ChangeType: parts[5],
		})
	}

	// Build filter sets for O(1) lookup.
	typeSet := make(map[string]bool, len(opts.Types))
	for _, t := range opts.Types {
		typeSet[t] = true
	}
	userSet := make(map[string]bool, len(opts.Users))
	for _, u := range opts.Users {
		userSet[u] = true
	}

	// Iterate in reverse (most recent first), deduplicate, filter.
	seen := make(map[string]bool)
	var result []ChangeEntry
	for i := len(allEntries) - 1; i >= 0 && len(result) < count; i-- {
		e := allEntries[i]

		// Deduplicate: only most recent change per page.
		if seen[e.PagePath] {
			continue
		}

		// Filter by type.
		if len(typeSet) > 0 && !typeSet[e.ChangeType] {
			continue
		}

		// Filter by user.
		if len(userSet) > 0 && !userSet[e.Author] {
			continue
		}

		// Filter by path prefix (include/exclude).
		if !matchPathFilters(e.PagePath, opts.IncludePaths, opts.ExcludePaths) {
			continue
		}

		seen[e.PagePath] = true
		result = append(result, e)
	}

	return result, nil
}

// FirstAuthor returns the author of the first (oldest) changelog entry for a page.
// Returns empty string if no entry is found.
func (c *Changelog) FirstAuthor(pagePath string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.path)
	if err != nil {
		return ""
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 4 {
			continue
		}
		if parts[1] == pagePath {
			return parts[3]
		}
	}
	return ""
}

// matchPathFilters checks if pagePath matches the include/exclude path prefix filters.
func matchPathFilters(pagePath string, include, exclude []string) bool {
	// If include list is non-empty, page must match at least one include prefix.
	if len(include) > 0 {
		matched := false
		for _, prefix := range include {
			if strings.HasPrefix(pagePath, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check exclude list.
	for _, prefix := range exclude {
		if strings.HasPrefix(pagePath, prefix) {
			return false
		}
	}

	return true
}

func md5sum(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}
