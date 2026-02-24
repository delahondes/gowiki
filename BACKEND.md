# Backend architecture and responsibilities

## Purpose

The backend is the single source of truth for the wiki.
It owns persistence, structure, consistency, and cross-document reasoning.

The frontend (raw Markdown editor or ProseMirror editor) is a projection layer:
it edits documents, but it does not decide how documents are stored, composed, or related.

This separation is intentional and fundamental.

## Core principles

### Backend authority
- The backend owns:
  - document storage
  - document identity
  - document relationships (include)
  - media lifecycle
  - indexing and search
- The backend enforces invariants.
- The frontend never "fixes" backend inconsistencies.

### Editor transparency
- The backend only knows about Markdown, which is the ground truth of the document.
- Rendering or editing the document (raw Markdown or ProseMirror) is the responsibility of the frontend.
- The backend does not encode editor-specific semantics.

### Human-readable storage
- Documents are stored as Markdown files.
- Storage must remain:
  - inspectable by humans
  - editable outside the system
  - compatible with version control (e.g. Git)

## Storage model

### Pages
- Each page is stored as a Markdown file.
- Pages are organized in a namespace-to-path mapping (Dokuwiki-style).
- Path ≠ identity forever: pages may be renamed without losing identity.

### Namespaces and index pages

Paths always denote namespaces. A namespace may contain an index page that represents its main content.

When a namespace has an index page, that page is rendered at the namespace path itself (e.g. `regulatory/qms` renders `regulatory/qms/index`). The `/index` suffix is an internal storage detail and is not exposed as part of the public URL.

Initially, a path may correspond to a single page. If that page later needs to grow into a namespace, this is an explicit structural operation that converts the page into a namespace index page. After this conversion, the path is reserved for the namespace and can no longer be used as a standalone page.

Note: it is thus forbidden to create a `regulatory/qms` content page in the `regulatory` namespace after the transformation of `regulatory/qms` into a namespace, to ensure uniqueness of path.

This model ensures stable, intuitive URLs and avoids ambiguity between pages and namespaces.

### Atomicity
- Writes must be atomic.
- Partial writes must not leave corrupted state.

### Version storage (Dokuwiki-inspired)

Full versions are stored in an attic folder, gzipped, one file per version:

```
data/attic/path/to/page/
  1.md.gz
  2.md.gz
  ...
```

A per-page index file records metadata for each version:

```
data/attic/path/to/page/index.json
```

Each index entry contains: version number, timestamp, author, md5 of Markdown content, and published/draft status.

A global append-only changes log tracks all edits across the wiki:

```
data/changes.log
```

Each line records: page path, version number, timestamp, author, edit summary, change type.

Diffs are computed on the fly from stored full versions, never stored separately.

### Draft storage

Each user may hold at most one draft per page. Drafts are stored separately from published versions:

```
data/drafts/{username}/path/to/page.md
```

A draft is a full Markdown file. It is not versioned until published. It is auto-saved every 2 minutes during editing.

### User storage

Users are stored in a hand-editable site-level metadata file:

```
data/meta/users.json
```

Hand-editability is an explicit anti-lockout mechanism, consistent with the configuration file philosophy. The admin UI (v0.6) is a view over this file, not its source of truth.

## Media management

### Media storage
- Media files are stored under `data/content/` alongside pages (same root).
- Media files must have an extension — extension-less files are forbidden.
- Media have stable identifiers.

### Reference tracking
- The backend tracks which pages reference which media files.
- Reference tracking is immediate on every write, not batch-based.
- This map is the foundation for orphan detection.

### Orphan detection
- When a media item becomes unreferenced (no page links to it):
  - the backend detects it immediately
  - the frontend may prompt the user for deletion
- Orphan handling is backend-driven.

### Versioning (future)
- Media versioning is not required initially.
- The backend design must not prevent it.

## Document lifecycle and locking

### Published versions
- A published version is a numbered, immutable snapshot of a page.
- Version numbers are integers starting at 1, incrementing on each publish.
- Each version is associated with an md5 of its Markdown content.
- Future validation plugins may influence versioning numbering — the design must not hardcode assumptions about numbering semantics.

### Draft state
- A draft is user-owned. Only one draft may exist per page per user.
- As long as a draft exists, the document is locked to that user.
- The draft owner sees their draft when viewing the page.
- Other users see the latest published version, or a 404 if no published version exists.

### Editing flow
While editing, the user may:
- **Save and continue**: saves the draft, remains in edit mode. Undo/redo works across saves within the same draft.
- **Save to draft**: saves the draft and returns to view mode, showing the draft.
- **Save and publish**: publishes a new version. If the draft content is identical to the latest published version (md5 match), the draft is discarded with no new version created.
- **Cancel**: exits edit mode, draft is preserved.
- **Cancel and discard draft**: exits edit mode, draft is deleted, lock is released.

Auto-save to draft occurs every 2 minutes during editing.

### View mode actions (draft owner)
- **Publish**: publishes the current draft as a new version.
- **Cancel draft**: discards the draft and releases the lock.

### Locking and admin override
- A document locked by a draft cannot be edited by other users.
- Admins may discard any user's draft and release the lock.
- Discarding another user's draft is a destructive action and must be logged in changes.log.
- Admin override UI is part of v0.6.

## Document structure: include

### Include
- A page may include another page verbatim (or a part of it).
- Include is lightweight content reuse.
- Included content remains owned by the original page.
- Include syntax: `{include path=/path/to/page}`

