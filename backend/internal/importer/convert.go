package importer

import (
	"fmt"
	"strings"
)

// blockState tracks the converter's current state.
type blockState int

const (
	stateNormal blockState = iota
	stateCode
	stateFile
	stateNowiki
	stateReviewflow
	stateStruct
	stateFigure
	stateNBNote
	stateNBWarn
	stateFold
)

// ConvertPage converts a full DokuWiki page to Gowiki Markdown.
// pagePath is the source page path relative to pages/ (e.g. "ns/page.txt"),
// used for resolving relative links.
// pagesDir is the root pages/ directory, used to resolve include anchors.
func ConvertPage(content string, pagePath string, pagesDir string) *ConvertResult {
	currentNS := NamespaceOf(pagePath)
	lines := strings.Split(content, "\n")
	result := &ConvertResult{TotalLines: len(lines)}

	// Check if this is a slider page
	hasSlider := false
	for _, line := range lines {
		if reSlider.MatchString(line) {
			hasSlider = true
			break
		}
	}
	if hasSlider {
		out, flagged := ConvertSliderPage(lines, currentNS, pagesDir)
		result.Markdown = strings.Join(out, "\n")
		result.Flagged = flagged
		result.ConvertLines = result.TotalLines - len(flagged)
		return result
	}

	var output []string
	var state blockState
	var blockBuf []string
	var blockLang string // for code blocks
	var tableLines []string
	var wrapStack []WrapBlock
	wrapDepth := 0
	var wrapBuf []string
	nbFirstLine := "" // first line content after NB:: marker

	flushTable := func() {
		if len(tableLines) == 0 {
			return
		}
		converted, flagged := ConvertTable(tableLines, currentNS)
		output = append(output, converted...)
		result.Flagged = append(result.Flagged, flagged...)
		tableLines = nil
	}

	emitImageLine := func(line string) {
		// Handle the \x01IMG{...}\x01 markers from inline conversion
		if !strings.Contains(line, "\x01IMG{") {
			output = append(output, line)
			return
		}
		// Split on image markers and emit property lines before each image
		parts := strings.Split(line, "\x01")

		// Check if image is standalone (no other text on the line)
		hasOtherContent := false
		for _, part := range parts {
			if !strings.HasPrefix(part, "IMG{") && strings.TrimSpace(part) != "" {
				hasOtherContent = true
				break
			}
		}

		if !hasOtherContent {
			// Standalone image: emit property on separate line, then image
			for _, part := range parts {
				if strings.HasPrefix(part, "IMG{") {
					props := parseImageMarker(part)
					propLine, imgLine := buildImageLines(props)
					if propLine != "" {
						output = append(output, propLine)
					}
					output = append(output, imgLine)
				}
			}
		} else {
			// Inline image: emit {image ...}![](path) inline within the text
			var assembled []string
			for _, part := range parts {
				if strings.HasPrefix(part, "IMG{") {
					props := parseImageMarker(part)
					propLine, imgLine := buildImageLines(props)
					if propLine != "" {
						assembled = append(assembled, propLine+imgLine)
					} else {
						assembled = append(assembled, imgLine)
					}
				} else if part != "" {
					assembled = append(assembled, part)
				}
			}
			if len(assembled) > 0 {
				output = append(output, strings.Join(assembled, ""))
			}
		}
	}

	for lineNum, line := range lines {
		_ = lineNum // available for flagging

		switch state {
		case stateCode, stateFile:
			closePat := reCodeClose
			if state == stateFile {
				closePat = reFileClose
			}
			if closePat.MatchString(line) {
				// Emit code block
				output = append(output, "```"+blockLang)
				output = append(output, blockBuf...)
				output = append(output, "```")
				state = stateNormal
				result.ConvertLines += len(blockBuf) + 2
			} else {
				blockBuf = append(blockBuf, line)
			}
			continue

		case stateNowiki:
			if reNowikiBlockClose.MatchString(line) {
				// Emit as code block
				output = append(output, "```")
				output = append(output, blockBuf...)
				output = append(output, "```")
				state = stateNormal
				result.ConvertLines += len(blockBuf) + 2
			} else {
				blockBuf = append(blockBuf, line)
			}
			continue

		case stateReviewflow:
			blockBuf = append(blockBuf, line)
			if strings.TrimSpace(line) == "~~" {
				converted := ConvertReviewflow(blockBuf)
				output = append(output, converted)
				state = stateNormal
				result.ConvertLines += len(blockBuf)
			}
			continue

		case stateStruct:
			blockBuf = append(blockBuf, line)
			if reStructClose.MatchString(strings.TrimSpace(line)) && len(blockBuf) > 1 {
				// Flag the struct block
				result.Flagged = append(result.Flagged, FlaggedLine{
					LineNum: lineNum - len(blockBuf) + 1,
					Reason:  "struct_block",
					Content: strings.Join(blockBuf, "\n"),
				})
				output = append(output, "```")
				output = append(output, blockBuf...)
				output = append(output, "```")
				state = stateNormal
			}
			continue

		case stateFigure:
			if reFigureClose.MatchString(line) {
				blockBuf = append(blockBuf, line)
				converted, flagged := ConvertFigure(blockBuf, currentNS)
				output = append(output, converted...)
				result.Flagged = append(result.Flagged, flagged...)
				result.ConvertLines += len(blockBuf)
				state = stateNormal
			} else {
				blockBuf = append(blockBuf, line)
			}
			continue

		case stateNBNote, stateNBWarn:
			trimmedLine := strings.TrimSpace(line)
			closedOnThisLine := reNBClose.MatchString(trimmedLine)
			// Also check for ::NB at the end of a content line
			if !closedOnThisLine && strings.HasSuffix(trimmedLine, "::NB") {
				closedOnThisLine = true
				// Add the content before ::NB
				content := strings.TrimSuffix(trimmedLine, "::NB")
				if strings.TrimSpace(content) != "" {
					blockBuf = append(blockBuf, strings.TrimSpace(content))
				}
			}
			if closedOnThisLine {
				isWarn := state == stateNBWarn
				var nbLines []string
				if nbFirstLine != "" {
					nbLines = append(nbLines, nbFirstLine)
				}
				nbLines = append(nbLines, blockBuf...)
				converted := ConvertNBBlock(nbLines, isWarn, currentNS)
				output = append(output, converted...)
				result.ConvertLines += len(blockBuf) + 2
				state = stateNormal
			} else {
				blockBuf = append(blockBuf, line)
			}
			continue

		case stateFold:
			if reFoldClose.MatchString(line) {
				// Emit accumulated content (fold wrapper stripped)
				for _, bl := range blockBuf {
					converted := convertNormalLine(bl, currentNS, pagesDir)
					emitImageLine(converted)
				}
				result.ConvertLines += len(blockBuf) + 2
				state = stateNormal
			} else {
				blockBuf = append(blockBuf, line)
			}
			continue

		case stateNormal:
			// Fall through to normal processing below
		}

		trimmed := strings.TrimSpace(line)

		// --- Block openers ---

		// Code block: only if closing tag is not also on this line
		if m := reCodeOpen.FindStringSubmatch(trimmed); m != nil {
			afterOpen := trimmed[len(m[0]):]
			if !strings.Contains(strings.ToLower(afterOpen), "</code>") {
				flushTable()
				state = stateCode
				blockLang = m[1]
				blockBuf = nil
				result.ConvertLines++
				continue
			}
			// Inline <code>text</code> — fall through to normal processing
		}
		if m := reFileOpen.FindStringSubmatch(trimmed); m != nil {
			afterOpen := trimmed[len(m[0]):]
			if !strings.Contains(strings.ToLower(afterOpen), "</file>") {
				flushTable()
				state = stateFile
				parts := strings.Fields(m[1])
				blockLang = ""
				if len(parts) > 0 {
					blockLang = parts[0]
				}
				blockBuf = nil
				result.ConvertLines++
				continue
			}
		}

		// Nowiki block: only if closing tag is not also on this line
		if reNowikiBlockOpen.MatchString(trimmed) {
			if !strings.Contains(strings.ToLower(trimmed), "</nowiki>") {
				flushTable()
				state = stateNowiki
				blockBuf = nil
				result.ConvertLines++
				continue
			}
		}

		// REVIEWFLOW block
		if reReviewflowOpen.MatchString(trimmed) {
			flushTable()
			state = stateReviewflow
			blockBuf = []string{trimmed}
			continue
		}

		// Struct block
		if reStructOpen.MatchString(trimmed) {
			flushTable()
			state = stateStruct
			blockBuf = []string{trimmed}
			continue
		}

		// Figure block
		if reFigureOpen.MatchString(trimmed) {
			flushTable()
			state = stateFigure
			blockBuf = []string{trimmed}
			continue
		}

		// NB block — check for single-line first: NB:: text ::NB
		if m := reNBWarnOpen.FindStringSubmatch(trimmed); m != nil {
			flushTable()
			rest := strings.TrimSpace(m[1])
			if strings.HasSuffix(rest, "::NB") {
				// Single-line NB block
				content := strings.TrimSuffix(rest, "::NB")
				converted := ConvertNBBlock([]string{strings.TrimSpace(content)}, true, currentNS)
				output = append(output, converted...)
				result.ConvertLines++
			} else {
				state = stateNBWarn
				nbFirstLine = rest
				blockBuf = nil
			}
			continue
		}
		if m := reNBOpen.FindStringSubmatch(trimmed); m != nil {
			flushTable()
			rest := strings.TrimSpace(m[1])
			if strings.HasSuffix(rest, "::NB") {
				// Single-line NB block
				content := strings.TrimSuffix(rest, "::NB")
				converted := ConvertNBBlock([]string{strings.TrimSpace(content)}, false, currentNS)
				output = append(output, converted...)
				result.ConvertLines++
			} else {
				state = stateNBNote
				nbFirstLine = rest
				blockBuf = nil
			}
			continue
		}

		// Fold block
		if reFoldOpen.MatchString(trimmed) {
			flushTable()
			state = stateFold
			blockBuf = nil
			result.ConvertLines++
			continue
		}

		// --- WRAP handling ---
		// WRAP blocks need depth tracking since they nest.
		if m := reWrapOpen.FindStringSubmatch(trimmed); m != nil {
			wb := ParseWrapClasses(m[1])
			wrapStack = append(wrapStack, wb)
			wrapDepth++

			classification := ClassifyWrap(wb)
			switch classification {
			case "admonition":
				flushTable()
				// Find the admonition class
				admonClass := "note"
				for _, c := range wb.Classes {
					switch c {
					case "important":
						admonClass = "important"
					case "info":
						admonClass = "note"
					case "tip":
						admonClass = "tip"
					case "warning":
						admonClass = "warning"
					}
				}
				props := fmt.Sprintf("{blockquote class=%s}", admonClass)
				output = append(output, props)
				// Remaining WRAP content will be prefixed with > as we process it
				wrapBuf = append(wrapBuf, "admonition")
			case "column":
				flushTable()
				width := wb.Width
				if width == "" {
					width = "49%"
				}
				// Determine wrap direction (default left)
				wrapDir := "left"
				props := fmt.Sprintf("{blockquote wrap=%s width=%s}", wrapDir, width)
				output = append(output, props)
				wrapBuf = append(wrapBuf, "column")
			default:
				// strip or group: just track depth, don't emit anything
				wrapBuf = append(wrapBuf, "strip")
			}
			result.ConvertLines++
			continue
		}

		if reWrapClose.MatchString(trimmed) && wrapDepth > 0 {
			wrapDepth--
			if len(wrapStack) > 0 {
				wrapStack = wrapStack[:len(wrapStack)-1]
			}
			if len(wrapBuf) > 0 {
				wrapBuf = wrapBuf[:len(wrapBuf)-1]
			}
			result.ConvertLines++
			continue
		}

		// --- Check if we're inside a blockquote-producing WRAP ---
		inBlockquote := false
		for _, wt := range wrapBuf {
			if wt == "admonition" || wt == "column" {
				inBlockquote = true
				break
			}
		}

		// --- Table lines ---
		if IsTableLine(trimmed) {
			tableLines = append(tableLines, line)
			result.ConvertLines++
			continue
		}
		flushTable()

		// --- Normal line conversion ---
		converted := convertNormalLine(line, currentNS, pagesDir)
		if inBlockquote && strings.TrimSpace(converted) != "" {
			converted = "> " + converted
		}
		emitImageLine(converted)
		result.ConvertLines++
	}

	// Flush remaining table
	flushTable()

	// Handle unterminated blocks
	if len(blockBuf) > 0 && state != stateNormal {
		result.Flagged = append(result.Flagged, FlaggedLine{
			Reason:  "unterminated_block",
			Content: fmt.Sprintf("Block state %d with %d lines", state, len(blockBuf)),
		})
		output = append(output, blockBuf...)
	}

	// Post-process: ensure blank lines around block-level directives
	output = ensureBlankLinesAroundDirectives(output)

	result.Markdown = strings.Join(output, "\n")
	return result
}

