package todo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"gowiki/backend/internal/database"
)

// TodoStore provides CRUD operations for tasks backed by PostgreSQL.
type TodoStore struct {
	pool *database.Pool
}

// NewTodoStore creates a new store.
func NewTodoStore(pool *database.Pool) *TodoStore {
	return &TodoStore{pool: pool}
}

// newID generates a 32-char hex ID using crypto/rand.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Create inserts a new task.
func (s *TodoStore) Create(ctx context.Context, req CreateRequest) (*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	id := newID()
	now := time.Now().UTC()

	if req.Priority == "" {
		req.Priority = PriorityNormal
	}
	if req.Source == "" {
		req.Source = SourceAPI
	}
	if req.Assignee.Resolution == "" {
		req.Assignee.Resolution = "any"
	}
	if req.Assignee.Type == "" {
		req.Assignee.Type = "user"
	}

	_, err := p.Exec(ctx, `
		INSERT INTO todo_tasks (
			id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10,
			$11, $12, $13, $14, $15,
			$16,
			$17, $18, $19, $20, $21, $22, $23,
			$24, $25, $26, $27, $28
		)`,
		id, req.Title, req.Description, string(StatusOpen), string(req.Source), req.SourcePage, req.NodeKey,
		req.Assignee.Type, req.Assignee.Target, req.Assignee.Resolution,
		nullableDate(req.DueDate), req.Recurrence.Type, req.Recurrence.Days, req.Recurrence.Every, req.Recurrence.Unit,
		req.RecurrenceGroupID,
		req.WikiAction.Type, req.WikiAction.Page, req.WikiAction.Pattern,
		req.WikiAction.Template, req.WikiAction.Schema, req.WikiAction.Field, req.WikiAction.Value,
		req.Tags, string(req.Priority), req.CreatedBy, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert task: %w", err)
	}

	return &Task{
		ID:          id,
		Title:       req.Title,
		Description: req.Description,
		Status:      StatusOpen,
		Source:      req.Source,
		SourcePage:  req.SourcePage,
		NodeKey:     req.NodeKey,
		Assignee:    req.Assignee,
		DueDate:     req.DueDate,
		Recurrence:  req.Recurrence,
		WikiAction:  req.WikiAction,
		Tags:        req.Tags,
		Priority:    req.Priority,
		CreatedBy:   req.CreatedBy,
		CreatedAt:   now,
		UpdatedAt:   now,
		RecurrenceGroupID: req.RecurrenceGroupID,
	}, nil
}

// Get retrieves a single task by ID.
func (s *TodoStore) Get(ctx context.Context, id string) (*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	row := p.QueryRow(ctx, `
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks WHERE id = $1`, id)

	return scanTask(row)
}