### Circular include detection
- The backend detects and rejects circular includes at save time (not render time).
- If saving a page would create a direct or transitive include loop, the backend returns an error.
- No partial saves, no silent failures.
- The frontend does not attempt to detect or prevent cycles locally.

### Composition

The wiki exposes a fixed set of editable regions:
- the main page content,
- a sidebar,
- and a footer.

Sidebar and footer content are shared across the site and stored as Markdown documents.

Other regions such as the header (navigation, search) and the action bar (edit, history, tools) are not document-editable and are controlled by the site template and plugins.

### Editing

When editing, non-editable parts of the page (sidebar, footer, included content) are shown greyed out so the layout is intuitive, but clearly marked as non-editable. Included content renders as a greyed-out zone outside the editing flow, with a property panel to control the include path.

### Page Templates

Page templates are special content pages that:
- do not appear in search (only in the site map)
- are visible only to users with write permission
- have scopes: when editing a new page, the nearest template in the folder hierarchy (going recursively up) is used as initial content
- may include active replacement variables (Dokuwiki-style)

## Search and indexing

### Full-text search
- The backend maintains a search index over all pages.
- Search is incremental and near-instant.

### User experience requirements
- Search should propose results as the user types.
- Results should be tolerant to partial matches, typos, and prefixes.

### Consistency
- The index must reflect the current state of documents.
- Index updates are triggered by writes.
- Search indexes published content only, not drafts.

## Admin page

Administration concerns are intentionally kept minimal in early versions.

The admin page is not required for basic usage and is introduced late to avoid premature coupling. Its purpose is to expose system-level configuration and status, not to provide feature-specific controls.

### Configuration management
- Configuration is a core backend concern, not a plugin.
- Configuration is stored in simple file-based formats (e.g. INI or YAML).
- File-based configuration provides an explicit anti-lockout mechanism.
- An eventual admin UI is a view over this configuration, not its source of truth.

### Authentication
- Local user authentication is supported.
- The backend is designed to support multiple authentication backends (e.g. LDAP, OAuth).
- Authentication backends are core components, not plugins.

### Access control
- Access control follows a namespace-based, declarative model inspired by Dokuwiki.
- ACL rules may use regular expressions and variables (e.g. USER).
- This model is considered complete and is not intended to be extended unless necessary.

### Admin capabilities (v0.6)
- View and manage all users.
- Discard any user's draft and release the associated lock (destructive, logged).
- View configuration and system status.

## Plugins

Plugins extend the wiki's document semantics and user interface without owning core system responsibilities.

Plugins are implemented in TypeScript on the frontend side. They may extend Markdown syntax, the ProseMirror document model, editor behavior, HTML rendering, and export styling (e.g. print/PDF CSS). Plugins may also declare limited, scoped state requirements that are persisted by the backend, but they do not define storage formats, persistence rules, or security policies.

Plugins do not manage authentication, access control, configuration, document identity, storage layout, search indexing, or export mechanisms. These remain core backend responsibilities.

Plugins may be loaded at runtime by the frontend as independent bundles. Disabling a plugin must not corrupt documents or make content unreadable. Disabling a plugin may cause loss of specialized semantics, but must never break document loading, editing, or storage as Markdown.

NB: some features that appear as plugins in Dokuwiki (e.g. PDF export, history) are implemented as core capabilities in Gowiki, though plugins may contribute declarative extensions (such as rendering rules or metadata).

## Structured data

### Motivation
- Equivalent to Dokuwiki's Struct plugin.
- Allows pages to expose structured fields.

### Backend ownership
- Schemas, fields, and queries are backend concepts.
- The frontend only renders and edits structured values.

This is out of scope until v0.7 but must be anticipated in the backend design.

## Frontend / backend boundary

### Backend responsibilities
- Validate edits
- Resolve structure
- Maintain consistency
- Detect circular includes
- Track media references
- Manage draft state and document locking
- Provide composed views
- Provide search results (published content only)

### Frontend responsibilities
- Edit Markdown content
- Display composed documents
- Provide editor UX
- Never enforce backend rules locally

## Milestones

### v0.1 ✓ — Single-page correctness
- Edit and persist main page content.
- Render sidebar and footer composition.
- Save and reload pages reliably.
- Render included content as read-only.
- Media upload and management.

### v0.2 ✓ — Site-level consistency
- Reference tracking: backend maintains media→pages map, updated on every write.
- Orphan detection: backend detects unreferenced media immediately, frontend prompts for deletion.
- Circular include detection: backend rejects cycles at save time with an error.

### v0.3 ✓ — Search
- Full-text search, incremental, typo-tolerant.
- Language-specific syntax highlighting in code blocks.

### v0.4 ✓ — Editing robustness
- Correct copy/paste semantics across editable and non-editable regions.
- Document validity enforced after every edit operation.

### v0.5 — History, diff, and draft workflow
- Page locking: a user holding a draft locks the document.
- Draft state: user-owned, auto-saved every 2 minutes, stored under data/drafts/.
- Full editing flow: save and continue, save to draft, save and publish, cancel, cancel and discard draft.
- View mode actions for draft owner: publish, cancel draft.
- Version storage: full copies in data/attic/, gzipped, per-page index.json, global changes.log.
- Page history UI and diff between any two versions.
- Undo/rollback to any published version.

### v0.6 — Admin page
- Configuration UI, authentication backends, ACL management.
- User management.
- Admin override: discard any user's draft, release lock, log the action.

### v0.7 — Structured data
- Structured fields associated with pages.
- Querying and rendering of structured data.