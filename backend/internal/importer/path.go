package importer

import (
	"path"
	"strings"
)

// PageSourceToTarget converts a DokuWiki page path (relative to pages/)
// to a Gowiki content path (relative to content/).
// e.g. "ns/page.txt" -> "ns/page.md", "ns/start.txt" -> "ns/index.md"
func PageSourceToTarget(srcPath string) string {
	// Remove .txt extension, add .md
	p := strings.TrimSuffix(srcPath, ".txt")

	// Rename start -> index
	dir, base := path.Split(p)
	if base == "start" {
		base = "index"
	}
	p = path.Join(dir, base) + ".md"

	return p
}

// TemplateSourceToTarget converts a DokuWiki template path.
// "templates/study.txt" -> "study/_template.md"
func TemplateSourceToTarget(srcPath string) string {
	// templates/X.txt -> X/_template.md
	p := strings.TrimSuffix(srcPath, ".txt")
	p = strings.TrimPrefix(p, "templates/")
	return p + "/_template.md"
}

// IsTemplatePath returns true if the source path is under templates/.
func IsTemplatePath(srcPath string) bool {
	return strings.HasPrefix(srcPath, "templates/")
}

// DokuWikiLinkToPath converts a DokuWiki internal link target to a Gowiki path.
// currentNS is the namespace of the page containing the link (e.g. "regulatory/smq/ps01").
func DokuWikiLinkToPath(link string, currentNS string) string {
	// Strip any query parameters (e.g. ?rev=xxx)
	if idx := strings.Index(link, "?"); idx >= 0 {
		link = link[:idx]
	}

	// Replace : with /
	link = strings.ReplaceAll(link, ":", "/")

	// Normalize .foo/... to ./foo/... — DokuWiki treats .page and .:page identically
	// as relative to the current namespace. After colon-to-slash conversion, .:page
	// becomes ./page (handled by path.Join), but .page stays as .page which
	// path.Join treats as a literal ".page" directory name.
	if len(link) > 1 && link[0] == '.' && link[1] != '/' && link[1] != '.' {
		link = "./" + link[1:]
	}

	// Handle absolute links (leading /)
	if strings.HasPrefix(link, "/") {
		return normalizeLinkPath(link)
	}

	// Handle relative links (no leading /)
	// Handles both ./page (current ns) and ../page (parent ns)
	if strings.HasPrefix(link, ".") {
		resolved := path.Join("/"+currentNS, link)
		return normalizeLinkPath(resolved)
	}

	// No leading / or . ->
	// DokuWiki resolution: if the link contains / (was :), it's absolute from root.
	// Only single-segment links (no /) are relative to current namespace.
	if strings.Contains(link, "/") {
		return normalizeLinkPath("/" + link)
	}
	if currentNS != "" && currentNS != "." {
		resolved := "/" + currentNS + "/" + link
		return normalizeLinkPath(resolved)
	}
	return normalizeLinkPath("/" + link)
}

// DokuWikiMediaToPath converts a DokuWiki media reference to a Gowiki path.
func DokuWikiMediaToPath(mediaRef string, currentNS string) string {
	mediaRef = strings.TrimSpace(mediaRef)

	// Replace : with /
	mediaRef = strings.ReplaceAll(mediaRef, ":", "/")

	// Normalize .foo to ./foo (same fix as DokuWikiLinkToPath)
	if len(mediaRef) > 1 && mediaRef[0] == '.' && mediaRef[1] != '/' && mediaRef[1] != '.' {
		mediaRef = "./" + mediaRef[1:]
	}

	// Handle absolute (leading /)
	if strings.HasPrefix(mediaRef, "/") {
		return path.Clean(mediaRef)
	}

	// Handle relative with .
	if strings.HasPrefix(mediaRef, ".") {
		if strings.HasPrefix(mediaRef, "./") {
			return path.Clean("/" + currentNS + "/" + mediaRef[2:])
		}
		return path.Clean("/" + currentNS + "/" + mediaRef[1:])
	}

	// Same DokuWiki rule: if it contains / (was :), it's absolute from root.
	// Only single-segment refs are relative.
	if strings.Contains(mediaRef, "/") {
		return path.Clean("/" + mediaRef)
	}
	if currentNS != "" && currentNS != "." {
		return path.Clean("/" + currentNS + "/" + mediaRef)
	}
	return path.Clean("/" + mediaRef)
}

// normalizeLinkPath cleans a link path and renames start -> index,
// then simplifies /path/index to /path (they resolve to the same content).
func normalizeLinkPath(p string) string {
	p = path.Clean(p)

	// Rename trailing /start to /index
	if strings.HasSuffix(p, "/start") {
		p = p[:len(p)-len("start")] + "index"
	}
	if p == "/start" {
		p = "/index"
	}

	// Simplify /path/index to /path (they resolve to the same content)
	if strings.HasSuffix(p, "/index") {
		simplified := strings.TrimSuffix(p, "/index")
		if simplified == "" {
			simplified = "/"
		}
		p = simplified
	}

	return p
}

// NamespaceOf returns the namespace (directory) portion of a page path.
// e.g. "regulatory/smq/ps01/page" -> "regulatory/smq/ps01"
func NamespaceOf(pagePath string) string {
	pagePath = strings.TrimPrefix(pagePath, "/")
	pagePath = strings.TrimSuffix(pagePath, ".md")
	dir := path.Dir(pagePath)
	if dir == "." {
		return ""
	}
	return dir
}
