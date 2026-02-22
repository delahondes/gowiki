package markdown

import (
	"regexp"
	"sort"
	"strings"
)

// includeRe matches {include path=VALUE} where VALUE can be:
//   - quoted: {include path="some/path"} or {include path='some/path'}
//   - unquoted: {include path=some/path}
//
// The directive must be on its own line (leading/trailing whitespace allowed).
var includeRe = regexp.MustCompile(`^\s*\{include\s+path=(?:"([^"]+)"|'([^']+)'|(\S+?))\s*\}\s*$`)

// ExtractIncludes extracts include directives from markdown content.
// Pattern: {include path=<value>} -- must be on its own line.
// The path value follows page resolution rules (not file paths):
//   - "/footer" -> "footer"
//   - "./subpage" -> resolved relative to page namespace
//
// Skips content inside fenced code blocks.
// Returns deduplicated list of resolved page paths, sorted.
func ExtractIncludes(content string, pagePath string) []string {
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

		match := includeRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		// Extract the path value from whichever capture group matched.
		var val string
		if match[1] != "" {
			val = match[1]
		} else if match[2] != "" {
			val = match[2]
		} else {
			val = match[3]
		}

		resolved := ResolvePath(pagePath, val)
		if resolved == "" {
			continue
		}
		if !seen[resolved] {
			seen[resolved] = true
			result = append(result, resolved)
		}
	}

	sort.Strings(result)
	return result
}
