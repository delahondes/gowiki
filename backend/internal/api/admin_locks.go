package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/storage"
)

// handleListLocks returns all current draft locks across the wiki.
// GET /api/admin/locks
func (s *Server) handleListLocks(w http.ResponseWriter, _ *http.Request) {
	locks := s.draftManager.ListLocks()
	if locks == nil {
		locks = []storage.LockInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"locks": locks})
}

// handleAdminDiscardDraft forcefully discards any user's draft.
// DELETE /api/admin/drafts/{path}
func (s *Server) handleAdminDiscardDraft(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	adminUser := UsernameFromContext(r.Context())

	lock := s.draftManager.GetLock(pagePath)
	if lock.Owner == "" {
		writeError(w, http.StatusNotFound, "no draft exists for this page")
		return
	}

	draftOwner := lock.Owner

	// Force discard: admin discards another user's draft.
	err := s.draftManager.AdminDiscardDraft(pagePath, draftOwner)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Log to changelog — this is a destructive action.
	if s.changelog != nil {
		s.changelog.Append(pagePath, 0, adminUser,
			fmt.Sprintf("admin override: discarded draft by %s", draftOwner), "admin")
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"discarded":    pagePath,
		"draft_owner":  draftOwner,
		"discarded_by": adminUser,
	})
}