// Update modifies a task with the given patch.
func (s *TodoStore) Update(ctx context.Context, id string, patch Patch) (*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	var sets []string
	var args []any
	argN := 1

	if patch.Title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", argN))
		args = append(args, *patch.Title)
		argN++
	}
	if patch.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", argN))
		args = append(args, *patch.Description)
		argN++
	}
	if patch.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argN))
		args = append(args, string(*patch.Status))
		argN++
	}
	if patch.Assignee != nil {
		sets = append(sets, fmt.Sprintf("assignee_type = $%d", argN))
		args = append(args, patch.Assignee.Type)
		argN++
		sets = append(sets, fmt.Sprintf("assignee_target = $%d", argN))
		args = append(args, patch.Assignee.Target)
		argN++
		sets = append(sets, fmt.Sprintf("assignee_resolution = $%d", argN))
		args = append(args, patch.Assignee.Resolution)
		argN++
	}
	if patch.DueDate != nil {
		sets = append(sets, fmt.Sprintf("due_date = $%d", argN))
		args = append(args, nullableDate(*patch.DueDate))
		argN++
	}
	if patch.Recurrence != nil {
		sets = append(sets, fmt.Sprintf("recur_type = $%d", argN))
		args = append(args, patch.Recurrence.Type)
		argN++
		sets = append(sets, fmt.Sprintf("recur_days = $%d", argN))
		args = append(args, patch.Recurrence.Days)
		argN++
		sets = append(sets, fmt.Sprintf("recur_every = $%d", argN))
		args = append(args, patch.Recurrence.Every)
		argN++
		sets = append(sets, fmt.Sprintf("recur_unit = $%d", argN))
		args = append(args, patch.Recurrence.Unit)
		argN++
	}
	if patch.WikiAction != nil {
		sets = append(sets, fmt.Sprintf("wiki_action_type = $%d", argN))
		args = append(args, patch.WikiAction.Type)
		argN++
		sets = append(sets, fmt.Sprintf("wiki_action_page = $%d", argN))
		args = append(args, patch.WikiAction.Page)
		argN++
		sets = append(sets, fmt.Sprintf("wiki_action_pattern = $%d", argN))
		args = append(args, patch.WikiAction.Pattern)
		argN++
		sets = append(sets, fmt.Sprintf("wiki_action_template = $%d", argN))
		args = append(args, patch.WikiAction.Template)
		argN++
		sets = append(sets, fmt.Sprintf("wiki_action_schema = $%d", argN))
		args = append(args, patch.WikiAction.Schema)
		argN++
		sets = append(sets, fmt.Sprintf("wiki_action_field = $%d", argN))
		args = append(args, patch.WikiAction.Field)
		argN++
		sets = append(sets, fmt.Sprintf("wiki_action_value = $%d", argN))
		args = append(args, patch.WikiAction.Value)
		argN++
	}
	if patch.Tags != nil {
		sets = append(sets, fmt.Sprintf("tags = $%d", argN))
		args = append(args, *patch.Tags)
		argN++
	}
	if patch.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", argN))
		args = append(args, string(*patch.Priority))
		argN++
	}

	if len(sets) == 0 {
		return s.Get(ctx, id)
	}

	sets = append(sets, fmt.Sprintf("updated_at = $%d", argN))
	args = append(args, time.Now().UTC())
	argN++

	args = append(args, id)
	query := fmt.Sprintf("UPDATE todo_tasks SET %s WHERE id = $%d", strings.Join(sets, ", "), argN)

	tag, err := p.Exec(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("task not found")
	}

	return s.Get(ctx, id)
}

// Complete marks a task as done by the given user.
// For group tasks with resolution="all", records per-user completion
// and promotes to done when all members have completed.
func (s *TodoStore) Complete(ctx context.Context, id, userID string, resolver GroupResolver) (*Task, bool, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, false, fmt.Errorf("database not connected")
	}

	task, err := s.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if task.Status == StatusDone || task.Status == StatusCancelled {
		return task, false, nil
	}

	// Record completion.
	_, err = p.Exec(ctx, `
		INSERT INTO todo_completions (task_id, user_id, completed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (task_id, user_id) DO NOTHING`,
		id, userID, time.Now().UTC())
	if err != nil {
		return nil, false, fmt.Errorf("record completion: %w", err)
	}

	// Check if task should be promoted to done.
	promoted := false
	if task.Assignee.Type == "group" && task.Assignee.Resolution == "all" && resolver != nil {
		members := resolver.GroupMembers(task.Assignee.Target)
		completions, err := s.ListCompletions(ctx, id)
		if err != nil {
			return nil, false, err
		}
		completedUsers := make(map[string]bool, len(completions))
		for _, c := range completions {
			completedUsers[c.UserID] = true
		}
		allDone := true
		for _, m := range members {
			if !completedUsers[m] {
				allDone = false
				break
			}
		}
		if allDone && len(members) > 0 {
			promoted = true
		}
	} else {
		// For "any" resolution or user tasks, immediate completion.
		promoted = true
	}

	if promoted {
		now := time.Now().UTC()
		_, err = p.Exec(ctx, `UPDATE todo_tasks SET status = $1, updated_at = $2 WHERE id = $3`,
			string(StatusDone), now, id)
		if err != nil {
			return nil, false, fmt.Errorf("promote task to done: %w", err)
		}
	}

	task, err = s.Get(ctx, id)
	return task, promoted, err
}

// Reopen sets a task back to open status.
func (s *TodoStore) Reopen(ctx context.Context, id string) (*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	now := time.Now().UTC()
	_, err := p.Exec(ctx, `UPDATE todo_tasks SET status = $1, updated_at = $2 WHERE id = $3`,
		string(StatusOpen), now, id)
	if err != nil {
		return nil, fmt.Errorf("reopen task: %w", err)
	}

	// Clear completions.
	_, _ = p.Exec(ctx, `DELETE FROM todo_completions WHERE task_id = $1`, id)

	return s.Get(ctx, id)
}

