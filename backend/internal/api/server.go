package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"gowiki/backend/internal/storage"
)

type PageStore interface {
	Get(pagePath string) (storage.Page, error)
	Put(pagePath, markdown string) (storage.PutResult, error)
}

type OrphanDetector interface {
	FindOrphans() ([]string, error)
	GetReferencingPages(mediaPath string) []string
}

type MediaStore interface {
	List(namespacePath string) ([]storage.MediaEntry, error)
	Put(namespacePath, fileName string, content io.Reader, overwrite bool) (storage.MediaEntry, error)
	Delete(mediaPath string) error
	ResolvePath(mediaPath string) (string, error)
}

type SearchStore interface {
	Search(query string, limit int) ([]storage.SearchResult, error)
}

type LogoResolver interface {
	ResolveLogo() (string, error)
}

type Server struct {
	store          PageStore
	mediaStore     MediaStore
	orphanDetector OrphanDetector
	searchStore    SearchStore
	logoResolver   LogoResolver
	serveWeb       bool
	webDirPath     string
}

func NewRouter(store PageStore, mediaStore MediaStore, orphanDetector OrphanDetector, searchStore SearchStore, logoResolver LogoResolver, serveWeb bool, webDirPath string) http.Handler {
	s := &Server{
		store:          store,
		mediaStore:     mediaStore,
		orphanDetector: orphanDetector,
		searchStore:    searchStore,
		logoResolver:   logoResolver,
		serveWeb:       serveWeb,
		webDirPath:     webDirPath,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	r.Get("/api/health", s.handleHealth)
	r.Get("/api/pages/*", s.handleGetPage)
	r.Put("/api/pages/*", s.handlePutPage)

	r.Get("/api/media", s.handleListMedia)
	r.Get("/api/media/", s.handleListMedia)
	r.Get("/api/media/*", s.handleListMedia)
	r.Post("/api/media", s.handleUploadMedia)
	r.Post("/api/media/", s.handleUploadMedia)
	r.Post("/api/media/*", s.handleUploadMedia)
	r.Delete("/api/media/*", s.handleDeleteMedia)
	r.Get("/api/media-orphans", s.handleMediaOrphans)

	r.Get("/api/search", s.handleSearch)
	r.Get("/api/site/logo", s.handleSiteLogo)

	r.Get("/media/*", s.handleServeMedia)
	r.Get(`/{path:.*\..*}`, s.handleFilePath)

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
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
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
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	var req putPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	result, err := s.store.Put(pagePath, req.Markdown)
	if errors.Is(err, storage.ErrNamespaceConflict) {
		writeError(w, http.StatusConflict, "a namespace directory exists at this path")
		return
	}
	var cycleErr *storage.CircularIncludeError
	if errors.As(err, &cycleErr) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": cycleErr.Error(),
			"cycle": cycleErr.Cycle,
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	nsPath := strings.TrimSpace(chi.URLParam(r, "*"))
	entries, err := s.mediaStore.List(nsPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    nsPath,
		"entries": entries,
	})
}

func parseBoolFlag(raw string) bool {
	if raw == "" {
		return false
	}
	ok, err := strconv.ParseBool(raw)
	if err == nil {
		return ok
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "yes", "y":
		return true
	default:
		return false
	}
}

func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	nsPath := strings.TrimSpace(chi.URLParam(r, "*"))
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	overwrite := parseBoolFlag(r.FormValue("overwrite"))
	entry, err := s.mediaStore.Put(nsPath, header.Filename, file, overwrite)
	if errors.Is(err, storage.ErrMediaConflict) {
		writeError(w, http.StatusConflict, "media file already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"entry": entry})
}

func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	mediaPath := strings.TrimSpace(chi.URLParam(r, "*"))
	if mediaPath == "" {
		writeError(w, http.StatusBadRequest, "missing media path")
		return
	}

	// Check if the media is still referenced by any pages.
	if pages := s.orphanDetector.GetReferencingPages(mediaPath); len(pages) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":            "media is still referenced",
			"referencing_pages": pages,
		})
		return
	}

	err := s.mediaStore.Delete(mediaPath)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "media file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": mediaPath})
}

func (s *Server) handleMediaOrphans(w http.ResponseWriter, _ *http.Request) {
	orphans, err := s.orphanDetector.FindOrphans()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orphans": orphans})
}

func (s *Server) handleServeMedia(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(chi.URLParam(r, "*"))
	if raw == "" {
		http.NotFound(w, r)
		return
	}

	cleaned := path.Clean("/" + raw)
	cleaned = strings.TrimPrefix(cleaned, "/")
	targetPath, err := s.mediaStore.ResolvePath(cleaned)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(targetPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, filepath.Clean(targetPath))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}

	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 50 {
		limit = 50
	}

	results, err := s.searchStore.Search(q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleSiteLogo(w http.ResponseWriter, _ *http.Request) {
	logoPath, err := s.logoResolver.ResolveLogo()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logoPath == "" {
		writeError(w, http.StatusNotFound, "no logo found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": logoPath})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
