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
	if err := s.configStore.Update(newConfig); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.configStore.Get())
}
