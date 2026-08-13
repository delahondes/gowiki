package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/database"
)

// handleDatabaseStatus returns the current database connection status.
// GET /api/admin/database/status
func (s *Server) handleDatabaseStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := s.configStore.Get()
	connected := false
	if s.dbPool != nil {
		connected = s.dbPool.IsConnected()
	}
	resp := map[string]any{
		"connected":      connected,
		"dsn_configured": cfg.Database.DSN != "",
		"enabled":        cfg.Database.Enabled,
	}
	if connected && s.todoService == nil {
		resp["restart_required"] = true
		resp["restart_message"] = "Server restart required to activate the todo plugin."
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDatabaseTest tests a DSN without saving it.
// POST /api/admin/database/test
func (s *Server) handleDatabaseTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DSN string `json:"dsn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.DSN == "" {
		writeError(w, http.StatusBadRequest, "dsn is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := database.TestConnection(ctx, req.DSN); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// handleDatabaseConnect reconnects the pool using the config DSN.
// POST /api/admin/database/connect
func (s *Server) handleDatabaseConnect(w http.ResponseWriter, r *http.Request) {
	if s.dbPool == nil {
		writeError(w, http.StatusInternalServerError, "database pool not initialized")
		return
	}

	cfg := s.configStore.Get()
	if cfg.Database.DSN == "" {
		writeError(w, http.StatusBadRequest, "no DSN configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.dbPool.Connect(ctx, cfg.Database.DSN); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Run migrations after connecting.
	if err := database.RunMigrations(ctx, s.dbPool); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":          false,
			"error":            "connected but migration failed: " + err.Error(),
			"connected":        true,
			"migration_failed": true,
		})
		return
	}

	// Initialize schema and data stores.
	s.schemaStore = database.NewSchemaStore(s.dbPool)
	s.dataStore = database.NewDataStore(s.dbPool, s.schemaStore)

	// Check if plugins need a restart to activate.
	restartRequired := s.todoService == nil // todo wasn't active at startup
	resp := map[string]any{
		"success":   true,
		"connected": true,
	}
	if restartRequired {
		resp["restart_required"] = true
		resp["message"] = "Database connected. Server restart required to activate the todo plugin."
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Schema admin handlers ---

// handleListDatabaseTables lists all table definitions.
// GET /api/admin/database/tables
func (s *Server) handleListDatabaseTables(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tables, err := s.schemaStore.ListTables(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tables == nil {
		tables = []database.TableDef{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": tables})
}

// handleCreateDatabaseTable creates a new table definition.
// POST /api/admin/database/tables
func (s *Server) handleCreateDatabaseTable(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	var t database.TableDef
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	username := UsernameFromContext(r.Context())
	if err := s.schemaStore.CreateTable(r.Context(), &t, username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// handleGetDatabaseTable returns a table definition with its fields.
// GET /api/admin/database/tables/{id}
func (s *Server) handleGetDatabaseTable(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid table id")
		return
	}
	t, err := s.schemaStore.GetTable(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleUpdateDatabaseTable updates a table definition.
// PUT /api/admin/database/tables/{id}
func (s *Server) handleUpdateDatabaseTable(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid table id")
		return
	}
	var t database.TableDef
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	t.ID = id
	username := UsernameFromContext(r.Context())
	if err := s.schemaStore.UpdateTable(r.Context(), &t, username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleMigratePagePaths recomputes every row's expected page path from the
// table's current page_folder template and renames the wiki page for each row
// whose current page_path no longer matches. Use this after changing the
// page_folder pattern (e.g. adding "@server_name" to a table that previously
// used numeric ids), or to normalize rows created before a slug rule tightened.
//
// The rename uses the standard page-move plumbing so incoming links can be
// rewritten and row ids are preserved (see storage.PageRenamer).
//
// POST /api/admin/database/tables/{id}/migrate-page-paths
// Body: { "dry_run": bool, "update_links": bool }
func (s *Server) handleMigratePagePaths(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil || s.dataStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid table id")
		return
	}
	table, err := s.schemaStore.GetTable(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if table.PageFolder == "" {
		writeError(w, http.StatusBadRequest, "table has no page_folder — nothing to migrate")
		return
	}

	// Body is optional. Default: dry_run=true, update_links=true.
	req := struct {
		DryRun      *bool `json:"dry_run"`
		UpdateLinks *bool `json:"update_links"`
	}{}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	updateLinks := true
	if req.UpdateLinks != nil {
		updateLinks = *req.UpdateLinks
	}

	mover, canMove := s.store.(PageMover)
	if !canMove && !dryRun {
		writeError(w, http.StatusNotImplemented, "store does not support moves")
		return
	}

	// Iterate every row in the table (no filters, high limit).
	ctx := r.Context()
	rows, _, err := s.dataStore.QueryRows(ctx, table.Name, database.QueryParams{Limit: 10000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type action struct {
		ID     int    `json:"id"`
		From   string `json:"from"`
		To     string `json:"to"`
		Status string `json:"status"` // "moved" | "would_move" | "skipped_same_path" | "skipped_conflict" | "error"
		Error  string `json:"error,omitempty"`
	}
	actions := make([]action, 0, len(rows))
	author := UsernameFromContext(ctx)

	moved, skipped, errors := 0, 0, 0
	for _, row := range rows {
		expected := resolvePageFolder(table.PageFolder, row.ID, row.Fields)
		a := action{ID: row.ID, From: row.PagePath, To: expected}

		if row.PagePath == expected {
			a.Status = "skipped_same_path"
			skipped++
			actions = append(actions, a)
			continue
		}

		if dryRun {
			a.Status = "would_move"
			actions = append(actions, a)
			continue
		}

		if _, err := mover.Move(row.PagePath, expected, false, updateLinks, author); err != nil {
			a.Status = "error"
			a.Error = err.Error()
			errors++
			actions = append(actions, a)
			continue
		}
		a.Status = "moved"
		moved++
		actions = append(actions, a)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"table":        table.Name,
		"page_folder":  table.PageFolder,
		"dry_run":      dryRun,
		"update_links": updateLinks,
		"total_rows":   len(rows),
		"summary": map[string]int{
			"moved":    moved,
			"skipped":  skipped,
			"errors":   errors,
			"would_move": func() int {
				if !dryRun {
					return 0
				}
				n := 0
				for _, a := range actions {
					if a.Status == "would_move" {
						n++
					}
				}
				return n
			}(),
		},
		"actions": actions,
	})
}

// handleDeleteDatabaseTable deletes a table definition and its data.
// DELETE /api/admin/database/tables/{id}
func (s *Server) handleDeleteDatabaseTable(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid table id")
		return
	}
	username := UsernameFromContext(r.Context())
	if err := s.schemaStore.DeleteTable(r.Context(), id, username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": "ok"})
}

// handleCreateDatabaseField adds a field to a table.
// POST /api/admin/database/tables/{id}/fields
func (s *Server) handleCreateDatabaseField(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid table id")
		return
	}
	var f database.FieldDef
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	f.TableID = tableID
	username := UsernameFromContext(r.Context())
	if err := s.schemaStore.CreateField(r.Context(), &f, username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

// handleUpdateDatabaseField updates a field definition.
// PUT /api/admin/database/tables/{id}/fields/{fid}
func (s *Server) handleUpdateDatabaseField(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid table id")
		return
	}
	fid, err := strconv.Atoi(chi.URLParam(r, "fid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid field id")
		return
	}
	var f database.FieldDef
	if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	f.ID = fid
	f.TableID = tableID
	username := UsernameFromContext(r.Context())
	if err := s.schemaStore.UpdateField(r.Context(), &f, username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handleArchiveDatabaseField soft-deletes a field.
// DELETE /api/admin/database/tables/{id}/fields/{fid}
func (s *Server) handleArchiveDatabaseField(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	fid, err := strconv.Atoi(chi.URLParam(r, "fid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid field id")
		return
	}
	username := UsernameFromContext(r.Context())
	if err := s.schemaStore.ArchiveField(r.Context(), fid, username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"archived": "ok"})
}

// handleDatabaseTableHistory returns schema change history for a table.
// GET /api/admin/database/tables/{id}/history
func (s *Server) handleDatabaseTableHistory(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid table id")
		return
	}
	entries, err := s.schemaStore.GetHistory(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entries == nil {
		entries = []database.SchemaHistoryEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": entries})
}
