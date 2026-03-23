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

// ApplyTagMutations processes template tag mutations on markdown content.
// Each mutation is either "tag" (remove) or "old:new" (replace).
// It modifies {tag ...} directives in-place, removing empty directives entirely.
func ApplyTagMutations(content string, mutations []string) string {
	if len(mutations) == 0 {
		return content
	}

	// Parse mutations into remove set and replace map.
	remove := map[string]bool{}
	replace := map[string]string{}
	for _, m := range mutations {
		if idx := strings.Index(m, ":"); idx > 0 {
			replace[m[:idx]] = m[idx+1:]
		} else {
			remove[m] = true
		}
	}

	lines := strings.Split(content, "\n")
	inCodeBlock := false
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			result = append(result, line)
			continue
		}
		if inCodeBlock {
			result = append(result, line)
			continue
		}

		m := tagDirectiveRe.FindStringSubmatch(line)
		if m == nil {
			result = append(result, line)
			continue
		}

		// Process tag values.
		values := strings.Fields(m[1])
		var newValues []string
		for _, v := range values {
			if remove[v] {
				continue // remove this tag
			}
			if rep, ok := replace[v]; ok {
				newValues = append(newValues, rep)
			} else {
				newValues = append(newValues, v)
			}
		}

		if len(newValues) == 0 {
			// All tags removed — drop the entire directive line.
			continue
		}
		result = append(result, "{tag "+strings.Join(newValues, " ")+"}")
	}

	return strings.Join(result, "\n")
}
