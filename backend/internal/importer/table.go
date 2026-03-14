package importer

import (
	"fmt"
	"regexp"
	"strings"
)

// Table conversion: DokuWiki tables use ^ for header cells and | for data cells.
// Gowiki uses standard pipe tables with a --- separator row.

var (
	// Table formula: ~~=formula~~
	reFormula = regexp.MustCompile(`~~=([^~]+)~~`)

	// Cell color: @Color:content or @#hex:content
	reCellColor = regexp.MustCompile(`@([A-Za-z]+|#[0-9a-fA-F]{3,6}):`)

	// Vertical text: !!content!!
	reVerticalText = regexp.MustCompile(`^!!(.+)!!$`)

	// WRAP tags inside table cells
	reWrapInCell = regexp.MustCompile(`(?i)</?WRAP[^>]*>`)
)

// ConvertTable converts a group of DokuWiki table lines to Gowiki Markdown.
func ConvertTable(lines []string, currentNS string) ([]string, []FlaggedLine) {
	if len(lines) == 0 {
		return nil, nil
	}

	var flagged []FlaggedLine
	var rows [][]tableCell

	for _, line := range lines {
		cells, flags := parseTableRow(line, currentNS)
		rows = append(rows, cells)
		flagged = append(flagged, flags...)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	// Detect header pattern
	headerRow := -1
	headerCol := false

	// Check if first row is all headers
	if isAllHeader(rows[0]) {
		headerRow = 0
	}

	// Check for header column pattern: first cell of each row is header
	if headerRow < 0 && len(rows) >= 1 {
		allFirstHeader := true
		for _, row := range rows {
			if len(row) > 0 && !row[0].isHeader {
				allFirstHeader = false
				break
			}
		}
		if allFirstHeader {
			headerCol = true
		}
	}

	// Build output
	var out []string

	// Determine max columns
	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}

	// If header column, add property line
	if headerCol && headerRow < 0 {
		out = append(out, "{table headers=1c}")
	}

	for i, row := range rows {
		line := renderTableRow(row, maxCols)
		out = append(out, line)

		// Insert separator after header row
		if i == headerRow {
			sep := "|"
			for j := 0; j < maxCols; j++ {
				sep += " --- |"
			}
			out = append(out, sep)
		}
	}

	// If no header row was detected, we need a separator after the first data row.
	// In Gowiki pipe tables, a separator is mandatory.
	if headerRow < 0 {
		sep := "|"
		for j := 0; j < maxCols; j++ {
			sep += " --- |"
		}
		// When headerCol is true, out[0] is the property line {table headers=1st_col}
		// and out[1] is the first data row. Insert separator after the first data row.
		insertAfter := 0
		if headerCol {
			insertAfter = 1
		}
		if insertAfter < len(out) {
			result := make([]string, 0, len(out)+1)
			result = append(result, out[:insertAfter+1]...)
			result = append(result, sep)
			result = append(result, out[insertAfter+1:]...)
			out = result
		}
	}

	return out, flagged
}

type tableCell struct {
	content  string
	isHeader bool
	isMerge  bool // ::: vertical merge
	color    string
	vtext    bool // !!text!! vertical text
}

// parseTableRow parses a DokuWiki table row into cells.
func parseTableRow(line string, currentNS string) ([]tableCell, []FlaggedLine) {
	var flagged []FlaggedLine
	line = strings.TrimSpace(line)

	// Tokenize: split by ^ and | while tracking which delimiter was used
	var cells []tableCell
	if len(line) == 0 {
		return nil, nil
	}

	// Remove trailing delimiter
	if line[len(line)-1] == '|' || line[len(line)-1] == '^' {
		line = line[:len(line)-1]
	}

	// Split using a state-aware approach since cells can contain | in links
	parts, delimiters := splitTableRow(line)

	for i, part := range parts {
		cell := tableCell{}
		if i < len(delimiters) {
			cell.isHeader = delimiters[i] == '^'
		}

		content := strings.TrimSpace(part)

		// Handle vertical merge :::
		if content == ":::" {
			cell.isMerge = true
			cell.content = "^^"
			cells = append(cells, cell)
			continue
		}

		// Handle cell colors @Color:content
		if m := reCellColor.FindStringIndex(content); m != nil {
			colorMatch := reCellColor.FindStringSubmatch(content)
			cell.color = strings.ToLower(colorMatch[1])
			content = content[m[1]:]
		}

		// Handle vertical text !!content!!
		// Skip if content contains \\ (line break) or (( (footnote) — these don't render well vertically.
		if vm := reVerticalText.FindStringSubmatch(content); vm != nil {
			inner := vm[1]
			if !strings.Contains(inner, `\\`) && !strings.Contains(inner, "((") {
				cell.vtext = true
			}
			content = inner
		}

		// Strip WRAP tags inside cells
		content = reWrapInCell.ReplaceAllString(content, "")

		// Handle formulas
		if fm := reFormula.FindStringSubmatch(content); fm != nil {
			formula := fm[1]
			converted := convertFormula(formula)
			content = reFormula.ReplaceAllString(content, converted)
			if strings.Contains(converted, "IMPORT:FLAG") {
				flagged = append(flagged, FlaggedLine{Reason: "table_formula", Content: fm[0]})
			}
		}

		// Convert template variables and inline markup in cell content
		content = ConvertTemplateVars(content)
		content = ConvertInline(content, currentNS, "table")
		content = strings.TrimSpace(content)

		cell.content = content
		cells = append(cells, cell)
	}

	return cells, flagged
}

