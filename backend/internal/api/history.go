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
	if entries == nil {
		writeJSON(w, http.StatusOK, map[string]any{"versions": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": entries})
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
	if err != nil || from < 1 {
		writeError(w, http.StatusBadRequest, "invalid 'from' version")
		return
	}
	to, err := strconv.ParseInt(toStr, 10, 64)
	if err != nil || to < 0 {
		writeError(w, http.StatusBadRequest, "invalid 'to' version")
		return
	}

	fromContent, err := s.atticStore.ReadVersion(pagePath, from)
	if err != nil {
		writeError(w, http.StatusNotFound, "from version not found")
		return
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

	writeJSON(w, http.StatusOK, map[string]any{
		"path":     pagePath,
		"version":  version,
		"markdown": string(content),
	})
}
