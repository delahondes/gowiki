# Comment Plugin — Specification

## Motivation

Margin comments provide a way to annotate wiki pages without modifying page content. They are useful for review, discussion, and collaborative editing. Comments appear in a sidebar alongside the text they reference, similar to Word/Google Docs.

## Core Principles

1. **Comments are metadata, not content.** Adding, editing, or deleting a comment never creates a new page version. Comments are stored as a sidecar file in `data/meta/`, completely decoupled from the Markdown source.

2. **Comments anchor to text selections.** Each comment references a specific text fragment in the page. When the page is rendered in view mode, the anchored text is highlighted and the comment appears in a margin sidebar.

3. **Anchoring is best-effort.** If the page text changes and the anchored fragment can no longer be found, the comment becomes "orphaned" — still visible in the sidebar but without a highlight. It is never silently deleted.

4. **No comments in edit mode.** Comments are a view-mode overlay. The ProseMirror editor does not know about comments. This keeps the editor simple and avoids interactions between comment anchoring and content editing.

5. **Plugin boundary respected.** The comment plugin owns its full vertical slice (storage, API, rendering) but does not touch authentication, ACL, storage layout, or page versioning. Disabling the plugin must not corrupt documents.

## Data Model

### Comment Entry

```json
{
  "id": "c_a1b2c3d4",
  "anchor": {
    "selected": "the highlighted text",
    "before": "20 chars of context before",
    "after": "20 chars of context after"
  },
  "text": "This paragraph needs rewording.",
  "author": "alice",
  "created_at": "2026-03-08T14:30:00Z",
  "updated_at": "2026-03-08T14:30:00Z",
  "resolved": false
}
```

Fields:

| Field | Type | Description |
|---|---|---|
| `id` | string | Unique ID, `c_` + 8 hex chars from SHA1(selected + text + timestamp) |
| `anchor.selected` | string | The exact text fragment the comment is anchored to |
| `anchor.before` | string | ~20 chars of plain text before the selection (for disambiguation) |
| `anchor.after` | string | ~20 chars of plain text after the selection (for disambiguation) |
| `text` | string | The comment body (plain text, may support basic formatting later) |
| `author` | string | Username of the comment author |
| `created_at` | string | RFC3339 timestamp |
| `updated_at` | string | RFC3339 timestamp, updated on edit |
| `resolved` | bool | Whether the comment has been marked as resolved |

### Storage

Per-page sidecar file at `data/meta/{page-path}.comments.json`, following the same pattern as reviewflow (`{page-path}.reviewflow.json`).

Example: comments on `/docs/setup` → `data/meta/docs/setup.comments.json`

The file contains a JSON array of comment entries:

```json
[
  { "id": "c_a1b2c3d4", "anchor": { ... }, "text": "...", ... },
  { "id": "c_e5f6g7h8", "anchor": { ... }, "text": "...", ... }
]
```

Empty array or missing file = no comments. When the last comment is deleted, the file is removed.

## Backend

### Store

`backend/internal/comment/store.go` — follows the reviewflow store pattern:

- `Store.statePath(pagePath)` → `meta/{page-path}.comments.json`
- `Store.Load(pagePath) ([]Comment, error)` — returns `[]Comment` (empty if file missing)
- `Store.Save(pagePath, []Comment) error` — atomic write
- `Store.Delete(pagePath) error` — removes the file
- `Store.Rename(oldPath, newPath) error` — moves the sidecar file (called by `Move()`)

### Service

`backend/internal/comment/service.go` — business logic layer:

- `Create(pagePath, anchor, text, author) (Comment, error)` — generates ID, appends to list
- `Update(pagePath, commentID, newText, user) error` — only author or admin can edit
- `Resolve(pagePath, commentID, user) error` — toggles resolved flag
- `Delete(pagePath, commentID, user) error` — only author or admin can delete
- `List(pagePath) ([]Comment, error)` — returns all comments for a page

### API Routes

Registered under `/api/plugin/comment/v1/`, following the existing plugin route pattern.

| Method | Route | Description | Permission |
|---|---|---|---|
| `GET` | `/api/plugin/comment/v1/{path}` | List comments for a page | view |
| `POST` | `/api/plugin/comment/v1/{path}` | Create a comment | edit |
| `PUT` | `/api/plugin/comment/v1/{path}/{id}` | Update comment text | edit (author or admin) |
| `PATCH` | `/api/plugin/comment/v1/{path}/{id}/resolve` | Toggle resolved | edit |
| `DELETE` | `/api/plugin/comment/v1/{path}/{id}` | Delete a comment | edit (author or admin) |

Request body for POST:
```json
{
  "anchor": { "selected": "...", "before": "...", "after": "..." },
  "text": "Comment body"
}
```

Request body for PUT:
```json
{ "text": "Updated comment body" }
```

Response for GET:
```json
{
  "comments": [ ... ],
  "orphaned": ["c_a1b2c3d4"]
}
```

The `orphaned` array lists comment IDs whose anchor text could not be found in the current page content. The backend computes this by running the anchoring algorithm against the current Markdown plain text.

### Integration with Move

