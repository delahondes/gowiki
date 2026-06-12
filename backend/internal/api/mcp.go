package api

import (
	"net/http"

	"gowiki/backend/internal/mcpserver"
)

// buildMCPHandler wires the MCP server with dependencies drawn from the
// api.Server. The returned http.Handler speaks MCP Streamable HTTP and
// expects to receive requests already authenticated by requireAuth.
func (s *Server) buildMCPHandler() http.Handler {
	var sitemap mcpserver.SitemapLister
	if lister, ok := s.store.(SitemapLister); ok {
		sitemap = lister
	}

	deps := mcpserver.Deps{
		Store:           s.store, // api.PageStore is a superset of mcpserver.PageStore
		Sitemap:         sitemap,
		Search:          s.searchStore,
		ACL:             s.aclStore,
		UserStore:       s.userStore,
		Backlinks:       s.backlinkProvider,
		TagIndex:        s.tagIndex,
		Reviewflow:      s.reviewflowService,
		DraftState:      s.draftManager,
		Todo:            s.todoService,
		SchemaStore:     s.schemaStore,
		DataStore:       s.dataStore,
		ExtractUsername: UsernameFromContext,
		RequireSummary:  s.configStore != nil && s.configStore.Get().AIAPI.RequireSummary,
	}
	return mcpserver.NewHandler(deps)
}
