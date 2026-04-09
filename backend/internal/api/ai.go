package api

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/markdown"
	"gowiki/backend/internal/storage"
)

// handleAINamespace lists pages and sub-namespaces under a path.
// GET /api/ai/v1/namespace/*
func (s *Server) handleAINamespace(w http.ResponseWriter, r *http.Request) {
	nsPath := strings.TrimSpace(chi.URLParam(r, "*"))
	nsPath = strings.Trim(nsPath, "/")
	username := UsernameFromContext(r.Context())

	lister, ok := s.store.(SitemapLister)
	if !ok {
		writeError(w, http.StatusInternalServerError, "namespace listing not supported")
		return
	}

	allPages, err := lister.ListAllPages()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	depth := 1
	if r.URL.Query().Get("depth") == "0" {
		depth = 0
	}
	includeMeta := r.URL.Query().Get("include_meta") == "true"

	type pageInfo struct {
		Path         string `json:"path"`
		Title        string `json:"title"`
		Version      int64  `json:"version,omitempty"`
		LastModified string `json:"last_modified,omitempty"`
		Author       string `json:"author,omitempty"`
	}
	type nsInfo struct {
		Path      string `json:"path"`
		PageCount int    `json:"page_count"`
	}

	pages := []pageInfo{}
	nsCounts := map[string]int{}

	prefix := nsPath
	if prefix != "" {
		prefix += "/"
	}

	for _, p := range allPages {
		// ListAllPages returns paths with leading /, normalize to bare path.
		pagePath := strings.TrimPrefix(p.Path, "/")
		if !strings.HasPrefix(pagePath, prefix) {
			continue
		}
		rel := strings.TrimPrefix(pagePath, prefix)

		// ACL check: user permission + @ai permission.
		// ACL patterns expect canonical paths with leading /.
		aclPath := "/" + pagePath
		if s.aclStore != nil && !s.aclStore.CheckPermission(username, s.effectiveGroups(username), aclPath, "view") {
			continue
		}
		if s.aclStore != nil && !s.aclStore.CheckPermission("@ai", nil, aclPath, "view") {
			continue
		}

		if depth == 1 {
			// Only direct children.
			if strings.Contains(rel, "/") {
				// Count for sub-namespace.
				parts := strings.SplitN(rel, "/", 2)
				nsName := prefix + parts[0]
				nsCounts[nsName]++
				continue
			}
		}

		pi := pageInfo{
			Path:  "/" + pagePath,
			Title: p.Title,
		}

		if includeMeta {
			if page, err := s.store.Get(pagePath); err == nil {
				pi.Version = page.Meta.Version
				pi.LastModified = page.Meta.UpdatedAt.Format("2006-01-02T15:04:05Z")
				pi.Author = page.Meta.Author
			}
		}

		pages = append(pages, pi)
	}

	namespaces := []nsInfo{}
	for ns, count := range nsCounts {
		namespaces = append(namespaces, nsInfo{Path: "/" + ns, PageCount: count})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"namespace":  "/" + nsPath,
		"pages":      pages,
		"namespaces": namespaces,
	})
}

// handleAIBatchRead reads multiple pages in a single request.
// POST /api/ai/v1/batch/read
func (s *Server) handleAIBatchRead(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())

	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if len(req.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "paths array is required")
		return
	}
	if len(req.Paths) > 20 {
		writeError(w, http.StatusBadRequest, "maximum 20 paths per batch")
		return
	}

	type pageResult struct {
		Path     string `json:"path"`
		Title    string `json:"title,omitempty"`
		Markdown string `json:"markdown,omitempty"`
		Version  int64  `json:"version,omitempty"`
		OK       bool   `json:"ok"`
		Error    string `json:"error,omitempty"`
	}

	results := make([]pageResult, len(req.Paths))
	for i, p := range req.Paths {
		pagePath := strings.Trim(p, "/")
		results[i].Path = p

		// ACL check: user permission + @ai permission.
		// ACL patterns expect canonical paths with leading /.
		aclPath := "/" + pagePath
		if s.aclStore != nil && !s.aclStore.CheckPermission(username, s.effectiveGroups(username), aclPath, "view") {
			results[i].Error = "access denied"
			continue
		}
		if s.aclStore != nil && !s.aclStore.CheckPermission("@ai", nil, aclPath, "view") {
			results[i].Error = "AI access denied for this page"
			continue
		}

		page, err := s.store.Get(pagePath)
		if err != nil {
			results[i].Error = "page not found"
			continue
		}

		results[i].OK = true
		results[i].Title = markdown.ExtractTitle(page.Markdown)
		results[i].Markdown = page.Markdown
		results[i].Version = page.Meta.Version
	}

	writeJSON(w, http.StatusOK, map[string]any{"pages": results})
}

