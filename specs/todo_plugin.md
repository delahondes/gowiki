# Gowiki — Todo Plugin Specification


## 1. Overview

The **Todo plugin** is a first-class task-management extension for Gowiki. It consolidates the functionality of several DokuWiki plugins (todo, bureaucracy, struct, and notification) into a single coherent system with a clean separation between a **Go backend API** and a **TypeScript frontend plugin**.

### Design Goals

- Assign tasks to individual users or groups, with fine-grained "any/all" semantics for group resolution
- Support optional due dates with simple recurrence (fixed delay in days, or calendar-based repetition)
- Optionally bind a task to a wiki action (read, edit, create, or set meta on a page)
- Expose a stable internal Go API so other plugins can create, query, or update tasks programmatically
- Notify assignees via email and/or an outbound webhook (Zulip, Slack, or any compatible system)
- **Requires PostgreSQL** — the plugin cannot be activated if the Gowiki instance has no PostgreSQL connection

---

## 2. Architecture

```
┌─────────────────────────────────────────────────────┐
│                   Gowiki Core                       │
│                                                     │
│  ┌──────────────────┐    ┌────────────────────────┐ │
│  │  Go Backend API  │◄───│  Other Go Plugins       │ │
│  │  /api/plugin/    │    │  (struct, acl, …)       │ │
│  │  todo/v1/…       │    └────────────────────────┘ │
│  │                  │                               │
│  │  TodoStore       │    ┌────────────────────────┐ │
│  │  (PostgreSQL)    │    │  Notification           │ │
│  │                  │    │  Dispatcher             │ │
│  │  RecurrenceEng.  │    │  (email + webhooks)     │ │
│  └────────┬─────────┘    └────────────────────────┘ │
│           │ REST / SSE                               │
└───────────┼─────────────────────────────────────────┘
            │
┌───────────▼─────────────────────────────────────────┐
│               TypeScript Frontend Plugin             │
│                                                     │
│  TodoGate (fetch wrapper + event bus)               │
│  ├── TodoWidget  (inline wiki node renderer)        │
│  ├── TodoPanel  (sidebar / dashboard view)          │
│  └── Hooks into other TS plugins (e.g. editor)      │
└─────────────────────────────────────────────────────┘
```

The **Go backend** is intentionally **not** a plugin binary — it compiles into the core Gowiki binary and registers itself at startup so that it is always available as a dependency for other plugins. The **TS frontend** is a standard Gowiki plugin bundle.

At startup the plugin checks for a live PostgreSQL connection. If none is configured, the plugin logs a warning and stays inactive — no routes are registered, no TS assets are served.

---

## 3. Data Model

### 3.1 Task

| Field | Type | Required | Notes |
|---|---|---|---|
| `id` | `uuid` | auto | Stable public identifier |
| `title` | `string` | ✓ | Short human label |
| `description` | `string` | | Markdown body |
| `status` | `enum` | ✓ | `open \| in_progress \| done \| cancelled` |
| `created_at` | `timestamp` | auto | |
| `updated_at` | `timestamp` | auto | |
| `created_by` | `user_id` | auto | |
| `source` | `enum` | auto | `wiki_node \| api` — see §8.4 |
| `assignee` | `Assignee` | ✓ | See §3.2 |
| `due` | `DueDate` | | See §3.3 |
| `wiki_action` | `WikiAction` | | See §3.4 |
| `source_page` | `page_path` | | Page where the task node lives (wiki_node tasks only) |
| `tags` | `[]string` | | Free-form labels |
| `priority` | `enum` | | `low \| normal \| high \| critical` |

### 3.2 Assignee

```jsonc
{
  "type": "user" | "group",
  "target": "<username or group name>",
  // only meaningful when type == "group":
  "resolution": "any" | "all"
  // "any"  → task is done when at least one member completes it
  // "all"  → every member must individually mark it done
}
```

When `resolution == "all"`, the store tracks per-member completion in a `completions` join table. The task's top-level `status` becomes `done` automatically once all current members have completed it. Individual completion records are visible only to the task creator and admins.

Group membership is re-evaluated live from the ACL/user plugin — roster changes are reflected without re-creating tasks.

### 3.3 DueDate and Recurrence

The plugin uses a deliberately simple recurrence model covering the two patterns useful in practice:

