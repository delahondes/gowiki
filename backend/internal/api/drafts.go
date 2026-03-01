package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/markdown"
	"gowiki/backend/internal/storage"
)

type DraftManager interface {
	EnterEditMode(pagePath, username string, force bool, currentPublished string) (markdown string, editToken string, err error)
	SaveDraft(pagePath, username, editToken, markdown string) error
	ReadDraft(pagePath, username string) (string, error)
	DiscardDraft(pagePath, username, editToken string) error
	Publish(pagePath, username, editToken string) (string, error)
	GetLock(pagePath string) storage.DraftLock
	ListLocks() []storage.LockInfo
	AdminDiscardDraft(pagePath, draftOwner string) error
}

func (s *Server) handleEnterEdit(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	username := UsernameFromContext(r.Context())
	force := r.URL.Query().Get("force") == "true"

	// Get current published content as fallback.
	var published string
	page, err := s.store.Get(pagePath)
	if err == nil {
		published = page.Markdown
	}

	markdown, editToken, err := s.draftManager.EnterEditMode(pagePath, username, force, published)
	if errors.Is(err, storage.ErrPageLocked) {
		lock := s.draftManager.GetLock(pagePath)
		writeJSON(w, http.StatusLocked, map[string]any{
			"error":    err.Error(),
			"locked_by": lock.Owner,
		})
		return
	}
	if errors.Is(err, storage.ErrEditSuperseded) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "you already have this page open in another session",
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"markdown":   markdown,
		"edit_token": editToken,
	})
}

func (s *Server) handleSaveDraft(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	username := UsernameFromContext(r.Context())
	var req struct {
		Markdown  string `json:"markdown"`
		EditToken string `json:"edit_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	err := s.draftManager.SaveDraft(pagePath, username, req.EditToken, req.Markdown)
	if errors.Is(err, storage.ErrEditSuperseded) || errors.Is(err, storage.ErrNoDraft) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "edit session superseded"})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "draft saved"})
}

func (s *Server) handlePublish(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	username := UsernameFromContext(r.Context())
	var req struct {
		EditToken    string `json:"edit_token"`
		Summary      string `json:"summary"`
		ForcePublish bool   `json:"force_publish"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Before publishing, check for database-row value conflicts.
	// A forced inline edit may have changed the published page while the draft was open.
	if !req.ForcePublish && s.schemaStore != nil {
		draftContent, err := s.draftManager.ReadDraft(pagePath, username)
		if err == nil {
			page, err := s.store.Get(pagePath)
			if err == nil {
				if conflict := findDatabaseRowConflict(draftContent, page.Markdown); conflict != nil {
					writeJSON(w, http.StatusConflict, map[string]any{
						"error":            "database_row_conflict",
						"table":            conflict.tableName,
						"draft_values":     conflict.draftFields,
						"published_values": conflict.publishedFields,
					})
					return
				}
			}
		}
	}

	md, err := s.draftManager.Publish(pagePath, username, req.EditToken)
	if errors.Is(err, storage.ErrEditSuperseded) || errors.Is(err, storage.ErrNoDraft) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "edit session superseded"})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Write through the page store (which handles archiving, indexes, etc.)
	result, err := s.store.Put(pagePath, md, username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

type databaseRowConflict struct {
	tableName       string
	draftFields     map[string]string
	publishedFields map[string]string
}

// findDatabaseRowConflict compares {database-row} blocks between draft and published content.
// Returns nil if no conflict.
func findDatabaseRowConflict(draftContent, publishedContent string) *databaseRowConflict {
	draftBlocks := markdown.ExtractDatabaseRows(draftContent)
	pubBlocks := markdown.ExtractDatabaseRows(publishedContent)
	if len(draftBlocks) == 0 || len(pubBlocks) == 0 {
		return nil
	}

	pubMap := make(map[string]map[string]string)
	for _, b := range pubBlocks {
		pubMap[b.TableName] = b.Fields
	}

	for _, db := range draftBlocks {
		pub, ok := pubMap[db.TableName]
		if !ok {
			continue
		}
		// Compare field values — any difference means a forced inline edit happened.
		for k, dv := range db.Fields {
			if pv, ok := pub[k]; ok && dv != pv {
				return &databaseRowConflict{
					tableName:       db.TableName,
					draftFields:     db.Fields,
					publishedFields: pub,
				}
			}
		}
		for k, pv := range pub {
			if dv, ok := db.Fields[k]; ok && dv != pv {
				return &databaseRowConflict{
					tableName:       db.TableName,
					draftFields:     db.Fields,
					publishedFields: pub,
				}
			}
		}
	}
	return nil
}

func (s *Server) handleDiscardDraft(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	username := UsernameFromContext(r.Context())
	editToken := r.URL.Query().Get("edit_token")
	err := s.draftManager.DiscardDraft(pagePath, username, editToken)
	if errors.Is(err, storage.ErrNotDraftOwner) {
		writeError(w, http.StatusForbidden, "not the draft owner")
		return
	}
	if errors.Is(err, storage.ErrEditSuperseded) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "another editing session is active"})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "draft discarded"})
}
