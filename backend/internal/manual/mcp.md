# MCP (Model Context Protocol) Server

Gowiki exposes its AI-oriented API as a full MCP server at `/api/mcp/v1`. An MCP client (Claude Desktop, Claude.ai, Cursor, any MCP-compatible agent) can connect to the wiki as a tool provider, giving the LLM structured access to your pages, todos, structured-data tables, and reviewflow state.

The MCP server is a thin wrapper around the existing AI Content API (`/api/ai/v1/…`). Everything it does is subject to the same ACL model: the authenticated user AND the `@ai` pseudo-subject must both have permission to view (or edit) a page for the operation to succeed.

## Connection

- **URL:** @`https://{{SERVER}}/api/mcp/v1`
- **Transport:** Streamable HTTP (MCP 2025-03-26 spec)
- **Auth:** OAuth 2.0 with browser-based login and consent (no token to copy-paste)

Discovery is handled by the client — the server publishes:

- `/.well-known/oauth-protected-resource` (RFC 9728)
- `/.well-known/oauth-authorization-server` (RFC 8414)
- Dynamic client registration at `/oauth/register` (RFC 7591)

On first use the client is redirected to a wiki login page, then to a consent screen, and finally receives an access token bound to your identity. Revoke it any time under **Admin → Tokens**. Only S256 PKCE is accepted.

### Claude Desktop

Claude Desktop's stable config schema does not yet accept remote HTTP MCP servers directly, so the path is to run Anthropic's `mcp-remote` proxy locally — it speaks stdio to Claude Desktop and forwards to the wiki over HTTP. Requires Node.js on the machine (any recent LTS).

The config file lives at:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`
- **Linux:** `~/.config/Claude/claude_desktop_config.json`

Open it directly, or from Claude Desktop go to **Settings → Developer → Edit Config** (**Application bureau → Développeur → Modifier la config** in French).

![Claude Desktop → Developer → Modifier la config](./screenshots/42.png)

For this wiki the URL is @`https://{{SERVER}}/api/mcp/v1`.

Pick one of the two auth flows below, save the file, then **fully quit** Claude Desktop (Cmd-Q on macOS, right-click → Quit on Windows — closing the window is not enough) and relaunch.

**Option A — Bearer token (recommended):** deterministic, no browser step, tools appear immediately.

1. Create an API token: **Admin → Tokens → New token** on the wiki. Copy the `gwk_...` value shown once.
2. Add the entry with a hard-coded `Authorization` header:

   ```json
   {
     "mcpServers": {
       "gowiki": {
         "command": "npx",
         "args": [
           "-y",
           "mcp-remote",
           "https://YOUR-WIKI-HOST/api/mcp/v1",
           "--header",
           "Authorization: Bearer gwk_YOUR_TOKEN"
         ]
       }
     }
   }
   ```

   The `--header` value must be a single string in `"Name: Value"` form. Keep the token secret — this file is plaintext on your machine.

**Option B — OAuth (browser consent flow):** no token in the config, but relies on `mcp-remote` opening a browser tab on first launch and completing an OAuth callback back to a local port. Prone to silent failure on Desktop (blocked popups, callback port collisions, sandbox restrictions).

```json
{
  "mcpServers": {
    "gowiki": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://YOUR-WIKI-HOST/api/mcp/v1"]
    }
  }
}
```

![Editing claude_desktop_config.json with the mcp-remote proxy entry](./screenshots/43.png)

