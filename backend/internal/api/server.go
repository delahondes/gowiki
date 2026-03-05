package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/config"
	"gowiki/backend/internal/database"
	"gowiki/backend/internal/markdown"
	"gowiki/backend/internal/storage"
)

type PageStore interface {
	Get(pagePath string) (storage.Page, error)
	Put(pagePath, markdown, author string) (storage.PutResult, error)
	Delete(pagePath, author string) (storage.DeleteResult, error)
	CheckNamespaceConflict(pagePath string) error
}

type OrphanDetector interface {
	FindOrphans() ([]string, error)
	GetReferencingPages(mediaPath string) []string
}

type MediaStore interface {
	List(namespacePath string) ([]storage.MediaEntry, error)
	Put(namespacePath, fileName string, content io.Reader, overwrite bool, author string) (storage.MediaEntry, error)
	Delete(mediaPath string) error
	ResolvePath(mediaPath string) (string, error)
}

type SearchStore interface {
	Search(query string, limit int) ([]storage.SearchResult, error)
}

type AtticStore interface {
	ListVersions(pagePath string) ([]storage.AtticEntry, error)
	ReadVersion(pagePath string, version int64) ([]byte, error)
	GetEntry(pagePath string, version int64) (*storage.AtticEntry, error)
}

type LogoResolver interface {
	ResolveLogo() (string, error)
}

type MediaAtticStore interface {
	ReadVersion(mediaPath string, version int64) ([]byte, error)
}

type MediaVersionStoreReader interface {
	GetVersion(mediaPath string) int64
}

type Server struct {
	store             PageStore
	mediaStore        MediaStore
	orphanDetector    OrphanDetector
	searchStore       SearchStore
	atticStore        AtticStore
	draftManager      DraftManager
	logoResolver      LogoResolver
	mediaAtticStore   MediaAtticStore
	mediaVersionStore MediaVersionStoreReader
	configStore       *config.Store
	userStore         *auth.UserStore
	groupStore        *auth.GroupStore
	sessionStore      *auth.SessionStore
	aclStore          *auth.ACLStore
	changelog         *storage.Changelog
	dbPool            *database.Pool
	schemaStore       *database.SchemaStore
	dataStore         *database.DataStore
	tagIndex            *storage.TagIndex
	// Persistent browser context for PDF export (nil if Chrome not available).
	browserAllocCtx    context.Context
	browserAllocCancel context.CancelFunc
	// Tracks pages where a forced inline edit modified the published content
	// while a draft was open. Checked at publish time to warn the user.
	inlineEditConflicts sync.Map // map[pagePath string]tableName string
	oauthClient         *auth.OAuthClient
	serveWeb            bool
	webDirPath          string
}

