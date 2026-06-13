package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"

	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/database"
	"gowiki/backend/internal/markdown"
	"gowiki/backend/internal/storage"
	"gowiki/backend/internal/todo"
)

func registerTools(srv *mcpsrv.MCPServer, deps Deps) {
	registerConventionsTool(srv, deps)
	registerListNamespaceTool(srv, deps)
	registerReadPagesBatchTool(srv, deps)
	registerGetPageMetaTool(srv, deps)
	registerSearchPagesTool(srv, deps)
	registerGetReviewflowStatusTool(srv, deps)
	registerPreviewPageDiffTool(srv, deps)
	registerWritePageTool(srv, deps)
	registerListTodosTool(srv, deps)
	registerCompleteTodoTool(srv, deps)
	registerListDatabaseTablesTool(srv, deps)
	registerQueryDatabaseRowsTool(srv, deps)
}

// ── ACL helpers ─────────────────────────────────────────────────────────

// canView enforces the dual-ACL model: the authenticated user plus the @ai
// pseudo-subject must both have view permission on the page.
func (d Deps) canView(ctx context.Context, pagePath string) bool {
	if d.ACL == nil {
		return true
	}
	username := d.ExtractUsername(ctx)
	aclPath := "/" + strings.TrimPrefix(pagePath, "/")
	if !d.ACL.CheckPermission(username, d.effectiveGroups(username), aclPath, "view") {
		return false
	}
	return d.ACL.CheckAIPermission(aclPath, "view")
}

// canEdit mirrors canView but for edit permission.
func (d Deps) canEdit(ctx context.Context, pagePath string) bool {
	if d.ACL == nil {
		return true
	}
	username := d.ExtractUsername(ctx)
	aclPath := "/" + strings.TrimPrefix(pagePath, "/")
	if !d.ACL.CheckPermission(username, d.effectiveGroups(username), aclPath, "edit") {
		return false
	}
	return d.ACL.CheckAIPermission(aclPath, "edit")
}

func (d Deps) effectiveGroups(username string) []string {
	if username == "" || d.UserStore == nil {
		return nil
	}
	u, err := d.UserStore.Get(username)
	if err != nil {
		return nil
	}
	return u.EffectiveGroups()
}

// jsonResult marshals v to JSON and wraps it as an MCP text result.
func jsonResult(v any) *mcpgo.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult("internal: failed to marshal response: " + err.Error())
	}
	return textResult(string(b))
}

// ── get_conventions ─────────────────────────────────────────────────────

func registerConventionsTool(srv *mcpsrv.MCPServer, _ Deps) {
	tool := mcpgo.NewTool("get_conventions",
		mcpgo.WithDescription(
			"Return the Gowiki Markdown dialect rules, content guidelines, and write conventions. "+
				"CALL THIS BEFORE your first content read or write — the dialect rejects several common "+
				"CommonMark constructs (underscore is underline, not italic; * is not a list marker; raw HTML is forbidden).",
		),
	)
	srv.AddTool(tool, func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return jsonResult(conventionsPayload()), nil
	})
}

// ── list_namespace ──────────────────────────────────────────────────────

func registerListNamespaceTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("list_namespace",
		mcpgo.WithDescription(
			"List the pages and sub-namespaces under a given namespace path. "+
				"Use depth=0 for a recursive listing, depth=1 for direct children only.",
		),
		mcpgo.WithString("path",
			mcpgo.Description("Namespace path, leading slash optional. Empty or '/' lists the root."),
		),
		mcpgo.WithNumber("depth",
			mcpgo.Description("1 = direct children only (default). 0 = unlimited recursion."),
		),
		mcpgo.WithBoolean("include_meta",
			mcpgo.Description("Include version, last_modified, author for each page."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.Sitemap == nil {
			return errorResult("namespace listing not available"), nil
		}
		nsPath := strings.Trim(req.GetString("path", ""), "/")
		depth := req.GetInt("depth", 1)
		includeMeta := req.GetBool("include_meta", false)

		allPages, err := deps.Sitemap.ListAllPages()
		if err != nil {
			return errorResult("list pages: " + err.Error()), nil
		}

		prefix := nsPath
		if prefix != "" {
			prefix += "/"
		}

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

		for _, p := range allPages {
			pagePath := strings.TrimPrefix(p.Path, "/")
			if prefix != "" && !strings.HasPrefix(pagePath, prefix) {
				continue
			}
			rel := strings.TrimPrefix(pagePath, prefix)

			if !deps.canView(ctx, pagePath) {
				continue
			}

			if depth == 1 && strings.Contains(rel, "/") {
				parts := strings.SplitN(rel, "/", 2)
				nsName := prefix + parts[0]
				nsCounts[nsName]++
				continue
			}

			pi := pageInfo{Path: "/" + pagePath, Title: p.Title}
			if includeMeta && deps.Store != nil {
				if page, err := deps.Store.Get(pagePath); err == nil {
					pi.Version = page.Meta.Version
					pi.LastModified = page.Meta.UpdatedAt.Format(time.RFC3339)
					pi.Author = page.Meta.Author
				}
			}
			pages = append(pages, pi)
		}

		namespaces := []nsInfo{}
		for ns, count := range nsCounts {
			namespaces = append(namespaces, nsInfo{Path: "/" + ns, PageCount: count})
		}

		return jsonResult(map[string]any{
			"namespace":  "/" + nsPath,
			"pages":      pages,
			"namespaces": namespaces,
		}), nil
	})
}

// ── read_pages_batch ────────────────────────────────────────────────────

func registerReadPagesBatchTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("read_pages_batch",
		mcpgo.WithDescription(
			"Read up to 20 pages in a single call. Returns the raw Markdown, title, and current version for each. "+
				"Always prefer this over multiple single-page reads.",
		),
		mcpgo.WithArray("paths",
			mcpgo.Required(),
			mcpgo.Description("Page paths (leading slash optional). Maximum 20."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		paths := req.GetStringSlice("paths", nil)
		if len(paths) == 0 {
			return errorResult("paths is required"), nil
		}
		if len(paths) > 20 {
			return errorResult("maximum 20 paths per batch"), nil
		}
		if deps.Store == nil {
			return errorResult("page store not available"), nil
		}

		type pageResult struct {
			Path     string `json:"path"`
			Title    string `json:"title,omitempty"`
			Markdown string `json:"markdown,omitempty"`
			Version  int64  `json:"version,omitempty"`
			OK       bool   `json:"ok"`
			Error    string `json:"error,omitempty"`
		}

		results := make([]pageResult, len(paths))
		for i, p := range paths {
			pagePath := strings.Trim(p, "/")
			results[i].Path = p

			if !deps.canView(ctx, pagePath) {
				results[i].Error = "access denied"
				continue
			}

			page, err := deps.Store.Get(pagePath)
			if err != nil {
				results[i].Error = "page not found"
				continue
			}
			results[i].OK = true
			results[i].Title = markdown.ExtractTitle(page.Markdown)
			results[i].Markdown = page.Markdown
			results[i].Version = page.Meta.Version
		}

		return jsonResult(map[string]any{"pages": results}), nil
	})
}

// ── get_page_meta ───────────────────────────────────────────────────────

func registerGetPageMetaTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("get_page_meta",
		mcpgo.WithDescription(
			"Return structured metadata for a page: title, version, last_modified, author, tags, backlinks, reviewflow status, "+
				"and edit state. If a `lock` field is present, a user is actively editing the page; if a `draft` field is "+
				"present, unpublished work exists (lock and draft are independent — drafts can outlive their lock). "+
				"write_page will refuse while either is present.",
		),
		mcpgo.WithString("path",
			mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		pagePath := strings.TrimSpace(req.GetString("path", ""))
		pagePath = strings.TrimPrefix(pagePath, "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		if !deps.canView(ctx, pagePath) {
			return errorResult("access denied"), nil
		}
		if deps.Store == nil {
			return errorResult("page store not available"), nil
		}
		page, err := deps.Store.Get(pagePath)
		if err != nil {
			return errorResult("page not found"), nil
		}

		resp := map[string]any{
			"path":          "/" + pagePath,
			"title":         markdown.ExtractTitle(page.Markdown),
			"version":       page.Meta.Version,
			"last_modified": page.Meta.UpdatedAt,
			"author":        page.Meta.Author,
		}
		if deps.TagIndex != nil {
			resp["tags"] = deps.TagIndex.GetTagsForPage(path.Clean("/" + pagePath))
		}
		if deps.Backlinks != nil {
			resp["backlinks"] = deps.Backlinks.GetBacklinks(pagePath)
		}
		if deps.Reviewflow != nil {
			if status, err := deps.Reviewflow.GetStatus(pagePath); err == nil && status != nil {
				resp["reviewflow"] = status
			}
		}
		if deps.DraftState != nil {
			if lock := deps.DraftState.GetLock(pagePath); lock.Owner != "" {
				resp["lock"] = map[string]any{
					"owner": lock.Owner,
					"since": lock.Since,
				}
			}
			if draft, ok := deps.DraftState.FindAnyDraft(pagePath); ok {
				resp["draft"] = map[string]any{
					"owner": draft.Owner,
					"since": draft.Since,
				}
			}
		}
		return jsonResult(resp), nil
	})
}

// ── search_pages ────────────────────────────────────────────────────────

func registerSearchPagesTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("search_pages",
		mcpgo.WithDescription(
			"Search wiki pages. Use `query` for full-text search (typo-tolerant, ranked, returns snippets) "+
				"or `tag` to list every page bearing a given tag. The two can be combined: when both are set, "+
				"the tag-tagged pages are narrowed to those whose path or title contains `query` (case-insensitive). "+
				"At least one of `query` or `tag` is required. All results respect the caller's ACL.",
		),
		mcpgo.WithString("query",
			mcpgo.Description("Full-text query string. Typo-tolerant. Required unless `tag` is set."),
		),
		mcpgo.WithString("tag",
			mcpgo.Description("Tag name to filter by. When set, results come from the tag index (no snippets)."),
		),
		mcpgo.WithNumber("limit",
			mcpgo.Description("Maximum results to return (default 20, max 100)."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		query := strings.TrimSpace(req.GetString("query", ""))
		tag := strings.TrimSpace(req.GetString("tag", ""))
		if query == "" && tag == "" {
			return errorResult("either 'query' or 'tag' must be provided"), nil
		}
		limit := req.GetInt("limit", 20)
		if limit < 1 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}

		// Tag branch: take pages from the tag index, narrow by query if both are set.
		if tag != "" {
			if deps.TagIndex == nil {
				return errorResult("tag index not available"), nil
			}
			entries := deps.TagIndex.GetPagesForTag(tag, "", nil)
			needle := strings.ToLower(query)
			out := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				if !deps.canView(ctx, e.Path) {
					continue
				}
				if needle != "" {
					if !strings.Contains(strings.ToLower(e.Path), needle) &&
						!strings.Contains(strings.ToLower(e.Title), needle) {
						continue
					}
				}
				out = append(out, map[string]any{
					"path":  e.Path,
					"title": e.Title,
				})
				if len(out) >= limit {
					break
				}
			}
			return jsonResult(map[string]any{"results": out, "tag": tag}), nil
		}

		// Full-text branch.
		if deps.Search == nil {
			return errorResult("search not available"), nil
		}
		results, err := deps.Search.Search(query, limit)
		if err != nil {
			return errorResult("search failed: " + err.Error()), nil
		}
		filtered := results[:0]
		for _, r := range results {
			if deps.canView(ctx, r.Path) {
				filtered = append(filtered, r)
			}
		}
		return jsonResult(map[string]any{"results": filtered}), nil
	})
}