// splitTableRow splits a table row by | and ^ delimiters, returning parts and delimiters.
// Handles [[ ]] links and {{ }} media references that may contain | inside them.
func splitTableRow(line string) ([]string, []byte) {
	var parts []string
	var delimiters []byte
	var current strings.Builder
	inLink := 0
	inMedia := 0

	for i := 0; i < len(line); i++ {
		ch := line[i]

		// Track link nesting [[ ]]
		if i+1 < len(line) && ch == '[' && line[i+1] == '[' {
			inLink++
			current.WriteByte(ch)
			continue
		}
		if i+1 < len(line) && ch == ']' && line[i+1] == ']' {
			inLink--
			current.WriteByte(ch)
			continue
		}

		// Track media nesting {{ }}
		if i+1 < len(line) && ch == '{' && line[i+1] == '{' {
			inMedia++
			current.WriteByte(ch)
			continue
		}
		if i+1 < len(line) && ch == '}' && line[i+1] == '}' {
			inMedia--
			current.WriteByte(ch)
			continue
		}

		// Delimiters only at top level
		if inLink == 0 && inMedia == 0 && (ch == '|' || ch == '^') {
			parts = append(parts, current.String())
			delimiters = append(delimiters, ch)
			current.Reset()
			continue
		}

		current.WriteByte(ch)
	}

	// Don't forget the last part
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	// The first "part" before the first delimiter is empty (row starts with | or ^)
	// so we skip it
	if len(parts) > 0 && len(delimiters) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	} else if len(delimiters) > 0 {
		// First delimiter is the row opener, associated with the first real cell
		parts = parts[1:]
	}

	return parts, delimiters
}

// renderTableRow renders cells as a Gowiki pipe table row.
func renderTableRow(cells []tableCell, maxCols int) string {
	var b strings.Builder
	b.WriteByte('|')
	for i := 0; i < maxCols; i++ {
		if i < len(cells) {
			c := cells[i]
			b.WriteByte(' ')
			// Emit cell directive if color or vertical text
			var dirParts []string
			if c.color != "" {
				dirParts = append(dirParts, "color="+c.color)
			}
			if c.vtext {
				dirParts = append(dirParts, "vtext=upward")
			}
			if len(dirParts) > 0 {
				b.WriteString("{" + strings.Join(dirParts, " ") + "} ")
			}
			b.WriteString(c.content)
			b.WriteString(" |")
		} else {
			b.WriteString(" |")
		}
	}
	return b.String()
}

// isAllHeader returns true if all cells in the row are header cells.
func isAllHeader(cells []tableCell) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !c.isHeader {
			return false
		}
	}
	return true
}

// convertFormula converts a DokuWiki table formula to Gowiki syntax.
// DokuWiki: ~~=sum(range(col(),1,col(),row()-1))~~ -> =SUM(ABOVE)
// Complex formulas are flagged.
func convertFormula(formula string) string {
	f := strings.TrimSpace(formula)
	fl := strings.ToLower(f)

	// Common pattern: sum of column above
	if strings.Contains(fl, "sum") && strings.Contains(fl, "range(col(),1,col(),row()-1)") {
		// Check for trailing %
		suffix := ""
		if strings.HasSuffix(f, "%") {
			suffix = "%"
		}
		return "=SUM(ABOVE)" + suffix
	}

	// Other formulas: flag for manual review
	return fmt.Sprintf("=(%s) `IMPORT:FLAG formula`", f)
}

// IsTableLine returns true if the line starts with | or ^.
func IsTableLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) > 0 && (trimmed[0] == '|' || trimmed[0] == '^')
}