// Cancel sets a task to cancelled status.
func (s *TodoStore) Cancel(ctx context.Context, id string) (*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	now := time.Now().UTC()
	_, err := p.Exec(ctx, `UPDATE todo_tasks SET status = $1, updated_at = $2 WHERE id = $3`,
		string(StatusCancelled), now, id)
	if err != nil {
		return nil, fmt.Errorf("cancel task: %w", err)
	}
	return s.Get(ctx, id)
}

// List returns tasks matching the given options with cursor-based pagination.
func (s *TodoStore) List(ctx context.Context, opts ListOptions) ([]*Task, string, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, "", fmt.Errorf("database not connected")
	}

	var conditions []string
	var args []any
	argN := 1

	if opts.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argN))
		args = append(args, string(opts.Status))
		argN++
	}
	if opts.Assignee != "" {
		conditions = append(conditions, fmt.Sprintf("assignee_target = $%d", argN))
		args = append(args, opts.Assignee)
		argN++
	}
	if opts.Page != "" {
		conditions = append(conditions, fmt.Sprintf("source_page = $%d", argN))
		args = append(args, opts.Page)
		argN++
	}
	if opts.Tag != "" {
		conditions = append(conditions, fmt.Sprintf("tags LIKE $%d", argN))
		args = append(args, "%"+opts.Tag+"%")
		argN++
	}
	if opts.Priority != "" {
		conditions = append(conditions, fmt.Sprintf("priority = $%d", argN))
		args = append(args, string(opts.Priority))
		argN++
	}

	// Cursor decode: base64 of "updated_at|id".
	if opts.Cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(opts.Cursor)
		if err == nil {
			parts := strings.SplitN(string(decoded), "|", 2)
			if len(parts) == 2 {
				conditions = append(conditions, fmt.Sprintf("(updated_at, id) < ($%d, $%d)", argN, argN+1))
				args = append(args, parts[0], parts[1])
				argN += 2
			}
		}
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := opts.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := fmt.Sprintf(`
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks %s
		ORDER BY updated_at DESC, id DESC
		LIMIT $%d`, where, argN)
	args = append(args, limit+1) // fetch one extra for cursor

	rows, err := p.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		t, err := scanTaskFromRows(rows)
		if err != nil {
			return nil, "", err
		}
		tasks = append(tasks, t)
	}

	nextCursor := ""
	if len(tasks) > limit {
		last := tasks[limit-1]
		tasks = tasks[:limit]
		cursorVal := last.UpdatedAt.Format(time.RFC3339Nano) + "|" + last.ID
		nextCursor = base64.StdEncoding.EncodeToString([]byte(cursorVal))
	}

	return tasks, nextCursor, nil
}

// ListForPage returns all tasks associated with a page.
func (s *TodoStore) ListForPage(ctx context.Context, pagePath string) ([]*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks WHERE source_page = $1
		ORDER BY created_at ASC`, pagePath)
	if err != nil {
		return nil, fmt.Errorf("list tasks for page: %w", err)
	}
	defer rows.Close()

	return collectTasks(rows)
}

// ListMine returns tasks assigned to a user directly or via group membership.
func (s *TodoStore) ListMine(ctx context.Context, userID string, groups []string) ([]*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	targets := []string{userID}
	targets = append(targets, groups...)

	rows, err := p.Query(ctx, `
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks
		WHERE status IN ('open', 'in_progress')
		  AND assignee_target = ANY($1)
		ORDER BY due_date ASC NULLS LAST, priority DESC, created_at ASC`, targets)
	if err != nil {
		return nil, fmt.Errorf("list my tasks: %w", err)
	}
	defer rows.Close()

	return collectTasks(rows)
}

// ListCompletions returns all completions for a task.
func (s *TodoStore) ListCompletions(ctx context.Context, taskID string) ([]*Completion, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `
		SELECT task_id, user_id, completed_at, acknowledged_version
		FROM todo_completions WHERE task_id = $1
		ORDER BY completed_at ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list completions: %w", err)
	}
	defer rows.Close()

	var completions []*Completion
	for rows.Next() {
		var c Completion
		if err := rows.Scan(&c.TaskID, &c.UserID, &c.CompletedAt, &c.AcknowledgedVersion); err != nil {
			return nil, fmt.Errorf("scan completion: %w", err)
		}
		completions = append(completions, &c)
	}
	return completions, nil
}

