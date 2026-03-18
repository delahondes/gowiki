# Todo Tasks

The todo plugin lets you assign and track tasks within wiki pages.

## 1. Requirements

Todo requires a PostgreSQL database connection (configured in Admin > Configuration > Database).

## 1. Syntax

A todo is a self-contained directive on its own line:

```markdown
{todo title="Review the quality manual" assign="alice" due=2026-04-01}
```

All properties are specified within the curly braces. The description, if needed, is also a property:

```markdown
{todo title="Translate section 3" assign="bob" description="Translate from French to English, preserve formatting"}
```

## 1. Properties

| Property | Description | Example |
| --- | --- | --- |
| title | Task title (required) | `title="Review document"` |
| assign | Assignee (user or group:groupname) | `assign="alice"` or `assign="group:editors"` |
| due | Due date | `due=2026-04-01` |
| recur | Recurrence in days | `recur=30` |
| priority | Priority level | `priority=high` |
| action | Required action: read, edit, create, meta | `action=read` |
| description | Longer description | `description="Details here"` |
| tags | Comma-separated tags | `tags="urgent,quality"` |

## 1. Group assignments

- `assign="alice"` — assigns to user alice
- `assign="group:editors"` — assigns to the editors group
- `assign="alice,group:editors"` — assigns to both

When assigned to a group, the `resolution` property controls completion:
- `resolution=any` (default) — any group member can complete the task
- `resolution=all` — all group members must acknowledge

## 1. Task lifecycle

1. Task is created when the page is saved
2. Assignees see the task in their task panel and receive notifications
3. Assignees acknowledge the task (e.g. by reading the page for `action=read`)
4. Completed tasks can recur if `recur` is set

## 1. Todo list

Display a list of tasks using the `{todo-list}` directive:

```markdown
{todo-list}
```

Optional filters:

```markdown
{todo-list assign="alice" status="open,in_progress" priority="high,urgent"}
```

If no assignee is specified, the list shows tasks for the current user.

## 1. Notifications

When configured, assignees receive notifications via:
- **Email** — SMTP settings in Admin > Configuration
- **Webhooks** — for Slack, Zulip, or other integrations
- **Real-time** — in-browser notifications via SSE
