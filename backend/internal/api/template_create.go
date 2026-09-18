package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gowiki/backend/internal/markdown"
	"gowiki/backend/internal/storage"
)

// ── Types ──────────────────────────────────────────────────────────────

// TemplateCreateRequest is the JSON body accepted by both the HTTP
// endpoint and (via a shared struct) the MCP tool. The reviewflow
// overrides are optional — any key left empty falls through to the
// template-reviewflow arg, then the template's own reviewflow value, then
// "1.0" for version.
type TemplateCreateRequest struct {
	TemplatePath       string            `json:"template_path"`
	Path               string            `json:"path"`
	Title              string            `json:"title"`
	ReviewflowOverride map[string]string `json:"reviewflow,omitempty"`
	Summary            string            `json:"summary"`
}

// TemplateCreateResult is what a successful create returns — enough to
// let a caller record what was issued without re-reading the page.
type TemplateCreateResult struct {
	Path                string `json:"path"`
	Version             int64  `json:"version"`
	TemplatePath        string `json:"template_path"`
	TemplateVersion     int64  `json:"template_version"`
	TemplateVersionTag  string `json:"template_version_tag,omitempty"`
	Stamp               string `json:"stamp"`
	ReviewflowResolved  string `json:"reviewflow_resolved,omitempty"`
}

// TemplateCreateError is a caller-facing failure. Kind classifies it so
// the HTTP layer can pick the right status and the MCP layer can format a
// helpful message; Message is safe to hand to an end user.
type TemplateCreateError struct {
	Kind    string
	Message string
}

func (e *TemplateCreateError) Error() string { return e.Message }

// httpStatus returns the status code that maps a given Kind.
func (e *TemplateCreateError) httpStatus() int {
	switch e.Kind {
	case "not_template", "not_validated", "invalid_target", "destination_exists":
		return http.StatusConflict
	case "template_not_found":
		return http.StatusNotFound
	case "forbidden":
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}

// ── Core action ────────────────────────────────────────────────────────

// createPageFromTemplate performs every step of the create-from-template
// flow. HTTP + MCP share this method. It refuses (rather than degrades)
// on any consistency check: not a template, reviewflow open, destination
// taken. On success it returns the concrete versions written to the new
// page's stamp — the caller records those, not what the template looks
// like now.
func (s *Server) createPageFromTemplate(req TemplateCreateRequest, author string) (*TemplateCreateResult, error) {
	tplPath := strings.TrimSpace(req.TemplatePath)
	dstPath := strings.TrimSpace(req.Path)
	title := strings.TrimSpace(req.Title)
	if tplPath == "" || dstPath == "" {
		return nil, &TemplateCreateError{Kind: "invalid_target", Message: "template_path and path are required"}
	}
	if title == "" {
		return nil, &TemplateCreateError{Kind: "invalid_target", Message: "title is required"}
	}

	tplStorage := strings.TrimPrefix(tplPath, "/")
	// Namespace-index shorthand: /foo/ → foo/index
	if strings.HasSuffix(tplStorage, "/") {
		tplStorage += "index"
	}
	dstStorage := strings.TrimPrefix(dstPath, "/")
	if strings.HasSuffix(dstStorage, "/") {
		dstStorage += "index"
	}
	if dstStorage == "" {
		return nil, &TemplateCreateError{Kind: "invalid_target", Message: "destination path is empty"}
	}
	if dstStorage == tplStorage {
		return nil, &TemplateCreateError{Kind: "invalid_target", Message: "destination and template are the same page"}
	}

	// Refuse when the destination already exists.
	if s.store.Exists(dstStorage) {
		return nil, &TemplateCreateError{Kind: "destination_exists", Message: fmt.Sprintf("destination %s already exists", dstPath)}
	}

	// Load the template.
	tpl, err := s.store.Get(tplStorage)
	if err != nil {
		return nil, &TemplateCreateError{Kind: "template_not_found", Message: fmt.Sprintf("template %s not found: %v", tplPath, err)}
	}

	// Verify the source really is a template.
	if !markdown.IsTemplatePage(tpl.Markdown) {
		return nil, &TemplateCreateError{Kind: "not_template", Message: fmt.Sprintf("%s is not a template (no {template} directive)", tplPath)}
	}

	// Verify the template's reviewflow, when it has one, is fully validated.
	// A template with NO reviewflow is legitimate (spec §5) — just skip the
	// check and stamp with the page-version fallback.
	templateOwnRF := markdown.ParseReviewflowArgs(tpl.Markdown)
	hasReviewflow := len(templateOwnRF) > 0
	versionTag := ""
	if hasReviewflow {
		if s.reviewflowService == nil {
			return nil, &TemplateCreateError{Kind: "not_validated", Message: "reviewflow service unavailable"}
		}
		status, rfErr := s.reviewflowService.GetStatus(tplStorage)
		if rfErr == nil && status != nil {
			if !status.IsFullyValidated {
				missing := sortedRoles(status.MissingRoles)
				return nil, &TemplateCreateError{
					Kind:    "not_validated",
					Message: fmt.Sprintf("template %s is not fully validated — missing role(s): %s. Complete the review before issuing documents from it.", tplPath, strings.Join(missing, ", ")),
				}
			}
			versionTag = status.VersionTag
		}
	}

	// Split payload.
	_, payload, ok := markdown.SplitTemplatePayload(tpl.Markdown)
	if !ok {
		// Shouldn't happen — IsTemplatePage said yes.
		return nil, &TemplateCreateError{Kind: "not_template", Message: fmt.Sprintf("%s: {template} directive not found while splitting payload", tplPath)}
	}

	// Merge reviewflow args. Only produce args when the template actually
	// carries {template-reviewflow} OR when the caller passed overrides.
	// If neither, the created document has no reviewflow — the template
	// may be one that doesn't want to require review.
	tplRF, tplRFPresent := markdown.ParseTemplateReviewflowArgs(payload)
	var mergedRF map[string]string
	if tplRFPresent || len(req.ReviewflowOverride) > 0 {
		mergedRF = markdown.MergeReviewflowArgs(req.ReviewflowOverride, tplRF, templateOwnRF)
	}

	// Compose the stamp.
	stamp := markdown.TemplateStampArgs{
		TemplatePath:        storage.CanonicalPath(tplStorage),
		TemplateTitle:       markdown.ExtractTitle(tpl.Markdown),
		TemplatePageVersion: tpl.Meta.Version,
		VersionTag:          versionTag,
	}

	// Resolve.
	resolved := markdown.ResolveTemplatePayload(payload, markdown.TemplateResolveOpts{
		Stamp:          stamp,
		Title:          title,
		ReviewflowArgs: mergedRF,
	})

	// Write.
	summary := strings.TrimSpace(req.Summary)
	authorField := author
	if summary != "" {
		authorField = author + " | " + summary
	}
	put, err := s.store.Put(dstStorage, resolved, authorField)
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", dstPath, err)
	}

	result := &TemplateCreateResult{
		Path:               storage.CanonicalPath(dstStorage),
		Version:            put.Page.Meta.Version,
		TemplatePath:       stamp.TemplatePath,
		TemplateVersion:    stamp.TemplatePageVersion,
		TemplateVersionTag: stamp.VersionTag,
		Stamp:              markdown.FormatTemplateStamp(stamp),
	}
	if mergedRF != nil {
		result.ReviewflowResolved = formatReviewflowKV(mergedRF)
	}
	return result, nil
}