// isBlockDirective returns true if the line is a block-level directive
// that needs blank lines around it.
func isBlockDirective(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "{include ") ||
		t == "{changes}" ||
		strings.HasPrefix(t, "{reviewflow ")
}

// ensureBlankLinesAroundDirectives adds blank lines before and after
// block-level directives ({include}, {changes}, {reviewflow}).
func ensureBlankLinesAroundDirectives(lines []string) []string {
	var result []string
	prevWasDirective := false

	for _, line := range lines {
		isDir := isBlockDirective(line)
		isBlank := strings.TrimSpace(line) == ""

		// After a directive, ensure blank line before non-blank content
		if prevWasDirective && !isBlank && !isDir {
			result = append(result, "")
		}

		// Before a directive, ensure blank line after non-blank content
		if isDir && len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
			result = append(result, "")
		}

		result = append(result, line)
		prevWasDirective = isDir
	}

	return result
}

// convertNormalLine converts a single non-block line.
func convertNormalLine(line string, currentNS string, pagesDir string) string {
	trimmed := strings.TrimSpace(line)

	// Empty line
	if trimmed == "" {
		return ""
	}

	// Horizontal rule
	if reHRule.MatchString(trimmed) {
		return "---"
	}

	// NOCACHE / NOTOC: drop silently
	if reNocache.MatchString(trimmed) || reNotoc.MatchString(trimmed) {
		return ""
	}

	// PDFNS: drop and flag
	if rePDFNS.MatchString(trimmed) {
		return ""
	}

	// Heading
	if h, ok := ConvertHeading(trimmed); ok {
		return h
	}

	// List
	if l, ok := ConvertListLine(line, currentNS); ok {
		return l
	}

	// Struct variable references: flag but preserve
	if reStructVar.MatchString(trimmed) || reStructBurVar.MatchString(trimmed) {
		// Convert to template var syntax and flag
		line = reStructVar.ReplaceAllString(line, "{{${1}}}")
		line = reStructBurVar.ReplaceAllString(line, "{{${1}}}")
	}

	// Plugin conversions (order matters)
	line = ConvertTag(line)
	line = ConvertInclude(line, currentNS, pagesDir)
	line = ConvertACK(line)
	line = ConvertTodo(line)
	line = ConvertChanges(line)
	line = ConvertTemplateVars(line)

	// Inline conversion
	line = ConvertInline(line, currentNS, "paragraph")

	return line
}

