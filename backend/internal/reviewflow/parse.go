package reviewflow

import (
	"regexp"
	"strings"
)

var directiveRe = regexp.MustCompile(`(?m)^\{reviewflow\s+(.*?)\}\s*$`)

// ParseDirective extracts the reviewflow directive from markdown content.
// Returns roles (all non-"version" key=value pairs), the version tag, and
// whether a directive was found.
func ParseDirective(markdown string) (roles map[string]string, versionTag string, found bool) {
	m := directiveRe.FindStringSubmatch(markdown)
	if m == nil {
		return nil, "", false
	}

	roles = make(map[string]string)
	pairs := parseKeyValues(m[1])
	for k, v := range pairs {
		if k == "version" {
			versionTag = v
		} else {
			roles[k] = v
		}
	}
	if len(roles) == 0 {
		return nil, "", false
	}
	return roles, versionTag, true
}

// parseKeyValues parses key=value or key="value" pairs from a directive body.
func parseKeyValues(s string) map[string]string {
	result := make(map[string]string)
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		// Find key.
		eqIdx := strings.IndexByte(s, '=')
		if eqIdx < 0 {
			break
		}
		key := strings.TrimSpace(s[:eqIdx])
		s = s[eqIdx+1:]

		// Find value.
		s = strings.TrimLeft(s, " \t")
		var val string
		if len(s) > 0 && s[0] == '"' {
			// Quoted value.
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				val = s[1:]
				s = ""
			} else {
				val = s[1 : end+1]
				s = s[end+2:]
			}
		} else {
			// Unquoted value: up to next space.
			spIdx := strings.IndexAny(s, " \t")
			if spIdx < 0 {
				val = s
				s = ""
			} else {
				val = s[:spIdx]
				s = s[spIdx:]
			}
		}
		if key != "" {
			result[key] = val
		}
		s = strings.TrimLeft(s, " \t")
	}
	return result
}
