package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/storage"
)

// TemplateResolver is implemented by storage.FileStore.
type TemplateResolver interface {
	ResolveTemplate(pagePath string) (markdown string, templatePath string, err error)
}

// TemplateLister is implemented by storage.FileStore.
type TemplateLister interface {
	ListTemplates() ([]storage.TemplateEntry, error)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	resolver, ok := s.store.(TemplateResolver)
	if !ok {
		writeError(w, http.StatusNotImplemented, "templates not supported")
		return
	}

	markdown, tmplPath, err := resolver.ResolveTemplate(pagePath)
	if errors.Is(err, storage.ErrNoTemplate) {
		writeError(w, http.StatusNotFound, "no template found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"markdown":      markdown,
		"template_path": tmplPath,
	})
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	lister, ok := s.store.(TemplateLister)
	if !ok {
		writeError(w, http.StatusNotImplemented, "templates not supported")
		return
	}

	templates, err := lister.ListTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if templates == nil {
		templates = []storage.TemplateEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"templates": templates,
	})
}
