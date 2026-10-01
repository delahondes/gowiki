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

// SlugifyHeading mirrors the frontend compiler/slugify.ts: lowercase,
// collapse any run of non-alnum characters to a single hyphen, trim
// leading/trailing hyphens. Must agree with the frontend byte-for-byte
// so a fragment anchor the browser accepts resolves the same way here
// — otherwise this tool would report "dead" anchors that render fine
// (or vice versa).
//
// The frontend uses `[^a-z0-9]+` on the already-lowercased string, so
// any character outside ASCII [a-z0-9] is a separator (an é becomes
// a hyphen, not an "e"). We do NOT ASCII-fold; that would diverge.
func SlugifyHeading(text string) string {
	var b strings.Builder
	prevHyphen := true // treat the implicit start as a hyphen so leading runs collapse
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	out := b.String()
	// Trim trailing hyphen (leading already handled by prevHyphen=true).
	out = strings.TrimRight(out, "-")
	if out == "" {
		return "heading"
	}
	return out
}

// numberedHeadingPrefixRe matches the dialect's `N. ` numbering prefix
// on a heading. The frontend parser strips this prefix at parse time
// (see core_nodes.ts `gowiki_numbered_heading` core rule) so the
// resulting node's textContent — and hence its slug — does NOT include
// the number. The backend extractor must apply the same strip, otherwise
// a href like `#documentation-effort` into a `## 1. Documentation effort`
// heading would be reported as a dead fragment.
var numberedHeadingPrefixRe = regexp.MustCompile(`^\d+\. `)

// ExtractHeadingSlugs walks the markdown source and returns the set of
// anchor slugs — one per ATX heading, with the frontend's "same slug
// twice → suffix the duplicates with -1, -2, …" rule so the server
// agrees with the browser on which anchors actually exist.
// Skips headings inside fenced code blocks; `#` lines there are code,
// not section headings.
//
// The result is a set (map to empty struct) so a caller can answer
// "is this fragment a real anchor?" with one lookup. Called by the
// list_broken_links tool when check_fragments is on.
func ExtractHeadingSlugs(content string) map[string]struct{} {
	headingRe := regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	slugs := make(map[string]struct{})
	counts := make(map[string]int)
	inCodeBlock := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}
		// Only ATX headings; the dialect doesn't support setext.
		m := headingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		text := stripInlineMarkup(m[2])
		// Numbered-heading prefix is syntactic, not part of the slug:
		// `## 1. Documentation effort` slugifies as `documentation-effort`,
		// not `1-documentation-effort`. The counter is re-computed at
		// render time; two headings both marked "1." still produce
		// distinct slugs via the duplicate-suffix rule below.
		text = numberedHeadingPrefixRe.ReplaceAllString(text, "")
		base := SlugifyHeading(text)
		n := counts[base]
		counts[base] = n + 1
		slug := base
		if n > 0 {
			slug = base + "-" + itoa(n)
		}
		slugs[slug] = struct{}{}
	}
	return slugs
}

// itoa is a thin wrapper so the heading-slug code stays dep-free
// (strconv is one import away but the single integer stringification
// isn't worth pulling it in).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// linkInHeadingRe matches a markdown link INSIDE a heading. We want to
// keep only the visible label — the frontend slugifies heading
// textContent, which the browser renders as the label without the URL.
var linkInHeadingRe = regexp.MustCompile(`\[((?:[^\\\]]|\\.)*)\]\([^)]*\)`)

// stripInlineMarkup turns a raw heading source into the plain text a
// reader would see on the page — then SlugifyHeading can run on
// something that matches the browser's `textContent` byte-for-byte.
// Drops asterisks / underscores / backticks (bold / italic / code
// delimiters) and reduces markdown links to their label.
func stripInlineMarkup(s string) string {
	// Reduce [label](url) to label first — order matters, otherwise
	// stripping `[` and `]` leaves the URL and parens visible.
	s = linkInHeadingRe.ReplaceAllString(s, "$1")

	var b strings.Builder
	skipNext := false
	for _, r := range s {
		if skipNext {
			skipNext = false
			b.WriteRune(r)
			continue
		}
		switch r {
		case '\\':
			// Escape — keep the next literal char without re-interpreting it.
			skipNext = true
		case '*', '_', '`':
			// Drop remaining markup punctuation; leave inner text.
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

