package database

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ErrTableNotEmpty is returned by destructive schema operations that require
// the underlying data table to be empty (rename a column, change its type,
// hard-delete it). Callers surface this as HTTP 409 and the MCP layer maps it
// to a clean error message.
var ErrTableNotEmpty = errors.New("table has active rows")

// SchemaStore provides CRUD operations for table and field definitions.
type SchemaStore struct {
	pool *Pool
}

// NewSchemaStore creates a new SchemaStore.
func NewSchemaStore(pool *Pool) *SchemaStore {
	return &SchemaStore{pool: pool}
}

// dataTableName returns the dynamic data table name for a given table definition.
func dataTableName(name string) string {
	return "_" + name
}

// validNameRe validates table and field names (alphanumeric + underscore).
var validNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ListTables returns all table definitions.
func (s *SchemaStore) ListTables(ctx context.Context) ([]TableDef, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `SELECT id, name, label, scope_regexp, page_folder, default_sort_field, default_sort_order, page_template_path, created_at, updated_at FROM database_tables ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []TableDef
	for rows.Next() {
		var t TableDef
		if err := rows.Scan(&t.ID, &t.Name, &t.Label, &t.ScopeRegexp, &t.PageFolder, &t.DefaultSortField, &t.DefaultSortOrder, &t.PageTemplatePath, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// GetTable returns a table definition with its fields.
func (s *SchemaStore) GetTable(ctx context.Context, id int) (*TableDef, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	var t TableDef
	err := p.QueryRow(ctx, `SELECT id, name, label, scope_regexp, page_folder, default_sort_field, default_sort_order, page_template_path, created_at, updated_at FROM database_tables WHERE id = $1`, id).
		Scan(&t.ID, &t.Name, &t.Label, &t.ScopeRegexp, &t.PageFolder, &t.DefaultSortField, &t.DefaultSortOrder, &t.PageTemplatePath, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get table: %w", err)
	}

	fields, err := s.ListFields(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Fields = fields
	return &t, nil
}

// GetTableByName returns a table definition by name, with its fields.
func (s *SchemaStore) GetTableByName(ctx context.Context, name string) (*TableDef, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	var t TableDef
	err := p.QueryRow(ctx, `SELECT id, name, label, scope_regexp, page_folder, default_sort_field, default_sort_order, page_template_path, created_at, updated_at FROM database_tables WHERE name = $1`, name).
		Scan(&t.ID, &t.Name, &t.Label, &t.ScopeRegexp, &t.PageFolder, &t.DefaultSortField, &t.DefaultSortOrder, &t.PageTemplatePath, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get table by name: %w", err)
	}

	fields, err := s.ListFields(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Fields = fields
	return &t, nil
}

// CreateTable creates a table definition and its corresponding dynamic data table.
func (s *SchemaStore) CreateTable(ctx context.Context, t *TableDef, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	if !validNameRe.MatchString(t.Name) {
		return fmt.Errorf("invalid table name %q: must match [a-z][a-z0-9_]*", t.Name)
	}

	if t.DefaultSortOrder == "" {
		t.DefaultSortOrder = "asc"
	}
	if t.ScopeRegexp == "" {
		t.ScopeRegexp = ".*"
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `INSERT INTO database_tables (name, label, scope_regexp, page_folder, default_sort_field, default_sort_order, page_template_path) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at, updated_at`,
		t.Name, t.Label, t.ScopeRegexp, t.PageFolder, t.DefaultSortField, t.DefaultSortOrder, t.PageTemplatePath).
		Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert table def: %w", err)
	}

	// Create the dynamic data table.
	dtName := dataTableName(t.Name)
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id SERIAL PRIMARY KEY,
		page_path TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`, quoteIdent(dtName))
	if _, err := tx.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create data table: %w", err)
	}

	// Record history.
	if _, err := tx.Exec(ctx, `INSERT INTO database_schema_history (table_id, changed_by, change_type, detail) VALUES ($1, $2, 'create_table', $3)`,
		t.ID, changedBy, "Created table "+t.Name); err != nil {
		return fmt.Errorf("record history: %w", err)
	}

	return tx.Commit(ctx)
}