// handleAIPreview computes a diff without saving.
// POST /api/ai/v1/preview/*
func (s *Server) handleAIPreview(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	username := UsernameFromContext(r.Context())

	// ACL check: user permission + @ai permission.
	if s.aclStore != nil && !s.aclStore.CheckPermission(username, s.effectiveGroups(username), pagePath, "view") {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	if s.aclStore != nil && !s.aclStore.CheckPermission("@ai", nil, pagePath, "view") {
		writeError(w, http.StatusForbidden, "AI access denied for this page")
		return
	}

	var req struct {
		Markdown string `json:"markdown"`
		Summary  string `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	currentMarkdown := ""
	currentVersion := int64(0)
	page, err := s.store.Get(pagePath)
	if err == nil {
		currentMarkdown = page.Markdown
		currentVersion = page.Meta.Version
	}

	hunks := storage.DiffLines(currentMarkdown, req.Markdown)

	added := 0
	removed := 0
	for _, h := range hunks {
		switch h.Op {
		case "insert":
			added++
		case "delete":
			removed++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":            pagePath,
		"current_version": currentVersion,
		"diff": map[string]any{
			"added":   added,
			"removed": removed,
			"hunks":   hunks,
		},
		"warnings": []string{},
	})
}

// handleAIMeta returns structured metadata for a page.
// GET /api/ai/v1/meta/*
func (s *Server) handleAIMeta(w http.ResponseWriter, r *http.Request) {
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	username := UsernameFromContext(r.Context())

	// ACL check: user permission + @ai permission.
	if s.aclStore != nil && !s.aclStore.CheckPermission(username, s.effectiveGroups(username), pagePath, "view") {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	if s.aclStore != nil && !s.aclStore.CheckPermission("@ai", nil, pagePath, "view") {
		writeError(w, http.StatusForbidden, "AI access denied for this page")
		return
	}

	page, err := s.store.Get(pagePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "page not found")
		return
	}

	resp := map[string]any{
		"path":          "/" + pagePath,
		"title":         markdown.ExtractTitle(page.Markdown),
		"version":       page.Meta.Version,
		"last_modified": page.Meta.UpdatedAt,
		"author":        page.Meta.Author,
	}

	// Tags.
	if s.tagIndex != nil {
		normalizedPath := path.Clean("/" + pagePath)
		tags := s.tagIndex.GetTagsForPage(normalizedPath)
		resp["tags"] = tags
	}

	// Backlinks.
	if s.backlinkProvider != nil {
		backlinks := s.backlinkProvider.GetBacklinks(pagePath)
		resp["backlinks"] = backlinks
	}

	// Reviewflow status.
	if s.reviewflowService != nil {
		status, rfErr := s.reviewflowService.GetStatus(pagePath)
		if rfErr == nil && status != nil {
			resp["reviewflow"] = status
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleAIConventions returns the rules AI agents must follow.
// GET /api/ai/v1/conventions
func (s *Server) handleAIConventions(w http.ResponseWriter, _ *http.Request) {
	conventions := map[string]any{
		"dialect": map[string]any{
			"name": "Gowiki Markdown",
			"description": "A bijective Markdown dialect. One canonical syntax per node type. Round-trip lossless.",
			"rules": []string{
				"*italic* only — _italic_ is NOT italic, it is underline",
				"**bold** only — __bold__ is rejected",
				"_underline_ — produces underline, NOT italic",
				"~~strikethrough~~",
				"~subscript~, ^superscript^",
				"ATX headings only (# H1, ## H2, etc.) — setext headings rejected",
				"- item for unordered lists — * is rejected as list marker",
				"1. item for ordered lists",
				"Raw HTML is forbidden — < and > are plain characters",
				"HTML entities are not interpreted — use UTF-8 directly",
				"Single newline in a paragraph = hard line break (<br>)",
				"Trailing spaces have no meaning — two-space line break rule does not exist",
				"\\n literal = explicit hard break (valid in lists and tables only, not in paragraphs)",
				"No column alignment syntax in tables",
				"Numbered headings use 1. prefix: ## 1. Title (not a property directive)",
				"^[inline footnote content] — supports inline markdown inside",
			},
			"forbidden": []string{
				"Do NOT use _text_ for italic — it means underline",
				"Do NOT use __text__ for bold",
				"Do NOT use * as a list marker",
				"Do NOT use raw HTML tags",
				"Do NOT use HTML entities (e.g. &amp;) — use the UTF-8 character directly",
				"Do NOT use setext headings (underline-style)",
				"Do NOT use trailing spaces for line breaks",
				"Do NOT use multi-body tables (<tbody>)",
			},
			"directives": map[string]string{
				"syntax":             "{directivename key=value key2=\"value with spaces\"} on its own line",
				"self_contained":     "{reviewflow-link version=2.0} — stands alone as its own node",
				"prefix":             "{image size=500px} followed by the target block on the next line",
				"properties_example": "{reviewflow version=1.0 author=alice reviewer=bob}",
			},
		},
		"content_rules": map[string]any{
			"page_links": []string{
				"/path/to/page → content/path/to/page.md",
				"/path/to/namespace/ → content/path/to/namespace/index.md",
				"./page → adjacent page.md relative to current page",
			},
			"attachment_links": []string{
				"Attachments must have a file extension",
				"/path/to/file.ext → content/path/to/file.ext",
				"./page.md is the raw attachment; ./page is the rendered page",
			},
			"namespace_constraint": "If content/path/to/ns/ directory exists, content/path/to/ns.md must NOT exist",
			"metadata_location":    "data/meta/ mirrors content/ structure, with .json extension instead of .md",
		},
		"conventions": map[string]any{
			"summary_format":    "[AI: <tool_name>] <description of change>",
			"summary_example":   "[AI: Claude] Translate section 3 to English",
			"summary_required":  "Summary is required for all token-authenticated writes",
			"optimistic_locking": "Always read the page first, then write with expected_version set to the version you read",
			"user_agent":        "Set User-Agent: <tool>/1.0 (gowiki-ai-api; user=<username>)",
			"authentication":    "Preferred: Authorization: Bearer gwk_<token> header. Fallback: ?token=gwk_<token> query parameter (for platforms that cannot set custom headers).",
		},
		"quality_checks": map[string]any{
			"render_endpoint":  "GET /api/render/{path} — returns rendered HTML via headless browser",
			"render_usage":     "Use after writing a page to verify it renders correctly. Check the 'errors' array and look for '⚠ Directive' in the HTML to detect broken formatting.",
			"render_note":      "The rendered HTML is a derived product of the canonical markdown source. Markdown is always the ground truth.",
			"systematic_check": "Combine GET /api/sitemap with GET /api/render/{path} to scan all pages for rendering errors.",
		},
		"do_not": []string{
			"Do not introduce alternative Markdown syntaxes — bijectivity is non-negotiable",
			"Do not store metadata under data/content/",
			"Do not create extension-less files under data/content/",
			"Do not create content/path/to/ns.md if content/path/to/ns/ exists",
			"Do not silently change document structure — preserve existing formatting",
			"Do not remove or reformat content you were not asked to change",
		},
	}
	writeJSON(w, http.StatusOK, conventions)
}

// effectiveGroups returns the effective groups for a username.
func (s *Server) effectiveGroups(username string) []string {
	if username == "" || s.userStore == nil {
		return nil
	}
	user, err := s.userStore.Get(username)
	if err != nil {
		return nil
	}
	return user.EffectiveGroups()
}
