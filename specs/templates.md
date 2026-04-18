# Page Templates — Specification

## 1. Overview

Page templates provide default content for new pages. A file named `_template.md` placed in a namespace (directory) defines the initial markdown content for any new page created in that namespace or its sub-namespaces.

Alongside the default, a namespace can hold any number of **named templates** (`_template<name>.md` or `_template_<slug>.md`). When more than one template applies to a target page the user gets a picker showing all candidates plus a "Blank page" option.

This follows the DokuWiki convention where `_template.txt` in a namespace applies to all pages created below it, extended with named variants for multi-template workflows.

### Design Goals

- **Convention over configuration** — a `_template*.md` file in a directory is a template for that directory
- **Inheritance with override** — templates cascade: closest wins, per slug
- **Editable as regular pages** — templates are standard markdown files, editable through the wiki UI
- **Runtime variables** — templates use the same `{{VARIABLES}}` as any other page, resolved at render time (no special substitution model)
- **Backend-driven** — the backend resolves which templates apply; the frontend never scans the filesystem

### Non-Goals

- Per-page template assignment via metadata — not planned
- Template inheritance merging (combine parent + child template) — closest wins, no merging
- Template-specific editor mode or preview
- Creation-time variable substitution — all variables are runtime (see §3)

---

## 2. File convention

### Location and naming

Three filename shapes are recognised. The **underscore between `_template` and the suffix is the constraint marker**:

| Filename | Slug | Constrained? |
|---|---|---|
| `_template.md` | `""` (default) | no — always applies |
| `_template1.md`, `_templatefoo.md` | `1`, `foo` | no — just a differentiator/label |
| `_template_sop.md`, `_template_foo_bar.md` | `sop`, `foo_bar` | yes — target page's filename must start with the slug (case-insensitive) |

- Default applies to every page in the namespace tree.
- An unconstrained named variant (`_templateX.md` with no underscore after `_template`) applies to every page in the namespace tree; its slug is just a label in the picker.
- A constrained variant (`_template_X.md`) applies only when the target page's last path segment starts with the slug.

Example:

```
data/content/
  _template.md                   ← default template, any new page uses it
  regulatory/
    qms/
      _template.md               ← overrides the root default for /regulatory/qms/**
      _template_sop.md           ← used only for pages whose name starts with "sop"
      _template_ins.md           ← used only for pages whose name starts with "ins"
      _template1.md              ← always applies (label "1"); shows up in the picker
    meetingminutes/
      _template.md
```

### Resolution order

When creating a new page at path `/regulatory/qms/sop01`:

1. Walk up from the target namespace to root, reading every `_template*.md` at each level.
2. Drop constrained templates whose slug isn't a prefix of the target page's filename.
3. Deduplicate by slug: for each slug, keep the variant found in the closest (deepest) namespace.
4. Result is the full list of applicable templates, default first.

At that point:

- **0 templates** → blank page (same default behavior as today).
- **1 template** → applied silently.
- **2+ templates** → the frontend shows a picker listing each template's label plus a "Blank page" option; the user's click decides.

### Visibility rules

`_template*.md` files are **excluded from the sitemap, orphan detection, and recent-changes listings**. They **remain indexed for full-text search** so admins can find them via the search bar. They are still editable through the standard page API (see §4).

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

### Applicable-templates endpoint (primary)

```
GET /api/templates/for/{path}
```

Returns every template applicable to the target path, after walking up the namespace tree and filtering constrained templates by the filename-prefix rule.

**Response (200):**
```json
{
  "templates": [
    {
      "slug": "",
      "label": "Default",
      "markdown": "# {{PAGE}}\n\nAuthor: {{AUTHORNAME}}\n",
      "template_path": "/regulatory/qms/_template",
      "constrained": false
    },
    {
      "slug": "sop",
      "label": "sop",
      "markdown": "# {{PAGE}}\n\n## Scope\n\n## Procedure\n",
      "template_path": "/regulatory/qms/_template_sop",
      "constrained": true
    }
  ]
}
```

The list is ordered default-first, then named templates alphabetically by slug. When the list is empty the frontend creates a blank page. When the list has exactly one entry it is applied silently. When it has two or more, the frontend shows a picker.

### Legacy single-template endpoint

```
GET /api/template/{path}
```

Backward-compatible: returns `{ markdown, template_path }` for the default template (or the first match if no default applies). Kept for existing integrations; new callers should use `/api/templates/for/{path}`.

**Response (404):** No template applies.

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

Returns every `_template*.md` file in the wiki (default and named variants). Used by the template management UI.

```json
{
  "templates": [
    { "namespace": "/", "path": "/_template" },
    { "namespace": "/regulatory/qms", "path": "/regulatory/qms/_template" },
    { "namespace": "/regulatory/qms", "path": "/regulatory/qms/_template_sop" },
    { "namespace": "/regulatory/qms", "path": "/regulatory/qms/_template_ins" }
  ]
}
```

---

## 5. Frontend integration

### New page creation flow

1. User navigates to a path that doesn't exist yet.
2. Frontend calls `GET /api/templates/for/{path}`.
3. Based on the returned list:
   - 0 entries → blank page with the default empty content
   - 1 entry → apply silently
   - 2+ entries → show a picker modal; each row is `label` + `template_path`, plus a trailing "Blank page" row
4. The chosen markdown (or blank) becomes the initial editor content and the editor opens.

The template fetch is a single additional API call during page creation. The picker is modal and resolves synchronously before the editor initializes, so there is no flash of incorrect content.

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

2. **Closest template wins per slug, no merging** — For each slug, the closest namespace wins. There is no mechanism to "extend" a template. If you need shared sections, use includes.

3. **Underscore = constraint** — `_templateX.md` means "unconstrained alternative named X"; `_template_X.md` means "applies only to pages whose filename starts with X". This makes the naming self-documenting: adding an underscore is the act of adding a filter.

4. **Picker, not auto-pick** — When several templates match, the user chooses. The system never picks silently between alternatives (the auto-pick behaviour only holds when exactly one template matches, i.e. today's behaviour).

5. **Templates are indexed for search but excluded from sitemap/orphan/recent-changes** — Admins should be able to find a template via full-text search; it just shouldn't appear as navigable content in the site's structural views.

6. **Backend resolves, no substitution** — The frontend calls one endpoint and gets back a list. The backend walks up directories, applies the slug-prefix filter, and returns matching templates as-is.

7. **Standard page API for editing** — Templates use the existing `GET/PUT /api/pages/` endpoints with `_template*` paths. No new editor infrastructure is needed.

8. **`_template*.md` naming** — The underscore prefix signals "special/hidden file," consistent with conventions in many systems. The `.md` extension is required by the content storage convention.
