package api

import (
	"encoding/json"
	"net/http"

	"gowiki/backend/internal/auth"
)

// handleListACL returns the current ACL ruleset.
// GET /api/admin/acl
func (s *Server) handleListACL(w http.ResponseWriter, _ *http.Request) {
	rules := s.aclStore.List()
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// handleReplaceACL replaces the entire ACL ruleset.
// PUT /api/admin/acl
func (s *Server) handleReplaceACL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rules []auth.ACLRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := s.aclStore.Replace(req.Rules); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rules": s.aclStore.List()})
}
