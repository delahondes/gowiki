package markdown

import (
	"regexp"
	"strings"
)

var tagDirectiveRe = regexp.MustCompile(`^\s*\{tag\s+(.+?)\s*\}\s*$`)

// ExtractTags returns all tags found in {tag ...} directives in the content.
// Tags are space-separated values in the directive. Code blocks are skipped.
func ExtractTags(content string) []string {
	lines := strings.Split(content, "\n")
	inCodeBlock := false
	seen := map[string]bool{}
	var tags []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		m := tagDirectiveRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		values := strings.Fields(m[1])
		for _, v := range values {
			if !seen[v] {
				seen[v] = true
				tags = append(tags, v)
			}
		}
	}
	return tags
}
