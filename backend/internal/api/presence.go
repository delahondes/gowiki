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
