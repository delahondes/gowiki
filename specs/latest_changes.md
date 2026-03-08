# Gowiki — Latest Changes Plugin Specification


## 1. Overview

The **latest changes plugin** embeds a list of recently changed pages into any wiki page. Inspired by DokuWiki's `plugin:changes`, it renders a live-updating list of page modifications using a self-contained directive.

### Design Goals

- **Simple directive** — `{changes}` with optional filtering parameters.
- **Backend-driven** — the backend reads the global `changes.log` and returns filtered results via a REST endpoint. The frontend never processes the raw log.
- **Deduplication** — only the most recent change per page is shown (like DokuWiki).
- **Minimal rendering** — a simple bulleted list with page link, author, and relative time.

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────┐
│                   Gowiki Core                         │
│                                                       │
│  ┌───────────────────────┐                            │
│  │  storage.Changelog    │                            │
│  │                       │                            │
│  │  Append() (existing)  │                            │
│  │  Read()   (new)       │                            │
│  └───────────┬───────────┘                            │
│              │                                        │
│  ┌───────────▼───────────┐                            │
│  │  api.Server           │                            │
│  │                       │                            │
│  │  GET /api/changes     │                            │
│  │  ?count=N&path=...    │                            │
│  │  &type=...&user=...   │                            │
│  └───────────────────────┘                            │
└──────────────┬────────────────────────────────────────┘
               │ REST
┌──────────────▼────────────────────────────────────────┐
│              TypeScript Frontend Plugin                │
│                                                       │
│  changes.ts                                           │
│  ├── Schema node (atom, block)                        │
│  ├── Self-contained directive parser/printer          │
│  ├── ChangesNodeView (bulleted list)                  │
│  └── Insert command                                   │
└───────────────────────────────────────────────────────┘
```

No new Go package is needed. The `Changelog` struct already exists in `storage/`; the `Read()` method is added there. The API handler lives on the existing `Server` struct.

---

## 3. Markdown Syntax

The latest changes plugin uses a self-contained directive:

```markdown
{changes}
{changes count=15}
{changes count=10 path=/projects}
{changes count=20 path=/projects,-/projects/archive type=edit,create user=alice}
```

### Attribute reference

| Attribute | Required | Default | Notes |
|---|---|---|---|
| `count` | No | `10` | Number of changes to show (max 100) |
| `path` | No | *(all)* | Comma-separated path prefixes. Prefix with `-` to exclude. |
| `type` | No | *(all)* | Comma-separated change types: `edit`, `delete`, `create`, `migrate`, `admin` |
| `user` | No | *(all)* | Comma-separated usernames to filter by |

### Rules

- All parameters are optional. A bare `{changes}` shows the 10 most recent changes across the entire wiki.
- Path prefixes match from the start: `path=/projects` matches `/projects/foo` and `/projects/bar/baz`.
- Exclusions (`-/path`) are applied after inclusions.
- The `count` parameter is clamped to 1–100.
- Only the most recent change per page is shown (deduplication).

### Examples

```markdown
{changes}

{changes count=5}

{changes count=20 path=/projects}

{changes path=/docs,-/docs/archive type=edit,create}

