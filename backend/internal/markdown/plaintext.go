package markdown

import (
	"regexp"
	"strings"
)

// Patterns for stripping markdown syntax.
var (
	// Heading markers: lines starting with one or more #.
	headingRe = regexp.MustCompile(`^(#{1,6})\s+`)

	// Bold: **text**
	boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)

	// Italic: *text*
	italicRe = regexp.MustCompile(`\*(.+?)\*`)

	// Underline: _text_
	underlineRe = regexp.MustCompile(`_(.+?)_`)

	// Strikethrough: ~~text~~
	strikethroughRe = regexp.MustCompile(`~~(.+?)~~`)

	// Inline code: `text`
	inlineCodeRe = regexp.MustCompile("`([^`]+)`")

	// Images: ![alt](url) or ![alt](url "title")
	stripImageRe = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)

	// Links: [text](url) or [text](url "title")
	stripLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]+\)`)

	// Property/directive lines: {name key=value}
	directiveRe = regexp.MustCompile(`^\s*\{[a-zA-Z]\S*(?:\s+[^}]*)?\}\s*$`)

	// Code fence markers: ``` or ```language
	codeFenceRe = regexp.MustCompile("^\\s*```")

	// Unordered list markers: - item
	ulMarkerRe = regexp.MustCompile(`^(\s*)- `)

	// Ordered list markers: 1. item
	olMarkerRe = regexp.MustCompile(`^(\s*)\d+\. `)

	// Table row separator: |---|---|
	tableSepRe = regexp.MustCompile(`^\s*\|[\s:|-]+\|\s*$`)

	// Table pipe markers
	tablePipeRe = regexp.MustCompile(`\|`)

	// H1 heading for title extraction: # Title
	h1Re = regexp.MustCompile(`^#\s+(.+)$`)
)

// StripMarkdown removes markdown syntax from content to produce plain indexable text.
// It removes: heading markers, bold/italic/underline markers, link syntax, image syntax,
// code fences, directives, property lines, and table separators.
func StripMarkdown(content string) string {
	lines := strings.Split(content, "\n")
	var result []string

	inCodeBlock := false
	for _, line := range lines {
		if codeFenceRe.MatchString(line) {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			// Include code block contents as plain text.
			result = append(result, line)
			continue
		}

		// Skip directive/property lines.
		if directiveRe.MatchString(line) {
			continue
		}

		// Skip table separator rows.
		if tableSepRe.MatchString(line) {
			continue
		}

		// Strip heading markers.
		line = headingRe.ReplaceAllString(line, "")

		// Strip images (keep alt text).
		line = stripImageRe.ReplaceAllString(line, "$1")

		// Strip links (keep link text).
		line = stripLinkRe.ReplaceAllString(line, "$1")

		// Strip bold markers.
		line = boldRe.ReplaceAllString(line, "$1")

		// Strip italic markers.
		line = italicRe.ReplaceAllString(line, "$1")

		// Strip underline markers.
		line = underlineRe.ReplaceAllString(line, "$1")

		// Strip strikethrough markers.
		line = strikethroughRe.ReplaceAllString(line, "$1")

		// Strip inline code markers.
		line = inlineCodeRe.ReplaceAllString(line, "$1")

		// Strip list markers (keep content).
		line = ulMarkerRe.ReplaceAllString(line, "$1")
		line = olMarkerRe.ReplaceAllString(line, "$1")

		// Strip table pipes.
		line = tablePipeRe.ReplaceAllString(line, " ")

		result = append(result, line)
	}

	text := strings.Join(result, "\n")
	// Collapse multiple blank lines.
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(text)
}

// ExtractTitle returns the text of the first H1 heading in the markdown content,
// or an empty string if no H1 is found.
func ExtractTitle(content string) string {
	lines := strings.Split(content, "\n")
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
		if m := h1Re.FindStringSubmatch(trimmed); m != nil {
			title := m[1]
			// Strip inline formatting from the title.
			title = boldRe.ReplaceAllString(title, "$1")
			title = italicRe.ReplaceAllString(title, "$1")
			title = underlineRe.ReplaceAllString(title, "$1")
			title = inlineCodeRe.ReplaceAllString(title, "$1")
			return strings.TrimSpace(title)
		}
	}
	return ""
}
