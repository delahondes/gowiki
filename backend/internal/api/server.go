package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"wikidown/backend/internal/storage"
)

type PageStore interface {
	Get(pagePath string) (storage.Page, error)
	Put(pagePath, markdown string) (storage.Page, error)
}

type Server struct {
	store      PageStore
	serveWeb   bool
	webDirPath string
}

func NewRouter(store PageStore, serveWeb bool, webDirPath string) http.Handler {
	s := &Server{
		store:      store,
		serveWeb:   serveWeb,
		webDirPath: webDirPath,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	r.Get("/api/health", s.handleHealth)
	r.Get("/api/pages/{path:.*}", s.handleGetPage)
	r.Put("/api/pages/{path:.*}", s.handlePutPage)

	if serveWeb {
		r.NotFound(s.handleFrontend)
	}
	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "path"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	page, err := s.store.Get(pagePath)
	if errors.Is(err, storage.ErrPageNotFound) {
		writeError(w, http.StatusNotFound, "page not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, page)
}

type putPageRequest struct {
	Markdown string `json:"markdown"`
}

func (s *Server) handlePutPage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "path"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	var req putPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	page, err := s.store.Put(pagePath, req.Markdown)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