// UpdateTable updates a table definition's metadata (not schema).
func (s *SchemaStore) UpdateTable(ctx context.Context, t *TableDef, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	_, err := p.Exec(ctx, `UPDATE database_tables SET label=$1, scope_regexp=$2, page_folder=$3, default_sort_field=$4, default_sort_order=$5, page_template_path=$6, updated_at=NOW() WHERE id=$7`,
		t.Label, t.ScopeRegexp, t.PageFolder, t.DefaultSortField, t.DefaultSortOrder, t.PageTemplatePath, t.ID)
	if err != nil {
		return fmt.Errorf("update table: %w", err)
	}

	// Record history.
	_, _ = p.Exec(ctx, `INSERT INTO database_schema_history (table_id, changed_by, change_type, detail) VALUES ($1, $2, 'update_table', 'Updated table settings')`, t.ID, changedBy)
	return nil
}

// DeleteTable deletes a table definition and drops the dynamic data table.
func (s *SchemaStore) DeleteTable(ctx context.Context, id int, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	// Get the table name first for dropping the data table.
	var name string
	err := p.QueryRow(ctx, `SELECT name FROM database_tables WHERE id = $1`, id).Scan(&name)
	if err != nil {
		return fmt.Errorf("get table name: %w", err)
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Drop junction tables for multi_enum fields.
	fieldRows, err := tx.Query(ctx, `SELECT name FROM database_fields WHERE table_id = $1 AND type = 'multi_enum'`, id)
	if err != nil {
		return fmt.Errorf("list multi_enum fields: %w", err)
	}
	var multiFields []string
	for fieldRows.Next() {
		var fn string
		if err := fieldRows.Scan(&fn); err != nil {
			fieldRows.Close()
			return err
		}
		multiFields = append(multiFields, fn)
	}
	fieldRows.Close()

	for _, fn := range multiFields {
		jt := fmt.Sprintf("%s__%s", dataTableName(name), fn)
		if _, err := tx.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteIdent(jt))); err != nil {
			return fmt.Errorf("drop junction table %s: %w", jt, err)
		}
	}

	// Drop the auto_increment sequences.
	seqRows, err := tx.Query(ctx, `SELECT name FROM database_fields WHERE table_id = $1 AND type = 'auto_increment'`, id)
	if err != nil {
		return fmt.Errorf("list auto_increment fields: %w", err)
	}
	var autoFields []string
	for seqRows.Next() {
		var fn string
		if err := seqRows.Scan(&fn); err != nil {
			seqRows.Close()
			return err
		}
		autoFields = append(autoFields, fn)
	}
	seqRows.Close()

	for _, fn := range autoFields {
		seqName := fmt.Sprintf("%s_%s_seq", dataTableName(name), fn)
		if _, err := tx.Exec(ctx, fmt.Sprintf("DROP SEQUENCE IF EXISTS %s", quoteIdent(seqName))); err != nil {
			return fmt.Errorf("drop sequence %s: %w", seqName, err)
		}
	}

	// Drop the data table.
	dtName := dataTableName(name)
	if _, err := tx.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteIdent(dtName))); err != nil {
		return fmt.Errorf("drop data table: %w", err)
	}

	// Delete the table definition (cascades to fields, enum_values, history).
	if _, err := tx.Exec(ctx, `DELETE FROM database_tables WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete table def: %w", err)
	}

	return tx.Commit(ctx)
}

// ListFields returns all fields for a table, ordered by display_order.
func (s *SchemaStore) ListFields(ctx context.Context, tableID int) ([]FieldDef, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `SELECT id, table_id, name, label, type, required, default_value, display_order, placeholder, foreign_key, display_column, created_at, archived_at FROM database_fields WHERE table_id = $1 ORDER BY display_order, id`, tableID)
	if err != nil {
		return nil, fmt.Errorf("list fields: %w", err)
	}
	defer rows.Close()

	var fields []FieldDef
	for rows.Next() {
		var f FieldDef
		if err := rows.Scan(&f.ID, &f.TableID, &f.Name, &f.Label, &f.Type, &f.Required, &f.DefaultValue, &f.DisplayOrder, &f.Placeholder, &f.ForeignKey, &f.DisplayColumn, &f.CreatedAt, &f.ArchivedAt); err != nil {
			return nil, fmt.Errorf("scan field: %w", err)
		}

		// Load enum values if applicable.
		if f.Type == FieldTypeEnum || f.Type == FieldTypeMultiEnum {
			vals, err := s.getEnumValues(ctx, f.ID)
			if err != nil {
				return nil, err
			}
			f.EnumValues = vals
		}
		fields = append(fields, f)
	}
	return fields, rows.Err()
}

// CreateField adds a field to a table and alters the dynamic data table.
func (s *SchemaStore) CreateField(ctx context.Context, f *FieldDef, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	if !validNameRe.MatchString(f.Name) {
		return fmt.Errorf("invalid field name %q: must match [a-z][a-z0-9_]*", f.Name)
	}
	if !ValidFieldTypes[f.Type] {
		return fmt.Errorf("invalid field type %q", f.Type)
	}

	// Get the table name.
	var tableName string
	if err := p.QueryRow(ctx, `SELECT name FROM database_tables WHERE id = $1`, f.TableID).Scan(&tableName); err != nil {
		return fmt.Errorf("get table name: %w", err)
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `INSERT INTO database_fields (table_id, name, label, type, required, default_value, display_order, placeholder, foreign_key, display_column) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, created_at`,
		f.TableID, f.Name, f.Label, f.Type, f.Required, f.DefaultValue, f.DisplayOrder, f.Placeholder, f.ForeignKey, f.DisplayColumn).
		Scan(&f.ID, &f.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert field def: %w", err)
	}

	dtName := dataTableName(tableName)

	if f.Type == FieldTypeMultiEnum {
		// Create junction table instead of column.
		jt := fmt.Sprintf("%s__%s", dtName, f.Name)
		ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			row_id INTEGER NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			value TEXT NOT NULL,
			UNIQUE (row_id, value)
		)`, quoteIdent(jt), quoteIdent(dtName))
		if _, err := tx.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("create junction table: %w", err)
		}
	} else if f.Type == FieldTypeAutoIncrement {
		// Create sequence and column with default.
		seqName := fmt.Sprintf("%s_%s_seq", dtName, f.Name)
		if _, err := tx.Exec(ctx, fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s", quoteIdent(seqName))); err != nil {
			return fmt.Errorf("create sequence: %w", err)
		}
		sqlType := SQLTypeForField(f.Type)
		alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s NOT NULL DEFAULT nextval('%s')",
			quoteIdent(dtName), quoteIdent(f.Name), sqlType, seqName)
		if _, err := tx.Exec(ctx, alterSQL); err != nil {
			return fmt.Errorf("add auto_increment column: %w", err)
		}
	} else {
		sqlType := SQLTypeForField(f.Type)
		defaultClause := ""
		switch f.Type {
		case FieldTypeText, FieldTypePageLink, FieldTypeEnum, FieldTypeImage, FieldTypeColor, FieldTypeUser:
			defaultClause = "DEFAULT ''"
		case FieldTypeInteger, FieldTypeFloat, FieldTypeTag, FieldTypeLookup:
			defaultClause = "DEFAULT 0"
		case FieldTypeBoolean:
			defaultClause = "DEFAULT FALSE"
		}
		alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s %s",
			quoteIdent(dtName), quoteIdent(f.Name), sqlType, defaultClause)
		if _, err := tx.Exec(ctx, alterSQL); err != nil {
			return fmt.Errorf("add column: %w", err)
		}
	}

	// Save enum values if provided.
	if (f.Type == FieldTypeEnum || f.Type == FieldTypeMultiEnum) && len(f.EnumValues) > 0 {
		if err := s.setEnumValuesTx(ctx, tx, f.ID, f.EnumValues); err != nil {
			return err
		}
	}

	// Record history.
	if _, err := tx.Exec(ctx, `INSERT INTO database_schema_history (table_id, changed_by, change_type, field_name, field_type, detail) VALUES ($1, $2, 'add_field', $3, $4, $5)`,
		f.TableID, changedBy, f.Name, f.Type, fmt.Sprintf("Added field %s (%s)", f.Name, f.Type)); err != nil {
		return fmt.Errorf("record history: %w", err)
	}

	return tx.Commit(ctx)
}