// UpsertForPage reconciles wiki_node tasks for a page.
// Creates new tasks for new directives, updates existing ones, and cancels removed ones.
func (s *TodoStore) UpsertForPage(ctx context.Context, pagePath string, directives []ParsedDirective, createdBy string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	// Get existing wiki_node tasks for this page.
	existing, err := s.ListForPage(ctx, pagePath)
	if err != nil {
		return err
	}

	existingByKey := make(map[string]*Task)
	for _, t := range existing {
		if t.Source == SourceWikiNode && t.NodeKey != "" {
			existingByKey[t.NodeKey] = t
		}
	}

	seenKeys := make(map[string]bool)
	for _, d := range directives {
		nodeKey := computeNodeKey(pagePath, d.Title, d.Assign)
		seenKeys[nodeKey] = true

		// Parse assignee.
		assignee := Assignee{Type: "user", Target: "", Resolution: "any"}
		if d.Assign != "" {
			target := strings.TrimPrefix(d.Assign, "@")
			if strings.HasPrefix(target, "group:") {
				assignee.Type = "group"
				assignee.Target = strings.TrimPrefix(target, "group:")
			} else {
				assignee.Target = target
			}
		}
		if d.Resolution != "" {
			assignee.Resolution = d.Resolution
		}

		priority := Priority(d.Priority)
		if priority == "" {
			priority = PriorityNormal
		}

		action := parseAction(d.Action)
		// Resolve relative action paths against the source page.
		if action.Page != "" {
			action.Page = resolveActionPath(pagePath, action.Page)
		}

		if existing, exists := existingByKey[nodeKey]; exists {
			// Update mutable fields on existing task.
			patch := Patch{}
			if d.Due != existing.DueDate {
				patch.DueDate = &d.Due
			}
			if action.Page != existing.WikiAction.Page || action.Type != existing.WikiAction.Type ||
				action.Pattern != existing.WikiAction.Pattern || action.Schema != existing.WikiAction.Schema ||
				action.Field != existing.WikiAction.Field || action.Value != existing.WikiAction.Value {
				patch.WikiAction = &action
			}
			if string(priority) != string(existing.Priority) {
				p := priority
				patch.Priority = &p
			}
			if d.Tags != existing.Tags {
				patch.Tags = &d.Tags
			}
			if d.Description != existing.Description {
				patch.Description = &d.Description
			}
			rec := parseRecur(d.Recur)
			if rec != existing.Recurrence {
				patch.Recurrence = &rec
			}
			if !patch.IsEmpty() {
				if _, err := s.Update(ctx, existing.ID, patch); err != nil {
					log.Printf("todo: upsert update failed for task %s: %v", existing.ID, err)
				}
			}
			continue
		}

		req := CreateRequest{
			Title:       d.Title,
			Description: d.Description,
			Source:      SourceWikiNode,
			SourcePage:  pagePath,
			NodeKey:     nodeKey,
			Assignee:    assignee,
			DueDate:     d.Due,
			Recurrence:  parseRecur(d.Recur),
			WikiAction:  action,
			Tags:        d.Tags,
			Priority:    priority,
			CreatedBy:   createdBy,
		}

		if _, err := s.Create(ctx, req); err != nil {
			log.Printf("todo: upsert create failed for page %s: %v", pagePath, err)
		}
	}

	// Delete tasks whose directives were removed from the page.
	for key, task := range existingByKey {
		if !seenKeys[key] {
			if err := s.Delete(ctx, task.ID); err != nil {
				log.Printf("todo: delete removed task %s failed: %v", task.ID, err)
			}
		}
	}

	return nil
}

