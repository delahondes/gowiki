package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/database"
	"gowiki/backend/internal/markdown"
)

// handleDatabaseSchema returns the schema (fields) for a data table.
// GET /api/database/{table}/schema
func (s *Server) handleDatabaseSchema(w http.ResponseWriter, r *http.Request) {
	if s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")
	t, err := s.schemaStore.GetTableByName(r.Context(), tableName)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// handleDatabaseQueryRows queries rows with filters.
// GET /api/database/{table}/rows
func (s *Server) handleDatabaseQueryRows(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")
	params := parseQueryParams(r)
	rows, total, err := s.dataStore.QueryRows(r.Context(), tableName, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []database.Row{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rows":  rows,
		"total": total,
	})
}

// handleDatabaseInsertRow inserts a new row.
// If the table has page_folder + index_field, also creates the wiki page.
// POST /api/database/{table}/rows
func (s *Server) handleDatabaseInsertRow(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil || s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")

	var row database.Row
	if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := s.dataStore.InsertRow(r.Context(), tableName, &row); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// If this table has page_folder + index_field, auto-create the wiki page.
	table, err := s.schemaStore.GetTableByName(r.Context(), tableName)
	if err == nil && table.PageFolder != "" && table.IndexField != "" {
		if indexVal, ok := row.Fields[table.IndexField]; ok && indexVal != nil {
			pageFolder := strings.TrimSuffix(strings.TrimPrefix(table.PageFolder, "/"), "/")
			pagePath := pageFolder + "/" + fmt.Sprintf("%v", indexVal)

			// Update page_path on the row in the database.
			row.PagePath = pagePath
			if err := s.dataStore.UpdatePagePath(r.Context(), tableName, row.ID, pagePath); err != nil {
				log.Printf("database: failed to set page_path for row %d: %v", row.ID, err)
			}

			// Build page markdown content.
			markdown := s.buildPageContent(table, &row)

			// Create the wiki page on disk.
			author := UsernameFromContext(r.Context())
			if _, err := s.store.Put(pagePath, markdown, author); err != nil {
				log.Printf("database: failed to create page %s: %v", pagePath, err)
			}
		}
	}

	writeJSON(w, http.StatusCreated, row)
}

// buildPageContent generates the markdown for an auto-created page-bound row.
func (s *Server) buildPageContent(table *database.TableDef, row *database.Row) string {
	// If a page template is configured, try to use it.
	if table.PageTemplatePath != "" {
		tmpl, err := s.store.Get(table.PageTemplatePath)
		if err == nil {
			return tmpl.Markdown
		}
	}

	// Default: heading + {database-row} block with field values.
	title := row.PagePath
	if idx := strings.LastIndex(title, "/"); idx >= 0 {
		title = title[idx+1:]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	sb.WriteString(fmt.Sprintf("{database-row table=%s}\n", table.Name))
	sb.WriteString("| Field | Value |\n")
	sb.WriteString("| --- | --- |\n")
	for _, f := range table.Fields {
		if f.ArchivedAt != nil {
			continue
		}
		val := ""
		if v, ok := row.Fields[f.Name]; ok && v != nil {
			val = fmt.Sprintf("%v", v)
		}
		sb.WriteString(fmt.Sprintf("| %s | %s |\n", f.Name, val))
	}
	sb.WriteString("\n")
	return sb.String()
}

// handleDatabaseGetRow returns a single row by ID.
// GET /api/database/{table}/rows/{id}
func (s *Server) handleDatabaseGetRow(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")
	rowID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid row id")
		return
	}
	row, err := s.dataStore.GetRow(r.Context(), tableName, rowID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// handleDatabaseUpdateRow updates fields of a row.
// If the row is page-bound, also updates the page's {database-row} block.
// Blocks the update if a draft exists for the page (unless ?force=true).
// PUT /api/database/{table}/rows/{id}
func (s *Server) handleDatabaseUpdateRow(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil || s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")
	rowID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid row id")
		return
	}

	var body struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Reject updates to the index field.
	table, err := s.schemaStore.GetTableByName(r.Context(), tableName)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if table.IndexField != "" {
		if _, ok := body.Fields[table.IndexField]; ok {
			writeError(w, http.StatusBadRequest, "cannot modify index field "+table.IndexField)
			return
		}
	}

	// Check for draft lock on the page-bound row before modifying anything.
	force := r.URL.Query().Get("force") == "true"
	existingRow, err := s.dataStore.GetRow(r.Context(), tableName, rowID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if existingRow.PagePath != "" && !force {
		lock := s.draftManager.GetLock(existingRow.PagePath)
		if lock.Owner != "" {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":       "page_draft_conflict",
				"draft_owner": lock.Owner,
			})
			return
		}
	}

	if err := s.dataStore.UpdateRow(r.Context(), tableName, rowID, body.Fields); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Return updated row.
	row, err := s.dataStore.GetRow(r.Context(), tableName, rowID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Sync changes to the page if this row is page-bound.
	if row.PagePath != "" {
		author := UsernameFromContext(r.Context())
		s.syncRowToPage(table, row, author)
	}

	writeJSON(w, http.StatusOK, row)
}

// handleDatabaseDeleteRow deletes a row.
// DELETE /api/database/{table}/rows/{id}
func (s *Server) handleDatabaseDeleteRow(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")
	rowID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid row id")
		return
	}
	if err := s.dataStore.DeleteRow(r.Context(), tableName, rowID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": "ok"})
}

// handleDatabaseGetRowByPage returns a row by page path.
// GET /api/database/{table}/page/*
func (s *Server) handleDatabaseGetRowByPage(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}
	row, err := s.dataStore.GetRowByPagePath(r.Context(), tableName, pagePath)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// handleDatabaseUpsertRowByPage upserts a row by page path.
// Rejects index field modifications and syncs changes to the page.
// Blocks the update if a draft exists for the page (unless ?force=true).
// PUT /api/database/{table}/page/*
func (s *Server) handleDatabaseUpsertRowByPage(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil || s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")
	pagePath := strings.TrimSpace(chi.URLParam(r, "*"))
	if pagePath == "" {
		writeError(w, http.StatusBadRequest, "missing page path")
		return
	}

	var body struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Reject updates to the index field.
	table, err := s.schemaStore.GetTableByName(r.Context(), tableName)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if table.IndexField != "" {
		if _, ok := body.Fields[table.IndexField]; ok {
			writeError(w, http.StatusBadRequest, "cannot modify index field "+table.IndexField)
			return
		}
	}

	// Check for draft lock before modifying anything.
	force := r.URL.Query().Get("force") == "true"
	if !force {
		lock := s.draftManager.GetLock(pagePath)
		if lock.Owner != "" {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":       "page_draft_conflict",
				"draft_owner": lock.Owner,
			})
			return
		}
	}

	row, err := s.dataStore.UpsertPageRow(r.Context(), tableName, pagePath, body.Fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Sync changes to the page.
	author := UsernameFromContext(r.Context())
	s.syncRowToPage(table, row, author)

	writeJSON(w, http.StatusOK, row)
}

// syncRowToPage updates the {database-row} block in the page bound to this row.
func (s *Server) syncRowToPage(table *database.TableDef, row *database.Row, author string) {
	if row.PagePath == "" {
		return
	}

	page, err := s.store.Get(row.PagePath)
	if err != nil {
		log.Printf("database sync→page: cannot read page %s: %v", row.PagePath, err)
		return
	}

	// Build ordered field names and string values from the row.
	var fieldNames []string
	values := make(map[string]string)
	for _, f := range table.Fields {
		if f.ArchivedAt != nil {
			continue
		}
		fieldNames = append(fieldNames, f.Name)
		if v, ok := row.Fields[f.Name]; ok && v != nil {
			values[f.Name] = fmt.Sprintf("%v", v)
		} else {
			values[f.Name] = ""
		}
	}

	updated := markdown.ReplaceDatabaseRowBlock(page.Markdown, table.Name, fieldNames, values)
	if updated == page.Markdown {
		return // no change
	}

	if _, err := s.store.Put(row.PagePath, updated, author); err != nil {
		log.Printf("database sync→page: cannot save page %s: %v", row.PagePath, err)
	}
}

// handleDatabaseExportCSV exports all rows as CSV.
// GET /api/database/{table}/export/csv
func (s *Server) handleDatabaseExportCSV(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil || s.schemaStore == nil {
		writeError(w, http.StatusServiceUnavailable, "database not connected")
		return
	}
	tableName := chi.URLParam(r, "table")
	table, err := s.schemaStore.GetTableByName(r.Context(), tableName)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	rows, _, err := s.dataStore.QueryRows(r.Context(), tableName, database.QueryParams{Limit: 10000})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename="+tableName+".csv")

	// Header row.
	var headers []string
	headers = append(headers, "id", "page_path")
	for _, f := range table.Fields {
		if f.ArchivedAt == nil {
			headers = append(headers, f.Name)
		}
	}
	w.Write([]byte(strings.Join(headers, ",") + "\n"))

	// Data rows.
	for _, row := range rows {
		var vals []string
		vals = append(vals, strconv.Itoa(row.ID), csvEscape(row.PagePath))
		for _, f := range table.Fields {
			if f.ArchivedAt != nil {
				continue
			}
			v, ok := row.Fields[f.Name]
			if !ok || v == nil {
				vals = append(vals, "")
			} else {
				vals = append(vals, csvEscape(strings.TrimSpace(strings.Replace(strings.Replace(
					formatValue(v), "\n", " ", -1), "\r", "", -1))))
			}
		}
		w.Write([]byte(strings.Join(vals, ",") + "\n"))
	}
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func formatValue(v any) string {
	switch val := v.(type) {
	case []string:
		return strings.Join(val, "; ")
	case []any:
		var parts []string
		for _, item := range val {
			parts = append(parts, strings.TrimSpace(strings.Replace(strings.Replace(
				formatValue(item), "\n", " ", -1), "\r", "", -1)))
		}
		return strings.Join(parts, "; ")
	default:
		return strings.TrimSpace(strings.Replace(strings.Replace(
			strings.Replace(strings.Replace(
				jsonString(val), "\n", " ", -1), "\r", "", -1),
			`"`, "", -1), `\`, "", -1))
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	// Remove surrounding quotes if present.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}

// parseQueryParams extracts filter, sort, order, limit, offset from request.
func parseQueryParams(r *http.Request) database.QueryParams {
	params := database.QueryParams{
		Sort:  r.URL.Query().Get("sort"),
		Order: r.URL.Query().Get("order"),
	}

	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			params.Limit = n
		}
	}
	if params.Limit == 0 || params.Limit > 1000 {
		params.Limit = 50
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			params.Offset = n
		}
	}

	// Parse filters: ?filter=field=value&filter=field>value
	for _, raw := range r.URL.Query()["filter"] {
		f := parseFilter(raw)
		if f != nil {
			params.Filters = append(params.Filters, *f)
		}
	}

	return params
}

func parseFilter(raw string) *database.Filter {
	// Try two-char operators first.
	for _, op := range []string{"!=", "<=", ">="} {
		idx := strings.Index(raw, op)
		if idx > 0 {
			return &database.Filter{
				Field:    raw[:idx],
				Operator: op,
				Value:    raw[idx+len(op):],
			}
		}
	}
	// Single-char operators.
	for _, op := range []string{"~", "<", ">", "="} {
		idx := strings.Index(raw, op)
		if idx > 0 {
			return &database.Filter{
				Field:    raw[:idx],
				Operator: op,
				Value:    raw[idx+len(op):],
			}
		}
	}
	return nil
}