// ── get_reviewflow_status ───────────────────────────────────────────────

func registerGetReviewflowStatusTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("get_reviewflow_status",
		mcpgo.WithDescription(
			"Return the reviewflow state of a page: configured roles, confirmations on the current page version, and validation status.",
		),
		mcpgo.WithString("path",
			mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.Reviewflow == nil {
			return errorResult("reviewflow service not available"), nil
		}
		pagePath := strings.TrimPrefix(strings.TrimSpace(req.GetString("path", "")), "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		if !deps.canView(ctx, pagePath) {
			return errorResult("access denied"), nil
		}
		status, err := deps.Reviewflow.GetStatus(pagePath)
		if err != nil {
			return errorResult("reviewflow: " + err.Error()), nil
		}
		return jsonResult(status), nil
	})
}

// ── preview_page_diff ───────────────────────────────────────────────────

func registerPreviewPageDiffTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("preview_page_diff",
		mcpgo.WithDescription(
			"Dry-run a page edit: returns the diff (hunks, added/removed line counts) without saving. "+
				"Use this to show the user what write_page would do before committing.",
		),
		mcpgo.WithString("path", mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional."),
		),
		mcpgo.WithString("markdown", mcpgo.Required(),
			mcpgo.Description("Proposed new markdown in the Gowiki dialect."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		pagePath := strings.TrimPrefix(strings.TrimSpace(req.GetString("path", "")), "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		newMarkdown, err := req.RequireString("markdown")
		if err != nil {
			return errorResult("markdown is required"), nil
		}
		if !deps.canView(ctx, pagePath) {
			return errorResult("access denied"), nil
		}
		currentMarkdown := ""
		currentVersion := int64(0)
		if deps.Store != nil {
			if page, err := deps.Store.Get(pagePath); err == nil {
				currentMarkdown = page.Markdown
				currentVersion = page.Meta.Version
			}
		}
		hunks := storage.DiffLines(currentMarkdown, newMarkdown)
		added, removed := 0, 0
		for _, h := range hunks {
			switch h.Op {
			case "insert":
				added++
			case "delete":
				removed++
			}
		}
		return jsonResult(map[string]any{
			"path":            "/" + pagePath,
			"current_version": currentVersion,
			"diff": map[string]any{
				"added":   added,
				"removed": removed,
				"hunks":   hunks,
			},
		}), nil
	})
}

// ── write_page ──────────────────────────────────────────────────────────

func registerWritePageTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("write_page",
		mcpgo.WithDescription(
			"Create or update a page. Requires edit permission for the caller AND the @ai subject. "+
				"The summary must start with '[AI: <tool>]' (e.g. '[AI: Claude] Translate section 3'). "+
				"Always call preview_page_diff first and show the diff to the user.",
		),
		mcpgo.WithString("path", mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional. Namespace indexes end with '/'."),
		),
		mcpgo.WithString("markdown", mcpgo.Required(),
			mcpgo.Description("New markdown content in the Gowiki dialect."),
		),
		mcpgo.WithString("summary", mcpgo.Required(),
			mcpgo.Description("Change summary. Required for audit. Format: '[AI: <tool-name>] <description>'."),
		),
		mcpgo.WithNumber("expected_version",
			mcpgo.Description("Optional optimistic lock. If set, the write fails when the current page version differs."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		pagePath := strings.TrimPrefix(strings.TrimSpace(req.GetString("path", "")), "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		newMarkdown, err := req.RequireString("markdown")
		if err != nil {
			return errorResult("markdown is required"), nil
		}
		summary := strings.TrimSpace(req.GetString("summary", ""))
		if deps.RequireSummary && summary == "" {
			return errorResult("summary is required — format '[AI: <tool>] <description>'"), nil
		}
		expectedVersion := int64(req.GetInt("expected_version", 0))

		if deps.Store == nil {
			return errorResult("page store not available"), nil
		}
		if !deps.canEdit(ctx, pagePath) {
			return errorResult("edit permission denied"), nil
		}

		if deps.DraftState != nil {
			if lock := deps.DraftState.GetLock(pagePath); lock.Owner != "" {
				return errorResult(fmt.Sprintf(
					"page is locked by %s (since %s) — wait for them to finish editing before writing",
					lock.Owner, lock.Since,
				)), nil
			}
			if draft, ok := deps.DraftState.FindAnyDraft(pagePath); ok {
				return errorResult(fmt.Sprintf(
					"page has an unpublished draft by %s (since %s) — wait for it to be published or discarded before writing",
					draft.Owner, draft.Since,
				)), nil
			}
		}

		if expectedVersion > 0 {
			if page, err := deps.Store.Get(pagePath); err == nil {
				if page.Meta.Version != expectedVersion {
					return errorResult(fmt.Sprintf(
						"version conflict: page is at %d, expected %d — re-read before writing",
						page.Meta.Version, expectedVersion,
					)), nil
				}
			}
		}

		username := deps.ExtractUsername(ctx)
		author := username
		if summary != "" {
			author = author + " | " + summary
		}
		existedBefore := deps.Store.Exists(pagePath)
		result, err := deps.Store.Put(pagePath, newMarkdown, author)
		if err != nil {
			return errorResult("write failed: " + err.Error()), nil
		}
		return jsonResult(map[string]any{
			"path":    "/" + pagePath,
			"version": result.Page.Meta.Version,
			"created": !existedBefore,
			"updated": existedBefore,
		}), nil
	})
}

// ── list_todos ──────────────────────────────────────────────────────────

func registerListTodosTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("list_todos",
		mcpgo.WithDescription(
			"List todo tasks with optional filters. Fields: id, title, status, assignee, due_date, priority, source_page, tags.",
		),
		mcpgo.WithString("status",
			mcpgo.Description("Comma-separated: open,in_progress,done,cancelled. Default: open,in_progress."),
		),
		mcpgo.WithString("assignee",
			mcpgo.Description("Username or group name to filter by."),
		),
		mcpgo.WithString("page",
			mcpgo.Description("Exact page path to filter by."),
		),
		mcpgo.WithString("page_prefix",
			mcpgo.Description("Namespace prefix (e.g. '/projects/') to scope results."),
		),
		mcpgo.WithString("tag",
			mcpgo.Description("Filter by tag substring."),
		),
		mcpgo.WithString("due_before",
			mcpgo.Description("Only tasks due on or before YYYY-MM-DD."),
		),
		mcpgo.WithString("priority",
			mcpgo.Description("Comma-separated priorities: low, normal, high, urgent."),
		),
		mcpgo.WithNumber("limit",
			mcpgo.Description("Max tasks to return (default 50, max 100)."),
		),
	)
	srv.AddTool(tool, func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.Todo == nil {
			return errorResult("todo plugin not available"), nil
		}
		opts := todo.ListOptions{
			Status:     todo.Status(req.GetString("status", "")),
			Assignee:   req.GetString("assignee", ""),
			Page:       req.GetString("page", ""),
			PagePrefix: req.GetString("page_prefix", ""),
			Tag:        req.GetString("tag", ""),
			DueBefore:  req.GetString("due_before", ""),
			Priority:   todo.Priority(req.GetString("priority", "")),
			Limit:      req.GetInt("limit", 50),
		}
		tasks, cursor, err := deps.Todo.Store().List(context.Background(), opts)
		if err != nil {
			return errorResult("list todos: " + err.Error()), nil
		}
		return jsonResult(map[string]any{
			"tasks":  tasks,
			"cursor": cursor,
		}), nil
	})
}

