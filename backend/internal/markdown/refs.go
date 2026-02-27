package markdown

import (
	"fmt"
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
	// Strip query parameters (e.g. ?v=2) before extension/path checks.
	rawPath := raw
	if idx := strings.Index(rawPath, "?"); idx >= 0 {
		rawPath = rawPath[:idx]
	}
	// Must have a file extension.
	ext := path.Ext(rawPath)
	if ext == "" {
		return
	}
	// Skip .md links (those are page links, not media refs).
	if strings.EqualFold(ext, ".md") {
		return
	}

	resolved := ResolvePath(pagePath, rawPath)
	if resolved == "" {
		return
	}
	if !seen[resolved] {
		seen[resolved] = true
		*result = append(*result, resolved)
	}
}

// mediaRefFullRe matches the URL inside ![alt](URL) or [text](URL), capturing the
// bracketed part and the URL portion separately for replacement.
// Group 1: everything up to and including the opening "(" — e.g. "![alt]("
// Group 2: the URL (no spaces, no closing paren)
// Group 3: optional title + closing ")" — e.g. ` "title")`
var mediaRefFullRe = regexp.MustCompile(`(!?\[(?:[^\\\]]|\\.)*\]\()([^)\s]+)((?:\s+"[^"]*")?\))`)

// ExpandMediaVersions rewrites bare media references in markdown to include ?v=N.
// A "bare" reference is one without an existing ?v= query parameter.
// Only expands references whose resolved media path has version > 1 in the lookup.
// pagePath is the current page path (for resolving relative references).
// getVersion returns the current version for a media path (0 if not tracked).
func ExpandMediaVersions(content string, pagePath string, getVersion func(mediaPath string) int64) string {
	lines := strings.Split(content, "\n")
	var changed bool

	inCodeBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		newLine := mediaRefFullRe.ReplaceAllStringFunc(line, func(match string) string {
			groups := mediaRefFullRe.FindStringSubmatch(match)
			if len(groups) < 4 {
				return match
			}
			prefix := groups[1] // e.g. "![alt](" or "[text]("
			url := groups[2]    // the URL
			suffix := groups[3] // e.g. ")" or ` "title")`

			// Skip external URLs.
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				return match
			}

			// Skip if already has ?v= parameter.
			if strings.Contains(url, "?v=") {
				return match
			}

			// Strip any existing query for path resolution.
			urlPath := url
			if idx := strings.Index(urlPath, "?"); idx >= 0 {
				urlPath = urlPath[:idx]
			}

			// Must have a file extension and not be .md.
			ext := path.Ext(urlPath)
			if ext == "" || strings.EqualFold(ext, ".md") {
				return match
			}

			resolved := ResolvePath(pagePath, urlPath)
			if resolved == "" {
				return match
			}

			ver := getVersion(resolved)
			if ver <= 1 {
				return match // v=1 is implicit for bare references
			}

			// Append ?v=N to the URL.
			newURL := fmt.Sprintf("%s?v=%d", url, ver)
			return prefix + newURL + suffix
		})

		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}

	if !changed {
		return content
	}
	return strings.Join(lines, "\n")
}
