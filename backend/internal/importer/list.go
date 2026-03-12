package importer

import (
	"regexp"
	"strings"
)

// DokuWiki list:
//   "  * item" = level 1 unordered (2 spaces)
//   "    * item" = level 2 unordered (4 spaces)
//   "  - item" = level 1 ordered (2 spaces)
//   "    - item" = level 2 ordered (4 spaces)

var reListLine = regexp.MustCompile(`^((?:  )+)([\*\-])\s(.*)$`)

// ConvertListLine converts a DokuWiki list line to Gowiki Markdown.
// Returns the converted line and true if it was a list line.
func ConvertListLine(line string, currentNS string) (string, bool) {
	m := reListLine.FindStringSubmatch(line)
	if m == nil {
		return line, false
	}

	indent := m[1]
	marker := m[2]
	content := m[3]

	// DokuWiki: 2 spaces per level. Level 1 = 2 spaces, Level 2 = 4 spaces, etc.
	level := len(indent) / 2
	if level < 1 {
		level = 1
	}

	// Convert inline markup in the content
	content = ConvertInline(content, currentNS, "list")

	// Gowiki indentation: 2 spaces per additional level (level 1 = no indent)
	gowikiIndent := strings.Repeat("  ", level-1)

	if marker == "-" {
		return gowikiIndent + "1. " + content, true
	}
	return gowikiIndent + "- " + content, true
}

// IsListLine returns true if the line looks like a DokuWiki list line.
func IsListLine(line string) bool {
	return reListLine.MatchString(line)
}