func NewRouter(store PageStore, mediaStore MediaStore, orphanDetector OrphanDetector, searchStore SearchStore, atticStore AtticStore, draftManager DraftManager, logoResolver LogoResolver, mediaAtticStore MediaAtticStore, mediaVersionStore MediaVersionStoreReader, configStore *config.Store, userStore *auth.UserStore, groupStore *auth.GroupStore, sessionStore *auth.SessionStore, aclStore *auth.ACLStore, changelog *storage.Changelog, dbPool *database.Pool, tagIndex *storage.TagIndex, browserAllocCtx context.Context, browserAllocCancel context.CancelFunc, serveWeb bool, webDirPath string) http.Handler {
	s := &Server{
		store:             store,
		mediaStore:        mediaStore,
		orphanDetector:    orphanDetector,
		searchStore:       searchStore,
		atticStore:        atticStore,
		draftManager:      draftManager,
		logoResolver:      logoResolver,
		mediaAtticStore:   mediaAtticStore,
		mediaVersionStore: mediaVersionStore,
		configStore:       configStore,
		userStore:         userStore,
		groupStore:        groupStore,
		sessionStore:      sessionStore,
		aclStore:          aclStore,
		changelog:         changelog,
		tagIndex:           tagIndex,
		browserAllocCtx:    browserAllocCtx,
		browserAllocCancel: browserAllocCancel,
		dbPool:   dbPool,
		serveWeb: serveWeb,
		webDirPath: webDirPath,
	}

	// If database pool is already connected, initialize stores.
	if dbPool != nil && dbPool.IsConnected() {
		s.schemaStore = database.NewSchemaStore(dbPool)
		s.dataStore = database.NewDataStore(dbPool, s.schemaStore)
	}

	// Initialize OAuth client if configured.
	if err := s.initOAuthClient(); err != nil {
		// Not an error — OAuth is simply not configured yet.
		log.Printf("oauth: %v (will initialize on first use if configured later)", err)
	} else {
		log.Printf("oauth: Azure AD provider initialized")
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	// Auth endpoints (public).
	r.Post("/api/auth/login", s.handleLogin)
	r.Post("/api/auth/logout", s.handleLogout)
	r.Get("/api/auth/me", s.handleMe)
	r.Get("/api/auth/providers", s.handleAuthProviders)
	r.Get("/api/auth/oauth/login", s.handleOAuthLogin)
	r.Get("/api/auth/oauth/callback", s.handleOAuthCallback)

	r.Get("/api/health", s.handleHealth)

	// Read endpoints — optional auth + ACL "view" permission.
	r.Group(func(r chi.Router) {
		r.Use(s.optionalAuth)
		r.Use(s.requirePermission("view"))
		r.Get("/api/pages/*", s.handleGetPage)
		r.Get("/api/history/*", s.handlePageHistory)
		r.Get("/api/versions/*", s.handlePageVersion)
		r.Get("/api/diff/*", s.handlePageDiff)
		r.Get("/api/media", s.handleListMedia)
		r.Get("/api/media/", s.handleListMedia)
		r.Get("/api/media/*", s.handleListMedia)
		r.Get("/api/media-orphans", s.handleMediaOrphans)
		r.Get("/api/media-version/*", s.handleServeMediaVersion)
		r.Get("/api/export/pdf/*", s.handleExportPDF)
	})

	// Public read endpoints — no ACL check (search, logo, site info, sitemap).
	r.Group(func(r chi.Router) {
		r.Use(s.optionalAuth)
		r.Get("/api/search", s.handleSearch)
		r.Get("/api/sitemap", s.handleSitemap)
		r.Get("/api/tags", s.handleTagQuery)
		r.Get("/api/site/logo", s.handleSiteLogo)
		r.Get("/api/site/info", s.handleSiteInfo)
	})

	// Write endpoints — require auth + ACL "edit" permission.
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.requirePermission("edit"))
		r.Put("/api/pages/*", s.handlePutPage)
		r.Post("/api/edit/*", s.handleEnterEdit)
		r.Put("/api/draft/*", s.handleSaveDraft)
		r.Post("/api/publish/*", s.handlePublish)
		r.Delete("/api/draft/*", s.handleDiscardDraft)
		r.Post("/api/media", s.handleUploadMedia)
		r.Post("/api/media/", s.handleUploadMedia)
		r.Post("/api/media/*", s.handleUploadMedia)
	})

	// Delete endpoints — require auth + ACL "delete" permission.
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.requirePermission("delete"))
		r.Delete("/api/pages/*", s.handleDeletePage)
		r.Delete("/api/media/*", s.handleDeleteMedia)
	})

	// Admin endpoints — require auth + admin group.
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Use(s.requireAdmin)

		r.Get("/api/admin/users", s.handleListUsers)
		r.Post("/api/admin/users", s.handleCreateUser)
		r.Put("/api/admin/users/{username}", s.handleUpdateUser)
		r.Delete("/api/admin/users/{username}", s.handleDeleteUser)
		r.Put("/api/admin/users/{username}/password", s.handleSetPassword)

		r.Get("/api/admin/groups", s.handleListGroups)
		r.Post("/api/admin/groups", s.handleCreateGroup)
		r.Put("/api/admin/groups/{name}", s.handleUpdateGroup)
		r.Delete("/api/admin/groups/{name}", s.handleDeleteGroup)

		r.Get("/api/admin/config", s.handleGetConfig)
		r.Put("/api/admin/config", s.handleUpdateConfig)

		r.Get("/api/admin/acl", s.handleListACL)
		r.Put("/api/admin/acl", s.handleReplaceACL)

		r.Get("/api/admin/locks", s.handleListLocks)
		r.Delete("/api/admin/drafts/*", s.handleAdminDiscardDraft)

		// Database admin endpoints.
		r.Get("/api/admin/database/status", s.handleDatabaseStatus)
		r.Post("/api/admin/database/test", s.handleDatabaseTest)
		r.Post("/api/admin/database/connect", s.handleDatabaseConnect)

		// Database schema admin endpoints.
		r.Get("/api/admin/database/tables", s.handleListDatabaseTables)
		r.Post("/api/admin/database/tables", s.handleCreateDatabaseTable)
		r.Get("/api/admin/database/tables/{id}", s.handleGetDatabaseTable)
		r.Put("/api/admin/database/tables/{id}", s.handleUpdateDatabaseTable)
		r.Delete("/api/admin/database/tables/{id}", s.handleDeleteDatabaseTable)
		r.Post("/api/admin/database/tables/{id}/fields", s.handleCreateDatabaseField)
		r.Put("/api/admin/database/tables/{id}/fields/{fid}", s.handleUpdateDatabaseField)
		r.Delete("/api/admin/database/tables/{id}/fields/{fid}", s.handleArchiveDatabaseField)
		r.Get("/api/admin/database/tables/{id}/history", s.handleDatabaseTableHistory)
	})

	// Database data endpoints — read (optional auth).
	r.Group(func(r chi.Router) {
		r.Use(s.optionalAuth)
		r.Get("/api/database/{table}/schema", s.handleDatabaseSchema)
		r.Get("/api/database/{table}/rows", s.handleDatabaseQueryRows)
		r.Get("/api/database/{table}/rows/{id}", s.handleDatabaseGetRow)
		r.Get("/api/database/{table}/page/*", s.handleDatabaseGetRowByPage)
		r.Get("/api/database/{table}/export/csv", s.handleDatabaseExportCSV)
	})

	// Database data endpoints — write (require auth).
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Post("/api/database/{table}/rows", s.handleDatabaseInsertRow)
		r.Put("/api/database/{table}/rows/{id}", s.handleDatabaseUpdateRow)
		r.Delete("/api/database/{table}/rows/{id}", s.handleDatabaseDeleteRow)
		r.Put("/api/database/{table}/page/*", s.handleDatabaseUpsertRowByPage)
	})

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

	// Check draft/lock state.
	username := UsernameFromContext(r.Context())
	lock := s.draftManager.GetLock(pagePath)

	resp := map[string]any{
		"path":               page.Path,
		"markdown":           page.Markdown,
		"meta":               page.Meta,
		"is_namespace_index": page.IsNamespaceIndex,
	}

	// Include current version numbers for all referenced media files.
	if s.mediaVersionStore != nil {
		refs := markdown.ExtractMediaRefs(page.Markdown, page.Path)
		mediaVersions := make(map[string]int64, len(refs))
		for _, ref := range refs {
			ver := s.mediaVersionStore.GetVersion(ref)
			if ver > 0 {
				mediaVersions[ref] = ver
			}
		}
		if len(mediaVersions) > 0 {
			resp["media_versions"] = mediaVersions
		}
	}

	if lock.Owner != "" {
		resp["locked_by"] = lock.Owner
		// If the requester is the draft owner, return draft content.
		if username == lock.Owner {
			if draft, err := s.draftManager.ReadDraft(pagePath, username); err == nil {
				resp["markdown"] = draft
				resp["is_draft"] = true
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
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

	author := UsernameFromContext(r.Context())
	result, err := s.store.Put(pagePath, req.Markdown, author)
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

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	username := UsernameFromContext(r.Context())
	result, err := s.store.Delete(pagePath, username)
	if errors.Is(err, storage.ErrPageNotFound) {
		writeError(w, http.StatusNotFound, "page not found")
		return
	}
	if errors.Is(err, storage.ErrPageHasLock) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": err.Error(),
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted":        pagePath,
		"orphaned_media": result.OrphanedMedia,
		"included_by":    result.IncludedBy,
	})
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
	author := UsernameFromContext(r.Context())
	entry, err := s.mediaStore.Put(nsPath, header.Filename, file, overwrite, author)
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

	s.serveMediaWithVersioning(w, r, cleaned)
}