{changes user=alice,bob count=15}
```

---

## 4. Data Model

### 4.1 ChangeEntry (backend)

Parsed from a single line of `changes.log`:

| Field | Type | Notes |
|---|---|---|
| `Timestamp` | `time.Time` | UTC, RFC3339 format |
| `PagePath` | `string` | Page path (e.g. `projects/roadmap`) |
| `Version` | `int64` | Page version (Unix timestamp) |
| `Author` | `string` | Username who made the change |
| `Summary` | `string` | Change summary (usually empty for edits) |
| `ChangeType` | `string` | One of: `edit`, `delete`, `create`, `migrate`, `admin` |

### 4.2 ReadOptions (backend)

| Field | Type | Notes |
|---|---|---|
| `Count` | `int` | Max entries to return (default 10, max 100) |
| `IncludePaths` | `[]string` | Path prefixes to include (empty = all) |
| `ExcludePaths` | `[]string` | Path prefixes to exclude |
| `Types` | `[]string` | Change types to include (empty = all) |
| `Users` | `[]string` | Usernames to filter by (empty = all) |

### 4.3 API Response

```json
{
  "entries": [
    {
      "timestamp": "2026-03-08T14:23:01Z",
      "page": "projects/roadmap",
      "version": 1741441381,
      "author": "alice",
      "summary": "",
      "type": "edit"
    }
  ]
}
```

---

## 5. Backend API

### Endpoint

`GET /api/changes`

Registered in the public read group (with `optionalAuth`, no ACL check — same as `/api/search` and `/api/sitemap`).

### Query parameters

| Param | Type | Default | Notes |
|---|---|---|---|
| `count` | int | 10 | Clamped to 1–100 |
| `path` | string | *(empty)* | Comma-separated path prefixes, `-` prefix for exclusion |
| `type` | string | *(empty)* | Comma-separated change types |
| `user` | string | *(empty)* | Comma-separated usernames |

### Implementation

**`storage/changelog.go` — `Read(opts ReadOptions) ([]ChangeEntry, error)`**:

1. Read the entire `changes.log` file.
2. Parse lines in reverse order (most recent first).
3. For each line, parse the 6 tab-separated fields into a `ChangeEntry`.
4. Apply filters: path prefixes (include/exclude), change types, usernames.
5. Deduplicate: track seen page paths, skip entries for already-seen pages.
6. Stop after collecting `count` entries.
7. Return the collected entries.

**`api/server.go` — `handleRecentChanges`**:

1. Parse query parameters.
2. Parse `path` into include/exclude lists (split on `,`, entries starting with `-` go to exclude).
3. Call `s.changelog.Read(opts)`.
4. Return JSON response.

---

## 6. Frontend Plugin

### 6.1 Schema

```typescript
changes: {
  group: "block",
  atom: true,
  attrs: {
    count: { default: "10" },
    path: { default: "" },
    type: { default: "" },
    user: { default: "" },
  },
}
```

### 6.2 Directive Handling

Self-contained directive `{changes}` with properties:

| Property | Label | Default | Notes |
|---|---|---|---|
| `count` | Count | `"10"` | Number of changes to display |
| `path` | Path filter | `""` | Comma-separated path prefixes |
| `type` | Change types | `""` | Comma-separated types |
| `user` | Users | `""` | Comma-separated usernames |

### 6.3 Serialization (PM → Markdown)

```
{changes count=15 path=/projects type=edit user=alice}
```

Only non-default attributes are serialized. A bare `{changes}` is emitted when all attributes are at their defaults.

### 6.4 NodeView (ChangesNodeView)

On mount, fetches `GET /api/changes?count=N&path=...&type=...&user=...` and renders results as a bulleted list.

**List item format:**

```
• Page Title — author, 2 hours ago
```

- **Page title**: derived from path — last segment, humanized (dashes/underscores to spaces, title case). Rendered as a link to the page.
- **Author**: username (displayed as-is; no display name resolution for simplicity).
- **Relative time**: e.g. "3 minutes ago", "2 days ago", "1 month ago".
- **Change type badge**: small colored badge for non-edit types (`create`, `delete`, `migrate`, `admin`). Edits have no badge (most common case).

**States:**

- Loading: italic "Loading..." text.
- Empty: "No recent changes" text.
- Error: red italic error message.

**Update behavior**: re-fetches when any attribute changes.

### 6.5 Insert Command

`reg.registerCommand("changes", "insert", ...)` — inserts a `{changes}` node and opens the properties panel.

### 6.6 CSS Classes

| Class | Purpose |
|---|---|
| `.gowiki-changes` | Outer wrapper |
| `.gowiki-changes-loading` | Loading state |
| `.gowiki-changes-error` | Error state |
| `.gowiki-changes-empty` | Empty state |
| `.gowiki-changes-list` | The `<ul>` list |
| `.gowiki-changes-item` | Each `<li>` item |
| `.gowiki-changes-link` | Page link |
| `.gowiki-changes-meta` | Author + time span |
| `.gowiki-changes-badge` | Change type badge |

---

## 7. Files Summary

| File | Purpose |
|---|---|
| `backend/internal/storage/changelog.go` | Add `ChangeEntry`, `ReadOptions`, `Read()` method |
| `backend/internal/api/server.go` | Add `handleRecentChanges`, register `GET /api/changes` route |
| `frontend/plugins/changes.ts` | Full frontend plugin (schema, directive, NodeView, command, styles) |
| `frontend/plugins/index.ts` | Register changes plugin |

---

## 8. Decisions

1. **No new Go package** — the changes endpoint is simple enough to live on the existing `Changelog` struct and `Server`. No routing sub-package needed.

2. **Deduplication** — only the most recent change per page is shown. This matches DokuWiki's behavior and avoids flooding the list when a page is edited multiple times.

3. **No display name resolution** — author is shown as the raw username. This keeps the plugin simple and avoids extra API calls. Display name resolution can be added later if needed.

4. **Path prefix filtering** — uses path prefixes (not namespace names) to be consistent with Gowiki's path-based addressing. The `-` prefix for exclusion follows a common convention.

5. **Public read endpoint** — `GET /api/changes` uses `optionalAuth` with no ACL check, consistent with `/api/search` and `/api/sitemap`. The changelog itself does not contain sensitive content (just page paths, authors, and timestamps).

6. **String attrs** — all schema attributes are strings (not numbers) for consistency with other Gowiki plugins (include, tag-query). The backend parses `count` as an integer.