When `FileStore.Move()` renames a page, it must also call `comment.Store.Rename(oldPath, newPath)` to move the sidecar file. Added to the move flow alongside attic and reviewflow renames.

## Frontend

### Plugin Structure

`frontend/plugins/comment.ts` — registers:

1. **View-mode overlay** (not a ProseMirror schema extension): comments are rendered as DOM decorations in view mode only. No ProseMirror nodes or marks for comments.

2. **Sidebar container**: a `<div id="comment-sidebar">` appended to the page layout, positioned alongside the content area.

3. **Highlight injection**: in view mode, after the ProseMirror document is rendered, the plugin walks the DOM to find and highlight anchored text using `<span class="comment-highlight">` wrappers.

### Anchoring Algorithm (Frontend)

The frontend must locate each comment's `anchor.selected` text in the rendered DOM. Strategy (3-step fallback, inspired by the reference implementation):

1. **Exact match with context**: search for `before + selected + after` as a substring in the page's text content. If found, compute the range covering just the `selected` portion.

2. **Exact match without context**: search for `selected` alone. If multiple matches, use `before`/`after` context to disambiguate (pick the match with the best surrounding context similarity).

3. **Fuzzy match**: if no exact match, attempt a fuzzy search (e.g., allow minor whitespace differences). This handles cases where formatting changed but the text is substantially the same.

4. **Orphaned**: if no match, the comment is displayed in the sidebar without a highlight, marked as orphaned.

### Context Extraction (Frontend)

When the user creates a comment, the frontend extracts the anchor from the current DOM selection:

- `selected`: the selected text (plain text, normalized whitespace)
- `before`: up to 20 characters of plain text immediately before the selection in the same block
- `after`: up to 20 characters of plain text immediately after the selection in the same block

This is computed from the DOM, not from the ProseMirror document model, since comments are a view-mode concept.

### Sidebar Rendering

Each comment in the sidebar shows:
- Author name and timestamp
- Comment text
- "Reply" / "Edit" / "Resolve" / "Delete" actions (contextual)
- A colored connector line to the highlighted text

Comments are vertically positioned to align with their anchor text. When anchors are close together, comments stack with minimal overlap.

Resolved comments are collapsed by default (shown as a small indicator) with an option to expand.

### Interaction Flow

1. **Create**: user selects text in view mode → clicks "Comment" button (or keyboard shortcut) → a comment input appears in the sidebar at the corresponding vertical position → user types and submits → `POST` to backend → highlight injected.

2. **Edit**: user clicks edit icon on their own comment → inline edit in sidebar → `PUT` to backend.

3. **Resolve**: user clicks resolve → comment collapses, `PATCH` to backend.

4. **Delete**: user clicks delete on their own comment → confirmation → `DELETE` to backend → highlight removed.

### CSS

The plugin registers its own stylesheet via `reg.registerStyle("comment", commentStyles)`. Key elements:

- `.comment-highlight` — subtle background color (yellow-ish), darker when hovered or when sidebar comment is focused
- `#comment-sidebar` — positioned to the right of the content area, scrolls with the page
- `.comment-box` — individual comment card with author, text, actions
- `.comment-orphaned` — dimmed style for orphaned comments
- `.comment-resolved` — collapsed style

## Edge Cases

### Page versioning
Comments are NOT versioned with the page. They persist across page edits. If a page is rolled back to an older version, comments remain — some may become orphaned if their anchor text no longer exists.

### Concurrent edits
Two users can add comments simultaneously without conflict (append-only). The backend loads the full list, appends, and saves atomically. If two writes race, last-write-wins — acceptable because comments are independent entries.

### Long selections
Anchor text is capped at 200 characters. If the selection is longer, it is truncated (with the full selection still used for matching, but only the first 200 chars stored in `anchor.selected`). Context matching (`before`/`after`) helps disambiguate.

### Namespace index pages
Comments on `/ns/` are stored at `data/meta/ns/index.comments.json`, following the same convention as other metadata files.

### Move/rename
The comment sidecar file is moved along with the page. Comment anchors reference text content, not page paths, so they remain valid after a rename.

## Out of Scope (for v1)

- **Threaded replies**: v1 has flat comments only. Threading can be added later with a `parent_id` field.
- **Notifications**: no email or in-app notifications when comments are added. Can be added as a separate feature.
- **Comment on media**: only page text can be commented on.
- **Rich text in comments**: comment body is plain text in v1.
- **Comment history/audit**: no edit history for individual comments.
- **Permissions beyond author/admin**: no per-comment ACL.

## Files

| Action | File | Description |
|---|---|---|
| Create | `backend/internal/comment/store.go` | File-based comment storage |
| Create | `backend/internal/comment/service.go` | Business logic |
| Create | `backend/internal/comment/handlers.go` | HTTP handlers |
| Create | `backend/internal/comment/model.go` | Data types |
| Create | `frontend/plugins/comment.ts` | Full frontend plugin |
| Modify | `backend/internal/api/server.go` | Register comment routes |
| Modify | `backend/internal/storage/pages.go` | Add comment rename to Move flow |
| Modify | `frontend/main.js` | Add "Comment" button in view mode toolbar |
