package markdown

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// imageRe matches ![alt](path) or ![alt](path "title").
var imageRe = regexp.MustCompile(`!\[(?:[^\\\]]|\\.)*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// linkRe matches [text](path) or [text](path "title"), but not images (no leading !).
var linkRe = regexp.MustCompile(`(?:^|[^!])\[(?:[^\\\]]|\\.)*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// ExtractMediaRefs extracts media file references from markdown content.
// Finds image refs ![alt](path) and link refs [text](path).
// Only includes paths that have a file extension AND the extension is NOT .md
// (links to .md files are page links, not media refs).
// Skips content inside fenced code blocks (``` markers).
// pagePath is used to resolve relative paths.
// Returns deduplicated, sorted list of normalized absolute paths.
func ExtractMediaRefs(content string, pagePath string) []string {
	lines := strings.Split(content, "\n")
	seen := make(map[string]bool)
	var result []string

	inCodeBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		// Extract image refs.
		for _, match := range imageRe.FindAllStringSubmatch(line, -1) {
			raw := match[1]
			addMediaRef(raw, pagePath, seen, &result)
		}

		// Extract link refs.
		for _, match := range linkRe.FindAllStringSubmatch(line, -1) {
			raw := match[1]
			addMediaRef(raw, pagePath, seen, &result)
		}
	}

	sort.Strings(result)
	return result
}

// addMediaRef resolves and deduplicates a media reference path.
func addMediaRef(raw, pagePath string, seen map[string]bool, result *[]string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	// Skip external URLs.
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return
	}
	// Must have a file extension.
	ext := path.Ext(raw)
	if ext == "" {
		return
	}
	// Skip .md links (those are page links, not media refs).
	if strings.EqualFold(ext, ".md") {
		return
	}

	resolved := ResolvePath(pagePath, raw)
	if resolved == "" {
		return
	}
	if !seen[resolved] {
		seen[resolved] = true
		*result = append(*result, resolved)
	}
}