// imageProps holds parsed image marker properties.
type imageProps struct {
	size    string
	align   string
	caption string
	path    string
}

// parseImageMarker parses an IMG{...} marker string.
func parseImageMarker(marker string) imageProps {
	// IMG{size=... align=... caption=... path=...}
	p := imageProps{}
	marker = strings.TrimPrefix(marker, "IMG{")
	marker = strings.TrimSuffix(marker, "}")

	for _, part := range splitImageMarkerParts(marker) {
		if strings.HasPrefix(part, "size=") {
			p.size = part[5:]
		} else if strings.HasPrefix(part, "align=") {
			p.align = part[6:]
		} else if strings.HasPrefix(part, "caption=") {
			p.caption = part[8:]
		} else if strings.HasPrefix(part, "path=") {
			p.path = part[5:]
		}
	}
	return p
}

// splitImageMarkerParts splits key=value pairs, handling values that might contain spaces.
func splitImageMarkerParts(s string) []string {
	var parts []string
	keys := []string{"size=", "align=", "caption=", "path="}
	for len(s) > 0 {
		s = strings.TrimSpace(s)
		// Find which key starts here
		bestIdx := len(s)
		for _, k := range keys {
			if strings.HasPrefix(s, k) {
				// Find the end of this value: next key or end of string
				valStart := len(k)
				valEnd := len(s)
				for _, nextK := range keys {
					idx := strings.Index(s[valStart:], " "+nextK)
					if idx >= 0 && valStart+idx < valEnd {
						valEnd = valStart + idx
					}
				}
				parts = append(parts, s[:valEnd])
				s = s[valEnd:]
				bestIdx = 0
				break
			}
		}
		if bestIdx > 0 {
			break // no key found, stop
		}
	}
	return parts
}

// buildImageLines creates the property line and image line from parsed props.
func buildImageLines(p imageProps) (string, string) {
	imgLine := fmt.Sprintf("![%s](%s)", p.caption, p.path)

	var props []string
	if p.size != "" {
		props = append(props, "size="+ConvertImageSize(p.size))
	}
	if p.align != "" {
		props = append(props, "align="+p.align)
	}

	if len(props) == 0 {
		return "", imgLine
	}

	propLine := "{image " + strings.Join(props, " ") + "}"
	return propLine, imgLine
}