// ListWikiActionTasks returns open tasks with a matching wiki action.
func (s *TodoStore) ListWikiActionTasks(ctx context.Context, actionType, pagePath string) ([]*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks
		WHERE status IN ('open', 'in_progress')
		  AND wiki_action_type = $1
		  AND wiki_action_page = $2`, actionType, pagePath)
	if err != nil {
		return nil, fmt.Errorf("list wiki action tasks: %w", err)
	}
	defer rows.Close()

	return collectTasks(rows)
}

// ListCreateActionTasks returns open tasks with wiki_action_type='create' and a non-empty pattern.
func (s *TodoStore) ListCreateActionTasks(ctx context.Context) ([]*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks
		WHERE status IN ('open', 'in_progress')
		  AND wiki_action_type = 'create'
		  AND wiki_action_pattern != ''`)
	if err != nil {
		return nil, fmt.Errorf("list create action tasks: %w", err)
	}
	defer rows.Close()

	return collectTasks(rows)
}

// ListDueBetween returns tasks with due dates in the given range.
func (s *TodoStore) ListDueBetween(ctx context.Context, from, to time.Time) ([]*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks
		WHERE status IN ('open', 'in_progress')
		  AND due_date >= $1 AND due_date <= $2`, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("list due tasks: %w", err)
	}
	defer rows.Close()

	return collectTasks(rows)
}

// ListOverdue returns open tasks past their due date.
func (s *TodoStore) ListOverdue(ctx context.Context, now time.Time) ([]*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks
		WHERE status IN ('open', 'in_progress')
		  AND due_date < $1
		  AND due_date IS NOT NULL`, now.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("list overdue tasks: %w", err)
	}
	defer rows.Close()

	return collectTasks(rows)
}

// CancelAllForPage cancels all open tasks for a page (used on page delete).
func (s *TodoStore) CancelAllForPage(ctx context.Context, pagePath string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	_, err := p.Exec(ctx, `
		UPDATE todo_tasks SET status = $1, updated_at = $2
		WHERE source_page = $3 AND status IN ('open', 'in_progress')`,
		string(StatusCancelled), time.Now().UTC(), pagePath)
	if err != nil {
		return fmt.Errorf("cancel tasks for page: %w", err)
	}
	return nil
}

