package importer

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Inline syntax conversion: DokuWiki inline markup -> Gowiki Markdown.
// The approach: protect code spans and links first, then convert remaining inline syntax.

var (
	// Code spans: ''text'' (DokuWiki monospace)
	reMonospace = regexp.MustCompile(`''(.+?)''`)

	// Nowiki inline: <nowiki>text</nowiki>
	reNowikiInline = regexp.MustCompile(`(?i)<nowiki>(.*?)</nowiki>`)

	// Inline code: <code>text</code> (without language specifier)
	reCodeInline = regexp.MustCompile(`(?i)<code>(.+?)</code>`)

	// Italic: //text//
	// Must not match URLs (://), so require non-: before opening //
	reItalic = regexp.MustCompile(`(?:^|[^:])//(.+?)//`)

	// Underline: __text__
	reUnderline = regexp.MustCompile(`__(.+?)__`)

	// Subscript: <sub>text</sub>
	reSubscript = regexp.MustCompile(`(?i)<sub>(.*?)</sub>`)

	// Superscript: <sup>text</sup>
	reSuperscript = regexp.MustCompile(`(?i)<sup>(.*?)</sup>`)

	// Deleted/strikethrough: <del>text</del>
	reDel = regexp.MustCompile(`(?i)<del>(.*?)</del>`)

	// Footnotes: ((text))
	reFootnote = regexp.MustCompile(`\(\((.+?)\)\)`)

	// DokuWiki links: [[target|text]] or [[target]]
	reLink = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

	// DokuWiki images/media: {{target|caption}} or {{target}}
	// Must not match template vars like {{PAGE}}
	reMedia = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

	// Forced line break: \\ at end of line or \\  (with trailing spaces)
	reLineBreak = regexp.MustCompile(`\\\\\s*$`)

	// Line break mid-line: \\  followed by more content (not at end)
	reLineBreakMid = regexp.MustCompile(`\\\\\s+`)

	// Bare URLs not already inside []() or [[]] — matches http(s)://... at word boundary
	reBareURL = regexp.MustCompile(`(?:^|(?:\s))((https?://[^\s<>)\]]+))`)

	// Bare email addresses not inside links
	reBareEmail = regexp.MustCompile(`(?:^|(?:\s))(([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}))`)
)

// placeholder tracking for protect-and-restore
type protector struct {
	index    int
	replaced map[string]string
}

func newProtector() *protector {
	return &protector{replaced: make(map[string]string)}
}

func (p *protector) protect(s string) string {
	key := fmt.Sprintf("\x00PH%d\x00", p.index)
	p.index++
	p.replaced[key] = s
	return key
}

func (p *protector) restore(s string) string {
	for key, val := range p.replaced {
		s = strings.ReplaceAll(s, key, val)
	}
	return s
}

// ConvertInline converts DokuWiki inline markup to Gowiki Markdown.
// context: "paragraph", "list", or "table" — affects line break handling.
func ConvertInline(line string, currentNS string, context string) string {
	prot := newProtector()

	// Step 1: Protect code spans (monospace)
	// Also strip <nowiki>...</nowiki> inside monospace — in DokuWiki,
	// ''<nowiki>text</nowiki>'' is used to prevent parsing inside code spans.
	line = reMonospace.ReplaceAllStringFunc(line, func(m string) string {
		inner := reMonospace.FindStringSubmatch(m)[1]
		inner = reNowikiInline.ReplaceAllString(inner, "$1")
		return prot.protect("`" + inner + "`")
	})

	// Step 2: Protect nowiki inline
	line = reNowikiInline.ReplaceAllStringFunc(line, func(m string) string {
		inner := reNowikiInline.FindStringSubmatch(m)[1]
		return prot.protect("`" + inner + "`")
	})

	// Step 2b: Convert inline <code>text</code> to backtick code spans
	line = reCodeInline.ReplaceAllStringFunc(line, func(m string) string {
		inner := reCodeInline.FindStringSubmatch(m)[1]
		return prot.protect("`" + inner + "`")
	})

	// Step 3: Convert links (protect the result)
	line = reLink.ReplaceAllStringFunc(line, func(m string) string {
		inner := reLink.FindStringSubmatch(m)[1]
		converted := convertLink(inner, currentNS)
		return prot.protect(converted)
	})

	// Step 4: Convert images/media (protect the result)
	// Note: images that need property lines are handled at the block level.
	// Here we only handle inline media references.
	line = reMedia.ReplaceAllStringFunc(line, func(m string) string {
		inner := reMedia.FindStringSubmatch(m)[1]
		// Skip template variables like {{PAGE}}, {{AUTHOR}}, etc.
		if isTemplateVar(inner) {
			return prot.protect("{{" + inner + "}}")
		}
		converted := convertMediaInline(inner, currentNS)
		return prot.protect(converted)
	})

	// Step 5: Convert italic //text// -> *text*
	// Careful: don't match URLs containing ://
	line = convertItalic(line)

	// Step 6: Convert underline __text__ -> _text_
	line = reUnderline.ReplaceAllString(line, `_${1}_`)

	// Step 7: Convert sub/sup/del
	line = reSubscript.ReplaceAllString(line, `~${1}~`)
	line = reSuperscript.ReplaceAllString(line, `^${1}^`)
	line = reDel.ReplaceAllString(line, `~~${1}~~`)

	// Step 8: Convert footnotes ((text)) -> ^[text]
	line = reFootnote.ReplaceAllString(line, `^[${1}]`)

	// Step 8b: Convert custom DokuWiki entities to UTF-8
	line = convertDokuWikiEntities(line)

	// Step 8c: Convert DokuWiki icons to UTF-8
	line = convertDokuWikiIcons(line)

	// Step 8d: Auto-link bare URLs (https://...) -> [](https://...)
	line = convertBareURLs(line, prot)

	// Step 8e: Auto-link bare emails (user@domain) -> [](mailto:user@domain)
	line = convertBareEmails(line, prot)

	// Step 9: Handle line breaks \\
	if context == "table" || context == "list" || context == "blockquote" {
		// In tables, lists, and blockquotes: \\ -> \n literal
		line = reLineBreakMid.ReplaceAllString(line, `\n`)
		line = reLineBreak.ReplaceAllString(line, `\n`)
	} else {
		// In paragraphs: \\ at end of line is a newline (handled by Gowiki's
		// "single newline = hard break" rule, so just remove the \\)
		line = reLineBreakMid.ReplaceAllString(line, "\n")
		line = reLineBreak.ReplaceAllString(line, "")
	}

	// Restore protected spans
	line = prot.restore(line)

	return line
}