// ── complete_todo ───────────────────────────────────────────────────────

func registerCompleteTodoTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("complete_todo",
		mcpgo.WithDescription("Mark a todo task as done on behalf of the authenticated user."),
		mcpgo.WithString("id", mcpgo.Required(),
			mcpgo.Description("Task ID."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.Todo == nil {
			return errorResult("todo plugin not available"), nil
		}
		id, err := req.RequireString("id")
		if err != nil {
			return errorResult("id is required"), nil
		}
		username := deps.ExtractUsername(ctx)
		var resolver todo.GroupResolver
		if deps.UserStore != nil {
			resolver = &mcpGroupResolver{userStore: deps.UserStore}
		}
		task, err := deps.Todo.CompleteTask(context.Background(), id, username, resolver)
		if err != nil {
			return errorResult("complete: " + err.Error()), nil
		}
		return jsonResult(task), nil
	})
}

// mcpGroupResolver is a minimal GroupResolver that iterates the UserStore.
// It mirrors the authGroupResolver used by the todo HTTP handlers so that
// "all members" group completion works identically over MCP.
type mcpGroupResolver struct {
	userStore *auth.UserStore
}

func (r *mcpGroupResolver) GroupMembers(groupName string) []string {
	if r.userStore == nil {
		return nil
	}
	users := r.userStore.List()
	var members []string
	for _, u := range users {
		for _, g := range u.EffectiveGroups() {
			if g == groupName {
				members = append(members, u.Username)
				break
			}
		}
	}
	return members
}

