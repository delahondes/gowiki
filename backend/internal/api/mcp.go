package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"gowiki/backend/internal/database"
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
	var mover mcpserver.PageMover
	if m, ok := s.store.(PageMover); ok {
		mover = m
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
		Attic:           s.atticStore,
		Changelog:       s.changelog,
		Mover:           mover,
		RowWriter:       &mcpRowWriter{s: s},
		ExtractUsername: UsernameFromContext,
		RequireSummary:  s.configStore != nil && s.configStore.Get().AIAPI.RequireSummary,
	}
	return mcpserver.NewHandler(deps)
}

// mcpRowWriter adapts the API Server's row-write internals to the
// mcpserver.RowWriter interface — reuses resolvePageFolder, buildPageContent,
// store.Put, store.Delete, so MCP writes go through the same code path as the
// HTTP handlers.
type mcpRowWriter struct{ s *Server }

func (w *mcpRowWriter) InsertRowWithPage(ctx context.Context, tableName string, fields map[string]any, author string) (*mcpserver.RowInsertResult, error) {
	if w.s.dataStore == nil || w.s.schemaStore == nil {
		return nil, errors.New("database not connected")
	}
	table, err := w.s.schemaStore.GetTableByName(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("table lookup: %w", err)
	}
	row := database.Row{Fields: fields}
	if err := w.s.dataStore.InsertRow(ctx, tableName, &row); err != nil {
		return nil, fmt.Errorf("insert row: %w", err)
	}
	result := &mcpserver.RowInsertResult{Row: &row}
	if table.PageFolder == "" {
		return result, nil
	}
	pagePath := resolvePageFolder(table.PageFolder, row.ID, row.Fields)
	if err := w.s.dataStore.UpdatePagePath(ctx, tableName, row.ID, pagePath); err != nil {
		return result, fmt.Errorf("set page_path: %w", err)
	}
	row.PagePath = pagePath
	markdown := w.s.buildPageContent(table, &row)
	if _, err := w.s.store.Put(pagePath, markdown, author); err != nil {
		return result, fmt.Errorf("write bound page: %w", err)
	}
	result.PagePath = pagePath
	result.PageCreated = true
	return result, nil
}

func (w *mcpRowWriter) DeleteRowWithPage(ctx context.Context, tableName string, rowID int, author string) (*mcpserver.RowDeleteResult, error) {
	if w.s.dataStore == nil {
		return nil, errors.New("database not connected")
	}
	row, err := w.s.dataStore.GetRow(ctx, tableName, rowID)
	if err != nil {
		return nil, fmt.Errorf("row lookup: %w", err)
	}
	result := &mcpserver.RowDeleteResult{TableName: tableName, RowID: rowID}
	if row.PagePath != "" {
		result.PagePath = row.PagePath
		if _, err := w.s.store.Delete(row.PagePath, author); err != nil {
			// Log but don't fail the whole delete — a missing page is fine,
			// and other page-store errors shouldn't block the row cleanup.
			log.Printf("mcp: delete bound page %s for %s#%d: %v", row.PagePath, tableName, rowID, err)
		} else {
			result.PageDeleted = true
		}
	}
	if err := w.s.dataStore.DeleteRow(ctx, tableName, rowID); err != nil {
		return result, fmt.Errorf("delete row: %w", err)
	}
	return result, nil
}