// convertDokuWikiEntities replaces custom DokuWiki entity shortcuts with UTF-8.
// Order matters: longer patterns must come before shorter ones to avoid partial matches.
func convertDokuWikiEntities(line string) string {
	replacements := []struct{ old, new string }{
		{"<x>", "\u2612"}, // ☒ checked checkbox — must come before <>
		{"<>", "\u2610"},  // ☐ unchecked checkbox
		{"=>", "\u21D2"},  // ⇒ double arrow right
		{"->", "\u2192"},  // → arrow right
		{"<-", "\u2190"},  // ← arrow left
		{"\\_", "\u00A0"}, // non-breaking space
	}
	for _, r := range replacements {
		line = strings.ReplaceAll(line, r.old, r.new)
	}
	return line
}

// convertDokuWikiIcons replaces DokuWiki icon syntax with UTF-8 equivalents.
func convertDokuWikiIcons(line string) string {
	replacements := []struct{ old, new string }{
		{":!:", "\u26A0\uFE0F"},  // ⚠️
		{":?:", "\u2139\uFE0F"},  // ℹ️
		{"FIXME", "\u26A0\uFE0F FIXME"},
		{"DELETEME", "\u274C DELETEME"},
		{"TODO", "\u2611\uFE0F TODO"}, // Not used by DokuWiki natively but common in content
	}
	for _, r := range replacements {
		line = strings.ReplaceAll(line, r.old, r.new)
	}
	return line
}

// convertBareURLs wraps bare http(s) URLs in auto-link syntax [](url).
// Protected spans (already inside links/media) are skipped by the protector.
func convertBareURLs(line string, prot *protector) string {
	return reBareURL.ReplaceAllStringFunc(line, func(m string) string {
		sub := reBareURL.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		url := sub[1]
		// Replace just the URL portion, preserve any leading whitespace
		converted := prot.protect("[]("+url+")")
		return strings.Replace(m, url, converted, 1)
	})
}

// convertBareEmails wraps bare email addresses in mailto auto-link syntax.
func convertBareEmails(line string, prot *protector) string {
	return reBareEmail.ReplaceAllStringFunc(line, func(m string) string {
		sub := reBareEmail.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		email := sub[1]
		converted := prot.protect("[](mailto:" + email + ")")
		return strings.Replace(m, email, converted, 1)
	})
}

// convertItalic converts //text// to *text* while avoiding URLs.
func convertItalic(line string) string {
	// Find all // positions
	result := strings.Builder{}
	i := 0
	for i < len(line) {
		if i+1 < len(line) && line[i] == '/' && line[i+1] == '/' {
			// Check if this is part of a URL (preceded by :)
			if i > 0 && line[i-1] == ':' {
				result.WriteString("//")
				i += 2
				continue
			}
			// Find the closing //
			end := strings.Index(line[i+2:], "//")
			if end < 0 {
				// No closing //, leave as-is
				result.WriteString("//")
				i += 2
				continue
			}
			// Check that closing // is not preceded by : (URL)
			closePos := i + 2 + end
			if closePos > 0 && line[closePos-1] == ':' {
				result.WriteString("//")
				i += 2
				continue
			}
			inner := line[i+2 : closePos]
			result.WriteByte('*')
			result.WriteString(inner)
			result.WriteByte('*')
			i = closePos + 2
			continue
		}
		result.WriteByte(line[i])
		i++
	}
	return result.String()
}

