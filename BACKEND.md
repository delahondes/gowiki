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
- document relationships (include / inherit)
- media lifecycle
- indexing and search
- The backend enforces invariants.
- The frontend never “fixes” backend inconsistencies.

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

Note: it is thus forbidden to create a `regulatory/qms` content page in the `regulatory` namespace after the transformation to ensure unicity of path.

This model ensures stable, intuitive URLs and avoids ambiguity between pages and namespaces.

### Atomicity
- Writes must be atomic.
- Partial writes must not leave corrupted state.

### History
- The backend must be compatible with:
- diffing
- version history
- rollback

## Media management

### Media storage
- Media files are stored separately from pages.
- Media have stable identifiers.

### Reference tracking
- The backend tracks which pages reference which media.
- Reference tracking is immediate, not batch-based.

### Orphan detection
- When a media item becomes unreferenced:
- the backend detects it immediately
- the frontend may prompt the user for deletion
- Orphan handling is backend-driven.

### Versioning (future)
- Media versioning is not required initially.
- The backend design must not prevent it.

## Document structure: include

### Include
- A page may include another page verbatim (or a part of it).
- Include is lightweight content reuse.
- Included content remains owned by the original page.

### Composition

The wiki exposes a fixed set of editable regions:
- the main page content,
- a sidebar,
- and a footer.

Sidebar and footer content are shared across the site and stored as Markdown documents.
In the future we may decide to allow variation in certain subscopes.

Other regions such as the header (navigation, search) and the action bar (edit, history, tools) are not document-editable and are controlled by the site template and plugins.

### Editing

This is the same model as dokuwiki, only the content is editable and sidebar and footer are special content pages.
When editing, we want to show the non-editable parts of the page while editing, so it is intuitive for users, but clearly marked as non-editable (greyed out). 
We want to support include so it would render in the same way, a greyed out zone, out of the editing flow (but with a property panel to control the include).

### Page Templates

Users will want to have predefine content while editing a page. We will borrow Dokuwiki template concept: page templates are special content pages that do not show with search (only the site map will show them) and are visible only to users with write permission. They have scopes: when editing a new page the nearest page template available in the file arborescence (going recursively up in the folders) will be used as initial content for the page.

Like dokiwiki they may include active replacement variables.

## Search and indexing

### Full-text search
- The backend maintains a search index over all pages.
- Search is incremental and near-instant.

### User experience requirements
- Search should propose results as the user types.
- Results should be tolerant to:
- partial matches
- typos
- prefixes

### Consistency
- The index must reflect the current state of documents.
- Index updates are triggered by writes.

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


## Plugins

Plugins extend the wiki’s document semantics and user interface without owning core system responsibilities.

Plugins are implemented in TypeScript on the frontend side. They may extend Markdown syntax, the ProseMirror document model, editor behavior, HTML rendering, and export styling (e.g. print/PDF CSS). Plugins may also declare limited, scoped state requirements that are persisted by the backend, but they do not define storage formats, persistence rules, or security policies.

Plugins do not manage authentication, access control, configuration, document identity, storage layout, search indexing, or export mechanisms. These remain core backend responsibilities.

Plugins may be loaded at runtime by the frontend as independent bundles. Disabling a plugin must not corrupt documents or make content unreadable. Disabling a plugin may cause loss of specialized semantics, but must never break document loading, editing, or storage as Markdown.

NB: overall we are inspired by Dokuwiki but some features that appear as plugins in Dokuwiki (e.g. PDF export, history modifications) are implemented as core capabilities, though some plugins may contribute declarative extensions (such as rendering rules or metadata).

## Structured data

### Motivation
- Equivalent to Dokuwiki’s Struct plugin.
- Allows pages to expose structured fields.

### Backend ownership
- Schemas, fields, and queries are backend concepts.
- The frontend only renders and edits structured values.

This is out of scope for the first iteration but must be anticipated.

## Frontend / backend boundary

### Backend responsibilities
- Validate edits
- Resolve structure
- Maintain consistency
- Provide composed views
- Provide search results

### Frontend responsibilities
- Edit Markdown content
- Display composed documents
- Provide editor UX
- Never enforce backend rules locally




## Milestones

### v0.1 — Single-page correctness
- Edit and persist main page content.
- Render sidebar and footer composition.
- Save and reload pages reliably.
- Render included content as read-only.
- No media management, no history, no search.

### v0.2 — Site-level consistency
- Navigate between pages.
- Media upload, reference tracking, and orphan detection.
- Maintain consistency across pages and shared regions.

### v0.3 - Search
- Implement searching.

### v0.4 — Editing robustness
- Correct copy/paste semantics across editable and non-editable regions.
- Preserve editor equivalence guarantees under complex edits.

### v0.5 — History and diff
- Page history and diffing.
- Undo/rollback support based on persisted content.

### v0.6 - Admin page
- Add admin page

### v0.7 — Structured data
- Structured fields associated with pages.
- Querying and rendering of structured data.