**Fixed delay** — after the task is marked done, reopen it after N days. Useful for "check the backup logs every 3 days".

**Calendar repetition** — reopen at the next calendar unit boundary after completion. Useful for "renew the SSL certificate every year" or "send the monthly report every 3 months". As soon as a recurring task is marked done, the engine creates the next instance immediately.

```jsonc
{
  "at": "2026-04-01",          // first due date (YYYY-MM-DD, no time component)
  "recurrence": {
    // Option A — fixed delay in days:
    "type":   "delay",
    "days":   3

    // Option B — calendar repetition:
    // "type":     "calendar",
    // "every":    1,            // N units (e.g. 3 for "every 3 months")
    // "unit":     "day" | "week" | "month" | "year"
  }
}
```

When a recurring task is marked `done`, the engine computes the next due date and creates a fresh task linked to the same `recurrence_group_id`. The assignee is notified of the new instance.

### 3.4 WikiAction

Exactly one of the following shapes:

```jsonc
// Read a page (task done when the user visits it while authenticated)
{ "type": "read",   "page": "path/to/page" }

// Edit an existing page
{ "type": "edit",   "page": "path/to/page" }

// Create a new page from a template pattern
{
  "type":    "create",
  "pattern": "projects/{YYYY}/{slug}",  // {YYYY}, {MM}, {DD}, {slug} substituted
  "template": "templates:project"       // optional wiki page used as template
}

// Set a struct/meta field on a page
{
  "type":   "set_meta",
  "page":   "path/to/page",
  "schema": "project",
  "field":  "status",
  "value":  "approved"
}
```

The wiki action is optional. When present, completing the action via the wiki UI automatically marks the task `done` through Gowiki core event hooks.

---

## 4. Wiki Syntax

Tasks are declared as curly-brace block nodes, consistent with all other Gowiki block syntax.

**Multi-line form** (full options):

```
{todo
  title="Review Q1 budget"
  assign="@finance-team"
  resolution="all"
  due="2026-04-15"
  recur="monthly"
  priority="high"
  action="edit:path/to/budget-page"
}
```

**Single-line compact form** (title and assignee only):

```
{todo title="Review draft" assign="@alice" due="2026-04-30"}
```

**Attribute reference:**

| Attribute | Values | Notes |
|---|---|---|
| `title` | string | Required |
| `assign` | `@username` or `@groupname` | Required |
| `resolution` | `any` \| `all` | Only for group assignees; default `any` |
| `due` | `YYYY-MM-DD` | Optional |
| `recur` | `Nd` (e.g. `3d`) \| `daily` \| `weekly` \| `monthly` \| `yearly` \| `NM` (e.g. `3months`) | Requires `due` |
| `priority` | `low` \| `normal` \| `high` \| `critical` | Default `normal` |
| `action` | `read:path`, `edit:path`, `create:pattern`, `set_meta:path:schema:field:value` | Optional |
| `tags` | comma-separated strings | Optional |

The Go parser extracts `{todo}` nodes on page save and creates/updates corresponding task records. Removing the node from the page cancels the task. The TS plugin renders the node as an interactive widget in the page view.

---

## 5. Go Backend API

Base path: `/api/plugin/todo/v1`

All endpoints require a valid Gowiki session cookie or Bearer token. Responses are JSON.

### 5.1 Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/tasks` | List tasks (filterable, paginated) |
| `POST` | `/tasks` | Create a task |
| `GET` | `/tasks/{id}` | Get a single task |
| `PATCH` | `/tasks/{id}` | Update fields |
| `DELETE` | `/tasks/{id}` | Cancel / soft-delete |
| `POST` | `/tasks/{id}/complete` | Mark done (per-user for group tasks) |
| `POST` | `/tasks/{id}/reopen` | Reopen a completed task |
| `GET` | `/tasks/mine` | Tasks assigned to the calling user |
| `GET` | `/tasks/page/{path}` | Tasks linked to a specific wiki page |
| `GET` | `/stream` | SSE stream of task events for the calling user |

### 5.2 Query Parameters for `GET /tasks`

