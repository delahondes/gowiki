package todo

import (
	"crypto/sha1"
	"encoding/hex"
	"path"
	"regexp"
	"strings"
)

// todoDirectiveRe matches {todo ...} on a single line.
var todoDirectiveRe = regexp.MustCompile(`(?m)^\s*\{todo\s+(.+?)\}\s*$`)

// kvRe matches key=value or key="value with spaces".
var kvRe = regexp.MustCompile(`(\w+)=(?:"([^"]*?)"|'([^']*?)'|(\S+))`)

// ExtractTodoDirectives parses all {todo ...} blocks from markdown content.
func ExtractTodoDirectives(markdown string) []ParsedDirective {
	matches := todoDirectiveRe.FindAllStringSubmatch(markdown, -1)
	if len(matches) == 0 {
		return nil
	}

	var directives []ParsedDirective
	for _, m := range matches {
		body := m[1]
		kv := parseKeyValues(body)

		d := ParsedDirective{
			Title:       kv["title"],
			Assign:      kv["assign"],
			Resolution:  kv["resolution"],
			Due:         kv["due"],
			Recur:       kv["recur"],
			Priority:    kv["priority"],
			Action:      kv["action"],
			Tags:        kv["tags"],
			Description: kv["description"],
		}

		// Compute node_key as SHA1 of source_page:title:assign for stable identity.
		// source_page is added by the caller; here we compute partial key.
		d.NodeKey = computeNodeKey("", d.Title, d.Assign)

		directives = append(directives, d)
	}
	return directives
}

// parseKeyValues extracts key=value pairs from a directive body.
func parseKeyValues(body string) map[string]string {
	result := make(map[string]string)
	for _, m := range kvRe.FindAllStringSubmatch(body, -1) {
		key := m[1]
		// Pick the first non-empty capture group for the value.
		value := m[2]
		if value == "" {
			value = m[3]
		}
		if value == "" {
			value = m[4]
		}
		result[key] = value
	}
	return result
}

// computeNodeKey produces a stable identifier for matching wiki_node tasks across saves.
func computeNodeKey(pagePath, title, assign string) string {
	h := sha1.Sum([]byte(pagePath + ":" + title + ":" + assign))
	return hex.EncodeToString(h[:])
}

// parseRecur converts a recurrence string into a Recurrence struct.
// Formats: "3d" (delay 3 days), "daily", "weekly", "monthly", "yearly", "3months".
func parseRecur(raw string) Recurrence {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return Recurrence{}
	}

	switch raw {
	case "daily":
		return Recurrence{Type: "calendar", Every: 1, Unit: "day"}
	case "weekly":
		return Recurrence{Type: "calendar", Every: 1, Unit: "week"}
	case "monthly":
		return Recurrence{Type: "calendar", Every: 1, Unit: "month"}
	case "yearly":
		return Recurrence{Type: "calendar", Every: 1, Unit: "year"}
	}

	// Try "Nd" format for delay days.
	if strings.HasSuffix(raw, "d") {
		if n := parseInt(strings.TrimSuffix(raw, "d")); n > 0 {
			return Recurrence{Type: "delay", Days: n}
		}
	}

	// Try "Nmonths", "Nweeks", "Nyears", "Ndays" format.
	for _, unit := range []struct {
		suffix string
		name   string
	}{
		{"months", "month"},
		{"weeks", "week"},
		{"years", "year"},
		{"days", "day"},
	} {
		if strings.HasSuffix(raw, unit.suffix) {
			if n := parseInt(strings.TrimSuffix(raw, unit.suffix)); n > 0 {
				return Recurrence{Type: "calendar", Every: n, Unit: unit.name}
			}
		}
	}

	return Recurrence{}
}

// parseAction converts an action string into a WikiAction struct.
// Formats: "read:path", "edit:path", "create:pattern", "set_meta:path:schema:field:value".
func parseAction(raw string) WikiAction {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return WikiAction{}
	}

	parts := strings.SplitN(raw, ":", 2)
	if len(parts) < 2 {
		return WikiAction{}
	}

	actionType := strings.ToLower(parts[0])
	rest := parts[1]

	// Page paths are /-prefixed; just trim trailing slash if any.
	rest = strings.TrimRight(rest, "/")

	switch actionType {
	case "read", "edit":
		return WikiAction{Type: actionType, Page: rest}
	case "create":
		return WikiAction{Type: actionType, Pattern: rest}
	case "set_meta":
		// set_meta:path:schema:field:value
		metaParts := strings.SplitN(rest, ":", 4)
		if len(metaParts) < 4 {
			return WikiAction{}
		}
		return WikiAction{
			Type:   actionType,
			Page:   strings.TrimRight(metaParts[0], "/"),
			Schema: metaParts[1],
			Field:  metaParts[2],
			Value:  metaParts[3],
		}
	}
	return WikiAction{}
}

// resolveActionPath resolves an action path relative to the source page.
// Absolute paths (starting with /) are returned as-is.
// Relative paths (starting with ./ or without /) are resolved against the source page's namespace.
func resolveActionPath(sourcePage, actionPath string) string {
	actionPath = strings.TrimSpace(actionPath)
	if actionPath == "" {
		return ""
	}
	// Already absolute.
	if strings.HasPrefix(actionPath, "/") {
		return actionPath
	}
	// Resolve relative to source page namespace.
	namespace := path.Dir(sourcePage)
	resolved := path.Clean(namespace + "/" + actionPath)
	if !strings.HasPrefix(resolved, "/") {
		resolved = "/" + resolved
	}
	return resolved
}

// parseInt parses a non-negative integer from a string, returning 0 on failure.
func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
