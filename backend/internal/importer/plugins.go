package importer

import (
	"fmt"
	"regexp"
	"strings"
)

// Plugin syntax converters for DokuWiki -> Gowiki.

var (
	// Include: {{page>ns:page}} or {{page>ns:page#section&opts}}
	reInclude = regexp.MustCompile(`\{\{page>([^}]+)\}\}`)

	// Tag: {{tag>label1 label2}}
	reTag = regexp.MustCompile(`\{\{tag>([^}]+)\}\}`)

	// NOCACHE / NOTOC
	reNocache = regexp.MustCompile(`(?i)~~NOCACHE~~`)
	reNotoc   = regexp.MustCompile(`(?i)~~NOTOC~~`)

	// PDFNS: ~~PDFNS>namespace|text~~
	rePDFNS = regexp.MustCompile(`~~PDFNS>[^~]+~~`)

	// Changes: {{changes>...}}
	reChanges = regexp.MustCompile(`\{\{changes>[^}]*\}\}`)

	// Template variables: @!NAME!@
	reTemplateVar = regexp.MustCompile(`@!([A-Z_]+)!@`)

	// ACK: ~~ACK:groups~~ or ~~ACKNOWLEDGE~~
	reACK       = regexp.MustCompile(`~~ACK:([^~]+)~~`)
	reACKSimple = regexp.MustCompile(`~~ACKNOWLEDGE~~`)

	// TODO: <todo @user #due:YYYY-MM-DD>text</todo>
	reTodo = regexp.MustCompile(`(?i)<todo\s+([^>]*)>(.*?)</todo>`)

	// REVIEWFLOW: ~~REVIEWFLOW|...\n~~  (multi-line, handled at block level)
	reReviewflowOpen = regexp.MustCompile(`~~#?REVIEWFLOW\|`)

	// Horizontal rule: ---- (4+ dashes on own line)
	reHRule = regexp.MustCompile(`^-{4,}\s*$`)

	// NB block: NB:: ... ::NB or NB!:: ... ::NB
	reNBOpen     = regexp.MustCompile(`^NB::(.*)$`)
	reNBWarnOpen = regexp.MustCompile(`^NB!::(.*)$`)
	reNBClose    = regexp.MustCompile(`^::NB\s*$`)

	// Figure block: <figure>...</figure>
	reFigureOpen  = regexp.MustCompile(`(?i)^\s*<figure>\s*$`)
	reFigureClose = regexp.MustCompile(`(?i)^\s*</figure>\s*$`)
	reCaption     = regexp.MustCompile(`(?i)<caption>(.*?)</caption>`)

	// Struct blocks
	reStructOpen   = regexp.MustCompile(`^---- struct (table|lookup) ----`)
	reStructClose  = regexp.MustCompile(`^----\s*$`)
	reStructVar    = regexp.MustCompile(`\{\{\$([a-zA-Z0-9_.]+)\}\}`)
	reStructBurVar = regexp.MustCompile(`@@([a-zA-Z0-9_.]+)@@`)

	// Slider: <slider image.png>
	reSlider = regexp.MustCompile(`(?i)^<slider\s+([^>]+)>`)

	// Code block: <code lang>...</code>
	reCodeOpen  = regexp.MustCompile(`(?i)^<code\s*([a-z0-9_]*)>`)
	reCodeClose = regexp.MustCompile(`(?i)^</code>\s*$`)

	// File block: <file lang filename>...</file>
	reFileOpen  = regexp.MustCompile(`(?i)^<file\s*([^>]*)>`)
	reFileClose = regexp.MustCompile(`(?i)^</file>\s*$`)

	// Nowiki block: <nowiki>...</nowiki> (block-level)
	reNowikiBlockOpen  = regexp.MustCompile(`(?i)^<nowiki>\s*$`)
	reNowikiBlockClose = regexp.MustCompile(`(?i)^</nowiki>\s*$`)

	// WRAP blocks
	reWrapOpen  = regexp.MustCompile(`(?i)<WRAP\s+([^>]*)>`)
	reWrapClose = regexp.MustCompile(`(?i)</WRAP>`)

	// Fold block: ++++Title| ... ++++
	reFoldOpen  = regexp.MustCompile(`^\+\+\+\+([^|]*)\|?\s*$`)
	reFoldClose = regexp.MustCompile(`^\+\+\+\+\s*$`)
)

