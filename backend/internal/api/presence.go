package api

import (
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"gowiki/backend/internal/collab"
)

func (s *Server) handlePresenceWS(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())
	if username == "" {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	// Resolve display name.
	displayName := username
	if s.userStore != nil {
		if u, err := s.userStore.Get(username); err == nil && u.DisplayName != "" {
			displayName = u.DisplayName
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Allow connections from any origin (the wiki may be behind a reverse proxy).
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	client := collab.NewClient(s.presenceHub, conn, username, displayName)
	client.Run() // blocks until connection closes
}

// handleCollabWS upgrades to WebSocket for collaborative editing on a specific page.
// GET /api/ws/collab/{path...}
func (s *Server) handleCollabWS(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())
	if username == "" {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	pagePath := "/" + strings.TrimPrefix(r.URL.Path, "/api/ws/collab/")
	if pagePath == "/" {
		http.Error(w, "missing page path", http.StatusBadRequest)
		return
	}

	displayName := username
	if s.userStore != nil {
		if u, err := s.userStore.Get(username); err == nil && u.DisplayName != "" {
			displayName = u.DisplayName
		}
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	s.collabRelay.Join(conn, pagePath, username, displayName) // blocks
}

// handleCollabDraftRead lets any authenticated user read the current draft
// for a page that is being actively edited (locked). This is used when
// joining a collaborative session so the guest starts with the same content.
// GET /api/collab/draft/{path}
func (s *Server) handleCollabDraftRead(w http.ResponseWriter, r *http.Request) {
	pagePath := "/" + strings.TrimPrefix(r.URL.Path, "/api/collab/draft/")
	if pagePath == "/" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	// Only serve if the page is currently locked (someone is editing).
	lock := s.draftManager.GetLock(pagePath)
	if lock.Owner == "" {
		writeError(w, http.StatusNotFound, "no active editing session")
		return
	}

	content, err := s.draftManager.AdminReadDraft(pagePath, lock.Owner)
	if err != nil {
		// Fall back to published content.
		page, err := s.store.Get(pagePath)
		if err != nil {
			writeError(w, http.StatusNotFound, "page not found")
			return
		}
		content = page.Markdown
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"markdown": content,
		"owner":    lock.Owner,
	})
}