// UpdateField updates a field definition (metadata only, no type changes).
func (s *SchemaStore) UpdateField(ctx context.Context, f *FieldDef, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `UPDATE database_fields SET label=$1, required=$2, default_value=$3, display_order=$4, placeholder=$5, foreign_key=$6, display_column=$7 WHERE id=$8`,
		f.Label, f.Required, f.DefaultValue, f.DisplayOrder, f.Placeholder, f.ForeignKey, f.DisplayColumn, f.ID)
	if err != nil {
		return fmt.Errorf("update field: %w", err)
	}

	// Update enum values if applicable.
	if (f.Type == FieldTypeEnum || f.Type == FieldTypeMultiEnum) && f.EnumValues != nil {
		if err := s.setEnumValuesTx(ctx, tx, f.ID, f.EnumValues); err != nil {
			return err
		}
	}

	// Record history.
	if _, err := tx.Exec(ctx, `INSERT INTO database_schema_history (table_id, changed_by, change_type, field_name, detail) VALUES ($1, $2, 'update_field', $3, 'Updated field settings')`,
		f.TableID, changedBy, f.Name); err != nil {
		return fmt.Errorf("record history: %w", err)
	}

	return tx.Commit(ctx)
}

// ArchiveField soft-deletes a field by setting archived_at.
func (s *SchemaStore) ArchiveField(ctx context.Context, fieldID int, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	var tableID int
	var fieldName string
	err := p.QueryRow(ctx, `UPDATE database_fields SET archived_at = NOW() WHERE id = $1 AND archived_at IS NULL RETURNING table_id, name`, fieldID).
		Scan(&tableID, &fieldName)
	if err != nil {
		return fmt.Errorf("archive field: %w", err)
	}

	// Record history.
	_, _ = p.Exec(ctx, `INSERT INTO database_schema_history (table_id, changed_by, change_type, field_name, detail) VALUES ($1, $2, 'archive_field', $3, $4)`,
		tableID, changedBy, fieldName, "Archived field "+fieldName)
	return nil
}

