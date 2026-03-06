package todo

import "time"

// Status represents the lifecycle state of a task.
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusCancelled  Status = "cancelled"
)

// Priority represents the urgency of a task.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

// Source indicates how the task was created.
type Source string

const (
	SourceAPI      Source = "api"
	SourceWikiNode Source = "wiki_node"
)

// Task is the core domain object.
type Task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      Status   `json:"status"`
	Source      Source   `json:"source"`
	SourcePage  string   `json:"source_page,omitempty"`
	NodeKey     string   `json:"node_key,omitempty"`
	Assignee    Assignee `json:"assignee"`
	DueDate     string   `json:"due_date,omitempty"` // YYYY-MM-DD or ""
	Recurrence  Recurrence `json:"recurrence,omitempty"`
	WikiAction  WikiAction `json:"wiki_action,omitempty"`
	Tags        string   `json:"tags,omitempty"`
	Priority    Priority `json:"priority"`
	CreatedBy   string   `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	RecurrenceGroupID string `json:"recurrence_group_id,omitempty"`
}

// Assignee describes who is responsible for a task.
type Assignee struct {
	Type       string `json:"type"`       // "user" or "group"
	Target     string `json:"target"`     // username or group name
	Resolution string `json:"resolution"` // "any" or "all"
}

// Recurrence describes how a task repeats after completion.
type Recurrence struct {
	Type  string `json:"type,omitempty"`  // "delay" or "calendar"
	Days  int    `json:"days,omitempty"`  // for delay type
	Every int    `json:"every,omitempty"` // for calendar type
	Unit  string `json:"unit,omitempty"`  // "day", "week", "month", "year"
}

// IsZero returns true if no recurrence is configured.
func (r Recurrence) IsZero() bool {
	return r.Type == ""
}

// WikiAction describes an automatic action trigger.
type WikiAction struct {
	Type     string `json:"type,omitempty"`     // "read", "edit", "create", "set_meta"
	Page     string `json:"page,omitempty"`     // target page path
	Pattern  string `json:"pattern,omitempty"`  // glob pattern for "create"
	Template string `json:"template,omitempty"` // template path for "create"
	Schema   string `json:"schema,omitempty"`   // for "set_meta"
	Field    string `json:"field,omitempty"`    // for "set_meta"
	Value    string `json:"value,omitempty"`    // for "set_meta"
}

// IsZero returns true if no wiki action is configured.
func (w WikiAction) IsZero() bool {
	return w.Type == ""
}

// Completion records a single user's completion of a task.
type Completion struct {
	TaskID              string    `json:"task_id"`
	UserID              string    `json:"user_id"`
	CompletedAt         time.Time `json:"completed_at"`
	AcknowledgedVersion int64     `json:"acknowledged_version,omitempty"`
}

// Event is emitted via SSE when task state changes.
type Event struct {
	Type string `json:"type"` // "task.created", "task.updated", "task.completed", "task.reopened"
	Task *Task  `json:"task"`
}

// CreateRequest holds the fields for creating a new task.
type CreateRequest struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Source      Source     `json:"source"`
	SourcePage  string     `json:"source_page"`
	NodeKey     string     `json:"node_key"`
	Assignee    Assignee   `json:"assignee"`
	DueDate     string     `json:"due_date"`
	Recurrence  Recurrence `json:"recurrence"`
	WikiAction  WikiAction `json:"wiki_action"`
	Tags        string     `json:"tags"`
	Priority    Priority   `json:"priority"`
	CreatedBy   string     `json:"created_by"`
	RecurrenceGroupID string `json:"recurrence_group_id"`
}

// Patch holds optional fields for updating a task.
type Patch struct {
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	Status      *Status    `json:"status,omitempty"`
	Assignee    *Assignee  `json:"assignee,omitempty"`
	DueDate     *string    `json:"due_date,omitempty"`
	Recurrence  *Recurrence `json:"recurrence,omitempty"`
	WikiAction  *WikiAction `json:"wiki_action,omitempty"`
	Tags        *string    `json:"tags,omitempty"`
	Priority    *Priority  `json:"priority,omitempty"`
}

// IsEmpty returns true if no fields are set.
func (p Patch) IsEmpty() bool {
	return p.Title == nil && p.Description == nil && p.Status == nil &&
		p.Assignee == nil && p.DueDate == nil && p.Recurrence == nil &&
		p.WikiAction == nil && p.Tags == nil && p.Priority == nil
}

// ListOptions controls task listing with filtering and pagination.
type ListOptions struct {
	Status   Status `json:"status,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	Page     string `json:"page,omitempty"`
	Tag      string `json:"tag,omitempty"`
	Priority Priority `json:"priority,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// ParsedDirective is the result of extracting a {todo ...} directive from markdown.
type ParsedDirective struct {
	Title       string
	Assign      string
	Resolution  string
	Due         string
	Recur       string
	Priority    string
	Action      string
	Tags        string
	Description string
	NodeKey     string // computed from title + assign
}

// NotifyEvent describes a notification to be sent.
type NotifyEvent struct {
	Type      string // "assigned", "due_reminder", "overdue", "completed_all", "recurrence_spawned"
	Task      *Task
	Recipient string // email address or webhook target
	UserID    string
}

// GroupResolver resolves group membership for "all" resolution tasks.
type GroupResolver interface {
	GroupMembers(groupName string) []string
}
