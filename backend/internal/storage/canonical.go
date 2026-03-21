package storage

import "strings"

// CanonicalPath converts an internal storage path to a canonical page path.
//
// Storage paths are used by the file store (e.g. "index", "docs/guide", "docs/index").
// Canonical paths are used everywhere else (URLs, API, WebSocket, links):
//
//	"index"       → "/"
//	"docs/guide"  → "/docs/guide"
//	"docs/index"  → "/docs/"
//	"a/b/index"   → "/a/b/"
//	"a/b/page"    → "/a/b/page"
//
// The word "index" never appears in a canonical path.
func CanonicalPath(storagePath string) string {
	// Strip leading slash if present (some callers may pass "/path").
	storagePath = strings.TrimPrefix(storagePath, "/")

	if storagePath == "" || storagePath == "index" {
		return "/"
	}

	if strings.HasSuffix(storagePath, "/index") {
		return "/" + strings.TrimSuffix(storagePath, "index")
	}

	return "/" + storagePath
}