// SetEnumValues replaces enum values for a field.
func (s *SchemaStore) SetEnumValues(ctx context.Context, fieldID int, values []string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.setEnumValuesTx(ctx, tx, fieldID, values); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *SchemaStore) setEnumValuesTx(ctx context.Context, tx pgx.Tx, fieldID int, values []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM database_enum_values WHERE field_id = $1`, fieldID); err != nil {
		return fmt.Errorf("clear enum values: %w", err)
	}
	for i, v := range values {
		if _, err := tx.Exec(ctx, `INSERT INTO database_enum_values (field_id, value, display_order) VALUES ($1, $2, $3)`, fieldID, v, i); err != nil {
			return fmt.Errorf("insert enum value: %w", err)
		}
	}
	return nil
}

func (s *SchemaStore) getEnumValues(ctx context.Context, fieldID int) ([]string, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `SELECT value FROM database_enum_values WHERE field_id = $1 ORDER BY display_order`, fieldID)
	if err != nil {
		return nil, fmt.Errorf("get enum values: %w", err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// GetHistory returns schema change history for a table.
func (s *SchemaStore) GetHistory(ctx context.Context, tableID int) ([]SchemaHistoryEntry, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `SELECT id, table_id, changed_at, changed_by, change_type, field_name, field_type, detail FROM database_schema_history WHERE table_id = $1 ORDER BY changed_at DESC`, tableID)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	defer rows.Close()

	var entries []SchemaHistoryEntry
	for rows.Next() {
		var e SchemaHistoryEntry
		if err := rows.Scan(&e.ID, &e.TableID, &e.ChangedAt, &e.ChangedBy, &e.ChangeType, &e.FieldName, &e.FieldType, &e.Detail); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// countActiveRows returns the current row count for the given table's data
// table. Used by destructive schema operations to gate on emptiness.
func (s *SchemaStore) countActiveRows(ctx context.Context, tx pgx.Tx, tableName string) (int, error) {
	dtName := dataTableName(tableName)
	var n int
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(dtName))
	if err := tx.QueryRow(ctx, sql).Scan(&n); err != nil {
		return 0, fmt.Errorf("count active rows: %w", err)
	}
	return n, nil
}

// getFieldByID returns a field definition by id (no enum values loaded).
func (s *SchemaStore) getFieldByID(ctx context.Context, tx pgx.Tx, fieldID int) (*FieldDef, error) {
	var f FieldDef
	err := tx.QueryRow(ctx, `SELECT id, table_id, name, label, type, required, default_value, display_order, placeholder, foreign_key, display_column, created_at, archived_at FROM database_fields WHERE id = $1`, fieldID).
		Scan(&f.ID, &f.TableID, &f.Name, &f.Label, &f.Type, &f.Required, &f.DefaultValue, &f.DisplayOrder, &f.Placeholder, &f.ForeignKey, &f.DisplayColumn, &f.CreatedAt, &f.ArchivedAt)
	if err != nil {
		return nil, fmt.Errorf("get field: %w", err)
	}
	return &f, nil
}

// getTableNameByID looks up a table's name by its id.
func (s *SchemaStore) getTableNameByID(ctx context.Context, tx pgx.Tx, tableID int) (string, error) {
	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM database_tables WHERE id = $1`, tableID).Scan(&name); err != nil {
		return "", fmt.Errorf("get table name: %w", err)
	}
	return name, nil
}

