package database

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// DataStore provides row CRUD operations on dynamic data tables.
type DataStore struct {
	pool        *Pool
	schemaStore *SchemaStore
}

// NewDataStore creates a new DataStore.
func NewDataStore(pool *Pool, schemaStore *SchemaStore) *DataStore {
	return &DataStore{pool: pool, schemaStore: schemaStore}
}

// InsertRow inserts a row into a dynamic data table.
func (ds *DataStore) InsertRow(ctx context.Context, tableName string, row *Row) error {
	table, err := ds.schemaStore.GetTableByName(ctx, tableName)
	if err != nil {
		return fmt.Errorf("get table schema: %w", err)
	}

	p := ds.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	dtName := dataTableName(tableName)
	activeFields := activeFieldMap(table.Fields)

	// Build column list and values.
	cols := []string{"page_path"}
	args := []any{row.PagePath}
	placeholders := []string{"$1"}
	idx := 2

	var multiEnumFields []FieldDef

	for _, f := range table.Fields {
		if f.ArchivedAt != nil {
			continue
		}
		if f.Type == FieldTypeMultiEnum {
			multiEnumFields = append(multiEnumFields, f)
			continue
		}
		if f.Type == FieldTypeAutoIncrement {
			continue // auto-generated
		}
		val, ok := row.Fields[f.Name]
		if !ok {
			continue
		}
		cols = append(cols, quoteIdent(f.Name))
		args = append(args, convertValue(f.Type, val))
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx))
		idx++
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING id, created_at, updated_at",
		quoteIdent(dtName), strings.Join(cols, ", "), strings.Join(placeholders, ", "))

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, sql, args...).Scan(&row.ID, &row.CreatedAt, &row.UpdatedAt); err != nil {
		return fmt.Errorf("insert row: %w", err)
	}

	// Handle multi_enum fields via junction tables.
	for _, f := range multiEnumFields {
		vals, ok := row.Fields[f.Name]
		if !ok {
			continue
		}
		if err := ds.setMultiEnumValues(ctx, tx, dtName, f.Name, row.ID, vals); err != nil {
			return err
		}
	}

	// Read back auto_increment values.
	for _, f := range table.Fields {
		if f.ArchivedAt != nil || f.Type != FieldTypeAutoIncrement {
			continue
		}
		var v int64
		if err := tx.QueryRow(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE id = $1", quoteIdent(f.Name), quoteIdent(dtName)), row.ID).Scan(&v); err == nil {
			if row.Fields == nil {
				row.Fields = make(map[string]any)
			}
			row.Fields[f.Name] = v
		}
	}

	_ = activeFields // used for validation context
	return tx.Commit(ctx)
}

// UpdateRow updates specific fields of a row.
func (ds *DataStore) UpdateRow(ctx context.Context, tableName string, rowID int, fields map[string]any) error {
	table, err := ds.schemaStore.GetTableByName(ctx, tableName)
	if err != nil {
		return fmt.Errorf("get table schema: %w", err)
	}

	p := ds.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	dtName := dataTableName(tableName)
	active := activeFieldMap(table.Fields)

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var setClauses []string
	var args []any
	idx := 1

	for name, val := range fields {
		f, ok := active[name]
		if !ok {
			continue
		}
		if f.Type == FieldTypeAutoIncrement {
			continue
		}
		if f.Type == FieldTypeMultiEnum {
			if err := ds.setMultiEnumValues(ctx, tx, dtName, f.Name, rowID, val); err != nil {
				return err
			}
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", quoteIdent(name), idx))
		args = append(args, convertValue(f.Type, val))
		idx++
	}

	if len(setClauses) > 0 {
		setClauses = append(setClauses, fmt.Sprintf("updated_at = NOW()"))
		args = append(args, rowID)
		sql := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d",
			quoteIdent(dtName), strings.Join(setClauses, ", "), idx)
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			return fmt.Errorf("update row: %w", err)
		}
	}

	return tx.Commit(ctx)
}

