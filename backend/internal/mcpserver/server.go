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