// DeleteField hard-deletes a field: drops the SQL column (or junction table
// for multi_enum / sequence for auto_increment), clears any enum values, and
// removes the field row. Refuses with ErrTableNotEmpty when the underlying
// data table has any active row. Historical row-bound pages in the attic keep
// working — the row NodeView renders schema-absent fields as ghost entries.
func (s *SchemaStore) DeleteField(ctx context.Context, fieldID int, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	f, err := s.getFieldByID(ctx, tx, fieldID)
	if err != nil {
		return err
	}
	tableName, err := s.getTableNameByID(ctx, tx, f.TableID)
	if err != nil {
		return err
	}

	n, err := s.countActiveRows(ctx, tx, tableName)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTableNotEmpty
	}

	dtName := dataTableName(tableName)

	// Storage-shape branch by field type.
	switch f.Type {
	case FieldTypeMultiEnum:
		jt := fmt.Sprintf("%s__%s", dtName, f.Name)
		if _, err := tx.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteIdent(jt))); err != nil {
			return fmt.Errorf("drop junction table: %w", err)
		}
	case FieldTypeAutoIncrement:
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s", quoteIdent(dtName), quoteIdent(f.Name))); err != nil {
			return fmt.Errorf("drop auto_increment column: %w", err)
		}
		seqName := fmt.Sprintf("%s_%s_seq", dtName, f.Name)
		if _, err := tx.Exec(ctx, fmt.Sprintf("DROP SEQUENCE IF EXISTS %s", quoteIdent(seqName))); err != nil {
			return fmt.Errorf("drop sequence: %w", err)
		}
	default:
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s", quoteIdent(dtName), quoteIdent(f.Name))); err != nil {
			return fmt.Errorf("drop column: %w", err)
		}
	}

	// Clear enum values (safe even for non-enum fields — no-op).
	if _, err := tx.Exec(ctx, `DELETE FROM database_enum_values WHERE field_id = $1`, f.ID); err != nil {
		return fmt.Errorf("clear enum values: %w", err)
	}

	// Delete the field row.
	if _, err := tx.Exec(ctx, `DELETE FROM database_fields WHERE id = $1`, f.ID); err != nil {
		return fmt.Errorf("delete field def: %w", err)
	}

	// History.
	if _, err := tx.Exec(ctx, `INSERT INTO database_schema_history (table_id, changed_by, change_type, field_name, field_type, detail) VALUES ($1, $2, 'delete_field', $3, $4, $5)`,
		f.TableID, changedBy, f.Name, f.Type, "Deleted field "+f.Name); err != nil {
		return fmt.Errorf("record history: %w", err)
	}

	return tx.Commit(ctx)
}