func (s *Server) handleServeMediaVersion(w http.ResponseWriter, r *http.Request) {
	mediaPath := strings.TrimSpace(chi.URLParam(r, "*"))
	if mediaPath == "" {
		http.NotFound(w, r)
		return
	}

	cleaned := path.Clean("/" + mediaPath)
	cleaned = strings.TrimPrefix(cleaned, "/")

	vStr := r.URL.Query().Get("v")
	version, err := strconv.ParseInt(vStr, 10, 64)
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, "invalid version number")
		return
	}

	if s.mediaAtticStore == nil {
		writeError(w, http.StatusNotFound, "media versioning not available")
		return
	}

	content, err := s.mediaAtticStore.ReadVersion(cleaned, version)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Set Content-Type based on file extension.
	ext := filepath.Ext(cleaned)
	ct := "application/octet-stream"
	switch strings.ToLower(ext) {
	case ".png":
		ct = "image/png"
	case ".jpg", ".jpeg":
		ct = "image/jpeg"
	case ".gif":
		ct = "image/gif"
	case ".svg":
		ct = "image/svg+xml"
	case ".webp":
		ct = "image/webp"
	case ".pdf":
		ct = "application/pdf"
	case ".txt":
		ct = "text/plain"
	case ".html", ".htm":
		ct = "text/html"
	case ".css":
		ct = "text/css"
	case ".js":
		ct = "application/javascript"
	case ".json":
		ct = "application/json"
	case ".xml":
		ct = "application/xml"
	case ".zip":
		ct = "application/zip"
	case ".mp4":
		ct = "video/mp4"
	case ".webm":
		ct = "video/webm"
	case ".mp3":
		ct = "audio/mpeg"
	case ".ogg":
		ct = "audio/ogg"
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	w.Write(content)
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

func (s *Server) handleSiteInfo(w http.ResponseWriter, _ *http.Request) {
	cfg := s.configStore.Get()
	writeJSON(w, http.StatusOK, map[string]any{
		"title":         cfg.Site.Title,
		"toc_max_level": cfg.Site.TOCMaxLevel,
	})
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
