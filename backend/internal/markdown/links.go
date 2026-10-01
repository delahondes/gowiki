package markdown

import (
	"path"
	"regexp"
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

// LinkOccurrence is one internal page-link appearance in a page. Keeps
// enough context for a broken-link report to point the human at the
// right spot: original href (as written), resolved absolute page path
// (what Exists is called on), label text, and 1-based line number.
type LinkOccurrence struct {
	Href     string // href as it appears in the markdown source
	Resolved string // absolute page path the href resolves to
	Label    string // visible link text between the brackets
	Line     int    // 1-based source line
}

// linkFullRe matches [label](href) and captures both groups, so a
// broken-link report can show the human-readable label alongside the
// href. Not folded into linkRe because the latter is used by
// ExtractPageLinks' resolved-paths-only path and splitting that call
// site in two would just move complexity around.
var linkFullRe = regexp.MustCompile(`(^|[^!])\[((?:[^\\\]]|\\.)*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// ExtractLinkOccurrences returns every INTERNAL page link in content,
// one entry per occurrence (no dedup — a href appearing twice reports
// twice, so the "which spot do I fix?" question has one answer per row).
// Skips images, external URLs, non-page extensions, pure fragments,
// and code blocks (fenced or indented).
//
// It intentionally does NOT resolve `#anchor` fragments against the
// target page's headings — a href like `/path/to/page#missing-section`
// still resolves cleanly to `/path/to/page` here, and the fragment-
// mismatch is reported manually by the operator. Making the resolver
// render each target page's headings would 10× the cost for a
// correctness class the frontend's own link-decorator doesn't cover
// either (see core_nodes.ts gowiki-link-missing).
func ExtractLinkOccurrences(content string, pagePath string) []LinkOccurrence {
	lines := strings.Split(content, "\n")
	var out []LinkOccurrence
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
		for _, match := range linkFullRe.FindAllStringSubmatch(line, -1) {
			label := match[2]
			raw := strings.TrimSpace(match[3])
			if raw == "" {
				continue
			}
			if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
				continue
			}
			rawPath := raw
			if idx := strings.Index(rawPath, "#"); idx >= 0 {
				rawPath = rawPath[:idx]
			}
			if idx := strings.Index(rawPath, "?"); idx >= 0 {
				rawPath = rawPath[:idx]
			}
			if rawPath == "" {
				continue
			}
			ext := path.Ext(rawPath)
			if ext != "" && !strings.EqualFold(ext, ".md") {
				continue
			}
			if strings.EqualFold(ext, ".md") {
				rawPath = strings.TrimSuffix(rawPath, ext)
			}
			resolved := ResolvePath(pagePath, rawPath)
			if resolved == "" {
				continue
			}
			out = append(out, LinkOccurrence{
				Href:     raw,
				Resolved: resolved,
				Label:    label,
				Line:     i + 1,
			})
		}
	}
	return out
}
