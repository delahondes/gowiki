package api

import (
	"encoding/json"
	"net/http"

	"gowiki/backend/internal/config"
)

// handleGetConfig returns the full site configuration.
// GET /api/admin/config
func (s *Server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	cfg := s.configStore.Get()
	writeJSON(w, http.StatusOK, cfg)
}

// handleUpdateConfig replaces the full site configuration.
// PUT /api/admin/config
func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var newConfig config.Config
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		writeError(w, http.StatusBadRequest, "invalid config JSON")
		return
	}

	// Preserve operational fields that the admin UI does not manage.
	// Without this, a save from the UI would zero these out and break the server.
	current := s.configStore.Get()
	if newConfig.DataDir == "" {
		newConfig.DataDir = current.DataDir
	}
	if newConfig.Server.Addr == "" {
		newConfig.Server.Addr = current.Server.Addr
	}
	if newConfig.Server.TLSDomain == "" {
		newConfig.Server.TLSDomain = current.Server.TLSDomain
	}
	if newConfig.Server.WebDir == "" {
		newConfig.Server.WebDir = current.Server.WebDir
	}

	if err := s.configStore.Update(newConfig); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Re-initialize OAuth client if config changed.
	if err := s.initOAuthClient(); err != nil {
		s.oauthClient = nil // Clear stale client if config is now invalid/empty.
	}

	writeJSON(w, http.StatusOK, s.configStore.Get())
}