// ConvertInclude converts a DokuWiki include to Gowiki.
func ConvertInclude(line string, currentNS string) string {
	return reInclude.ReplaceAllStringFunc(line, func(m string) string {
		inner := reInclude.FindStringSubmatch(m)[1]

		// Split off options: &noheader, &nofooter, &firstseconly, &noreadmore, &link
		parts := strings.Split(inner, "&")
		target := parts[0]
		hasFirstSecOnly := false
		for _, opt := range parts[1:] {
			if opt == "firstseconly" || opt == "firstseconly&noreadmore" {
				hasFirstSecOnly = true
			}
		}

		// Split off section anchor
		anchor := ""
		if idx := strings.Index(target, "#"); idx >= 0 {
			anchor = target[idx:]
			target = target[:idx]
		}

		// Convert path
		gowikiPath := DokuWikiLinkToPath(target, currentNS)

		if hasFirstSecOnly && anchor == "" {
			// firstseconly -> include first heading section
			// We'd need to resolve the first heading — use a placeholder
			anchor = "#first-heading"
		}

		return fmt.Sprintf("{include path=%s%s}", gowikiPath, anchor)
	})
}

// ConvertTag converts a DokuWiki tag directive to Gowiki.
func ConvertTag(line string) string {
	return reTag.ReplaceAllStringFunc(line, func(m string) string {
		inner := reTag.FindStringSubmatch(m)[1]
		tags := strings.Fields(inner)
		return "{tag " + strings.Join(tags, " ") + "}"
	})
}

// ConvertACK converts a DokuWiki ACK directive to Gowiki todo.
func ConvertACK(line string) string {
	if reACKSimple.MatchString(line) {
		return reACKSimple.ReplaceAllString(line, `{todo title="Acknowledge" action="read" resolution=all}`)
	}
	return reACK.ReplaceAllStringFunc(line, func(m string) string {
		groups := reACK.FindStringSubmatch(m)[1]
		// Strip @ prefix from group names
		parts := strings.Split(groups, ",")
		for i, p := range parts {
			parts[i] = strings.TrimPrefix(strings.TrimSpace(p), "@")
		}
		assign := strings.Join(parts, ",")
		return fmt.Sprintf(`{todo title="Acknowledge" assign="%s" action="read" resolution=all}`, assign)
	})
}

// ConvertTodo converts a DokuWiki todo to Gowiki.
func ConvertTodo(line string) string {
	return reTodo.ReplaceAllStringFunc(line, func(m string) string {
		parts := reTodo.FindStringSubmatch(m)
		attrs := parts[1]
		text := parts[2]

		props := fmt.Sprintf(`{todo title="%s"`, text)

		// Parse @user
		if idx := strings.Index(attrs, "@"); idx >= 0 {
			rest := attrs[idx+1:]
			user := strings.Fields(rest)[0]
			props += fmt.Sprintf(` assign="%s"`, user)
		}

		// Parse #due:YYYY-MM-DD
		if idx := strings.Index(attrs, "#"); idx >= 0 {
			rest := attrs[idx+1:]
			// Format: user:YYYY-MM-DD
			if colonIdx := strings.Index(rest, ":"); colonIdx >= 0 {
				date := strings.Fields(rest[colonIdx+1:])[0]
				props += fmt.Sprintf(` due=%s`, date)
			}
		}

		props += "}"
		return props
	})
}

// ConvertChanges converts a DokuWiki changes widget.
func ConvertChanges(line string) string {
	return reChanges.ReplaceAllString(line, "{changes}")
}

// ConvertTemplateVars converts DokuWiki template variables @!NAME!@ -> {{NAME}}.
func ConvertTemplateVars(line string) string {
	return reTemplateVar.ReplaceAllString(line, "{{${1}}}")
}

// ConvertReviewflow converts a multi-line REVIEWFLOW block to a single-line directive.
func ConvertReviewflow(lines []string) string {
	props := make(map[string]string)
	isDisabled := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Check for disabled marker ~~#REVIEWFLOW|
		if strings.HasPrefix(line, "~~#REVIEWFLOW") {
			isDisabled = true
			continue
		}
		if strings.HasPrefix(line, "~~REVIEWFLOW") || line == "~~" {
			continue
		}
		// Parse key=value
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			if key == "render" {
				continue // Skip render directive
			}
			// Strip @ from usernames
			value = strings.TrimPrefix(value, "@")
			props[key] = value
		}
	}

	if isDisabled {
		// Disabled reviewflow: preserve as code block
		return "```\n" + strings.Join(lines, "\n") + "\n```"
	}

	var parts []string
	// Output in a consistent order
	for _, key := range []string{"version", "author", "reviewer", "validation"} {
		if val, ok := props[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%s", key, val))
		}
	}
	// Add any remaining keys
	for key, val := range props {
		found := false
		for _, k := range []string{"version", "author", "reviewer", "validation"} {
			if key == k {
				found = true
				break
			}
		}
		if !found {
			parts = append(parts, fmt.Sprintf("%s=%s", key, val))
		}
	}

	return "{reviewflow " + strings.Join(parts, " ") + "}"
}