| Param | Values | Default |
|---|---|---|
| `status` | `open,in_progress,done,cancelled` | `open,in_progress` |
| `assignee` | username or group name | (all visible) |
| `page` | wiki page path | (all) |
| `tag` | tag string | (all) |
| `due_before` | ISO 8601 date | (unset) |
| `priority` | `low,normal,high,critical` | (all) |
| `limit` | integer 1–200 | `50` |
| `cursor` | opaque string | (first page) |

### 5.3 Go Plugin Interface

Other Go plugins import the package and interact through this interface:

```go
// package todoplugin

type Service interface {
    Create(ctx context.Context, t CreateRequest) (*Task, error)
    Get(ctx context.Context, id uuid.UUID) (*Task, error)
    Update(ctx context.Context, id uuid.UUID, patch Patch) (*Task, error)
    Complete(ctx context.Context, id uuid.UUID, userID string) (*Task, error)
    ListForPage(ctx context.Context, pagePath string, opts ListOptions) ([]*Task, error)
    Subscribe(ctx context.Context, userID string) (<-chan Event, error)
}

// Registration (called once at Gowiki startup):
func Register(core gowiki.Core) {
    core.RegisterService("todo", NewService(core))
}

// Retrieval from another plugin:
svc := core.Service("todo").(todoplugin.Service)
```

---

## 6. TypeScript Frontend Plugin

### 6.1 Package Layout

```
plugins/todo/
├── src/
│   ├── index.ts          // plugin entry — registers with Gowiki TS core
│   ├── gate.ts           // TodoGate: typed fetch wrapper + SSE client
│   ├── syntax.ts         // {todo …} node → DOM renderer
│   ├── components/
│   │   ├── TodoWidget.ts // inline task chip rendered in page body
│   │   ├── TodoPanel.ts  // sidebar dashboard
│   │   ├── TodoModal.ts  // create / edit task dialog
│   │   └── TodoBadge.ts  // notification badge in nav bar
│   └── types.ts          // mirrors Go data model
├── styles/
│   └── todo.css
└── manifest.json
```

### 6.2 TodoGate

`TodoGate` is the single point of contact between the TS plugin and the Go API:

```typescript
// gate.ts
export class TodoGate {
  constructor(private baseUrl = '/api/plugin/todo/v1') {}

  async list(opts: ListOptions): Promise<TaskList> { … }
  async create(req: CreateRequest): Promise<Task> { … }
  async complete(id: string): Promise<Task> { … }
  // … mirrors every REST endpoint

  /** SSE subscription — emits TaskEvent objects */
  subscribe(handler: (e: TaskEvent) => void): () => void { … }
}
```

`TodoGate` is exported on `window.__gowiki_plugins.todo.gate` so other TS plugins can push or subscribe to task events without importing the bundle directly.

### 6.3 TodoWidget

Replaces inline `{todo …}` nodes with an interactive chip:

- Shows assignee avatar(s), title, due date pill, priority badge
- Checkbox triggers `gate.complete(id)` and optimistically updates UI
- Click-to-expand opens `TodoModal` for editing (if user has permission)
- SSE events update the chip in real time without a page reload

### 6.4 TodoPanel

A collapsible sidebar section listing the calling user's open tasks, sorted by due date. Supports quick-complete, filter by tag/priority, and a "New task" button.

### 6.5 Editor Integration

When the Gowiki page editor is open, the TS plugin registers a toolbar button that inserts a `{todo …}` block at the cursor and opens `TodoModal` to fill in details.

---

## 7. Notifications

### 7.1 Email

The Go backend sends emails through Gowiki's existing SMTP configuration. Templates (Go `text/template` format) are stored in `plugins/todo/templates/`:

| Template | Trigger |
|---|---|
| `assigned.txt` / `.html` | Task created and assigned to user |
| `due_reminder.txt` / `.html` | N hours before due date (configurable) |
| `overdue.txt` / `.html` | Task past due and still open |
| `completed_all.txt` / `.html` | All members of a group task finished |
| `recurrence_spawned.txt` / `.html` | New recurrence instance created |

### 7.2 Outbound Webhook Hook System

The plugin supports any number of named webhook targets configured in `gowiki.toml`. Each target specifies a URL, an optional HMAC secret for request signing, and a Go `text/template` payload. When a notification event fires, the dispatcher POSTs the rendered payload to every enabled target.

This is intentionally transport-agnostic: Zulip, Slack, Mattermost, a custom HTTP receiver — anything that accepts a POST with a JSON or text body works.

