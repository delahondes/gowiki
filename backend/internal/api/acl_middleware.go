package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// requirePermission returns middleware that checks ACL for the given action.
// It expects the username to already be set in context (via optionalAuth or requireAuth).
// For unauthenticated users, username will be "" and groups will be nil,
// in which case only @all rules will match.
func (s *Server) requirePermission(action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pagePath := extractPagePath(r)

			username := UsernameFromContext(r.Context())

			var groups []string
			if username != "" {
				// Task 1 adds userStore.Get(username) which returns groups.
				// Until Task 1 is merged, we use a compatibility shim.
				groups = s.getUserGroups(username)
			}

			if !s.aclStore.CheckPermission(username, groups, pagePath, action) {
				writeError(w, http.StatusForbidden, "access denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractPagePath extracts the page/resource path from the request URL.
// It handles the chi wildcard parameter used across all page/media endpoints.
func extractPagePath(r *http.Request) string {
	// Try the chi wildcard parameter first (used by /api/pages/*, /api/history/*, etc.)
	if p := chi.URLParam(r, "*"); p != "" {
		return strings.TrimSpace(p)
	}
	// Fallback: extract from URL path after /api/ prefix.
	// This handles endpoints like /api/media, /api/search where there is no wildcard.
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	// Remove the endpoint prefix (pages/, history/, etc.)
	if idx := strings.Index(path, "/"); idx >= 0 {
		path = path[idx+1:]
	}
	return strings.TrimSpace(path)
}

// getUserGroups retrieves the groups for a user from the user store.
func (s *Server) getUserGroups(username string) []string {
	user, err := s.userStore.Get(username)
	if err == nil {
		return user.Groups
	}
	return nil
}
