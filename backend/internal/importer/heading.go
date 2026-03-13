package importer

import (
	"regexp"
	"strings"
)

// DokuWiki heading: ====== H1 ====== (6 = signs = level 1)
// Level mapping: 6 = -> #, 5 = -> ##, 4 = -> ###, 3 = -> ####, 2 = -> #####
var reHeading = regexp.MustCompile(`^(\s*)(={2,6})\s*(.+?)\s*={2,6}\s*$`)

// ConvertHeading converts a DokuWiki heading line to Gowiki ATX heading.
// Returns the converted line and true if it was a heading, or the original line and false.
func ConvertHeading(line string) (string, bool) {
	m := reHeading.FindStringSubmatch(line)
	if m == nil {
		return line, false
	}

	eqCount := len(m[2])
	// DokuWiki: 6 = signs = H1, 5 = H2, ..., 2 = H5
	level := 7 - eqCount
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}

	prefix := strings.Repeat("#", level)
	title := strings.TrimSpace(m[3])

	// Strip DokuWiki numbered heading plugin "- " prefix.
	// These headings use "- " as a placeholder for auto-numbering at render time.
	if strings.HasPrefix(title, "- ") {
		title = strings.TrimPrefix(title, "- ")
	}

	return prefix + " " + title, true
}

// HeadingAnchor converts a heading title to a Gowiki anchor slug.
// Used when resolving #section references in links.
func HeadingAnchor(title string) string {
	title = strings.ToLower(title)
	title = strings.TrimSpace(title)
	// Replace spaces and special chars with hyphens
	var b strings.Builder
	prevHyphen := false
	for _, r := range title {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	s := strings.Trim(b.String(), "-")
	return s
}
