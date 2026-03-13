package database

import (
	"context"
	"fmt"
)

// RunMigrations creates the meta-schema tables if they don't exist.
func RunMigrations(ctx context.Context, pool *Pool) error {
	p := pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	// Ensure search_path is set (needed after DROP/CREATE SCHEMA public).
	_, _ = p.Exec(ctx, `SET search_path TO public`)

	ddl := `
CREATE TABLE IF NOT EXISTS database_tables (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    label TEXT NOT NULL DEFAULT '',
    scope_regexp TEXT NOT NULL DEFAULT '.*',
    page_folder TEXT NOT NULL DEFAULT '',
    index_field TEXT NOT NULL DEFAULT '',
    default_sort_field TEXT NOT NULL DEFAULT '',
    default_sort_order TEXT NOT NULL DEFAULT 'asc',
    page_template_path TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS database_fields (
    id SERIAL PRIMARY KEY,
    table_id INTEGER NOT NULL REFERENCES database_tables(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    required BOOLEAN NOT NULL DEFAULT FALSE,
    default_value TEXT NOT NULL DEFAULT '',
    display_order INTEGER NOT NULL DEFAULT 0,
    placeholder TEXT NOT NULL DEFAULT '',
    foreign_key TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at TIMESTAMPTZ,
    UNIQUE (table_id, name)
);

CREATE TABLE IF NOT EXISTS database_enum_values (
    id SERIAL PRIMARY KEY,
    field_id INTEGER NOT NULL REFERENCES database_fields(id) ON DELETE CASCADE,
    value TEXT NOT NULL,
    display_order INTEGER NOT NULL DEFAULT 0,
    UNIQUE (field_id, value)
);

CREATE TABLE IF NOT EXISTS database_schema_history (
    id SERIAL PRIMARY KEY,
    table_id INTEGER NOT NULL REFERENCES database_tables(id) ON DELETE CASCADE,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    changed_by TEXT NOT NULL DEFAULT '',
    change_type TEXT NOT NULL,
    field_name TEXT NOT NULL DEFAULT '',
    field_type TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT ''
);
`
	_, err := p.Exec(ctx, ddl)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