// DeleteRow deletes a row by ID.
func (ds *DataStore) DeleteRow(ctx context.Context, tableName string, rowID int) error {
	p := ds.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	dtName := dataTableName(tableName)
	_, err := p.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = $1", quoteIdent(dtName)), rowID)
	if err != nil {
		return fmt.Errorf("delete row: %w", err)
	}
	return nil
}

// GetRow returns a single row by ID.
func (ds *DataStore) GetRow(ctx context.Context, tableName string, rowID int) (*Row, error) {
	table, err := ds.schemaStore.GetTableByName(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("get table schema: %w", err)
	}

	p := ds.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	dtName := dataTableName(tableName)
	cols := buildSelectColumns(table.Fields)

	sql := fmt.Sprintf("SELECT id, page_path, created_at, updated_at%s FROM %s WHERE id = $1",
		cols, quoteIdent(dtName))

	row := p.QueryRow(ctx, sql, rowID)
	result, err := scanRow(row, table.Fields)
	if err != nil {
		return nil, fmt.Errorf("get row: %w", err)
	}

	// Load multi_enum values.
	if err := ds.loadMultiEnumValues(ctx, dtName, table.Fields, result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetRowByPagePath returns a row by page_path.
func (ds *DataStore) GetRowByPagePath(ctx context.Context, tableName, pagePath string) (*Row, error) {
	table, err := ds.schemaStore.GetTableByName(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("get table schema: %w", err)
	}

	p := ds.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	dtName := dataTableName(tableName)
	cols := buildSelectColumns(table.Fields)

	sql := fmt.Sprintf("SELECT id, page_path, created_at, updated_at%s FROM %s WHERE page_path = $1",
		cols, quoteIdent(dtName))

	row := p.QueryRow(ctx, sql, pagePath)
	result, err := scanRow(row, table.Fields)
	if err != nil {
		return nil, fmt.Errorf("get row by page path: %w", err)
	}

	if err := ds.loadMultiEnumValues(ctx, dtName, table.Fields, result); err != nil {
		return nil, err
	}

	return result, nil
}

// UpsertPageRow inserts or updates a row based on page_path.
// If fields is nil, an existing row is left unchanged; a new row is created with defaults.
func (ds *DataStore) UpsertPageRow(ctx context.Context, tableName, pagePath string, fields map[string]any) (*Row, error) {
	existing, err := ds.GetRowByPagePath(ctx, tableName, pagePath)
	if err == nil && existing != nil {
		if len(fields) == 0 {
			return existing, nil // nothing to update
		}
		if err := ds.UpdateRow(ctx, tableName, existing.ID, fields); err != nil {
			return nil, err
		}
		return ds.GetRow(ctx, tableName, existing.ID)
	}

	// Insert.
	row := &Row{
		PagePath: pagePath,
		Fields:   fields,
	}
	if row.Fields == nil {
		row.Fields = make(map[string]any)
	}
	if err := ds.InsertRow(ctx, tableName, row); err != nil {
		return nil, err
	}
	return row, nil
}

// QueryRows queries rows with filters, sorting, and pagination.
func (ds *DataStore) QueryRows(ctx context.Context, tableName string, params QueryParams) ([]Row, int, error) {
	table, err := ds.schemaStore.GetTableByName(ctx, tableName)
	if err != nil {
		return nil, 0, fmt.Errorf("get table schema: %w", err)
	}

	p := ds.pool.GetPool()
	if p == nil {
		return nil, 0, fmt.Errorf("database not connected")
	}

	dtName := dataTableName(tableName)
	active := activeFieldMap(table.Fields)
	cols := buildSelectColumns(table.Fields)

	// Build WHERE clause.
	var whereClauses []string
	var args []any
	argIdx := 1

	for _, f := range params.Filters {
		fd, ok := active[f.Field]
		if !ok {
			// Allow filtering by standard columns too.
			if f.Field != "page_path" && f.Field != "id" {
				continue
			}
			fd = FieldDef{Name: f.Field, Type: FieldTypeText}
		}

		op := f.Operator
		switch op {
		case "=", "!=", "<", ">", "<=", ">=":
			whereClauses = append(whereClauses, fmt.Sprintf("%s %s $%d", quoteIdent(f.Field), op, argIdx))
			args = append(args, convertValue(fd.Type, f.Value))
			argIdx++
		case "~":
			whereClauses = append(whereClauses, fmt.Sprintf("%s ILIKE $%d", quoteIdent(f.Field), argIdx))
			args = append(args, "%"+f.Value+"%")
			argIdx++
		default:
			continue
		}
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Count total.
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s%s", quoteIdent(dtName), whereSQL)
	if err := p.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count rows: %w", err)
	}

	// Build ORDER BY.
	orderSQL := " ORDER BY id ASC"
	if params.Sort != "" {
		_, validSort := active[params.Sort]
		if validSort || params.Sort == "id" || params.Sort == "page_path" || params.Sort == "created_at" || params.Sort == "updated_at" {
			dir := "ASC"
			if strings.ToLower(params.Order) == "desc" {
				dir = "DESC"
			}
			orderSQL = fmt.Sprintf(" ORDER BY %s %s", quoteIdent(params.Sort), dir)
		}
	}

	// Build LIMIT/OFFSET.
	limitSQL := ""
	if params.Limit > 0 {
		limitSQL = fmt.Sprintf(" LIMIT %d", params.Limit)
	}
	if params.Offset > 0 {
		limitSQL += fmt.Sprintf(" OFFSET %d", params.Offset)
	}

	selectSQL := fmt.Sprintf("SELECT id, page_path, created_at, updated_at%s FROM %s%s%s%s",
		cols, quoteIdent(dtName), whereSQL, orderSQL, limitSQL)

	rows, err := p.Query(ctx, selectSQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()

	var results []Row
	for rows.Next() {
		r, err := scanRowFromRows(rows, table.Fields)
		if err != nil {
			return nil, 0, err
		}
		results = append(results, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Load multi_enum values for all rows.
	for i := range results {
		if err := ds.loadMultiEnumValues(ctx, dtName, table.Fields, &results[i]); err != nil {
			return nil, 0, err
		}
	}

	return results, total, nil
}

// UpdatePagePath sets the page_path for a row.
func (ds *DataStore) UpdatePagePath(ctx context.Context, tableName string, rowID int, pagePath string) error {
	p := ds.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}
	dtName := dataTableName(tableName)
	_, err := p.Exec(ctx, fmt.Sprintf("UPDATE %s SET page_path = $1, updated_at = NOW() WHERE id = $2", quoteIdent(dtName)), pagePath, rowID)
	if err != nil {
		return fmt.Errorf("update page path: %w", err)
	}
	return nil
}

// DeleteRowsByPagePath deletes all rows for a given page path.
func (ds *DataStore) DeleteRowsByPagePath(ctx context.Context, tableName, pagePath string) error {
	p := ds.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	dtName := dataTableName(tableName)
	_, err := p.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE page_path = $1", quoteIdent(dtName)), pagePath)
	if err != nil {
		return fmt.Errorf("delete rows by page path: %w", err)
	}
	return nil
}

// --- helpers ---

func activeFieldMap(fields []FieldDef) map[string]FieldDef {
	m := make(map[string]FieldDef)
	for _, f := range fields {
		if f.ArchivedAt == nil {
			m[f.Name] = f
		}
	}
	return m
}

func buildSelectColumns(fields []FieldDef) string {
	var cols []string
	for _, f := range fields {
		if f.ArchivedAt != nil || f.Type == FieldTypeMultiEnum {
			continue
		}
		cols = append(cols, quoteIdent(f.Name))
	}
	if len(cols) == 0 {
		return ""
	}
	return ", " + strings.Join(cols, ", ")
}

func scanRow(row pgx.Row, fields []FieldDef) (*Row, error) {
	r := &Row{Fields: make(map[string]any)}

	dests := []any{&r.ID, &r.PagePath, &r.CreatedAt, &r.UpdatedAt}
	for _, f := range fields {
		if f.ArchivedAt != nil || f.Type == FieldTypeMultiEnum {
			continue
		}
		var v any
		dests = append(dests, &v)
	}

	if err := row.Scan(dests...); err != nil {
		return nil, err
	}

	i := 4
	for _, f := range fields {
		if f.ArchivedAt != nil || f.Type == FieldTypeMultiEnum {
			continue
		}
		r.Fields[f.Name] = *(dests[i].(*any))
		i++
	}
	return r, nil
}

func scanRowFromRows(rows pgx.Rows, fields []FieldDef) (*Row, error) {
	r := &Row{Fields: make(map[string]any)}

	dests := []any{&r.ID, &r.PagePath, &r.CreatedAt, &r.UpdatedAt}
	for _, f := range fields {
		if f.ArchivedAt != nil || f.Type == FieldTypeMultiEnum {
			continue
		}
		var v any
		dests = append(dests, &v)
	}

	if err := rows.Scan(dests...); err != nil {
		return nil, err
	}

	i := 4
	for _, f := range fields {
		if f.ArchivedAt != nil || f.Type == FieldTypeMultiEnum {
			continue
		}
		r.Fields[f.Name] = *(dests[i].(*any))
		i++
	}
	return r, nil
}

func (ds *DataStore) setMultiEnumValues(ctx context.Context, tx pgx.Tx, dtName, fieldName string, rowID int, val any) error {
	jt := fmt.Sprintf("%s__%s", dtName, fieldName)

	// Delete existing values.
	if _, err := tx.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE row_id = $1", quoteIdent(jt)), rowID); err != nil {
		return fmt.Errorf("clear multi_enum %s: %w", fieldName, err)
	}

	// Insert new values.
	values := toStringSlice(val)
	for _, v := range values {
		if _, err := tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s (row_id, value) VALUES ($1, $2)", quoteIdent(jt)), rowID, v); err != nil {
			return fmt.Errorf("insert multi_enum %s: %w", fieldName, err)
		}
	}
	return nil
}

func (ds *DataStore) loadMultiEnumValues(ctx context.Context, dtName string, fields []FieldDef, row *Row) error {
	p := ds.pool.GetPool()
	if p == nil {
		return nil
	}

	for _, f := range fields {
		if f.ArchivedAt != nil || f.Type != FieldTypeMultiEnum {
			continue
		}
		jt := fmt.Sprintf("%s__%s", dtName, f.Name)
		meRows, err := p.Query(ctx, fmt.Sprintf("SELECT value FROM %s WHERE row_id = $1 ORDER BY value", quoteIdent(jt)), row.ID)
		if err != nil {
			return fmt.Errorf("load multi_enum %s: %w", f.Name, err)
		}
		var vals []string
		for meRows.Next() {
			var v string
			if err := meRows.Scan(&v); err != nil {
				meRows.Close()
				return err
			}
			vals = append(vals, v)
		}
		meRows.Close()
		if row.Fields == nil {
			row.Fields = make(map[string]any)
		}
		row.Fields[f.Name] = vals
	}
	return nil
}

func toStringSlice(val any) []string {
	switch v := val.(type) {
	case []string:
		return v
	case []any:
		var result []string
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
		return result
	case string:
		if v == "" {
			return nil
		}
		return strings.Split(v, ",")
	default:
		return nil
	}
}

func convertValue(fieldType string, val any) any {
	if val == nil {
		return nil
	}
	switch fieldType {
	case FieldTypeInteger, FieldTypeAutoIncrement, FieldTypeTag:
		switch v := val.(type) {
		case float64:
			return int64(v)
		case string:
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				return n
			}
			return 0
		default:
			return val
		}
	case FieldTypeFloat:
		switch v := val.(type) {
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
			return 0.0
		default:
			return val
		}
	case FieldTypeBoolean:
		switch v := val.(type) {
		case string:
			return v == "true" || v == "1" || v == "yes"
		case bool:
			return v
		default:
			return false
		}
	default:
		return fmt.Sprintf("%v", val)
	}
}