On the first tool call a browser tab opens on the wiki asking you to sign in and approve. Once approved the tab closes and `mcp-remote` caches the token in `~/.mcp-auth/` for future runs. If nothing happens on the first call — no browser tab, no tools appearing — switch to Option A. Logs are at `~/Library/Logs/Claude/mcp*.log` (macOS) or `%APPDATA%\Claude\logs\` (Windows).

### Claude.ai (web, Pro / Team / Enterprise)

Claude.ai's web app supports remote MCP servers natively — no local proxy, no token to copy. The OAuth handshake described above happens in the browser and the tools appear directly in the workspace, scoped to your wiki identity.

1. In claude.ai, open **Settings** → **Connectors** (labelled **Integrations** in some workspaces). Click **Add custom connector**.
2. Paste `https://YOUR-WIKI-HOST/api/mcp/v1`. For this wiki that's @`https://{{SERVER}}/api/mcp/v1`. Confirm.
3. A wiki page opens asking you to sign in (if you're not already) and approve access. Approve.
4. Return to Claude — `gowiki` tools are now available in every conversation.

**Per-user vs org-wide.** On a Team or Enterprise plan an admin can add the connector once at the workspace level so every member inherits it, or each user can add it individually — both work. Either way, each user's session gets **their own** OAuth token bound to their own wiki identity, so page-level ACLs still apply per user (the `@ai` pseudo-subject gates it further).

**Revocation.** OAuth-issued tokens appear under **Admin → Tokens** on the wiki alongside static bearer tokens. Revoke any one to cut off the corresponding Claude.ai session; the next tool call will re-prompt for consent.

### Claude Code (CLI)

Claude Code supports remote HTTP MCP servers natively. Two paths:

**With a bearer token (simplest, no browser step):**

1. Create an API token: **Admin → Tokens → New token** on the wiki. Copy the `gwk_...` value shown once.
2. Register the connector — pick a scope:

   ```bash
   claude mcp add gowiki --transport http https://YOUR-WIKI-HOST/api/mcp/v1 \
     --header "Authorization: Bearer gwk_YOUR_TOKEN" -s local
   ```

   For this wiki the URL is @`https://{{SERVER}}/api/mcp/v1`.

   Scope choices (`-s` flag):

   - `local` (default) — this project only, private to you (stored in `.claude/settings.local.json`)
   - `user` — every project on this machine, private to you
   - `project` — this project, shared with teammates (stored in `.mcp.json`, commit to git)

3. Verify with `claude mcp list` or `claude mcp get gowiki`. In a fresh `claude` session the `mcp__gowiki__*` tools appear directly.

**With OAuth (browser consent flow):**

Omit the `--header` flag and Claude Code will fall back to OAuth on first use — it prints an authorization URL you open in a browser, log in on the wiki, click Approve, and Claude Code receives the token via a `http://localhost:PORT/callback` handoff. Same experience as Claude.ai's UI, just driven from the CLI.

**Troubleshooting — connector stuck on auth stubs:**

If `mcp__gowiki__authenticate` shows up instead of the real tool list, Claude Code has cached a failed handshake. In the running session type `/mcp` to open the MCP panel and pick **Reconnect** on `gowiki` — it clears the state and re-does the handshake with the current stored auth. Faster than restarting the whole CLI. Only fall back to `claude mcp remove gowiki && claude mcp add ...` when `/mcp` reconnect doesn't recover.

### mcp-inspector or any raw MCP client

If your client cannot do OAuth (older mcp-inspector, custom scripts, CI), fall back to a personal API token. Create one under **Admin → Tokens** and pass it in the Authorization header. For this wiki the URL is @`https://{{SERVER}}/api/mcp/v1`:

```
npx @modelcontextprotocol/inspector \
  --transport streamable-http \
  --url https://YOUR-WIKI-HOST/api/mcp/v1 \
  --header "Authorization=Bearer gwk_..."
```

## Tools

| Tool | Purpose |
|---|---|
| `get_conventions` | Markdown dialect rules and content guidelines. **Call first.** |
| `list_namespace` | Enumerate pages and sub-namespaces under a path |
| `read_pages_batch` | Read up to 20 pages in one call |
| `get_page_meta` | Page title, version, tags, backlinks, reviewflow status |
| `search_pages` | Full-text, typo-tolerant search — pass `tag` to filter by tag instead (combine with `query` to narrow by substring) |
| `get_reviewflow_status` | Reviewflow roles, confirmations, validation state |
| `preview_page_diff` | Dry-run edit — returns diff without saving |
| `write_page` | Create/update a page (full rewrite) — requires a summary |
| `edit_page` | Anchored search-and-replace edits — safer than `write_page` for any change smaller than a full rewrite (uniqueness constraint prevents accidental corruption) |
| `list_todos` | Todo tasks, filterable by status/assignee/namespace/due |
| `complete_todo` | Mark a todo as done |
| `list_database_tables` | Structured-data tables with field definitions |
| `query_database_rows` | Query rows from a structured-data table (accepts `filter=[…]`; use `@null` as the value for IS NULL / IS NOT NULL tests) |
| `insert_database_row` | Insert a row (page-bound tables also get their wiki page created) |
| `delete_database_row` | Delete a row (page-bound rows also have their page removed; refuses when other rows still reference this one — pass `force=true` to override; page history stays in the attic) |
| `delete_page` | Delete a page (archived to attic; row-bound pages also drop their DB row) |
| `create_database_table` | Create a new structured-data table (admin only) |
| `create_database_field` | Add a field to an existing table (admin only) |
| `update_database_table` | Update a table's metadata — label, page_folder, sort defaults, template (admin only, non-destructive) |
| `update_database_field` | Update a field's metadata — label, required, default, placeholder, enum_values (admin only; no rename or type change) |
| `move_page` | Rename a page to a new path (updates incoming links; supports `dry_run`) |
| `convert_page_to_namespace_index` | Turn a leaf page (`foo`) into a namespace index (`foo/`) — unblocks writes under it |
| `convert_page_to_regular_page` | Turn an empty namespace index (`foo/`) back into a leaf page (`foo`) |
| `list_page_history` | Every archived version of a page (version, timestamp, author, summary, md5) |
| `read_page_version` | Markdown of a specific historical version, with its metadata |
| `list_recent_changes` | Cross-page changelog, filterable by `since`/`until`/`author`/`path_prefix` — every entry ACL-checked |
| `diff_page_versions` | Line-level diff between two archived versions of the same page (same hunk shape as `preview_page_diff`) |

## Searching by tag

`search_pages` accepts an optional `tag` parameter alongside `query`. At least one of the two must be set:

- `query` alone — full-text, typo-tolerant FTS over page bodies. Returns snippets.
- `tag` alone — list every page bearing that tag (no snippets, no ranking).
- `tag` + `query` — pages bearing the tag, narrowed to those whose path or title contains `query` (case-insensitive substring).

All results are filtered by the caller's ACL.

Examples:

```json
{ "tag": "sop" }
{ "tag": "sop", "query": "biomscope" }
```

This is the same syntax the wiki search bar exposes as `tag:NAME [substring]`. See [Tags](/wiki/manual/tags) and [Search](/wiki/manual/search) for the user-facing equivalent.

## Editing pages — prefer `edit_page` over `write_page`

`write_page` replaces the entire markdown of a page. Cost aside, that means **every edit's risk is proportional to page size, not to the change** — retyping 300 lines to fix one word gives you 300 lines of opportunity to introduce truncations, forbidden HTML entities, or shifted heading levels. On a regulatory document, that turns a cosmetic tweak into a corruption vector.

**`edit_page(path, edits[], summary, expected_version?, dry_run?)`** is the safer default for anything short of a full rewrite. Each edit is `{old, new, replace_all?}`, and the tool enforces one strict rule:

- **`old` must occur exactly once in the current buffer.**
  - Zero occurrences → refused (the anchor is wrong, or the change is already applied).
  - Two or more without `replace_all: true` → refused (ambiguous).
  - With `replace_all: true` → every occurrence is replaced.

This uniqueness constraint is what makes the tool safe without line numbers: the anchor self-locates and self-validates. There's no line-number drift the way a unified diff would give you.

**Atomicity.** Edits apply in array order — edit *n+1* sees the buffer produced by edit *n*, letting you chain rewrites. If any single edit fails validation, the whole call is refused and no version is written. Never partial.

**Same gates as `write_page`.** Caller + `@ai` need `edit` permission. Optimistic locking via `expected_version` is honored. Draft locks by other users refuse the call. Summary follows the `[AI: <tool>] <description>` convention.

**Preview.** Pass `dry_run: true` to get the resulting diff (added/removed lines + hunks, same shape as `preview_page_diff`) without writing.

Typical uses where `edit_page` is right:
- Fix a typo, a link, a broken directive — one edit with a unique anchor.
- Rename a field across many mentions on one page — one edit with `replace_all: true`.
- Migrate `@@table.field@@` placeholders to `{{field}}` in template pages — one edit per placeholder, each anchored on the unique occurrence.

Use `write_page` only when you truly do want to replace the whole page (creating from scratch, wholesale reorganizations).

## Renaming and namespace conversion

Three tools cover the page-move plumbing that `write_page` alone can't handle:

- **`move_page(from_path, to_path, update_links?, move_media?, dry_run?)`** — rename a page. `update_links` defaults true (rewrites every incoming link across the wiki); `move_media` defaults false (shared media stays put); `dry_run` returns the preview (affected pages, media that would move) without writing. Requires edit permission on both source and destination folder, for the caller AND `@ai`.
- **`convert_page_to_namespace_index(page_path)`** — turns a leaf page (`foo`) into a namespace index (`foo/index.md`). Use this when `write_page` refuses with a namespace-conflict error because the parent path already exists as a leaf. Typical case: creating `/regulatory/qms/qara/sop14/rec01` fails because `sop14.md` exists; convert first, then write.
- **`convert_page_to_regular_page(page_path)`** — the inverse. Refuses if the namespace still has children.

All three refuse cleanly when the page has an active edit lock or draft (`ErrPageHasLock` surfaces as `page has an active edit lock or draft: …`), when the destination already exists, or when a namespace/leaf naming collision would violate the "one canonical path per page" invariant.

## Row inserts and deletes

`insert_database_row(table, fields)` and `delete_database_row(table, row_id)` mirror the row-write side of the HTTP API but with two important guarantees on top:

- **Symmetric page handling.** When the table has a `page_folder`, insert creates the associated wiki page (respecting the table's `page_template_path` if any); delete removes it. The HTTP delete alone leaves an orphan page — the MCP tool doesn't.
- **Audit-safe deletion.** The page is removed from the live content but its history stays in the attic and `list_recent_changes` shows both the creation and the deletion as page changelog entries (`change_type: "edit"` at write, `"delete"` at removal). Test rows in a regulatory register can be cleaned up without erasing the trail an auditor might inspect.

**ACL:**
- Page-bound tables — the caller AND `@ai` need edit permission on the `page_folder` namespace (insert) or delete permission on the specific bound page (delete).
- Non-page-bound tables — the caller must be in the `admin` group. This is deliberately strict: agent-driven row writes to schema-only tables can't be gated per-row via the page ACL, so admin is the only safe default.

Typical clean-up flow for a test round-trip: `insert_database_row` → verify the created page — `read_pages_batch` on the returned `page_path` — → `delete_database_row(row.id)`.

**Schema defaults.** On insert, any active field the caller doesn't supply falls back to the column's `default_value` (from the field definition, same value that pre-fills the `{database-newrow}` form), and only then to the SQL column default. So API inserts now behave like form inserts — you don't have to re-list defaults.

**Referential integrity on delete.** Before deleting, the tool scans every active `lookup` / `tag` field with `foreign_key` pointing at this table. If any row anywhere references the id being deleted, the tool returns `{"deleted": false, "references": […]}` listing the offenders and does not delete. Pass `force=true` to override — the referencing rows are then left dangling, so use only for cleanup where you know the references are stale.

**Standalone page deletion.** `delete_page(page_path)` covers pages that aren't row-bound. For row-bound pages, prefer `delete_database_row` — it goes through the RI check and gives you the symmetric insert/delete lifecycle.

## Schema management (admin only)

Four tools let an admin bootstrap and evolve structured-data tables from an agent, mirroring what the **Admin → Database** UI exposes:

- **`create_database_table`** — new table with name, label, and optional `page_folder`/`page_template_path`/sort defaults. The table's `name` and each field's `name`+`type` are **immutable** — pick carefully. `page_folder` may include `@field` substitution (e.g. `/regulatory/qms/soft/server/@server_name`).
- **`create_database_field`** — add a field. Types: `text`, `integer`, `float`, `boolean`, `date`, `datetime`, `page_link`, `enum`, `multi_enum`, `auto_increment`, `image`, `color`, `tag`, `lookup`, `user`. `enum_values` for `enum`/`multi_enum`; `foreign_key` for `tag`/`lookup`; `display_column` for `lookup`.
- **`update_database_table`** — non-destructive: change label, `page_folder`, template, sort defaults. Only supplied fields are updated. No renames.
- **`update_database_field`** — non-destructive: change label, `required`, `default_value`, `display_order`, `placeholder`, `enum_values`. No rename or type change; delete a field via the admin UI.

Every call is gated behind the caller's membership in the `admin` group — non-admin users get `access denied: schema management is admin-only`. All changes are recorded in `database_schema_history`. To delete a table or a field, use the web admin UI: that path preserves the confirmation prompts and is where destructive operations belong.

Typical bootstrap flow for a new SOP record: `create_database_table(name=…, label=…, page_folder=…)` → one `create_database_field` per column → write the record page with `{database-query table=NAME}` and `{database-newrow table=NAME}` directives via `write_page`.

## History and audit

Four tools give agents access to the page-history plumbing behind the wiki's own **History** view:

- **`list_recent_changes`** is the entry point for cross-page audit questions. Filter by any combination of `since` / `until` (RFC3339 timestamps), `author` (exact username), and `path_prefix`. Each returned entry is ACL-checked against the caller — pages you can't view are silently dropped.
- **`list_page_history`** returns the full version list of a single page.
- **`read_page_version`** returns the markdown of a specific archived version, plus its metadata.
- **`diff_page_versions`** returns a structured line-level diff between two versions (same hunk shape as `preview_page_diff` — `equal`/`insert`/`delete` with `added`/`removed` counts).

Typical audit flow: `list_recent_changes(author=X, since=…)` → pick an entry → `diff_page_versions(page, entry.version-1, entry.version)` to see what changed in that revision.

## Resources

Pages are exposed as `wiki:///path/to/page` URIs via a resource template. Clients can:

- List `wiki:///` as the starting entry point
- Fetch any page path directly through the template

`read_pages_batch` is generally faster for the LLM than issuing multiple `resources/read` calls, so resources are most useful when the client pins a URI as conversation context.

## Prompts

| Prompt | Arguments | Purpose |
|---|---|---|
| `summarize_page` | `path`, `max_bullets` | Bullet summary of a page |
| `draft_review_checklist` | `path` | Reviewer checklist — includes reviewflow state |
| `suggest_tags` | `path` | Propose tags, preferring existing ones |

## Conventions the client must follow

The server advertises these in its instructions and re-exposes them through `get_conventions`. In short:

1. Call `get_conventions` once at session start.
2. Always `preview_page_diff` before `write_page`, and show the diff to the user.
3. Every write requires a summary formatted `[AI: <tool>] <description>`.
4. Optimistic locking: read → preview → write with `expected_version` set to the read version.

## Relationship to the HTTP AI API

The MCP server reuses the same handlers conceptually but is a separate entry point. If you already integrate via `/api/ai/v1/…`, nothing changes — both paths stay available. New agent integrations should prefer MCP because:

- The tool contract is explicit (JSON Schema inputs, typed outputs).
- Resources give clients a native way to include pages as conversation context.
- Prompts centralize ready-made workflows instead of duplicating prompt templates across clients.
