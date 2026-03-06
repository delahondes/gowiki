package todo

import (
	"context"
	"fmt"

	"gowiki/backend/internal/database"
)

// RunMigrations creates the todo tables if they don't exist.
func RunMigrations(ctx context.Context, pool *database.Pool) error {
	p := pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	ddl := `
CREATE TABLE IF NOT EXISTS todo_tasks (
    id                  TEXT PRIMARY KEY,
    title               TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'open',
    source              TEXT NOT NULL DEFAULT 'api',
    source_page         TEXT NOT NULL DEFAULT '',
    node_key            TEXT NOT NULL DEFAULT '',
    assignee_type       TEXT NOT NULL DEFAULT 'user',
    assignee_target     TEXT NOT NULL DEFAULT '',
    assignee_resolution TEXT NOT NULL DEFAULT 'any',
    due_date            DATE,
    recur_type          TEXT NOT NULL DEFAULT '',
    recur_days          INTEGER NOT NULL DEFAULT 0,
    recur_every         INTEGER NOT NULL DEFAULT 1,
    recur_unit          TEXT NOT NULL DEFAULT '',
    recurrence_group_id TEXT NOT NULL DEFAULT '',
    wiki_action_type    TEXT NOT NULL DEFAULT '',
    wiki_action_page    TEXT NOT NULL DEFAULT '',
    wiki_action_pattern TEXT NOT NULL DEFAULT '',
    wiki_action_template TEXT NOT NULL DEFAULT '',
    wiki_action_schema  TEXT NOT NULL DEFAULT '',
    wiki_action_field   TEXT NOT NULL DEFAULT '',
    wiki_action_value   TEXT NOT NULL DEFAULT '',
    tags                TEXT NOT NULL DEFAULT '',
    priority            TEXT NOT NULL DEFAULT 'normal',
    created_by          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS todo_completions (
    task_id      TEXT NOT NULL REFERENCES todo_tasks(id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_todo_tasks_status ON todo_tasks(status);
CREATE INDEX IF NOT EXISTS idx_todo_tasks_source_page ON todo_tasks(source_page);
CREATE INDEX IF NOT EXISTS idx_todo_tasks_assignee ON todo_tasks(assignee_target);
CREATE INDEX IF NOT EXISTS idx_todo_tasks_due ON todo_tasks(due_date);
CREATE UNIQUE INDEX IF NOT EXISTS idx_todo_tasks_node_key ON todo_tasks(source_page, node_key)
    WHERE source = 'wiki_node' AND node_key != '';
`
	_, err := p.Exec(ctx, ddl)
	if err != nil {
		return fmt.Errorf("run todo migrations: %w", err)
	}

	// Add acknowledged_version column for read-ack tracking.
	_, err = p.Exec(ctx, `ALTER TABLE todo_completions ADD COLUMN IF NOT EXISTS acknowledged_version BIGINT NOT NULL DEFAULT 0`)
	if err != nil {
		return fmt.Errorf("add acknowledged_version column: %w", err)
	}

	return nil
}