// ConvertFigure converts a <figure> block to Gowiki image with caption.
func ConvertFigure(lines []string, currentNS string) ([]string, []FlaggedLine) {
	var images []string
	caption := ""
	var flagged []FlaggedLine
	inCaption := false

	reCaptionOpen := regexp.MustCompile(`(?i)<caption>\s*(.*)`)
	reCaptionClose := regexp.MustCompile(`(?i)(.*?)\s*</caption>`)

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip figure tags
		if reFigureOpen.MatchString(line) || reFigureClose.MatchString(line) {
			continue
		}

		// Handle multi-line captions
		if inCaption {
			if m := reCaptionClose.FindStringSubmatch(line); m != nil {
				if m[1] != "" {
					if caption != "" {
						caption += " "
					}
					caption += m[1]
				}
				inCaption = false
			} else {
				if caption != "" {
					caption += " "
				}
				caption += line
			}
			continue
		}

		// Single-line caption: <caption>text</caption>
		if m := reCaption.FindStringSubmatch(line); m != nil {
			caption = m[1]
			continue
		}

		// Caption open (multi-line): <caption>text...
		if m := reCaptionOpen.FindStringSubmatch(line); m != nil {
			inCaption = true
			caption = m[1]
			continue
		}

		// Extract images
		if reMedia.MatchString(line) {
			// There might be multiple images on one line
			matches := reMedia.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				inner := match[1]
				if isTemplateVar(inner) {
					continue
				}
				images = append(images, inner)
			}
			continue
		}

		// Other content in figure block — skip empty lines
		if line != "" {
			flagged = append(flagged, FlaggedLine{Reason: "figure_content", Content: line})
		}
	}

	if len(images) == 0 {
		return nil, flagged
	}

	var out []string

	if len(images) == 1 {
		// Single image: use {image} property with caption
		img := images[0]
		props, imgLine := convertFigureImage(img, currentNS)
		if caption != "" {
			props = appendProp(props, fmt.Sprintf(`caption="%s"`, caption))
		}
		if props != "" {
			out = append(out, props)
		}
		out = append(out, imgLine)
	} else {
		// Multiple images: blockquote panel
		out = append(out, fmt.Sprintf(`{blockquote class=custom color=lightgrey width=70%% image-width=%d%%}`, 100/len(images)-1))
		line := "> "
		for i, img := range images {
			_, imgLine := convertFigureImage(img, currentNS)
			if i > 0 {
				line += " "
			}
			line += imgLine
		}
		out = append(out, line)
		if caption != "" {
			out = append(out, "> "+caption)
		}
	}

	return out, flagged
}

// convertFigureImage converts a single DokuWiki image spec to property line + image line.
func convertFigureImage(imgSpec string, currentNS string) (string, string) {
	// Parse: path?size |caption
	parts := strings.SplitN(imgSpec, "|", 2)
	mediaSpec := strings.TrimSpace(parts[0])

	size := ""
	if idx := strings.Index(mediaSpec, "?"); idx >= 0 {
		size = mediaSpec[idx+1:]
		mediaSpec = mediaSpec[:idx]
	}

	mediaPath := DokuWikiMediaToPath(mediaSpec, currentNS)

	props := ""
	if size != "" {
		if !strings.Contains(size, "x") {
			size += "x"
		}
		props = fmt.Sprintf("{image size=%s}", size)
	}

	return props, fmt.Sprintf("![%s](%s)", "", mediaPath)
}

// appendProp appends a property to an existing {image ...} property line.
func appendProp(existing, prop string) string {
	if existing == "" {
		return "{image " + prop + "}"
	}
	// Insert before closing }
	return existing[:len(existing)-1] + " " + prop + "}"
}

// ConvertNBBlock converts an NB:: ... ::NB block to a Gowiki blockquote.
func ConvertNBBlock(lines []string, isWarning bool, currentNS string) []string {
	class := "note"
	if isWarning {
		class = "warning"
	}

	var out []string
	out = append(out, fmt.Sprintf("{blockquote class=%s}", class))

	for _, line := range lines {
		// Convert inline markup
		converted := ConvertInline(line, currentNS, "paragraph")
		out = append(out, "> "+converted)
	}

	return out
}