**Payload template variables:**

| Variable | Content |
|---|---|
| `.Event` | Event type: `assigned`, `reminder`, `overdue`, `completed`, `recurrence` |
| `.Task.Title` | Task title |
| `.Task.ID` | Task UUID |
| `.Task.Priority` | Priority string |
| `.Task.Due` | Formatted due date |
| `.Assignee` | Username or group name |
| `.PageURL` | Full URL to the source page (if any) |
| `.WikiActionSummary` | Human-readable description of the wiki action |

**Example `gowiki.toml` configuration:**

```toml
[plugin.todo]
  reminder_hours = [24, 2]

[plugin.todo.notify.email]
  enabled = true
  from    = "wiki@example.org"

# Zulip — uses Zulip's incoming-webhook format
[[plugin.todo.notify.webhook]]
  name         = "zulip"
  enabled      = true
  url          = "https://zulip.example.org/api/v1/external/gowiki?api_key=…"
  content_type = "application/json"
  payload_tmpl = '''
{
  "type":    "stream",
  "to":      "tasks",
  "topic":   "{{.Task.Title}}",
  "content": "**{{.Event}}** — {{.Assignee}}, due {{.Task.Due}}\n{{.PageURL}}"
}
'''

# Slack — uses Slack's incoming-webhook format
[[plugin.todo.notify.webhook]]
  name         = "slack"
  enabled      = false
  url          = "https://hooks.slack.com/services/…"
  content_type = "application/json"
  payload_tmpl = '''
{
  "text": "*{{.Event}}* — {{.Task.Title}} assigned to {{.Assignee}}, due {{.Task.Due}}"
}
'''
```

If at least one webhook target is enabled, **email is suppressed** for that notification event to avoid duplicates. The operator controls which channel takes precedence by enabling/disabling targets.

---

## 8. Interaction with Other Plugins

### 8.1 Struct / Meta Plugin

When a `set_meta` wiki action completes (Gowiki fires a `page.meta.updated` event), the todo engine checks whether any open task has that action as its `wiki_action` and closes it automatically.

### 8.2 Reviewflow Plugin

When the **reviewflow** plugin is active alongside the todo plugin, reviewflow automatically creates and manages todo tasks to track pending role confirmations.

#### Lifecycle

1. **Page saved with a `{reviewflow}` directive** — When a page containing a reviewflow directive is saved and the page version changes (content modified), reviewflow:
   - Cancels any existing open reviewflow todo tasks for that page
   - Creates one new todo task per role, assigned to the role's user

2. **Role confirmed** — When a user confirms their role, the task remains open (it tracks the page-level review, not individual confirmations). When **all roles** confirm and the version becomes fully validated, all reviewflow tasks for the page are cancelled.

3. **Directive removed** — If a page save removes the `{reviewflow}` directive, all open reviewflow tasks for that page are cancelled.

#### Task properties

| Field | Value |
|---|---|
| `title` | `Review (v2.1): alice as author on /path/to/page` |
| `source` | `api` |
| `source_page` | Page path where the reviewflow directive lives |
| `node_key` | SHA-1 of `reviewflow:{pagePath}:{role}:{user}` — stable identity |
| `assignee` | `{ type: "user", target: "{username}", resolution: "any" }` |
| `due_date` | Computed from the shortest applicable deadline in `reviewflow.deadlines` config |
| `tags` | `reviewflow` |
| `priority` | `normal` |
| `created_by` | `reviewflow` |

#### Due date computation

The due date is derived from the `reviewflow.deadlines` configuration map. For each role, the system looks up `deadlines[roleName]`, falling back to `deadlines["_default"]`. The **shortest** deadline across all roles is used as the due date for all tasks (set to `now + shortest_deadline`). If no deadlines are configured, tasks are created without a due date.

#### Configuration

Reviewflow deadlines are configured in `config.yaml`:

```yaml
reviewflow:
  enabled: true
  deadlines:
    _default: "168h"    # 7 days for any role without a specific deadline
    reviewer: "72h"     # 3 days for reviewers
    validator: "48h"    # 2 days for validators
```

The admin UI provides a dedicated "Reviewflow Plugin" section where deadlines can be edited (one `role=duration` pair per line).

