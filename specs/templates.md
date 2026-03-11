# Page Templates — Specification

## 1. Overview

Page templates provide default content for new pages. A file named `_template.md` placed in a namespace (directory) defines the initial markdown content for any new page created in that namespace or its sub-namespaces.

This follows the DokuWiki convention where `_template.txt` in a namespace applies to all pages created below it.

### Design Goals

- **Convention over configuration** — a `_template.md` file in a directory is the template for that directory
- **Inheritance with override** — templates cascade: closest `_template.md` wins
- **Editable as regular pages** — templates are standard markdown files, editable through the wiki UI
- **Runtime variables** — templates use the same `{{VARIABLES}}` as any other page, resolved at render time (no special substitution model)
- **Backend-driven** — the backend resolves which template applies; the frontend never searches for templates

### Non-Goals

- Template selection UI (pick from a list of templates) — may come later
- Per-page template assignment (metadata-based) — may come later
- Template inheritance merging (combine parent + child template) — closest wins, no merging
- Template-specific editor mode or preview
- Creation-time variable substitution — all variables are runtime (see §3)

---

## 2. File convention

### Location and naming

```
data/content/
  _template.md              ← root template (applies to all pages without a closer template)
  docs/
    _template.md            ← template for /docs/* pages
    setup/
      _template.md          ← template for /docs/setup/* pages
      install.md
    guide.md
  projects/
    project-a/
      index.md
      notes.md
```

### Resolution order

When creating a new page at path `/docs/setup/install`:

1. Check `data/content/docs/setup/_template.md` — if exists, use it
2. Check `data/content/docs/_template.md` — if exists, use it
3. Check `data/content/_template.md` — if exists, use it
4. No template found — use empty page (current behavior)

**Closest wins.** The first `_template.md` found walking up from the target namespace is used. No merging or chaining.

### Visibility rules

`_template.md` files are **hidden from normal wiki operation**:

- Not listed in sitemap
- Not listed in search results
- Not listed in recent changes
- Not listed in orphan detection
- Not navigable as regular pages (visiting `/_template` returns 404)
- Backlinks from templates are not tracked

Templates are only accessible through a dedicated management interface (see §6).

---

## 3. Variables — runtime only

Templates use the **same global variable system** as any other page. There is no creation-time substitution. The template markdown is copied as-is into the new page, and all `{{VARIABLES}}` remain in the stored markdown, resolved at render time.

This means a template like:

```markdown
# {{PAGE}}

Author: {{AUTHORNAME}}
Created: {{CREATIONDATE}}
Last updated: {{VERSIONDATE}} by {{LASTAUTHORNAME}} (v{{VERSION}})
```

Is stored verbatim in the new page's `.md` file. Every variable resolves at render time:

- `{{PAGE}}` → always shows the current page name (correct even after a rename)
- `{{AUTHORNAME}}` → always shows the page creator's current display name
- `{{CREATIONDATE}}` → page creation date (stable, derived from first version timestamp)
- `{{VERSIONDATE}}` → last edit date (updates on every save)
- `{{LASTAUTHORNAME}}` → last editor (updates on every save)

### New global variable required

`{{CREATIONDATE}}` is added to the global variable catalog (see `specs/global_variables.md`). It resolves to the page's creation date (`YYYY-MM-DD`), derived from the timestamp of version 1. This requires `created_at` to be stored in page metadata alongside the existing `created_by`.

### Why not one-shot substitution?

One model is simpler than two. With runtime-only variables:

- No separate "template variable" concept — `{{PAGE}}` works the same in a template, in a footer, in any page
- Values stay correct after renames, moves, or author profile changes
- The backend template endpoint just returns raw markdown — no substitution logic
- Template editing is pure WYSIWYG: what you write is exactly what gets stored

---

## 4. Backend API

### Template resolution endpoint

```
GET /api/template/{path}
```

Returns the raw template content for creating a new page at `{path}`. No variable substitution — just the file lookup.

**Response (200):**
```json
{
  "markdown": "# {{PAGE}}\n\nAuthor: {{AUTHORNAME}}\nCreated: {{CREATIONDATE}}\n",
  "template_path": "/docs/_template"
}
```

