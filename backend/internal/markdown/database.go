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

// ReplaceDatabaseRowBlock replaces the field/value table inside a {database-row table=tableName}
// block with new values. fieldNames controls the output order; values maps name→value.
// If the block is not found, the content is returned unchanged.
func ReplaceDatabaseRowBlock(content, tableName string, fieldNames []string, values map[string]string) string {
	lines := strings.Split(content, "\n")
	var result []string

	inCodeBlock := false
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			result = append(result, lines[i])
			i++
			continue
		}
		if inCodeBlock {
			result = append(result, lines[i])
			i++
			continue
		}

		match := databaseRowRe.FindStringSubmatch(lines[i])
		if match == nil {
			result = append(result, lines[i])
			i++
			continue
		}

		var matchedTable string
		if match[1] != "" {
			matchedTable = match[1]
		} else if match[2] != "" {
			matchedTable = match[2]
		} else {
			matchedTable = match[3]
		}

		if matchedTable != tableName {
			result = append(result, lines[i])
			i++
			continue
		}

		// Keep the directive line.
		result = append(result, lines[i])
		i++

		// Skip blank lines between directive and table.
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			result = append(result, lines[i])
			i++
		}

		// Skip old header, separator, and data rows.
		if i < len(lines) && tableRowRe.MatchString(lines[i]) {
			i++ // skip header
		}
		if i < len(lines) && dbTableSepRe.MatchString(lines[i]) {
			i++ // skip separator
		}
		for i < len(lines) && tableRowRe.MatchString(lines[i]) {
			i++ // skip data rows
		}

		// Write new table with updated values.
		result = append(result, "| Field | Value |")
		result = append(result, "| --- | --- |")
		for _, name := range fieldNames {
			val := values[name]
			result = append(result, "| "+name+" | "+val+" |")
		}

		continue
	}

	return strings.Join(result, "\n")
}

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

// templateVarRe matches {{fieldname}} template expressions.
var templateVarRe = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_.]*)\}\}`)

// ResolveTemplateVars replaces {{field}} expressions in text using the first
// database-row block's fields found in content. Returns the original text
// unchanged if no database-row block exists or no substitutions match.
func ResolveTemplateVars(text, content string) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	rows := ExtractDatabaseRows(content)
	if len(rows) == 0 {
		return text
	}
	// Merge all row fields (first block wins for duplicates).
	fields := make(map[string]string)
	for _, row := range rows {
		for k, v := range row.Fields {
			if _, exists := fields[k]; !exists {
				fields[k] = v
			}
		}
	}
	return templateVarRe.ReplaceAllStringFunc(text, func(match string) string {
		name := match[2 : len(match)-2]
		if val, ok := fields[name]; ok {
			return val
		}
		return match
	})
}