#### Implementation

The integration uses a `TodoIntegrator` interface defined in the reviewflow package, with a concrete `TodoAdapter` that wraps the todo `TodoService`. This avoids circular imports and keeps the coupling one-directional (reviewflow depends on todo, not the reverse).

```go
// In package reviewflow
type TodoIntegrator interface {
    CreateReviewTasks(pagePath string, roles map[string]string, versionTag string, dueDate string) error
    CancelReviewTasks(pagePath string) error
}
```

The adapter is wired at startup in `main.go` only when the todo service is available (database connected and todo not disabled). If the todo plugin is inactive, reviewflow operates normally without creating tasks.

#### Identifying reviewflow tasks

Reviewflow tasks are identifiable by:
- `tags = "reviewflow"` — used for filtering in cancel operations
- `created_by = "reviewflow"` — distinguishes from user-created tasks
- `source_page` matches the reviewflow page path

These tasks appear in the user's "My Tasks" list and follow all standard todo notification rules (email, webhooks, reminders, overdue alerts).

#### Todo inactivation on pages with pending review

When a page contains both a `{reviewflow}` directive and `{todo}` directives, the todo tasks are **inactive** as long as the reviewflow is not fully validated. The rationale is that the page content is in draft state and may still change, so the tasks defined in it should not be actionable until the content is approved.

**Behavior:**

- When the backend serves tasks for a page (`GET /tasks/page/*`), it checks whether the page has a reviewflow with at least one role not yet confirmed for the current page version. If so, all wiki-node tasks on that page (excluding reviewflow's own tasks tagged `"reviewflow"`) are marked `inactive: true` in the response.
- The `POST /tasks/{id}/complete` endpoint also enforces this: attempting to complete an inactive task returns `409 Conflict` with the message "task is inactive: page review is pending".
- The `inactive` field is computed at response time (like `warnings`) and is not stored in the database.
- Once the reviewflow is fully validated, the tasks become active automatically on the next fetch.

**Frontend rendering:**

- Inactive tasks are shown with reduced opacity and a dashed border.
- The checkbox is disabled, with a tooltip: "Page review is pending — task is inactive".

**Interface boundary:**

The todo package defines a `ReviewflowChecker` interface to avoid a circular dependency:

```go
// In package todo
type ReviewflowChecker interface {
    IsPageReviewPending(pagePath string) bool
}
```

The reviewflow `Service` implements this interface. The adapter is wired in `server.go` at route registration time. If no reviewflow service is available, no inactivation check is performed.

### 8.3 ACL / User Plugin

Group membership is re-evaluated live from the ACL/user plugin, so roster changes are reflected without re-creating tasks.

### 8.4 Search Plugin

Tasks are indexed as a first-class content type. Users can search `todo:@alice status:open` from the main search bar.

### 8.5 Audit / History Plugin

Tasks created from a `{todo}` wiki node are written to the audit log on every state change (create, update, complete, cancel), with full diff and user attribution.

Tasks created programmatically via the Go Service API are **not** written to the wiki audit log. The calling plugin is responsible for its own audit trail. This keeps the wiki audit log focused on human-initiated, page-bound actions.

---

## 9. Permissions

| Action | Minimum Role |
|---|---|
| View tasks assigned to self | Authenticated user |
| View tasks on a page | Read access to that page |
| View individual completions on a group "all" task | Task creator or admin only |
| Create a task | `editor` or above |
| Assign to another user | `editor` or above |
| Edit / delete any task | `admin` or task creator |
| Manage plugin config | `admin` |

---

## 10. Decisions

1. **Storage** — PostgreSQL only. The plugin refuses to activate if no PostgreSQL connection is available in the running Gowiki instance. No embedded fallback is provided.

2. **Recurrence model** — Two patterns: fixed delay in days (`3d`), and calendar repetition (`daily / weekly / Nmonths / yearly`). No cron expressions, no weekday selectors. Completing a recurring task immediately spawns the next instance.

3. **Group "all" completion visibility** — Per-member completion records are restricted to the task creator and admins.

4. **Notification priority** — If at least one outbound webhook is enabled, email is suppressed for that notification. Operators choose the active channel via config.

5. **No inbound webhook / chat-bot** — Task completion happens only through the wiki UI or the REST API.

