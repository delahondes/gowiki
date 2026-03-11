package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleCheckPages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Cap at 500 to prevent abuse.
	if len(req.Paths) > 500 {
		req.Paths = req.Paths[:500]
	}

	exists := make(map[string]bool, len(req.Paths))
	for _, p := range req.Paths {
		exists[p] = s.store.Exists(p)
	}

	writeJSON(w, http.StatusOK, map[string]any{"exists": exists})
}
