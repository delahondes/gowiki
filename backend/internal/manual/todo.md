# Todo Tasks

The todo plugin lets you assign and track tasks within wiki pages.

## 1. Requirements

Todo requires a PostgreSQL database connection (configured in Admin > Configuration > Database).

## 1. Syntax

```
{todo assign=alice due=2026-04-01 action=read}
Task description here
{/todo}
```

## 1. Properties

| Property | Description | Example |
| --- | --- | --- |
| assign | Assignee (user or group:groupname) | `assign=alice` or `assign=group:editors` |
| due | Due date | `due=2026-04-01` |
| action | Required action: read, edit, create, meta | `action=read` |
| recur | Recurrence in days | `recur=30` |

## 1. Group assignments

- `assign=alice` — assigns to user alice
- `assign=group:editors` — assigns to the editors group
- `assign=alice,group:editors` — assigns to both

## 1. Task lifecycle

1. Task is created when the page is saved
2. Assignees see the task in their task panel and receive notifications
3. Assignees acknowledge the task (e.g. by reading the page for `action=read`)
4. Completed tasks can recur if `recur` is set

## 1. Notifications

When configured, assignees receive notifications via:
- **Email** — SMTP settings in Admin > Configuration
- **Webhooks** — for Slack, Zulip, or other integrations
- **SSE** — real-time in-browser notifications
