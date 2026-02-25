package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/storage"
)

func (s *Server) handlePageHistory(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	entries, err := s.atticStore.ListVersions(pagePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	result := map[string]any{}
	if entries == nil {
		result["versions"] = []any{}
	} else {
		result["versions"] = entries
	}

	// Include draft info if a lock exists.
	lock := s.draftManager.GetLock(pagePath)
	if lock.Owner != "" {
		username := UsernameFromContext(r.Context())
		if username == lock.Owner {
			// Requester owns the draft — include has_changes.
			hasChanges := true
			draftContent, err := s.draftManager.ReadDraft(pagePath, username)
			if err == nil {
				page, err := s.store.Get(pagePath)
				if err == nil {
					hasChanges = draftContent != page.Markdown
				}
			}
			result["draft"] = map[string]any{
				"owner":       lock.Owner,
				"since":       lock.Since,
				"is_own":      true,
				"has_changes": hasChanges,
			}
		} else if username != "" {
			// Authenticated but not the owner.
			result["draft"] = map[string]any{
				"owner":  lock.Owner,
				"since":  lock.Since,
				"is_own": false,
			}
		}
		// Unauthenticated: omit draft field.
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePageDiff(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	from, err := strconv.ParseInt(fromStr, 10, 64)
	if err != nil || from < 0 {
		writeError(w, http.StatusBadRequest, "invalid 'from' version")
		return
	}
	to, err := strconv.ParseInt(toStr, 10, 64)
	if err != nil || to < -1 {
		writeError(w, http.StatusBadRequest, "invalid 'to' version")
		return
	}

	var fromContent []byte
	if from == 0 {
		// from=0 means current published version
		page, err := s.store.Get(pagePath)
		if err != nil {
			writeError(w, http.StatusNotFound, "page not found")
			return
		}
		fromContent = []byte(page.Markdown)
	} else {
		fromContent, err = s.atticStore.ReadVersion(pagePath, from)
		if err != nil {
			writeError(w, http.StatusNotFound, "from version not found")
			return
		}
	}

	var toContent []byte
	if to == 0 {
		// to=0 means current published version
		page, err := s.store.Get(pagePath)
		if err != nil {
			writeError(w, http.StatusNotFound, "page not found")
			return
		}
		toContent = []byte(page.Markdown)
	} else if to == -1 {
		// to=-1 means draft content — requires auth + ownership.
		username := UsernameFromContext(r.Context())
		if username == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		lock := s.draftManager.GetLock(pagePath)
		if lock.Owner != username {
			writeError(w, http.StatusForbidden, "not the draft owner")
			return
		}
		draftContent, err := s.draftManager.ReadDraft(pagePath, username)
		if err != nil {
			writeError(w, http.StatusNotFound, "draft not found")
			return
		}
		toContent = []byte(draftContent)
	} else {
		toContent, err = s.atticStore.ReadVersion(pagePath, to)
		if err != nil {
			writeError(w, http.StatusNotFound, "to version not found")
			return
		}
	}

	hunks := storage.DiffLines(string(fromContent), string(toContent))
	writeJSON(w, http.StatusOK, map[string]any{
		"path":  pagePath,
		"from":  from,
		"to":    to,
		"hunks": hunks,
	})
}

func (s *Server) handlePageVersion(w http.ResponseWriter, r *http.Request) {
	// URL: /api/versions/{pagepath}?v=N
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	vStr := r.URL.Query().Get("v")
	version, err := strconv.ParseInt(vStr, 10, 64)
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, "invalid version number")
		return
	}

	content, err := s.atticStore.ReadVersion(pagePath, version)
	if err != nil {
		writeError(w, http.StatusNotFound, "version not found")
		return
	}

	resp := map[string]any{
		"path":     pagePath,
		"version":  version,
		"markdown": string(content),
	}

	// Include media_refs from the attic entry if available.
	entry, entryErr := s.atticStore.GetEntry(pagePath, version)
	if entryErr == nil && entry != nil && len(entry.MediaRefs) > 0 {
		resp["media_refs"] = entry.MediaRefs
	}

	writeJSON(w, http.StatusOK, resp)
}
