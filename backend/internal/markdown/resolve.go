package markdown

import (
	"path"
	"strings"
)

// ResolvePath resolves a reference path relative to a page's namespace.
// pagePath is the page's logical path (e.g., "docs/intro").
// refPath is the reference target (e.g., "./diagram.png", "../logo.png", "/abs/path.png").
// Returns normalized absolute path within content/ (no leading slash).
//
// Rules:
//   - "/foo/bar.png" -> "foo/bar.png" (absolute, strip leading /)
//   - "./foo.png" relative to page namespace -> "docs/foo.png" (if page is "docs/intro")
//   - "../foo.png" -> "foo.png" (if page is "docs/intro")
//   - "foo.png" (no prefix) -> treated as relative to namespace
func ResolvePath(pagePath, refPath string) string {
	refPath = strings.TrimSpace(refPath)
	if refPath == "" {
		return ""
	}

	// Absolute path: strip leading slash and clean.
	if strings.HasPrefix(refPath, "/") {
		cleaned := path.Clean(refPath)
		cleaned = strings.TrimPrefix(cleaned, "/")
		if cleaned == "." || cleaned == "" {
			return ""
		}
		return cleaned
	}

	// Relative path: resolve against page's namespace (directory of the page).
	namespace := path.Dir(pagePath)
	if namespace == "." {
		namespace = ""
	}

	var joined string
	if namespace == "" {
		joined = refPath
	} else {
		joined = namespace + "/" + refPath
	}

	cleaned := path.Clean(joined)
	if cleaned == "." || cleaned == "" {
		return ""
	}
	// Prevent escaping the content root via excessive "../".
	if strings.HasPrefix(cleaned, "..") {
		return ""
	}
	return cleaned
}
