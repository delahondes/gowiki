package reviewflow

import (
	"regexp"
	"strings"
)

var directiveRe = regexp.MustCompile(`(?m)^\{reviewflow\s+(.*?)\}\s*$`)

// Directive is the parsed shape of a `{reviewflow ...}` block.
//
// Roles is role → user (same as before, callers that only need lookups
// keep working). RoleOrder is the same role names in the order they
// appeared in the source — the sequential-notification path uses this
// to know who to email FIRST when the page is saved and who is NEXT
// after each confirmation. Parallel reverts to the historical
// all-at-once behaviour when the author opts in via `parallel=true`.
type Directive struct {
	Roles      map[string]string
	RoleOrder  []string
	VersionTag string
	Parallel   bool
}

// ParseDirective extracts the reviewflow directive from markdown content.
// Returns nil when no directive is present or no roles were declared.
func ParseDirective(markdown string) (*Directive, bool) {
	m := directiveRe.FindStringSubmatch(markdown)
	if m == nil {
		return nil, false
	}

	pairs := parseKeyValues(m[1])
	d := &Directive{Roles: make(map[string]string)}
	for _, kv := range pairs {
		switch kv.Key {
		case "version":
			d.VersionTag = kv.Value
		case "parallel":
			// Any truthy string enables parallel (true/1/yes). Anything
			// else — including "false" — leaves the default sequential.
			v := strings.ToLower(kv.Value)
			d.Parallel = v == "true" || v == "1" || v == "yes"
		default:
			d.Roles[kv.Key] = kv.Value
			d.RoleOrder = append(d.RoleOrder, kv.Key)
		}
	}
	if len(d.Roles) == 0 {
		return nil, false
	}
	return d, true
}

// KV preserves the source order of directive attributes — needed so the
// sequential-notification path can hand out review tasks in the order the
// author declared them.
type KV struct {
	Key, Value string
}

// parseKeyValues parses key=value or key="value" pairs from a directive
// body, preserving source order. Duplicate keys keep the LAST value (same
// as the previous map-based behaviour) but appear once in the order of
// their first occurrence.
func parseKeyValues(s string) []KV {
	var result []KV
	seen := make(map[string]int) // key → index in result
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
			if idx, dup := seen[key]; dup {
				result[idx].Value = val
			} else {
				seen[key] = len(result)
				result = append(result, KV{Key: key, Value: val})
			}
		}
		s = strings.TrimLeft(s, " \t")
	}
	return result
}
