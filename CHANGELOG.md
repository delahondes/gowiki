# Changelog

All notable changes to Gowiki. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Dates are ISO-8601.

Gowiki is a from-scratch DokuWiki replacement built on a bijective Markdown
dialect with a ProseMirror-backed dual-mode editor. See `CLAUDE.md` for the
project's design invariants and `specs/` for the dialect specification.

## [Unreleased]

Nothing pending — the working tree matches `v1.0.0-rc.1`.

## [1.0.0-rc.1] — 2026-09-26

Release candidate for v1.0.0. No new features versus the v0.95 line; the RC
exists to stabilise the surface before cutting v1.0.0. Recent hardening in
this window:

### Added

- MCP draft-session tools: `enter_edit_session`, `save_edit_draft`,
  `read_edit_draft`, `publish_edit_draft`, `discard_edit_draft`. An AI
  agent can join a collaborative edit, iterate on the draft, and publish
  through the same server-side pipeline the HTTP handler uses (inline
  row-edit guard, flow-marker strip, database validation, page store,
  todo auto-complete).
- MCP `update_database_row` tool: partial patch that syncs the bound page.
- MCP `render_page` tool: returns the fully-rendered HTML the way a
  browser would see it (dynamic directives resolved).
- MCP attachment tools: `upload_attachment`, `upload_attachment_instructions`,
  `read_attachment`, `list_attachments`, `delete_attachment`. HTTP multipart
  is the primary transport; base64 is a small-file fallback with SHA-256 +
  size verification.
- Pivot tables: keep empty-axis rows via the `@null` sentinel, and remap
  or merge axis labels via `pivot_rows_labels` / `pivot_cols_labels`.
- Tag queries: `groupby=folder` renders group headings.
- Sitemap: auto-expand and `{favorites}` component.

### Changed

- `enter_edit_session` gates lock takeover on WebSocket presence rather
  than caller identity. Any live editor blocks the reclaim; a truly
  stale lock (closed tab, crashed session) is reclaimable regardless
  of who owned it. This replaces the earlier same-user `force=true`
  parameter.
- Disabling an account now revokes every live session for that user
  immediately (belt-and-braces: also blocked per-request in `requireAuth`).
- Save-and-continue toolbar icons rebalanced (small floppy + prominent
  right-arrow); draft-exit status message clarified.
- Search index tokenises directive names, keys, and values; index is
  rebuilt on startup.
- Reviewflow versions normalised via dot-tuple padding so `1` and `1.0`
  round-trip identically.

### Fixed

- Cancel-edit no longer deletes a pre-existing draft — it only discards
  unsaved changes since edit-mode entry. Deletion stays reachable via
  the explicit "Discard draft" action.
- Underscore-delimited underline (`_word_`) after a `\n` literal hard
  break inside a table cell or list item now opens as expected. Root
  cause: markdown-it's `_`-em rule refused to open between word
  characters (its `snake_case` guard) because the `\n` literal was still
  two plain characters at inline-parse time. Fix promotes `\n` literals
  to real newlines before the inline core rule runs.
- `render_page` MCP tool cookie bug: now uses `auth.CookieName` instead
  of a stale `"session"` literal.
- Highlighter un-highlight: fixed for the "caret inside a mark" case by
  detecting the run around the caret and removing the whole mark.
