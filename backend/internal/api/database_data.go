package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/database"
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
// POST /api/database/{table}/rows
func (s *Server) handleDatabaseInsertRow(w http.ResponseWriter, r *http.Request) {
	if s.dataStore == nil {
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
	writeJSON(w, http.StatusCreated, row)
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
// PUT /api/database/{table}/rows/{id}
func (s *Server) handleDatabaseUpdateRow(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
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
// PUT /api/database/{table}/page/*
func (s *Server) handleDatabaseUpsertRowByPage(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	row, err := s.dataStore.UpsertPageRow(r.Context(), tableName, pagePath, body.Fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, row)
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