func sortedRoles(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func formatReviewflowKV(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if v := m[k]; v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, " ")
}

// templateVersionTag returns the reviewflow VERSIONTAG for a template page,
// or "" when the template has no reviewflow. Small wrapper so the
// row-bound-page path in database_data.go can reuse the same computation.
func (s *Server) templateVersionTag(templatePath string) string {
	if s.reviewflowService == nil {
		return ""
	}
	status, err := s.reviewflowService.GetStatus(strings.TrimPrefix(templatePath, "/"))
	if err != nil || status == nil {
		return ""
	}
	return status.VersionTag
}

// ── HTTP handler ───────────────────────────────────────────────────────

// handleCreatePageFromTemplate is the HTTP entry point.
// POST /api/pages/from-template
func (s *Server) handleCreatePageFromTemplate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "page store unavailable")
		return
	}
	username := UsernameFromContext(r.Context())
	if username == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var body TemplateCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	// Basic ACL: caller must have edit permission on the destination
	// namespace, and view on the template. The requirePermission
	// middleware would gate any endpoint URL — but our destination
	// travels in the body, so we check here directly.
	if s.aclStore != nil {
		dstACL := "/" + strings.TrimPrefix(strings.TrimSpace(body.Path), "/")
		groups := s.userGroups(username)
		if !s.aclStore.CheckPermission(username, groups, dstACL, "edit") {
			writeError(w, http.StatusForbidden, "edit permission denied on destination")
			return
		}
		tplACL := "/" + strings.TrimPrefix(strings.TrimSpace(body.TemplatePath), "/")
		if !s.aclStore.CheckPermission(username, groups, tplACL, "view") {
			writeError(w, http.StatusForbidden, "view permission denied on template")
			return
		}
	}

	result, err := s.createPageFromTemplate(body, username)
	if err != nil {
		var terr *TemplateCreateError
		if errors.As(err, &terr) {
			writeJSON(w, terr.httpStatus(), map[string]any{
				"error": terr.Message,
				"kind":  terr.Kind,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// userGroups returns the caller's group list (empty when userStore is
// unavailable). Small helper: mirrors what the mcpserver.Deps helper does.
func (s *Server) userGroups(username string) []string {
	if s.userStore == nil || username == "" {
		return nil
	}
	u, err := s.userStore.Get(username)
	if err != nil {
		return nil
	}
	return u.EffectiveGroups()
}