// RenameField renames a field (SQL column + metadata row). Refuses with
// ErrTableNotEmpty when the underlying data table has any row. multi_enum
// junction tables and auto_increment sequences are renamed alongside.
func (s *SchemaStore) RenameField(ctx context.Context, fieldID int, newName string, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}
	if !validNameRe.MatchString(newName) {
		return fmt.Errorf("invalid field name %q: must match [a-z][a-z0-9_]*", newName)
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	f, err := s.getFieldByID(ctx, tx, fieldID)
	if err != nil {
		return err
	}
	if f.Name == newName {
		return tx.Commit(ctx) // no-op
	}
	tableName, err := s.getTableNameByID(ctx, tx, f.TableID)
	if err != nil {
		return err
	}

	n, err := s.countActiveRows(ctx, tx, tableName)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTableNotEmpty
	}

	dtName := dataTableName(tableName)

	switch f.Type {
	case FieldTypeMultiEnum:
		oldJT := fmt.Sprintf("%s__%s", dtName, f.Name)
		newJT := fmt.Sprintf("%s__%s", dtName, newName)
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quoteIdent(oldJT), quoteIdent(newJT))); err != nil {
			return fmt.Errorf("rename junction table: %w", err)
		}
	case FieldTypeAutoIncrement:
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", quoteIdent(dtName), quoteIdent(f.Name), quoteIdent(newName))); err != nil {
			return fmt.Errorf("rename column: %w", err)
		}
		oldSeq := fmt.Sprintf("%s_%s_seq", dtName, f.Name)
		newSeq := fmt.Sprintf("%s_%s_seq", dtName, newName)
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER SEQUENCE %s RENAME TO %s", quoteIdent(oldSeq), quoteIdent(newSeq))); err != nil {
			return fmt.Errorf("rename sequence: %w", err)
		}
	default:
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", quoteIdent(dtName), quoteIdent(f.Name), quoteIdent(newName))); err != nil {
			return fmt.Errorf("rename column: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE database_fields SET name=$1 WHERE id=$2`, newName, f.ID); err != nil {
		return fmt.Errorf("update field name: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO database_schema_history (table_id, changed_by, change_type, field_name, field_type, detail) VALUES ($1, $2, 'rename_field', $3, $4, $5)`,
		f.TableID, changedBy, newName, f.Type, fmt.Sprintf("Renamed %s -> %s", f.Name, newName)); err != nil {
		return fmt.Errorf("record history: %w", err)
	}

	return tx.Commit(ctx)
}

