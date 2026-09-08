// Package mcpserver exposes the Gowiki content and data APIs via the
// Model Context Protocol (MCP) over Streamable HTTP. It reuses the existing
// authentication and ACL enforcement from the HTTP API — clients authenticate
// with the same API tokens, and every tool/resource call is ACL-checked
// against both the authenticated user and the special `@ai` subject.
package mcpserver

import (
	"context"
	"net/http"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"

	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/database"
	"gowiki/backend/internal/reviewflow"
	"gowiki/backend/internal/storage"
	"gowiki/backend/internal/todo"
)

const (
	serverName    = "gowiki-mcp"
	serverVersion = "1.0.0"
)

// PageStore is the minimal page storage surface needed by MCP tools.
type PageStore interface {
	Get(pagePath string) (storage.Page, error)
	Put(pagePath, markdown, author string) (storage.PutResult, error)
	Delete(pagePath, author string) (storage.DeleteResult, error)
	Exists(pagePath string) bool
}

// SitemapLister lists every page known to the wiki.
type SitemapLister interface {
	ListAllPages() ([]storage.PageEntry, error)
}

// SearchStore provides full-text search.
type SearchStore interface {
	Search(query string, limit int) ([]storage.SearchResult, error)
}

// BacklinkProvider returns backlinks for a page.
type BacklinkProvider interface {
	GetBacklinks(pagePath string) []string
}

// AtticStore exposes the archived-version log for a page. Backed by the
// filesystem attic; used by list_page_history, read_page_version and
// diff_page_versions.
type AtticStore interface {
	ListVersions(pagePath string) ([]storage.AtticEntry, error)
	ReadVersion(pagePath string, version int64) ([]byte, error)
}

// PageMover is the rename/convert surface used by the move_page family. The
// concrete FileStore implements it; api.PageMover is the parallel interface
// on the API side. Both mirror the same underlying operations.
type PageMover interface {
	Move(oldPath, newPath string, moveMedia, updateLinks bool, author string) (storage.MoveResult, error)
	ConvertToNamespaceIndex(pagePath, author string) (storage.MoveResult, error)
	ConvertToRegularPage(pagePath, author string) (storage.MoveResult, error)
	PreviewMove(oldPath, newPath string, moveMedia bool) (storage.MovePreview, error)
}

// RowInsertResult is what InsertRowWithPage returns.
type RowInsertResult struct {
	Row      *database.Row `json:"row"`
	PagePath string        `json:"page_path,omitempty"`
	PageCreated bool       `json:"page_created"`
}

// RowDeleteResult is what DeleteRowWithPage returns.
type RowDeleteResult struct {
	TableName   string `json:"table"`
	RowID       int    `json:"row_id"`
	PagePath    string `json:"page_path,omitempty"`
	PageDeleted bool   `json:"page_deleted"`
}

// RowWriter exposes symmetric row insert/delete operations that also
// create / archive the row's bound page when the table has a page_folder.
// Implemented in api/mcp.go over Server internals (resolvePageFolder,
// buildPageContent, store.Put, store.Delete) so both the MCP tools and
// the existing HTTP handlers share one code path.
type RowWriter interface {
	// InsertRowWithPage inserts a row and, for page-bound tables, creates
	// the associated wiki page.
	InsertRowWithPage(ctx context.Context, tableName string, fields map[string]any, author string) (*RowInsertResult, error)
	// DeleteRowWithPage deletes a row and, if the row had a bound page,
	// deletes that page too (archiving it to the attic so the audit trail
	// remains intact).
	DeleteRowWithPage(ctx context.Context, tableName string, rowID int, author string) (*RowDeleteResult, error)
}

// ChangelogReader exposes the append-only global change log. Used by
// list_recent_changes for cross-page audit queries.
type ChangelogReader interface {
	Read(opts storage.ReadOptions) ([]storage.ChangeEntry, error)
}

// DraftStateProvider exposes draft and lock state. The MCP layer uses it to
// surface pending edits in get_page_meta and to refuse external writes that
// would race against an in-progress edit or clobber unpublished work. The
// lock and the draft file are independent: a lock without a draft is
// transient (cleaned up), and a draft without a lock is an orphan (admin
// cleared the lock or a session crashed).
type DraftStateProvider interface {
	GetLock(pagePath string) storage.DraftLock
	FindAnyDraft(pagePath string) (storage.DraftInfo, bool)
}

// UsernameExtractor pulls the authenticated username from a request context.
// The MCP handler is mounted behind the existing auth middleware, so the
// caller wires this to the same helper the HTTP API uses.
type UsernameExtractor func(ctx context.Context) string

// Deps is the bag of dependencies the MCP server needs. All fields are
// optional except Store, ACL, UserStore, and ExtractUsername. Missing
// optional deps cause the matching tools/resources to return a clear error.
type Deps struct {
	Store             PageStore
	Sitemap           SitemapLister
	Search            SearchStore
	ACL               *auth.ACLStore
	UserStore         *auth.UserStore
	Backlinks         BacklinkProvider
	TagIndex          *storage.TagIndex
	Reviewflow        *reviewflow.Service
	DraftState        DraftStateProvider
	Todo              *todo.TodoService
	SchemaStore       *database.SchemaStore
	DataStore         *database.DataStore
	Attic             AtticStore
	Changelog         ChangelogReader
	Mover             PageMover
	RowWriter         RowWriter
	ExtractUsername   UsernameExtractor
	RequireSummary    bool // when true, write_page rejects calls without a summary
}

// NewHandler builds an http.Handler that speaks MCP Streamable HTTP. Mount
// it under a route protected by the same auth middleware the AI API uses.
func NewHandler(deps Deps) http.Handler {
	if deps.ExtractUsername == nil {
		deps.ExtractUsername = func(context.Context) string { return "" }
	}

	srv := mcpsrv.NewMCPServer(
		serverName,
		serverVersion,
		mcpsrv.WithToolCapabilities(true),
		mcpsrv.WithResourceCapabilities(true, false),
		mcpsrv.WithPromptCapabilities(false),
		mcpsrv.WithInstructions(serverInstructions),
	)

	registerTools(srv, deps)
	registerResources(srv, deps)
	registerPrompts(srv, deps)

	return mcpsrv.NewStreamableHTTPServer(srv)
}

// serverInstructions is the human-readable prologue the MCP client shows to
// the LLM before calling any tool. Keep it short — the detailed rules live
// in the `get_conventions` tool.
const serverInstructions = `Gowiki wiki MCP server.

Before making any content edit, call the get_conventions tool once to load the
Markdown dialect rules and content guidelines. The dialect is bijective and
rejects several common CommonMark constructs (use *italic* not _italic_, and
_underline_ is NOT italic). Every tool that writes content requires a summary
of the form "[AI: <tool>] <description>".

Page paths are canonical: they start with "/", end with "/" for namespace
indexes, and never contain "/index". Attachments must have a file extension.

Resources expose pages as wiki:///path URIs. You can either list_resources and
read_resource, or use the read_pages_batch tool — the latter is faster for
reading several pages at once.`

// textResult returns an MCP tool result containing a single text block.
func textResult(text string) *mcpgo.CallToolResult {
	return mcpgo.NewToolResultText(text)
}

// errorResult returns an MCP tool result marked as an error.
func errorResult(msg string) *mcpgo.CallToolResult {
	return mcpgo.NewToolResultError(msg)
}
