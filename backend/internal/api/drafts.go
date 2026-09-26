package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	markdown_pkg "gowiki/backend/internal/markdown"
	"gowiki/backend/internal/storage"
)

var flowMarkerRe = regexp.MustCompile(`\{#/?@?[A-Za-z0-9._-]+/?\}`)

// stripFlowMarkers removes ephemeral flow markers from markdown.
// Preserves bookmark markers ({#!...}).
func stripFlowMarkers(md string) string {
	return flowMarkerRe.ReplaceAllStringFunc(md, func(match string) string {
		if strings.HasPrefix(match, "{#!") || strings.HasPrefix(match, "{#/!") {
			return match // preserve bookmarks
		}
		return ""
	})
}

type DraftManager interface {
	EnterEditMode(pagePath, username string, force bool, currentPublished string) (markdown string, editToken string, err error)
	SaveDraft(pagePath, username, editToken, markdown string) error
	ReadDraft(pagePath, username string) (string, error)
	DiscardDraft(pagePath, username, editToken string) error
	Publish(pagePath, username, editToken string) (string, error)
	GetLock(pagePath string) storage.DraftLock
	FindAnyDraft(pagePath string) (storage.DraftInfo, bool)
	ListLocks() []storage.LockInfo
	ListDrafts() []storage.DraftInfo
	AdminDiscardDraft(pagePath, draftOwner string) error
	AdminReadDraft(pagePath, owner string) (string, error)
	AdminReclaimDraft(pagePath, fromUser, toUser string) error
}