// RetypeField changes a field's type by dropping and re-adding the storage.
// Refuses with ErrTableNotEmpty when the underlying data table has any row.
// Same storage-shape branching as CreateField.
func (s *SchemaStore) RetypeField(ctx context.Context, fieldID int, newType string, changedBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}
	if !ValidFieldTypes[newType] {
		return fmt.Errorf("invalid field type %q", newType)
	}

	tx, err := p.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	f, err := s.getFieldByID(ctx, tx, fieldID)
	if err != nil {
		return err
	}
	if f.Type == newType {
		return tx.Commit(ctx) // no-op
	}
	tableName, err := s.getTableNameByID(ctx, tx, f.TableID)
	if err != nil {
		return err
	}

	n, err := s.countActiveRows(ctx, tx, tableName)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTableNotEmpty
	}

	dtName := dataTableName(tableName)

	// Drop the old storage.
	switch f.Type {
	case FieldTypeMultiEnum:
		jt := fmt.Sprintf("%s__%s", dtName, f.Name)
		if _, err := tx.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", quoteIdent(jt))); err != nil {
			return fmt.Errorf("drop old junction table: %w", err)
		}
	case FieldTypeAutoIncrement:
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s", quoteIdent(dtName), quoteIdent(f.Name))); err != nil {
			return fmt.Errorf("drop old auto_increment column: %w", err)
		}
		seqName := fmt.Sprintf("%s_%s_seq", dtName, f.Name)
		if _, err := tx.Exec(ctx, fmt.Sprintf("DROP SEQUENCE IF EXISTS %s", quoteIdent(seqName))); err != nil {
			return fmt.Errorf("drop old sequence: %w", err)
		}
	default:
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s", quoteIdent(dtName), quoteIdent(f.Name))); err != nil {
			return fmt.Errorf("drop old column: %w", err)
		}
	}

	// Add the new storage (mirrors CreateField's storage branch).
	switch newType {
	case FieldTypeMultiEnum:
		jt := fmt.Sprintf("%s__%s", dtName, f.Name)
		ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
			row_id INTEGER NOT NULL REFERENCES %s(id) ON DELETE CASCADE,
			value TEXT NOT NULL,
			UNIQUE (row_id, value)
		)`, quoteIdent(jt), quoteIdent(dtName))
		if _, err := tx.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("create junction table: %w", err)
		}
	case FieldTypeAutoIncrement:
		seqName := fmt.Sprintf("%s_%s_seq", dtName, f.Name)
		if _, err := tx.Exec(ctx, fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s", quoteIdent(seqName))); err != nil {
			return fmt.Errorf("create sequence: %w", err)
		}
		sqlType := SQLTypeForField(newType)
		alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s NOT NULL DEFAULT nextval('%s')",
			quoteIdent(dtName), quoteIdent(f.Name), sqlType, seqName)
		if _, err := tx.Exec(ctx, alterSQL); err != nil {
			return fmt.Errorf("add auto_increment column: %w", err)
		}
	default:
		sqlType := SQLTypeForField(newType)
		defaultClause := ""
		switch newType {
		case FieldTypeText, FieldTypePageLink, FieldTypeEnum, FieldTypeImage, FieldTypeColor, FieldTypeUser:
			defaultClause = "DEFAULT ''"
		case FieldTypeInteger, FieldTypeFloat, FieldTypeTag, FieldTypeLookup:
			defaultClause = "DEFAULT 0"
		case FieldTypeBoolean:
			defaultClause = "DEFAULT FALSE"
		}
		alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s %s",
			quoteIdent(dtName), quoteIdent(f.Name), sqlType, defaultClause)
		if _, err := tx.Exec(ctx, alterSQL); err != nil {
			return fmt.Errorf("add column: %w", err)
		}
	}

	// Clear enum values when moving out of enum-family; the caller can
	// repopulate through UpdateField afterwards.
	if f.Type == FieldTypeEnum || f.Type == FieldTypeMultiEnum {
		if newType != FieldTypeEnum && newType != FieldTypeMultiEnum {
			if _, err := tx.Exec(ctx, `DELETE FROM database_enum_values WHERE field_id = $1`, f.ID); err != nil {
				return fmt.Errorf("clear stale enum values: %w", err)
			}
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE database_fields SET type=$1 WHERE id=$2`, newType, f.ID); err != nil {
		return fmt.Errorf("update field type: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO database_schema_history (table_id, changed_by, change_type, field_name, field_type, detail) VALUES ($1, $2, 'retype_field', $3, $4, $5)`,
		f.TableID, changedBy, f.Name, newType, fmt.Sprintf("Retyped %s: %s -> %s", f.Name, f.Type, newType)); err != nil {
		return fmt.Errorf("record history: %w", err)
	}

	return tx.Commit(ctx)
}

// CountRows returns the number of active rows for a table. Used by the admin
// UI to decide whether to enable destructive field operations.
func (s *SchemaStore) CountRows(ctx context.Context, tableName string) (int, error) {
	p := s.pool.GetPool()
	if p == nil {
		return 0, fmt.Errorf("database not connected")
	}
	dtName := dataTableName(tableName)
	var n int
	sql := fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdent(dtName))
	if err := p.QueryRow(ctx, sql).Scan(&n); err != nil {
		return 0, fmt.Errorf("count rows: %w", err)
	}
	return n, nil
}

// quoteIdent quotes a PostgreSQL identifier to prevent injection.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