// convertLink converts the content of a [[ ]] DokuWiki link to Gowiki Markdown.
func convertLink(inner string, currentNS string) string {
	// Split on | for text
	parts := strings.SplitN(inner, "|", 2)
	target := strings.TrimSpace(parts[0])
	text := ""
	if len(parts) > 1 {
		text = strings.TrimSpace(parts[1])
	}

	// Interwiki links: wp>, doku>, etc.
	if idx := strings.Index(target, ">"); idx > 0 && !strings.Contains(target[:idx], "/") && !strings.Contains(target[:idx], ":") {
		prefix := target[:idx]
		term := target[idx+1:]
		if text == "" {
			text = term
		}
		// Flag as interwiki, return as plain text
		_ = prefix // could flag
		return text
	}

	// External links: http://, https://, ftp://, mailto:
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "ftp://") || strings.HasPrefix(target, "mailto:") {
		if text == "" {
			text = target
		}
		return fmt.Sprintf("[%s](%s)", text, target)
	}

	// Email addresses: DokuWiki auto-detects [[user@domain]] as mailto links
	if strings.Contains(target, "@") && !strings.Contains(target, "/") && !strings.Contains(target, ":") {
		mailto := "mailto:" + target
		if text == "" {
			return fmt.Sprintf("[](%s)", mailto)
		}
		return fmt.Sprintf("[%s](%s)", text, mailto)
	}

	// Handle this> links
	if strings.HasPrefix(target, "this>") {
		target = strings.TrimPrefix(target, "this>")
	}

	// Internal link: convert path
	// Split off anchor
	anchor := ""
	if idx := strings.Index(target, "#"); idx >= 0 {
		anchor = target[idx:]
		target = target[:idx]
	}

	gowikiPath := DokuWikiLinkToPath(target, currentNS)
	if text == "" {
		// Use last path segment as display text (like DokuWiki does)
		base := path.Base(gowikiPath)
		if base == "" || base == "." || base == "/" {
			text = gowikiPath
		} else {
			text = base
		}
	}

	return fmt.Sprintf("[%s](%s%s)", text, gowikiPath, anchor)
}

// convertMediaInline handles inline media references.
// For images, returns ![caption](/path). For non-images, returns [text](/path).
func convertMediaInline(inner string, currentNS string) string {
	// Split on | for caption/alt text
	parts := strings.SplitN(inner, "|", 2)
	mediaSpec := strings.TrimSpace(parts[0])
	caption := ""
	if len(parts) > 1 {
		caption = strings.TrimSpace(parts[1])
	}

	// Parse size: ?200 or ?200x100
	size := ""
	if idx := strings.Index(mediaSpec, "?"); idx >= 0 {
		size = mediaSpec[idx+1:]
		mediaSpec = mediaSpec[:idx]
	}

	// Detect alignment from spaces
	// {{ img}} = left, {{img }} = right, {{ img }} = center
	align := ""
	rawFirst := parts[0]
	hasLeadingSpace := len(rawFirst) > 0 && rawFirst[0] == ' '
	hasTrailingSpace := len(rawFirst) > 0 && rawFirst[len(rawFirst)-1] == ' '
	if hasLeadingSpace && hasTrailingSpace {
		align = "center"
	} else if hasLeadingSpace {
		align = "left"
	} else if hasTrailingSpace {
		align = "right"
	}

	// Convert media path
	mediaPath := DokuWikiMediaToPath(mediaSpec, currentNS)

	// Determine if this is an image or a file download
	if isImageExtension(mediaPath) {
		// For inline images without size/alignment, return simple ![caption](/path)
		// Images with size or alignment need property lines — but those are handled
		// at the block level in convert.go. Here we return the simple form.
		if size == "" && align == "" {
			return fmt.Sprintf("![%s](%s)", caption, mediaPath)
		}
		// Return a marker that convert.go will expand to property line + image
		return fmt.Sprintf("\x01IMG{size=%s align=%s caption=%s path=%s}\x01", size, align, caption, mediaPath)
	}

	// Non-image: file download link
	if caption == "" {
		caption = mediaPath[strings.LastIndex(mediaPath, "/")+1:]
	}
	return fmt.Sprintf("[%s](%s)", caption, mediaPath)
}

// isImageExtension returns true for common image file extensions.
func isImageExtension(p string) bool {
	p = strings.ToLower(p)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp"} {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

// isTemplateVar returns true if the content looks like a template variable.
func isTemplateVar(s string) bool {
	s = strings.TrimSpace(s)
	// Template vars are ALL_CAPS or field_name (no spaces, no slashes, no dots)
	if strings.ContainsAny(s, " /:.?") {
		return false
	}
	return true
}