// ConvertSliderPage converts a full page with <slider> tags to Gowiki slides.
func ConvertSliderPage(lines []string, currentNS string) ([]string, []FlaggedLine) {
	var flagged []FlaggedLine
	var slides [][]string
	var currentSlide []string
	backgrounds := make(map[string]int)
	title := ""

	for _, line := range lines {
		if m := reSlider.FindStringSubmatch(line); m != nil {
			// Save current slide
			if currentSlide != nil {
				slides = append(slides, currentSlide)
			}
			currentSlide = []string{}
			bg := strings.TrimSpace(m[1])
			backgrounds[bg]++
			continue
		}
		if currentSlide != nil {
			currentSlide = append(currentSlide, line)
		} else {
			// Content before first slider tag (tag line, etc.)
			// Check for tag directive
			trimmed := strings.TrimSpace(line)
			if reTag.MatchString(trimmed) {
				currentSlide = []string{ConvertTag(trimmed)}
			} else if trimmed != "" {
				currentSlide = []string{trimmed}
			}
		}
	}
	if currentSlide != nil {
		slides = append(slides, currentSlide)
	}

	// Find most common background
	mostCommonBG := ""
	maxCount := 0
	for bg, count := range backgrounds {
		if count > maxCount {
			mostCommonBG = bg
			maxCount = count
		}
	}

	// Extract title from first slide heading
	for _, slide := range slides {
		for _, line := range slide {
			if h, ok := ConvertHeading(line); ok {
				title = strings.TrimLeft(h, "# ")
				break
			}
		}
		if title != "" {
			break
		}
	}

	// Build output
	var out []string
	header := "{slides"
	if title != "" {
		header += fmt.Sprintf(` title="%s"`, title)
	}
	if mostCommonBG != "" {
		bgPath := DokuWikiMediaToPath(mostCommonBG, currentNS)
		header += fmt.Sprintf(` background=%s`, bgPath)
	}
	header += "}"
	out = append(out, header)
	out = append(out, "")

	for i, slide := range slides {
		if i > 0 {
			out = append(out, "")
			out = append(out, "---")
			out = append(out, "")
		}
		var slideTableLines []string
		flushSlideTable := func() {
			if len(slideTableLines) == 0 {
				return
			}
			converted, _ := ConvertTable(slideTableLines, currentNS)
			out = append(out, converted...)
			slideTableLines = nil
		}
		for _, line := range slide {
			// Strip WRAP tags
			line = reWrapOpen.ReplaceAllString(line, "")
			line = reWrapClose.ReplaceAllString(line, "")
			trimmed := strings.TrimSpace(line)
			// Accumulate table lines
			if IsTableLine(trimmed) {
				slideTableLines = append(slideTableLines, line)
				continue
			}
			flushSlideTable()
			// Apply normal line conversion (headings, lists, inline markup, etc.)
			line = convertNormalLine(line, currentNS)
			trimmed = strings.TrimSpace(line)
			if trimmed == "" && len(out) > 0 && out[len(out)-1] == "" {
				continue // Collapse multiple empty lines
			}
			out = append(out, line)
		}
		flushSlideTable()
	}

	return out, flagged
}

// WrapBlock represents a parsed WRAP block.
type WrapBlock struct {
	Classes []string
	Width   string
	Content []string
	IsGroup bool
}

// ParseWrapClasses extracts semantic classes from a WRAP opening tag.
func ParseWrapClasses(attrs string) WrapBlock {
	wb := WrapBlock{}
	parts := strings.Fields(strings.ToLower(attrs))
	for _, p := range parts {
		if strings.HasSuffix(p, "%") {
			wb.Width = p
		} else if p == "group" {
			wb.IsGroup = true
		} else {
			wb.Classes = append(wb.Classes, p)
		}
	}
	return wb
}

// ClassifyWrap determines the conversion strategy for a WRAP block.
func ClassifyWrap(wb WrapBlock) string {
	for _, c := range wb.Classes {
		switch c {
		case "important", "info", "tip", "warning":
			return "admonition"
		case "half", "third", "column":
			return "column"
		}
	}
	if wb.IsGroup {
		return "group"
	}
	// Check for known styling-only classes
	for _, c := range wb.Classes {
		switch c {
		case "center", "round", "white", "prewrap", "smalltext",
			"flexcenter", "halfhalfbigtext", "halfhalfsmalltext", "slide":
			return "strip"
		}
	}
	if len(wb.Classes) == 0 && wb.Width == "" {
		return "strip"
	}
	return "strip" // default: strip wrapper, keep content
}