func (s *Server) handleEnterEdit(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	username := UsernameFromContext(r.Context())
	force := r.URL.Query().Get("force") == "true"

	// Optional request body lets the frontend supply the initial markdown
	// for a new page (the chosen template, or empty for a blank page). When
	// present, the backend uses it verbatim instead of re-resolving a
	// template server-side. An empty string + InitialMarkdownSet==true means
	// "blank page requested" and is distinct from an absent field.
	var reqBody struct {
		InitialMarkdown    string `json:"initial_markdown"`
		InitialMarkdownSet bool   `json:"-"`
	}
	if r.Header.Get("Content-Type") != "" && r.ContentLength != 0 {
		raw := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&raw); err == nil {
			if v, ok := raw["initial_markdown"]; ok {
				if s, ok := v.(string); ok {
					reqBody.InitialMarkdown = s
					reqBody.InitialMarkdownSet = true
				} else if v == nil {
					reqBody.InitialMarkdownSet = true
				}
			}
		}
	}

	// Get current published content as fallback.
	var published string
	page, err := s.store.Get(pagePath)
	if err == nil {
		published = page.Markdown
	} else if errors.Is(err, storage.ErrPageNotFound) {
		// If the path has a trailing slash, the user wants a namespace index.
		// Pre-create the directory so the page store creates index.md.
		if strings.HasSuffix(pagePath, "/") {
			if creator, ok := s.store.(interface{ EnsureNamespaceDir(string) error }); ok {
				_ = creator.EnsureNamespaceDir(strings.TrimSuffix(pagePath, "/"))
			}
			pagePath = strings.TrimSuffix(pagePath, "/")
		}
		// New page — check namespace constraints before allowing edit.
		if nsErr := s.store.CheckNamespaceConflict(pagePath); nsErr != nil {
			var nce *storage.NamespaceConflictError
			if errors.As(nsErr, &nce) {
				writeJSON(w, http.StatusConflict, map[string]string{
					"error":            "namespace_conflict",
					"conflicting_page": nce.ConflictingPage,
					"message":          "Page " + nce.ConflictingPage + " must be converted to a namespace index first",
				})
				return
			}
		}
		// New page — prefer the explicit body, fall back to server-side default.
		if reqBody.InitialMarkdownSet {
			published = reqBody.InitialMarkdown
		} else if tmpl, ok := s.store.(TemplateResolver); ok {
			if content, _, resolveErr := tmpl.ResolveTemplate(pagePath); resolveErr == nil {
				published = content
			}
		}
		// Apply tag mutations (e.g. strip "tpl" tags) to any template-derived
		// content, regardless of whether it came from the body or the fallback.
		if published != "" && s.configStore != nil {
			mutations := s.configStore.Get().Tags.TemplateMutations
			if len(mutations) > 0 {
				published = markdown_pkg.ApplyTagMutations(published, mutations)
			}
		}
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

// PublishDraftErrorKind classifies a publish failure so callers (HTTP, MCP)
// can map to their own error surface without matching error strings.
type PublishDraftErrorKind string

const (
	PublishErrDatabaseRowConflict PublishDraftErrorKind = "database_row_conflict"
	PublishErrEditSuperseded      PublishDraftErrorKind = "edit_superseded"
	PublishErrNoDraft             PublishDraftErrorKind = "no_draft"
	PublishErrValidation          PublishDraftErrorKind = "validation"
	PublishErrInternal            PublishDraftErrorKind = "internal"
)

// PublishDraftError carries a machine-readable kind alongside the message.
type PublishDraftError struct {
	Kind    PublishDraftErrorKind
	Message string
	// Table is set when Kind is PublishErrDatabaseRowConflict.
	Table string
}

func (e *PublishDraftError) Error() string { return e.Message }

// PublishDraft runs the full publish pipeline for a draft: the inline-edit
// conflict guard, DraftManager.Publish, flow-marker stripping, database
// validation, page store write, and todo auto-completion. Shared between the
// HTTP handler and the MCP tool so both go through one code path.
func (s *Server) PublishDraft(pagePath, username, editToken string, forcePublish bool) (*storage.PutResult, *PublishDraftError) {
	pagePath = strings.TrimSpace(pagePath)
	if pagePath == "" {
		return nil, &PublishDraftError{Kind: PublishErrInternal, Message: "missing page path"}
	}

	if !forcePublish {
		if tableNameVal, ok := s.inlineEditConflicts.Load(pagePath); ok {
			table, _ := tableNameVal.(string)
			return nil, &PublishDraftError{
				Kind:    PublishErrDatabaseRowConflict,
				Message: "database row was edited inline while this draft was open; retry with force_publish=true to overwrite",
				Table:   table,
			}
		}
	}

	s.inlineEditConflicts.Delete(pagePath)

	md, err := s.draftManager.Publish(pagePath, username, editToken)
	if errors.Is(err, storage.ErrEditSuperseded) {
		return nil, &PublishDraftError{Kind: PublishErrEditSuperseded, Message: "edit session superseded"}
	}
	if errors.Is(err, storage.ErrNoDraft) {
		return nil, &PublishDraftError{Kind: PublishErrNoDraft, Message: "no draft to publish"}
	}
	if err != nil {
		return nil, &PublishDraftError{Kind: PublishErrInternal, Message: err.Error()}
	}

	md = stripFlowMarkers(md)

	if s.databaseSync != nil {
		if err := s.databaseSync.ValidatePageContent(pagePath, md); err != nil {
			return nil, &PublishDraftError{Kind: PublishErrValidation, Message: err.Error()}
		}
	}

	result, err := s.store.Put(pagePath, md, username)
	if err != nil {
		return nil, &PublishDraftError{Kind: PublishErrInternal, Message: err.Error()}
	}

	if s.todoService != nil {
		go s.todoService.AutoCompleteWikiAction(context.Background(), "edit", result.Page.Path, username)
		go s.todoService.AutoCompleteCreateAction(context.Background(), result.Page.Path, username)
		go s.todoService.ReopenReadTasks(context.Background(), result.Page.Path)
	}
	return &result, nil
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

	result, perr := s.PublishDraft(pagePath, username, req.EditToken, req.ForcePublish)
	if perr != nil {
		switch perr.Kind {
		case PublishErrDatabaseRowConflict:
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "database_row_conflict",
				"table": perr.Table,
			})
		case PublishErrEditSuperseded, PublishErrNoDraft:
			writeJSON(w, http.StatusConflict, map[string]any{"error": "edit session superseded"})
		case PublishErrValidation:
			writeError(w, http.StatusBadRequest, perr.Message)
		default:
			writeError(w, http.StatusInternalServerError, perr.Message)
		}
		return
	}

	writeJSON(w, http.StatusOK, result)
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

	// Clear any inline edit conflict flag for this page.
	s.inlineEditConflicts.Delete(pagePath)

	writeJSON(w, http.StatusOK, map[string]string{"status": "draft discarded"})
}