- `markdown`: the raw template content (variables not substituted)
- `template_path`: which `_template.md` was used (for display/debugging)

**Response (404):**
No template found — the frontend uses its default empty content.

The backend performs:
1. Walk up from the target page's namespace looking for `_template.md`
2. Read the template content
3. Return it as-is

### Template CRUD

Templates are edited through the existing page API with a reserved path prefix:

```
GET  /api/pages/_template              ← root template
GET  /api/pages/docs/_template         ← docs namespace template
PUT  /api/pages/docs/_template         ← save docs namespace template
```

The `_template` path segment maps to `_template.md` on disk. The backend allows reading and writing these files through the standard page API, but excludes them from sitemap, search indexing, changelog, and backlink tracking.

### Template listing

```
GET /api/templates
```

Returns all templates in the wiki:

```json
{
  "templates": [
    { "path": "/", "has_template": true },
    { "path": "/docs", "has_template": true },
    { "path": "/docs/setup", "has_template": false },
    { "path": "/projects", "has_template": true }
  ]
}
```

This is used by the template management UI.

---

## 5. Frontend integration

### New page creation flow

Current flow:
1. User clicks "New page" → enters path → navigates to `/{path}?action=create`
2. Page loads with `isNewPage = true`, `currentMarkdown = defaultMarkdown`
3. Auto-enters edit mode

New flow:
1. User clicks "New page" → enters path → navigates to `/{path}?action=create`
2. Frontend calls `GET /api/template/{path}`
3. If 200: use returned markdown as initial content
4. If 404: use empty default content (current behavior)
5. Auto-enters edit mode with the template content

The template fetch is a single additional API call during page creation. It happens before the editor is initialized, so there is no flash of empty content.

### Template editing

Templates are accessible from the admin page or via a "Manage templates" action. The editing experience is identical to regular page editing — the same ProseMirror editor, the same raw/visual toggle. The only difference is that the page path is `/_template` or `/docs/_template`.

---

## 6. Edge cases

### Template for namespace index pages

When creating a namespace index page (e.g., `/projects/` which maps to `projects/index.md`), the template resolution walks up from `projects/`, checking:
1. `data/content/projects/_template.md`
2. `data/content/_template.md`

The template in the same directory applies to the index page of that directory.

### Template self-reference

A `_template.md` file is never used as a template for itself. Editing a template is a direct edit, not a "create from template" operation.

### Deleting a template

Deleting `_template.md` simply removes the template — pages in that namespace will fall back to the parent template (or no template). Existing pages are unaffected.

### Page already exists

Templates only apply to **new** pages. Re-editing an existing page never re-applies the template.

---

## 7. Decisions

1. **Runtime variables, no creation-time substitution** — All `{{VARIABLES}}` in templates work exactly like in any other page: stored as-is, resolved at render time. This gives one consistent model instead of two. Values stay correct after renames and moves. The creation date use case is covered by `{{CREATIONDATE}}`, a new global variable resolved from page metadata.

2. **Closest template wins, no merging** — A `_template.md` in a child namespace completely overrides the parent template. There is no mechanism to "extend" a parent template. This keeps the mental model simple: one template file = one template. If you need shared sections, use includes.

3. **Hidden from normal wiki operation** — Templates are infrastructure, not content. They don't appear in search, sitemap, or recent changes. This prevents clutter and confusion. They're editable through the standard page editor but accessed through a separate management path.

4. **Backend resolves, no substitution** — The frontend never searches for `_template.md` files. It calls one endpoint and gets back raw markdown. The backend just walks up directories looking for the file — no variable processing needed.

5. **Standard page API for editing** — Templates use the existing `GET/PUT /api/pages/` endpoints with a `_template` path. No new editor infrastructure is needed. The `_` prefix convention ensures no collision with user pages (underscores in page names are valid but `_template` is explicitly reserved).

6. **`_template.md` naming** — The underscore prefix signals "special/hidden file," consistent with conventions in many systems. The `.md` extension is required by the content storage convention.
