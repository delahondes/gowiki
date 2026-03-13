# Gowiki — Reviewflow Plugin Specification


## 1. Overview

The **Reviewflow plugin** is a document validation workflow for Gowiki. A page declares named roles (author, reviewer, validator, or any custom role name), each assigned to a specific user. Every role-holder must confirm the current page version. Once all roles confirm, the version is tagged, recorded in history, and the page is marked as validated.

Adapted from the DokuWiki reviewflow plugin, redesigned from scratch for Gowiki's architecture.

### Design Goals

- **Arbitrary roles** — any `key=value` pair in the directive is a role (not limited to a fixed set like author/reviewer/validator). Per-role deadlines are configurable.
- **Version tagging** — each validation cycle is associated with a version tag (e.g. `v2.1`). Validated versions are recorded in history.
- **Content-triggered invalidation** — any content change resets all confirmations, requiring a new validation cycle.
- **Stale version detection** — a version tag that was already validated cannot be reused without updating it first.
- **Visual feedback** — the page background turns light red when validation is pending; the reviewflow block shows role status, deadlines, and overdue indicators.
- **Todo integration** — when the todo plugin is active, reviewflow automatically creates assignee tasks for pending confirmations.
- **File-based storage** — reviewflow state is stored as JSON files under `data/meta/`, since state is inherently per-page and does not require database queries.

---

## 2. Architecture

```
┌──────────────────────────────────────────────────────┐
│                   Gowiki Core                         │
│                                                       │
│  ┌───────────────────────┐                            │
│  │  reviewflow package   │                            │
│  │                       │    ┌─────────────────────┐ │
│  │  Service              │───▶│  Todo Plugin        │ │
│  │    SyncFromMarkdown() │    │  (optional)         │ │
│  │    Confirm()          │    │  Creates/cancels    │ │
│  │    GetStatus()        │    │  review tasks       │ │
│  │                       │    └─────────────────────┘ │
│  │  Store                │                            │
│  │    .reviewflow.json   │    ┌─────────────────────┐ │
│  │                       │───▶│  Attic              │ │
│  │  Handlers             │    │  PluginMeta on      │ │
│  │    GET /status/*      │    │  validated versions │ │
│  │    POST /confirm/*    │    └─────────────────────┘ │
│  └───────────┬───────────┘                            │
│              │ REST                                   │
└──────────────┼────────────────────────────────────────┘
               │
┌──────────────▼────────────────────────────────────────┐
│              TypeScript Frontend Plugin                │
│                                                       │
│  reviewflow.ts                                        │
│  ├── Schema node (atom, block)                        │
│  ├── Self-contained directive parser/printer          │
│  ├── ReviewflowNodeView (status card + confirm UI)    │
│  └── API gate (fetch wrapper)                         │
└───────────────────────────────────────────────────────┘
```

The Go backend package (`backend/internal/reviewflow/`) compiles into the core Gowiki binary. The TypeScript frontend is a standard Gowiki plugin bundle. The reviewflow service is always initialized at startup; the `reviewflow.enabled` config flag controls deadline computation and todo integration behavior.

---

## 3. Markdown Syntax

Reviewflow uses a self-contained directive on its own line:

```markdown
{reviewflow version=2.1 author=alice reviewer=bob validator=carol}
```

### Attribute reference

| Attribute | Required | Notes |
|---|---|---|
| `version` | Recommended | Version tag string (e.g. `2.1`, `v3.0`). Used for history labeling. |
| Any other key | At least one | Role assignment: `rolename=username`. Any key that is not `version` is treated as a role. |

### Rules

- Role names are arbitrary — `author`, `reviewer`, `validator`, `approver`, or any custom name.
- No `@` prefix — syntax is `author=alice`, not `author=@alice`.
- `group:editors` syntax is reserved for future group assignment (not yet implemented).
- Values may be quoted: `author="alice smith"` (for usernames with spaces, though uncommon).
- One directive per page — if multiple directives exist, only the first is recognized.
- The `version` key always serializes first, followed by roles in alphabetical order (deterministic round-trip).

### Examples

```markdown
{reviewflow version=1.0 author=alice reviewer=bob}

{reviewflow version=3.2 author=alice reviewer=bob validator=carol approver=dave}

{reviewflow author=alice reviewer=bob}
```

---

## 4. Data Model

### 4.1 State (persisted as JSON)

Stored at `data/meta/{pagePath}.reviewflow.json`:

| Field | Type | Notes |
|---|---|---|
| `roles` | `map[string]string` | Role name → username |
| `version_tag` | `string` | Version tag from directive |
| `current_page_version` | `int64` | Page version (attic timestamp) this state tracks |
| `confirmations` | `[]Confirmation` | All confirmations for the current version |
| `version_history` | `[]VersionRecord` | Append-only log of fully validated versions |
| `validated_page_version` | `int64` | Last fully validated page version |

### 4.2 Confirmation

| Field | Type | Notes |
|---|---|---|
| `page_version` | `int64` | Page version at time of confirmation |
| `role` | `string` | Role name |
| `user` | `string` | Username who confirmed |
| `timestamp` | `time.Time` | UTC timestamp |
| `version_tag` | `string` | Version tag at time of confirmation |

### 4.3 VersionRecord

Appended to `version_history` when all roles confirm:

| Field | Type | Notes |
|---|---|---|
| `page_version` | `int64` | The validated page version |
| `timestamp` | `time.Time` | When full validation was achieved |
| `confirmed_by` | `map[string]string` | Role → username for all confirmations |
| `version_tag` | `string` | Version tag of this validation cycle |

### 4.4 AtticMeta

Stored in `AtticEntry.PluginMeta["reviewflow"]` on the archived version entry:

| Field | Type | Notes |
|---|---|---|
| `version_tag` | `string` | Version tag |
| `confirmed_by` | `map[string]string` | Role → username |
| `is_validated` | `bool` | Whether this version was fully validated |

### 4.5 Status (computed response)

Returned by the API, not persisted:

| Field | Type | Notes |
|---|---|---|
| `roles` | `map[string]string` | All roles and their assignees |
| `version_tag` | `string` | Current version tag |
| `current_page_version` | `int64` | Current page version |
| `validated_page_version` | `int64` | Last validated version |
| `missing_roles` | `map[string]string` | Roles not yet confirmed (role → user) |
| `deadlines` | `map[string]string` | Role → absolute deadline (RFC 3339), if configured |
| `overdue_roles` | `[]string` | Roles past their deadline |
| `is_fully_validated` | `bool` | True when all roles confirmed |
| `version_history` | `[]VersionRecord` | Full validation history |

---

## 5. Workflow

### 5.1 Page Save (SyncFromMarkdown)

On every page save, the backend:

1. Parses the `{reviewflow ...}` directive from the markdown.
2. If the directive was **removed**: clears transient state (roles, confirmations), preserves version history, cancels any open todo tasks.
3. If the **page version changed** (content modified): resets all confirmations to empty (invalidation), cancels existing review tasks, creates new todo tasks for each role.
4. Updates stored roles, version tag, and current page version.

### 5.2 Confirmation

When a user confirms a role:

1. Validates that the role exists and is assigned to the requesting user.
2. Checks for duplicate confirmation (idempotent — re-confirming is a no-op).
3. Records the confirmation with timestamp.
4. If **all roles are now confirmed**:
   - Appends a `VersionRecord` to version history.
   - Sets `validated_page_version` to the current version.
   - Writes `AtticMeta` to the archived version's `PluginMeta["reviewflow"]`.
   - Cancels all open reviewflow todo tasks for the page.

### 5.3 Invalidation

Any content change (page save with a new version number) automatically:

- Clears all confirmations for the current cycle.
- Resets `missing_roles` to all roles.
- Creates fresh todo tasks (if todo plugin is active).

The version tag is preserved — the user must explicitly update it if they want a new tag.

### 5.4 Stale Version Tag Detection

If the current version tag matches a tag already in `version_history`, the frontend:

- Hides the "Confirm" buttons.
- Shows a warning: *"Version X was already validated. Update the version tag before approving."*

This prevents re-approving a document without bumping the version tag.

### 5.5 Bootstrapping (EnsureState)

Pages saved before the reviewflow plugin was deployed have no `.reviewflow.json` state file. When status is requested or a confirmation is attempted:

1. The service reads the current page content via the `PageReader` interface.
2. Parses the `{reviewflow}` directive from the markdown.
3. Creates and persists an initial state with all roles pending.

---

## 6. Backend API

