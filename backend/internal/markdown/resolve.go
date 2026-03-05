package markdown

import (
	"net/url"
	"path"
	"strings"
)

// ResolvePath resolves a reference path relative to a page's namespace.
// pagePath is the page's logical path (e.g., "/docs/intro").
// refPath is the reference target (e.g., "./diagram.png", "../logo.png", "/abs/path.png").
// Returns normalized absolute path within content/ with leading slash (e.g., "/docs/diagram.png").
//
// Percent-encoded characters in refPath (e.g. %20 for spaces) are decoded before resolution,
// so that markdown hrefs like "./my%20file.pdf" resolve to the file "my file.pdf" on disk.
//
// Rules:
//   - "/foo/bar.png" -> "/foo/bar.png" (absolute)
//   - "./foo.png" relative to page namespace -> "/docs/foo.png" (if page is "/docs/intro")
//   - "../foo.png" -> "/foo.png" (if page is "/docs/intro")
//   - "foo.png" (no prefix) -> treated as relative to namespace
func ResolvePath(pagePath, refPath string) string {
	refPath = strings.TrimSpace(refPath)
	if refPath == "" {
		return ""
	}
	// Decode percent-encoded characters (e.g. %20 → space).
	if decoded, err := url.PathUnescape(refPath); err == nil {
		refPath = decoded
	}

	// Absolute path: clean and return with leading slash.
	if strings.HasPrefix(refPath, "/") {
		cleaned := path.Clean(refPath)
		if cleaned == "." || cleaned == "/" {
			return ""
		}
		return cleaned
	}

	// Relative path: resolve against page's namespace (directory of the page).
	namespace := path.Dir(pagePath)
	if namespace == "." || namespace == "" {
		namespace = "/"
	}

	joined := namespace + "/" + refPath

	cleaned := path.Clean(joined)
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	if !strings.HasPrefix(cleaned, "/") {
		return ""
	}
	// Prevent escaping above root: count ".." segments in refPath and compare
	// against namespace depth.
	nsDepth := strings.Count(strings.Trim(namespace, "/"), "/")
	if strings.Trim(namespace, "/") != "" {
		nsDepth++
	}
	upCount := 0
	for _, seg := range strings.Split(refPath, "/") {
		if seg == ".." {
			upCount++
		} else if seg != "." && seg != "" {
			break
		}
	}
	if upCount > nsDepth {
		return ""
	}
	return cleaned
}