// ── list_database_tables ────────────────────────────────────────────────

func registerListDatabaseTablesTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("list_database_tables",
		mcpgo.WithDescription(
			"List all structured-data tables and their field definitions. Use this to discover queryable tables before calling query_database_rows.",
		),
	)
	srv.AddTool(tool, func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.SchemaStore == nil {
			return errorResult("database not connected"), nil
		}
		tables, err := deps.SchemaStore.ListTables(context.Background())
		if err != nil {
			return errorResult("list tables: " + err.Error()), nil
		}
		out := make([]map[string]any, 0, len(tables))
		for _, t := range tables {
			full, err := deps.SchemaStore.GetTable(context.Background(), t.ID)
			if err != nil {
				continue
			}
			out = append(out, map[string]any{
				"name":   t.Name,
				"label":  t.Label,
				"fields": full.Fields,
			})
		}
		return jsonResult(map[string]any{"tables": out}), nil
	})
}

// ── query_database_rows ─────────────────────────────────────────────────

func registerQueryDatabaseRowsTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("query_database_rows",
		mcpgo.WithDescription(
			"Query rows from a structured-data table. Supports field filters, sorting, and pagination. "+
				"First call list_database_tables to discover table names and field definitions.",
		),
		mcpgo.WithString("table", mcpgo.Required(),
			mcpgo.Description("Table name."),
		),
		mcpgo.WithString("sort",
			mcpgo.Description("Field name to sort by."),
		),
		mcpgo.WithString("order",
			mcpgo.Description("asc or desc (default asc)."),
		),
		mcpgo.WithNumber("limit",
			mcpgo.Description("Max rows (default 50, max 500)."),
		),
		mcpgo.WithNumber("offset",
			mcpgo.Description("Pagination offset (default 0)."),
		),
	)
	srv.AddTool(tool, func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.DataStore == nil {
			return errorResult("database not connected"), nil
		}
		tableName, err := req.RequireString("table")
		if err != nil {
			return errorResult("table is required"), nil
		}
		limit := req.GetInt("limit", 50)
		if limit > 500 {
			limit = 500
		}
		if limit < 1 {
			limit = 50
		}
		order := req.GetString("order", "asc")
		if order != "asc" && order != "desc" {
			order = "asc"
		}
		params := database.QueryParams{
			Sort:   req.GetString("sort", ""),
			Order:  order,
			Limit:  limit,
			Offset: req.GetInt("offset", 0),
		}
		rows, total, err := deps.DataStore.QueryRows(context.Background(), tableName, params)
		if err != nil {
			return errorResult("query: " + err.Error()), nil
		}
		return jsonResult(map[string]any{
			"rows":  rows,
			"total": total,
		}), nil
	})
}