- Table column-color and formula-color rules no longer shade header
  cells (they hid the header's semantic contrast).
- Sitemap: leaf URLs for namespace-index pages no longer double up their
  trailing slash.
- Signing certificate revocation: profile respects server-side
  revocation; distinguishes "revoked" from "fresh key awaiting a new
  certificate"; stale IndexedDB PEMs are purged so the download button
  reappears.
- Emoji rendering: added Apple/Segoe/Noto colour emoji to theme font
  stacks so the highlighter icon renders on Linux.
- `RefIndex` media key shape: lookups now consistently use the leading
  slash the index stores with.

## [0.9.x] — 2026-04 → 2026-09

The v0.9 line consolidated the platform into something that could
credibly replace a live DokuWiki installation. Milestone-level work:

### Reviewflow (author / reviewer / validator)

- Reviewflow directive with configurable roles and version tags.
- X.509 signing pipeline with per-user certificates issued from a
  company CA; certificate revocation with cascade.
- Observers: named accounts that see draft states without being in
  the review chain.
- Version history recorded per validated version; version links
  (`{version-link}`, `{reviewflow-link}`) resolve to stable snapshots.
- Todo integration: reviewflow assignments become todos.

### AI integration

- AI Content API: token-authenticated read/write endpoints with
  rate limiting and `require_summary` enforcement.
- MCP server (Model Context Protocol) exposing page, database,
  attachment, and (in v1.0.0-rc.1) draft-session tools over
  Streamable HTTP. Same ACL as the HTTP API, with an additional
  `@ai` subject check on every tool call.
- Integrated AI assistant (browser-side, server-proxied) with quick
  actions and per-page context.
- HTML rendering endpoint for AI check paths.

### Collaboration

- WebSocket presence (`/api/ws/collab`): live badges showing who is
  viewing / editing each page.
- Co-edition: multiple users on the same draft, backed by Yjs.
- Draft reclaim on stale/crashed sessions (raw-edit escape hatch,
  admin override, and now presence-gated takeover).
- Persistent error messages so a lost network doesn't hide state.

### Structured data — polish

- Database types (image, tag, user, lookup) with nested filtering.
- Inline row editing with conflict detection versus draft edits.
- Row-bound pages: restoring a page updates its bound row.
- Row-name indexing so bound pages can address rows by name.
- Database queries with vertical text, cell colours, and column
  alignment properties.
- Import: DokuWiki `struct` blocks converted to database tables +
  rows, with status/tag migration.

### Extended tables

- Formulas (spreadsheet-style, with `ABOVE`/`LEFT` ranges).
- Column properties: colour, alignment, width, decimals, vertical
  align — properties travel with columns when the column is moved.
- Cell merging (`<<`, `^^`) with menu entries.
- Vertical text and vertical align, including inside merged cells.
- Backticks protect cell content from directive/formula parsing.

### Templates and variables

- Multiple templates per page family; per-template variable defaults.
- Template stamping with reviewflow version pinning.
- Global variables and immediate rendering.

### Blocks and content

- Slides plugin (`{slide}` blocks with themes).
- Charts plugin (Chart.js) and mermaid diagrams; both support
  captions and PDF export.
- Bibliography (PubMed integration with API key).
- Spoilers (folded content).
- Multi-image figures with `image-width` and wrap properties on
  blockquotes.
- Footnotes (inline `^[…]` and referenced).
- Comments plugin: threaded, replies, anchoring to selection ranges;
  survives edits and remains visible on database-query rows.

### Site and platform

- Theming: light / dark modes with per-user override (allowed by
  admin) and code-block themes that follow the site theme.
- PDF export via headless Chrome (chromedp).
- Sitemap with folder/leaf distinction; forbidden pages honoured.
- Todos plugin with assignments, email notifications, per-page
  todo lists, admin panel.
- Backlinks index.
- Tags with query, exclusion, groupby, clickable rendering.
- OAuth Azure AD + group syncing.
- Persistent sessions with `Secure` cookies on HTTPS.
- Canonical page paths: leading `/`, trailing `/` for namespace
  indexes, `/index` never appears — enforced everywhere in the API.
- `@self` ACL subject.
- DokuWiki importer with users + ACLs, code-block conversion,
  monoline tables, reviewflow validation migration, UTF-8 picker,
  fallback-admin option.

## [0.8] — 2026-02 → 2026-03

Feature-completeness for a documentation wiki.

- PDF export.
- Sitemap and site map plugin.
- Tags plugin (`{tag}`, `{tag-query}`) with page/namespace guarding.
- Numbered headings (`## 1. Title`) surfacing in the TOC.
- Todo plugin (`{todo}`, `{todo-list}`) with target/assignee checks.
- Extended tables (formulas + cell merging).
- Templates fully working (variables, insertion).
- Reviewflow (initial cut, later matured in 0.9).
- Latest-change and backlinks plugins.
- Move plugin: page rename with link updates.
- OAuth (Azure) provider working with group sync.

## [0.7] — 2026-01

Structured data.

- Database plugin: schema store, data store, and `{database-row}` +
  `{database-query}` directives.
- Inline editing with edit-vs-draft conflict handling.
- Row-bound pages: restore a page → table row updates too.
- Property panels for database node types.

## [0.6] — late 2025

Admin and access control.

- Admin UI: users, groups, ACL, configuration, tokens.
- Local authentication with password hashing.
- Multi-provider ACL model.
- Media versioning (downloads and edit-session competition).
- Locks: same-user competing edits handled cleanly.
- Icons for download links, filename spaces supported.

## [0.5] — late 2025

Page history and drafts.

- Attic-backed page history with per-version storage.
- Rollback / restore-from-history flow.
- Drafts: durable per-user drafts with edit tokens.
- Publish / discard / auto-save cycle validated.
- History diff view (compact and side-by-side).

## [0.4] — mid 2025

Editing robustness.

- Copy/paste dialect enforcement in visual mode.
- Round-trip validation gate on save (serialize → parse → serialize).
- Blockquote wraps.
- Image resize, insertion, and copy/paste of large images.

## [0.3] — mid 2025

Search and syntax highlighting.

- Full-text search: incremental, typo-tolerant (Bleve).
- Language-specific code-block highlighting (highlight.js).
- Instant language change; language detection.
- Banner / header polish.

## [0.2] — mid 2025

Site-level consistency.

- Reference tracking (page → media map, updated on every write).
- Orphan detection.
- Nested includes with circular-include forbidden.

## [0.1] — early 2025

Single-page correctness.

- Editor foundation on ProseMirror with a bijective Markdown dialect.
- Save / reload cycle validated.
- Sidebar, footer, and included content rendered as read-only zones.
- Raw and visual edit modes with the same underlying document.
- Line breaks (soft, hard, Alt-Enter).
- Links (internal and external, external opens in new tab with icon).
- Tables (pipe syntax with directives).
- Media manager (upload, insert, downloadable link).
- Images with size property + drag-resize.
- Code blocks with tab/shift-tab indentation.
- Include directive.
- Property panels for image, table, include nodes.

---

[Unreleased]: /
[1.0.0-rc.1]: /