Base path: `/api/plugin/reviewflow/v1`

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/status/*` | Optional | Returns computed `Status` for a page |
| `POST` | `/confirm/*` | Required | Confirm a role. Body: `{"role": "reviewer"}` |

### 6.1 GET /status/{pagePath}?v=N

Returns a `Status` object. If no reviewflow directive exists on the page, returns an empty status with no roles.

**Optional query parameter**: `v` — page version number (int64). When provided, returns the status as of that specific version rather than the current page version. Used by the frontend when viewing historical versions. The backend checks `version_history` for fully validated versions, and falls back to scanning individual `Confirmation` records for partial confirmation state.

### 6.2 POST /confirm/{pagePath}

Request body:

```json
{"role": "reviewer"}
```

The username is extracted from the session cookie. Returns the updated `Status` on success. Errors:

| Status | Condition |
|---|---|
| 400 | Role not defined, role assigned to a different user, missing role field |
| 401 | Not authenticated |
| 500 | Internal error |

---

## 7. Configuration

```yaml
reviewflow:
  enabled: true
  deadlines:
    _default: "168h"    # 7 days for any role without a specific deadline
    reviewer: "72h"     # 3 days for reviewers
    validator: "48h"    # 2 days for validators
```

| Field | Type | Default | Notes |
|---|---|---|---|
| `enabled` | `bool` | `false` | Enables deadline computation and todo integration |
| `deadlines` | `map[string]string` | `{}` | Role name → Go duration string. `_default` is the fallback. |

Deadlines are validated at config save time (must be valid Go duration strings).

The admin UI provides a "Reviewflow Plugin" section with an enable checkbox and a textarea for deadlines (one `role=duration` per line).

### Deadline Computation

For each missing role, the system looks up `deadlines[roleName]`, falling back to `deadlines["_default"]`. The baseline time is the earliest confirmation timestamp for the current version, or the page save time from the attic if no confirmations exist yet. A role is overdue when `now > baseline + deadline`.

---

## 8. Todo Plugin Integration

When the todo plugin is active (database connected, todo not disabled), reviewflow automatically creates and manages todo tasks.

### Lifecycle

1. **Page invalidated** (new version saved) → cancel existing review tasks, create one new task per role.
2. **All roles confirmed** → cancel all review tasks.
3. **Directive removed** → cancel all review tasks.

### Task Properties

| Field | Value |
|---|---|
| `title` | `Review (v2.1): alice as author on /path/to/page` |
| `source` | `api` |
| `source_page` | Page path |
| `node_key` | SHA-1 of `reviewflow:{pagePath}:{role}:{user}` |
| `assignee` | `{ type: "user", target: "{username}", resolution: "any" }` |
| `due_date` | Shortest applicable deadline from config, as `YYYY-MM-DD` |
| `tags` | `reviewflow` |
| `priority` | `normal` |
| `created_by` | `reviewflow` |

### Implementation

The integration uses a `TodoIntegrator` interface in the reviewflow package, with a `TodoAdapter` concrete implementation wrapping `todo.TodoService`. This avoids circular imports. The adapter is wired at startup only when the todo service is available. If the todo plugin is inactive, reviewflow operates normally without creating tasks.

---

## 9. Frontend Plugin

### 9.1 Schema

```typescript
reviewflow: {
  group: "block",
  atom: true,
  attrs: {
    version: { default: "" },
    roles: { default: "{}" },  // JSON string of { roleName: username }
  },
}
```

### 9.2 Directive Handling

Uses `collectExtra: true` on the self-contained directive spec, which passes through arbitrary `key=value` pairs as raw strings (since role names are dynamic, not fixed properties).

**Registered properties** (editable via property panel):

| Property | Type | Notes |
|---|---|---|
| `version` | Text input | Version tag |
| `roles` | Multiline text | One `rolename=username` per line |

### 9.3 Serialization (PM → Markdown)

```
{reviewflow version=X role1=user1 role2=user2}
```

`version` always first, roles alphabetically sorted for deterministic round-trip.

### 9.4 NodeView (ReviewflowNodeView)

Renders a status card with:

- **Header**: "REVIEWFLOW" label + version tag badge + "DRAFT" or "Validated" badge.
- **Stale version warning**: orange bar when the version tag was already used in a previous cycle (suppressed when viewing history).
- **Role table**: columns for Role, Assignee, Status, Action.
  - Status: checkmark (confirmed), hourglass (pending), warning (overdue).
  - Action: "Confirm" button shown only for the current user's unconfirmed role (hidden when version tag is stale or when viewing history).
- **Page background**: `#fff5f5` (light red) when validation is pending; white when validated. Skipped when viewing history.
- **Border color**: green when fully validated, orange when overdue roles exist, grey otherwise.

On mount, fetches status from `GET /status/{pagePath}` (with `?v=N` when viewing a historical version) and resolves user display names via `/api/users/display`.

### 9.5 History Version Viewing

When viewing an old page version from the history UI, the NodeView shows the reviewflow status as it was for that specific version:

- The `viewVersion()` function in `main.js` sets `window.__gowikiViewingVersion` to the version number before mounting the read-only ProseMirror view. The NodeView captures this in its constructor as `historyVersion`.
- Status is fetched via `GET /status/{pagePath}?v=N`, which returns per-version confirmation state.
- Validated versions show the green "Validated" badge; unvalidated versions show "DRAFT" with per-role confirmation status (pending/confirmed).
- Confirm buttons are hidden (cannot confirm an old version).
- Stale version tag warnings are suppressed (not actionable in history context).
- Page background coloring is skipped (avoid altering the history view).

### 9.6 CSS Classes

| Class | Purpose |
|---|---|
| `.gowiki-reviewflow` | Outer wrapper |
| `.gowiki-rf-wrapper` | Inner card with border |
| `.gowiki-rf-wrapper--validated` | Green border |
| `.gowiki-rf-wrapper--overdue` | Orange border |
| `.gowiki-rf-header` | Header bar |
| `.gowiki-rf-draft-badge` | Red "DRAFT" label |
| `.gowiki-rf-validated-badge` | Green "Validated" label |
| `.gowiki-rf-stale-warning` | Orange warning bar |
| `.gowiki-rf-table` | Role status table |
| `.gowiki-rf-confirm-btn` | Blue confirm button |
| `.gowiki-rf-page-invalid` | Light red page background |

---

## 10. History Integration

### 10.1 History Table

The page history table includes a "Status" column. For each archived version:

- If `plugin_meta.reviewflow.is_validated` is true: displays a green "Validated" badge with the version tag pill.
- If present but not validated: shows role names without badge.
- If absent: no change (non-reviewflow pages).

The `AtticMeta` is written to `AtticEntry.PluginMeta["reviewflow"]` when a version becomes fully validated, making it available to the history API without extra queries.

### 10.2 Version Viewer

When viewing a specific archived version ("View" button in history), the reviewflow panel displays the validation status as of that version. The backend's `GetStatusForVersion(pagePath, version)` method:

1. Checks `version_history` — if the version was fully validated, returns all roles as confirmed.
2. Otherwise scans `Confirmations` for entries matching the requested version to show partial confirmation state.

This allows users to see exactly which roles had confirmed an older draft before it was superseded. The frontend adapts: confirm buttons are hidden, stale warnings suppressed, and page background coloring is skipped.

---

## 11. Storage Layout

```
data/meta/
  path/to/page.reviewflow.json     # reviewflow state (roles, confirmations, history)
  path/to/page.attic.json          # attic index — PluginMeta["reviewflow"] on validated entries
```

All state files use atomic writes (temp file + rename). The `.reviewflow.json` file is the single source of truth for the reviewflow state of a page.

---

## 12. Files Summary

| File | Purpose |
|---|---|
| `backend/internal/reviewflow/model.go` | Data types (State, Status, Confirmation, VersionRecord, AtticMeta) |
| `backend/internal/reviewflow/store.go` | File-based JSON persistence (Load, Save, Delete) |
| `backend/internal/reviewflow/parse.go` | Directive extraction from markdown (ParseDirective) |
| `backend/internal/reviewflow/service.go` | Business logic (SyncFromMarkdown, Confirm, GetStatus, EnsureState) |
| `backend/internal/reviewflow/handlers.go` | HTTP endpoints (GET /status, POST /confirm) |
| `backend/internal/reviewflow/todo_adapter.go` | Todo plugin integration adapter |
| `backend/internal/config/config.go` | ReviewflowConfig struct |
| `backend/internal/storage/attic.go` | PluginMeta field on AtticEntry, UpdateEntryMeta() |
| `backend/internal/storage/pages.go` | ReviewflowSyncer interface, called from Put() |
| `backend/cmd/server/main.go` | Service initialization and wiring |
| `frontend/plugins/reviewflow.ts` | Full frontend plugin (schema, directive, NodeView, API gate, styles) |
| `frontend/plugins/index.ts` | Plugin registration |
| `frontend/main.js` | History table reviewflow badges, admin UI section |

---

## 13. Future: X.509 Certificate Signing (Not Yet Implemented)

The current confirmation mechanism is a simple authenticated click — the user's session cookie proves identity. A planned follow-up phase will add cryptographic non-repudiation via X.509 certificate signing.

### Planned Design

- Each user can upload or generate a personal X.509 certificate (stored in the user profile).
- When confirming a role, the user signs a digest of the page content (or a canonical representation thereof) with their private key.
- The signature, certificate fingerprint, and timestamp are stored alongside the `Confirmation` record.
- Verification is performed server-side against the user's known certificate. Third parties (auditors, regulators) can independently verify signatures.
- A page-level "Validation Certificate" view would display all signatures for a validated version, exportable as a PDF or structured document.

### Motivation

In regulated environments (ISO 9001, pharmaceutical GxP, aerospace DO-178C), document review workflows require proof that a specific person approved a specific version of a document at a specific time. Simple session-based confirmation provides accountability (server logs who clicked), but not cryptographic non-repudiation (the user cannot later deny having approved). X.509 signing bridges this gap.

### Scope Boundaries

- Certificate management (generation, upload, revocation) is a separate concern from the reviewflow workflow itself.
- The reviewflow data model is designed to accommodate signatures: `Confirmation` can be extended with `signature` and `cert_fingerprint` fields without breaking existing state files.
- The frontend confirmation flow would change from a single click to a challenge-response flow (sign the digest, submit the signature).
- Certificate authority (CA) integration — whether to use a self-signed CA, an enterprise CA, or let users bring their own certificates — is an operational decision left to the deployment.

This phase will be designed and specified separately when prioritized.

---

## 14. Reviewflow Link Directive

### 14.1 Overview

The `{reviewflow-link}` directive inserts a reference to a previously validated version of a page. In view mode it renders as a clickable link that navigates to the historical version viewer for that specific validated version. This allows documents to cross-reference a known-good snapshot of another (or the same) page.

### 14.2 Markdown Syntax

```markdown
{reviewflow-link version=1.0}

{reviewflow-link version=1.0 page=/path/to/page}
```

### 14.3 Attribute Reference

| Attribute | Required | Default | Notes |
|---|---|---|---|
| `version` | Yes | — | Version tag string matching a `version_tag` in the target page's `version_history`. |
| `page` | No | Current page | Absolute page path. When omitted, resolves to the page containing the directive. |

### 14.4 Rendering

**View mode / read-only:**

Renders as an inline link: `[Page Title v1.0](/path/to/page?v=N)` where `N` is the `page_version` from the matching `VersionRecord` in the target page's reviewflow state. Clicking navigates to the history version viewer for that exact archived version.

- If the target page has no reviewflow state or the version tag is not found in `version_history`, renders as a warning badge: `"v1.0 (not found)"` with a dotted red underline.
- The page title is resolved from the target page's metadata at render time (not stored in the directive).

**Edit mode:**

Renders as an atom node showing the version tag and target page path (or "this page" when `page` is absent). Clicking selects the node and opens the property panel for editing.

### 14.5 Schema

```typescript
reviewflow_link: {
  group: "block",
  atom: true,
  attrs: {
    version: { default: "" },
    page: { default: "" },    // empty string = current page
  },
}
```

### 14.6 Resolution

On mount (NodeView constructor), the frontend fetches `GET /api/plugin/reviewflow/v1/status/{targetPage}` and scans `version_history` for a `VersionRecord` whose `version_tag` matches the directive's `version` attribute. If found, extracts `page_version` to build the link URL. If not found, displays the error state.

### 14.7 Serialization

**PM → Markdown:**

```
{reviewflow-link version=X}                    (when page is empty/current)
{reviewflow-link version=X page=/path/to/page} (when page is set)
```

`version` always first, `page` second if present. Deterministic round-trip.

### 14.8 Self-Contained Directive Registration

Uses `registerSelfContainedDirective("reviewflow-link", ...)` with two properties: `version` (text input, required) and `page` (text input, optional). No `collectExtra`.

---

## 15. Decisions (continued)

1. **Storage** — File-based JSON, not database. Reviewflow state is per-page and does not need cross-page queries, joins, or transactions. This aligns with how Gowiki stores page metadata under `data/meta/`.

2. **Arbitrary roles** — No fixed role taxonomy. Any `key=value` pair is a role. This maximizes flexibility for different organizations (some need author+reviewer, others need author+reviewer+validator+approver+...).

3. **No `@` prefix** — Unlike the todo plugin's `assign=@alice` syntax, reviewflow uses plain `author=alice`. The `@` prefix adds no information since reviewflow values are always usernames.

4. **Version tag semantics** — The version tag is informational (for humans and history labels), not a content hash. Users choose their own versioning scheme. Stale tag detection prevents accidental reuse.

5. **Content invalidation** — Any page save with a new version number resets all confirmations. There is no partial invalidation or "minor edit" bypass. This is strict by design: if the content changed, the review must restart.

6. **Todo integration is optional** — Reviewflow works standalone (file-based, no database needed). When the todo plugin is also active, reviewflow creates tasks as a convenience. The two plugins are loosely coupled via the `TodoIntegrator` interface.

7. **Confirmation is simple (for now)** — Authenticated session click. X.509 signing is a planned follow-up (see §13) and will be a non-breaking extension.
