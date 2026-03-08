package markdown

import (
	"path"
	"sort"
	"strings"
)

// ExtractPageLinks extracts internal page links from markdown content.
// Finds [text](path) links (not images) that point to internal pages:
// either extension-less paths or .md paths.
// Skips external URLs (http://, https://) and media references (paths with
// non-.md extensions).
// pagePath is used to resolve relative paths.
// Returns deduplicated, sorted list of normalized absolute page paths.
func ExtractPageLinks(content string, pagePath string) []string {
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

		for _, match := range linkRe.FindAllStringSubmatch(line, -1) {
			raw := strings.TrimSpace(match[1])
			if raw == "" {
				continue
			}
			// Skip external URLs.
			if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
				continue
			}
			// Strip fragment and query parameters.
			rawPath := raw
			if idx := strings.Index(rawPath, "#"); idx >= 0 {
				rawPath = rawPath[:idx]
			}
			if idx := strings.Index(rawPath, "?"); idx >= 0 {
				rawPath = rawPath[:idx]
			}
			// Skip pure fragment links (e.g. #heading).
			if rawPath == "" {
				continue
			}
			// Page links are extension-less or .md.
			ext := path.Ext(rawPath)
			if ext != "" && !strings.EqualFold(ext, ".md") {
				continue
			}
			// Strip .md suffix for resolution.
			if strings.EqualFold(ext, ".md") {
				rawPath = strings.TrimSuffix(rawPath, ext)
			}
			resolved := ResolvePath(pagePath, rawPath)
			if resolved == "" {
				continue
			}
			if !seen[resolved] {
				seen[resolved] = true
				result = append(result, resolved)
			}
		}
	}

	sort.Strings(result)
	return result
}