// Delete removes a task by ID.
func (s *TodoStore) Delete(ctx context.Context, id string) error {
	p := s.pool.GetPool()
	if p == nil {
		return fmt.Errorf("database not connected")
	}

	tag, err := p.Exec(ctx, `DELETE FROM todo_tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task not found")
	}
	return nil
}

// PendingAck represents a read-ack task pending for a user on a specific page.
type PendingAck struct {
	TaskID             string `json:"task_id"`
	Title              string `json:"title"`
	PreviousAckVersion int64  `json:"previous_ack_version"`
}

// ListPendingAcks returns open read-action tasks for a page that the given user
// (or their groups) must acknowledge.
func (s *TodoStore) ListPendingAcks(ctx context.Context, pagePath, userID string, groups []string) ([]PendingAck, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	targets := []string{userID}
	targets = append(targets, groups...)

	rows, err := p.Query(ctx, `
		SELECT t.id, t.title, COALESCE(c.acknowledged_version, 0) AS prev_ack_version
		FROM todo_tasks t
		LEFT JOIN todo_completions c ON c.task_id = t.id AND c.user_id = $3
		WHERE t.status IN ('open', 'in_progress')
		  AND t.wiki_action_type = 'read'
		  AND t.wiki_action_page = $1
		  AND t.assignee_target = ANY($2)`,
		pagePath, targets, userID)
	if err != nil {
		return nil, fmt.Errorf("list pending acks: %w", err)
	}
	defer rows.Close()

	var result []PendingAck
	for rows.Next() {
		var pa PendingAck
		if err := rows.Scan(&pa.TaskID, &pa.Title, &pa.PreviousAckVersion); err != nil {
			return nil, fmt.Errorf("scan pending ack: %w", err)
		}
		result = append(result, pa)
	}
	return result, nil
}

// Acknowledge records a read acknowledgement for a task, upserting the completion
// with the acknowledged version. Returns the updated task and whether it was promoted to done.
func (s *TodoStore) Acknowledge(ctx context.Context, id, userID string, version int64, resolver GroupResolver) (*Task, bool, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, false, fmt.Errorf("database not connected")
	}

	task, err := s.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if task.Status == StatusDone || task.Status == StatusCancelled {
		return task, false, nil
	}

	now := time.Now().UTC()
	_, err = p.Exec(ctx, `
		INSERT INTO todo_completions (task_id, user_id, completed_at, acknowledged_version)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (task_id, user_id) DO UPDATE
		SET completed_at = $3, acknowledged_version = $4`,
		id, userID, now, version)
	if err != nil {
		return nil, false, fmt.Errorf("record acknowledgement: %w", err)
	}

	// Check if task should be promoted to done (same logic as Complete).
	promoted := false
	if task.Assignee.Type == "group" && task.Assignee.Resolution == "all" && resolver != nil {
		members := resolver.GroupMembers(task.Assignee.Target)
		completions, err := s.ListCompletions(ctx, id)
		if err != nil {
			return nil, false, err
		}
		completedUsers := make(map[string]bool, len(completions))
		for _, c := range completions {
			completedUsers[c.UserID] = true
		}
		allDone := true
		for _, m := range members {
			if !completedUsers[m] {
				allDone = false
				break
			}
		}
		if allDone && len(members) > 0 {
			promoted = true
		}
	} else {
		promoted = true
	}

	if promoted {
		_, err = p.Exec(ctx, `UPDATE todo_tasks SET status = $1, updated_at = $2 WHERE id = $3`,
			string(StatusDone), now, id)
		if err != nil {
			return nil, false, fmt.Errorf("promote task to done: %w", err)
		}
	}

	task, err = s.Get(ctx, id)
	return task, promoted, err
}

// ReopenKeepCompletions sets a task back to open status but preserves completion
// records (used for read-ack tasks where we need the previous ack version).
func (s *TodoStore) ReopenKeepCompletions(ctx context.Context, id string) (*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	now := time.Now().UTC()
	_, err := p.Exec(ctx, `UPDATE todo_tasks SET status = $1, updated_at = $2 WHERE id = $3`,
		string(StatusOpen), now, id)
	if err != nil {
		return nil, fmt.Errorf("reopen task: %w", err)
	}

	return s.Get(ctx, id)
}

// ListDoneReadTasks returns completed read-action tasks for a page.
func (s *TodoStore) ListDoneReadTasks(ctx context.Context, pagePath string) ([]*Task, error) {
	p := s.pool.GetPool()
	if p == nil {
		return nil, fmt.Errorf("database not connected")
	}

	rows, err := p.Query(ctx, `
		SELECT id, title, description, status, source, source_page, node_key,
			assignee_type, assignee_target, assignee_resolution,
			due_date, recur_type, recur_days, recur_every, recur_unit,
			recurrence_group_id,
			wiki_action_type, wiki_action_page, wiki_action_pattern,
			wiki_action_template, wiki_action_schema, wiki_action_field, wiki_action_value,
			tags, priority, created_by, created_at, updated_at
		FROM todo_tasks
		WHERE status = 'done'
		  AND wiki_action_type = 'read'
		  AND wiki_action_page = $1`, pagePath)
	if err != nil {
		return nil, fmt.Errorf("list done read tasks: %w", err)
	}
	defer rows.Close()

	return collectTasks(rows)
}

// --- Scan helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanTask(row scannable) (*Task, error) {
	var t Task
	var dueDate *time.Time
	err := row.Scan(
		&t.ID, &t.Title, &t.Description, &t.Status, &t.Source, &t.SourcePage, &t.NodeKey,
		&t.Assignee.Type, &t.Assignee.Target, &t.Assignee.Resolution,
		&dueDate, &t.Recurrence.Type, &t.Recurrence.Days, &t.Recurrence.Every, &t.Recurrence.Unit,
		&t.RecurrenceGroupID,
		&t.WikiAction.Type, &t.WikiAction.Page, &t.WikiAction.Pattern,
		&t.WikiAction.Template, &t.WikiAction.Schema, &t.WikiAction.Field, &t.WikiAction.Value,
		&t.Tags, &t.Priority, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	if dueDate != nil {
		t.DueDate = dueDate.Format("2006-01-02")
	}
	return &t, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTaskFromRows(rows rowScanner) (*Task, error) {
	return scanTask(rows)
}

func collectTasks(rows interface {
	Next() bool
	Scan(dest ...any) error
}) ([]*Task, error) {
	var tasks []*Task
	for rows.Next() {
		t, err := scanTaskFromRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// nullableDate converts "" to nil for DB DATE columns.
func nullableDate(s string) any {
	if s == "" {
		return nil
	}
	return s
}
