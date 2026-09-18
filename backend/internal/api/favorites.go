package api

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"gowiki/backend/internal/markdown"
)

// handleGetMeFavorites returns the caller's favorites list, most-recently-
// added first. Empty list when the user has never starred anything.
//
// GET /api/auth/me/favorites
func (s *Server) handleGetMeFavorites(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())
	if username == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if s.userStore == nil {
		writeError(w, http.StatusServiceUnavailable, "user store unavailable")
		return
	}
	favs, err := s.userStore.GetFavorites(username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	// Enrich with page titles when the caller wants them (default on — the
	// UI needs them, and the extra map lookup is cheap). Skip when the page
	// no longer exists so a stale favorite still round-trips its path.
	items := make([]map[string]any, 0, len(favs))
	for _, f := range favs {
		item := map[string]any{
			"path":     f.Path,
			"added_at": f.AddedAt,
		}
		if page, err := s.store.Get(strings.TrimPrefix(f.Path, "/")); err == nil {
			if title := markdown.ExtractTitle(page.Markdown); title != "" {
				item["title"] = title
			}
			item["exists"] = true
		} else {
			item["exists"] = false
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorites": items})
}

// handleToggleMeFavorite adds/removes the caller's favorite for the given
// page path, returning the resulting state.
//
// POST /api/auth/me/favorites/toggle
// Body: { "path": "/regulatory/suivi/livrables" }
// Response: { "favorited": true|false, "path": "..." }
func (s *Server) handleToggleMeFavorite(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())
	if username == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if s.userStore == nil {
		writeError(w, http.StatusServiceUnavailable, "user store unavailable")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	p := normalizeFavoritePath(req.Path)
	if p == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	favorited, err := s.userStore.ToggleFavorite(username, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"favorited": favorited,
		"path":      p,
	})
}

// normalizeFavoritePath canonicalizes a favorite target: leading slash,
// trailing slash preserved for namespace indexes, "/index" segments stripped.
// Refuses empty paths and anything obviously not a page reference.
func normalizeFavoritePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	// Preserve trailing slash — it distinguishes /foo/ (namespace index) from
	// /foo (leaf page).
	hasTrailing := strings.HasSuffix(trimmed, "/") && trimmed != "/"
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "" {
		return ""
	}
	// path.Clean strips /index and trailing slashes we may want to keep.
	if strings.HasSuffix(cleaned, "/index") {
		cleaned = strings.TrimSuffix(cleaned, "index")
	} else if hasTrailing {
		cleaned += "/"
	}
	return cleaned
}
