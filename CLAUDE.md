# CLAUDE.md — Gowiki

## Project overview

Gowiki is a rewrite of Dokuwiki with three fundamental changes:
- Storage format is a custom bijective Markdown dialect (not Dokuwiki's syntax)
- The editor is tightly and natively integrated with ProseMirror (dual mode: raw Markdown + visual)
- The backend is Go; the frontend is TypeScript/ESM centered on ProseMirror

The canonical document format is Markdown (the custom dialect). The ProseMirror document model is a projection of it, not a source of truth.

## Repository layout

```
frontend/       Vite + ProseMirror document/editor engine (TypeScript/ESM)
backend/        Go API server (chi), authoritative for all storage
scripts/        Local orchestration helpers
data/
  content/      All user-managed files: .md pages and media attachments (same root)
  meta/         Metadata mirroring content/ structure, .json files only
```

## Architecture invariants — never violate these

- The Go backend never depends on Node.js, Vite, or the TypeScript toolchain.
- The backend is authoritative for: storage, document identity, document relationships, media lifecycle, indexing, search, access control, and export.
- The frontend is responsible for: document semantics, editing UX, rendering. It never enforces backend rules locally.
- The communication boundary between frontend and backend is HTTP REST only.
- Markdown is the ground truth. The backend only knows Markdown. The backend never encodes editor-specific semantics.
- The frontend never "fixes" backend inconsistencies.

## Development vs production

**Development:** frontend served by Vite (HMR, unbundled ESM), backend exposes API only. Run with `make dev` or separately via `make dev-backend` / `make dev-frontend`. Frontend proxies `/api/*` to `http://localhost:8080`.

**Production:** frontend bundled (core bundle + one bundle per plugin), served as static assets by the Go backend. Build with `make build-frontend`, serve with `make run-prod`.

## Custom Markdown dialect — key rules

The dialect is **bijective**: one canonical syntax per node type. No alternative syntaxes. CommonMark alternatives are explicitly rejected. When importing foreign Markdown, non-canonical syntax may be transformed on import.

Key divergences from CommonMark:
- `*italic*` only — `_italic_` is **underline**, not italic
- `**bold**` only — `__bold__` rejected
- `_underline_` — not italic
- ATX headings only (`#`) — setext headings rejected
- `- item` for unordered lists — `*` rejected
- Raw HTML forbidden — `<` and `>` are plain characters
- HTML entities not interpreted — use UTF-8 directly
- Single newline in a top-level paragraph = hard line break (`<br>`)
- Trailing spaces have no meaning — two-space line break rule does not exist
- `\n` literal = explicit hard line break (valid in lists and tables only, not in paragraphs where a bare newline already produces a hard break)
- Properties syntax: `{pluginname key=value}` on its own line before the target block
- No column alignment syntax in tables
- Multi-body tables (`<tbody>`) are not supported and must be rejected

Round-trip between raw, visual, and view modes must be lossless. Deterministic serialization is a hard requirement.

## Content and link resolution

All user content (pages and attachments) lives under `data/content/`. Metadata lives under `data/meta/` mirroring the same folder structure.

**Page links** (resolve to rendered page):
- `/path/to/page` → `content/path/to/page.md`
- `/path/to/namespace` or `/path/to/namespace/` → `content/path/to/namespace/index.md`
- `./page` → adjacent `page.md` relative to current page

**Attachment links** (resolve to raw file):
- `/path/to/file.ext` → `content/path/to/file.ext`
- Attachments **must have an extension** — extension-less files under content/ are forbidden
- `./page.md` is the raw attachment of `page.md`; `./page` is the rendered page

**Namespace constraint:** if `content/path/to/ns/` exists, `content/path/to/ns.md` must not exist.

Metadata file path derivation: replace `content/` with `meta/`, replace `.md` with `.json`.

## Plugin architecture

Plugins are TypeScript only. Each plugin owns its full vertical slice:
- ProseMirror node/mark schema extension
- Editor behavior (commands, keymaps, input rules)
- Markdown parser rules (dialect → PM)
- Markdown serializer rules (PM → dialect) — must be bijective
- HTML rendering rules
- Export/print CSS (optional)

Plugins are loaded as independent bundles at runtime. Disabling a plugin must never corrupt documents or break loading/editing/storage. It may cause loss of specialized semantics for nodes owned by that plugin.

Plugins do **not** own: authentication, access control, configuration, document identity, storage layout, search indexing, export mechanisms.

## Backend API (current)

```
GET  /api/health
GET  /api/pages/{path}          returns raw Markdown + page metadata
PUT  /api/pages/{path}          body: { "markdown": "..." }, atomic write, returns page payload
```

## Current implementation state

### Plugin and registry architecture
The plugin/core boundary is largely implemented. The canonical reference for how the registry works is `frontend/compiler/registry.ts`. The canonical reference for how a plugin is structured is `frontend/plugins/table.ts` — read this before writing or modifying any plugin.

Known smell: `frontend/compiler/core_nodes.ts` lines 114–129 contain a specific entry about the table plugin that may be a hardcoded reference leaking through the plugin boundary. This should be investigated and resolved as part of any work touching the plugin architecture.

### Visual editor — functional today
- Bold, italic
- Headings (ATX)
- Unordered and ordered lists, including nested sublists, with Tab/Shift-Tab to increase/decrease nesting
- Tables, with Tab to navigate between cells
- Images with drag-resize (Shift to constrain proportions), live-updating size property in both the node and the Markdown source
- Internal and external links (external links display a distinct icon and open in a new tab)
- Media manager: fully implemented frontend and backend, handles file upload and storage, inserts image nodes or download links. Currently limited to a flat file listing (no image thumbnails, no file-type icons in listing or rendering).
- Code blocks with Tab/Shift-Tab indent/deindent support and language specifier (syntax highlighting per language).
- Include nodes: implemented, renders as read-only zone in visual mode with property panel (yellow, consistent with other property panels). Include button in toolbar (visual and raw).
- Full-text search: incremental, typo-tolerant.
- Save/reload cycle is implemented and validated.
- Navigation between pages is implemented.
- New page creation is functional but minimal (creates a base .md, editing tested).

### Properties system
- Defined and working for: image nodes (size, drag-resize updates property live), table nodes (size property defined, drag-resize not yet implemented), include nodes (path property)
- Defined but not yet implemented for: headings (numbered heading property)

### Rendering model
Rendering is done entirely by ProseMirror. There is no separate rendering pipeline for view mode — the visual editor output and the rendered view are identical by construction. This is a deliberate architectural choice and should not be changed.

### v0.1–v0.3 status
v0.1, v0.2, and v0.3 are complete.

## Milestone targets

- **v0.1** ✓ — Single-page correctness: edit and persist main page content, render sidebar and footer composition, save/reload reliably, render included content as read-only.
- **v0.2** ✓ — Site-level consistency: reference tracking, orphan detection, circular include detection.
- **v0.3** ✓ — Search: full-text, incremental, typo-tolerant. Language-specific syntax highlighting in code blocks.
- **v0.4** — Editing robustness: correct copy/paste semantics, document validity after any edit.
- **v0.5** — History and diff: page history, rollback.
- **v0.6** — Admin page: configuration UI, authentication backends, ACL.
- **v0.7** — Structured data: per-page structured fields, queries, rendering.

## v0.4 detailed scope

### Copy/paste semantics
- Pasting into an editable region must strip or transform any content that violates the dialect (foreign Markdown syntax, raw HTML, unsupported node types).
- Pasting must never affect non-editable regions (sidebar, footer, included content).
- Pasting across the boundary of a non-editable region must be handled gracefully: split the paste around the non-editable zone, or reject with a clear user signal. Never silently corrupt the document.

### Document validity after any edit
- After any edit operation (paste, drag, undo, redo), the document must remain a valid Gowiki dialect document.
- Invalid states must be caught and corrected at the ProseMirror schema level, not silently stored.
- The Markdown produced after any edit must pass round-trip validation: serialize → parse → serialize must yield the same result.
- This applies in both visual and raw mode.

## v0.2 detailed scope (reference)

### Reference tracking
The backend maintains a map of which pages reference which media files. This map is updated immediately on every write (not batch-based). It is the foundation for orphan detection.

### Orphan detection
When a media file becomes unreferenced (no page links to it), the backend detects it immediately. The frontend may prompt the user for deletion. Orphan handling is backend-driven. Media versioning is out of scope.

### Circular include detection
The backend detects and rejects circular includes at save time (not render time). If saving a page would create a direct or transitive include loop, the backend returns an error. The frontend displays the error to the user. No partial saves, no silent failures. The frontend does not attempt to detect or prevent cycles locally.

## Dialect implementation status (reference)

| Feature | Status |
|---|---|
| `*italic*`, `**bold**`, `` `code` `` | implemented |
| `~~strikethrough~~` | planned |
| `_underline_` | planned |
| ATX headings | implemented |
| `- unordered list` | implemented |
| `1. ordered list` | implemented |
| `{heading numbered=true/false}` | planned |
| Pipe tables + directives | implemented |
| Multi-body tables | rejected |
| Properties/directives `{name key=value}` | implemented |
| Raw HTML forbidden | implemented |
| Single newline → hard break in paragraphs | implemented |
| `\n` literal → `<br>` (lists and tables only) | implemented |
| Code block language specifier + highlighting | implemented |
| Deterministic round-trip | partial |
| HTML entities not interpreted | partial |

## Do not

- Do not introduce alternative Markdown syntaxes — bijectivity is non-negotiable
- Do not store metadata under `data/content/`
- Do not create extension-less files under `data/content/`
- Do not create `content/path/to/ns.md` if `content/path/to/ns/` exists
- Do not make the backend depend on Node.js or the TS toolchain
- Do not encode editor semantics in the backend
- Do not let plugins touch authentication, ACL, storage layout, or search indexing
- Do not bundle core and plugins together — they must remain independently loadable
- Do not detect include cycles in the frontend — this is a backend responsibility
- Do not implement media versioning before v0.5
- Do not silently store invalid document states — enforce at the ProseMirror schema level