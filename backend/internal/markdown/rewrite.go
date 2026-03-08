package markdown

import (
	"path"
	"regexp"
	"strings"
)

// includePathRe matches {include path=VALUE} and captures the full directive,
// the prefix up to the path value, the path value itself, and the suffix.
// Used for replacement (not just extraction like includeRe).
var includePathRe = regexp.MustCompile(`(\{include\s+path=)(?:"([^"]+)"|'([^']+)'|(\S+?))(\s*\})`)

// RewritePageRef rewrites all links and includes in content that point to oldPagePath
// so they point to newPagePath instead. contextPagePath is the page containing the content
// (used to resolve relative references).
func RewritePageRef(content, oldPagePath, newPagePath, contextPagePath string) string {
	lines := strings.Split(content, "\n")
	var changed bool

	inCodeBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		newLine := rewritePageRefsInLine(line, oldPagePath, newPagePath, contextPagePath)

		// Also handle include directives.
		newLine = rewriteIncludeRef(newLine, oldPagePath, newPagePath, contextPagePath)

		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}

	if !changed {
		return content
	}
	return strings.Join(lines, "\n")
}

// rewritePageRefsInLine rewrites [text](url) and ![alt](url) page references in a single line.
func rewritePageRefsInLine(line, oldPagePath, newPagePath, contextPagePath string) string {
	return mediaRefFullRe.ReplaceAllStringFunc(line, func(match string) string {
		groups := mediaRefFullRe.FindStringSubmatch(match)
		if len(groups) < 4 {
			return match
		}
		prefix := groups[1]
		url := groups[2]
		suffix := groups[3]

		// Skip external URLs.
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			return match
		}

		// Strip query/fragment for resolution.
		urlPath := url
		var fragment, query string
		if idx := strings.Index(urlPath, "#"); idx >= 0 {
			fragment = urlPath[idx:]
			urlPath = urlPath[:idx]
		}
		if idx := strings.Index(urlPath, "?"); idx >= 0 {
			query = urlPath[idx:]
			urlPath = urlPath[:idx]
		}

		// Page links are extension-less or .md.
		ext := path.Ext(urlPath)
		if ext != "" && !strings.EqualFold(ext, ".md") {
			return match // media ref, not page ref
		}

		// Strip .md for resolution.
		resolvable := urlPath
		hadMD := false
		if strings.EqualFold(ext, ".md") {
			resolvable = strings.TrimSuffix(urlPath, ext)
			hadMD = true
		}

		resolved := ResolvePath(contextPagePath, resolvable)
		if resolved == "" || resolved != oldPagePath {
			return match
		}

		// Replace with new path (absolute).
		newURL := newPagePath
		if hadMD {
			newURL += ".md"
		}
		newURL += query + fragment
		return prefix + newURL + suffix
	})
}

// rewriteIncludeRef rewrites {include path=...} directives.
func rewriteIncludeRef(line, oldPagePath, newPagePath, contextPagePath string) string {
	return includePathRe.ReplaceAllStringFunc(line, func(match string) string {
		groups := includePathRe.FindStringSubmatch(match)
		if len(groups) < 6 {
			return match
		}
		prefix := groups[1] // "{include path="
		suffix := groups[5] // "}"

		// Extract path value from whichever group matched.
		var val string
		var quote string
		if groups[2] != "" {
			val = groups[2]
			quote = `"`
		} else if groups[3] != "" {
			val = groups[3]
			quote = `'`
		} else {
			val = groups[4]
		}

		resolved := ResolvePath(contextPagePath, val)
		if resolved == "" || resolved != oldPagePath {
			return match
		}

		if quote != "" {
			return prefix + quote + newPagePath + quote + suffix
		}
		return prefix + newPagePath + suffix
	})
}

