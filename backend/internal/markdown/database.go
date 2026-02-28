package markdown

import (
	"regexp"
	"strings"
)

// DatabaseRowBlock represents a {database-row table=...} block extracted from markdown.
type DatabaseRowBlock struct {
	TableName string
	Fields    map[string]string
}

// databaseRowRe matches {database-row table=VALUE} where VALUE can be quoted or unquoted.
var databaseRowRe = regexp.MustCompile(`^\s*\{database-row\s+table=(?:"([^"]+)"|'([^']+)'|(\S+?))\s*\}\s*$`)

// tableRowRe matches | field | value | table rows.
var tableRowRe = regexp.MustCompile(`^\s*\|(.+)\|(.+)\|\s*$`)

// dbTableSepRe matches | --- | --- | separator rows.
var dbTableSepRe = regexp.MustCompile(`^\s*\|[\s-]+\|[\s-]+\|\s*$`)

// ExtractDatabaseRows parses markdown content for {database-row table=...} blocks.
// Each block consists of the directive line followed by a 2-column table (Field | Value).
func ExtractDatabaseRows(content string) []DatabaseRowBlock {
	lines := strings.Split(content, "\n")
	var results []DatabaseRowBlock

	inCodeBlock := false
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		match := databaseRowRe.FindStringSubmatch(lines[i])
		if match == nil {
			continue
		}

		// Extract table name.
		var tableName string
		if match[1] != "" {
			tableName = match[1]
		} else if match[2] != "" {
			tableName = match[2]
		} else {
			tableName = match[3]
		}

		// Parse the following table block.
		fields := make(map[string]string)
		i++ // move past directive line

		// Skip empty lines between directive and table.
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}

		// Expect: | Field | Value | (header)
		if i < len(lines) && tableRowRe.MatchString(lines[i]) {
			i++ // skip header row
		}

		// Expect: | --- | --- | (separator)
		if i < len(lines) && dbTableSepRe.MatchString(lines[i]) {
			i++ // skip separator
		}

		// Parse data rows.
		for i < len(lines) {
			rowMatch := tableRowRe.FindStringSubmatch(lines[i])
			if rowMatch == nil {
				break
			}
			key := strings.TrimSpace(rowMatch[1])
			val := strings.TrimSpace(rowMatch[2])
			if key != "" {
				fields[key] = val
			}
			i++
		}
		i-- // back up since outer loop will increment

		if tableName != "" {
			results = append(results, DatabaseRowBlock{
				TableName: tableName,
				Fields:    fields,
			})
		}
	}
	return results
}