// RewriteMediaRef rewrites all media references (images/links with file extensions)
// in content that point to oldMediaPath so they point to newMediaPath instead.
func RewriteMediaRef(content, oldMediaPath, newMediaPath, contextPagePath string) string {
	lines := strings.Split(content, "\n")
	var changed bool

	inCodeBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		newLine := mediaRefFullRe.ReplaceAllStringFunc(line, func(match string) string {
			groups := mediaRefFullRe.FindStringSubmatch(match)
			if len(groups) < 4 {
				return match
			}
			prefix := groups[1]
			url := groups[2]
			suffix := groups[3]

			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				return match
			}

			// Strip query for resolution but preserve it.
			urlPath := url
			var query string
			if idx := strings.Index(urlPath, "?"); idx >= 0 {
				query = urlPath[idx:]
				urlPath = urlPath[:idx]
			}

			// Must have a file extension and not be .md.
			ext := path.Ext(urlPath)
			if ext == "" || strings.EqualFold(ext, ".md") {
				return match
			}

			resolved := ResolvePath(contextPagePath, urlPath)
			if resolved == "" || resolved != oldMediaPath {
				return match
			}

			return prefix + newMediaPath + query + suffix
		})

		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}

	if !changed {
		return content
	}
	return strings.Join(lines, "\n")
}

// RebaseRelativeRefs converts relative references in content from resolving
// against oldResolvePath to resolving against newResolvePath. Relative refs
// are replaced with their absolute resolved form so they remain correct
// after the page moves.
func RebaseRelativeRefs(content, oldResolvePath, newResolvePath string) string {
	_ = newResolvePath // We emit absolute paths, so the new resolve path isn't needed.

	lines := strings.Split(content, "\n")
	var changed bool

	inCodeBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if inCodeBlock {
			continue
		}

		newLine := rebaseLineRefs(line, oldResolvePath)
		newLine = rebaseIncludeRefs(newLine, oldResolvePath)

		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}

	if !changed {
		return content
	}
	return strings.Join(lines, "\n")
}

// rebaseLineRefs converts relative URLs in [text](url) and ![alt](url) to absolute.
func rebaseLineRefs(line, oldResolvePath string) string {
	return mediaRefFullRe.ReplaceAllStringFunc(line, func(match string) string {
		groups := mediaRefFullRe.FindStringSubmatch(match)
		if len(groups) < 4 {
			return match
		}
		prefix := groups[1]
		url := groups[2]
		suffix := groups[3]

		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			return match
		}
		// Only rebase relative refs (not already absolute).
		if strings.HasPrefix(url, "/") {
			return match
		}

		// Strip query/fragment for resolution.
		urlPath := url
		var extra string
		if idx := strings.Index(urlPath, "?"); idx >= 0 {
			extra = urlPath[idx:]
			urlPath = urlPath[:idx]
		}
		if idx := strings.Index(urlPath, "#"); idx >= 0 {
			if extra == "" {
				extra = urlPath[idx:]
			} else {
				extra = urlPath[idx:] + extra
			}
			urlPath = urlPath[:idx]
		}

		resolved := ResolvePath(oldResolvePath, urlPath)
		if resolved == "" {
			return match
		}

		return prefix + resolved + extra + suffix
	})
}

// rebaseIncludeRefs converts relative paths in {include path=...} to absolute.
func rebaseIncludeRefs(line, oldResolvePath string) string {
	return includePathRe.ReplaceAllStringFunc(line, func(match string) string {
		groups := includePathRe.FindStringSubmatch(match)
		if len(groups) < 6 {
			return match
		}
		prefix := groups[1]
		suffix := groups[5]

		var val string
		var quote string
		if groups[2] != "" {
			val = groups[2]
			quote = `"`
		} else if groups[3] != "" {
			val = groups[3]
			quote = `'`
		} else {
			val = groups[4]
		}

		// Only rebase relative refs.
		if strings.HasPrefix(val, "/") {
			return match
		}

		resolved := ResolvePath(oldResolvePath, val)
		if resolved == "" {
			return match
		}

		if quote != "" {
			return prefix + quote + resolved + quote + suffix
		}
		return prefix + resolved + suffix
	})
}
