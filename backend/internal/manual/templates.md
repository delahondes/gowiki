# Templates

Templates let you create new pages with pre-filled content. A namespace can have a single default template, or several named alternatives — when more than one template applies to a new page the wiki shows a picker so you can choose (or start blank).

## 1. Creating a template

A template is a regular page whose filename starts with `_template`, stored inside a namespace directory.

Three naming shapes are supported:

| Filename | Behaviour |
| --- | --- |
| `_template.md` | **Default** template — applied to every new page in the namespace (or any sub-namespace) that has no closer default. |
| `_template1.md`, `_templatefoo.md` | **Unconstrained alternative** — always applies. The suffix after `_template` is just a label shown in the picker ("1", "foo"). |
| `_template_sop.md`, `_template_meeting.md` | **Constrained alternative** — applies only to pages whose name starts with the slug (case-insensitive). `_template_sop.md` matches `sop01`, `sopnew`, but not `ins01`. The underscore after `_template` is the constraint marker. |

The slug after `_template_` can contain further underscores (e.g. `_template_foo_bar.md` → constrained to pages starting with `foo_bar`).

Examples:

- `content/regulatory/qms/_template.md` — default template for `/regulatory/qms/` and everything under it
- `content/regulatory/qms/_template_sop.md` — appears only for pages starting with `sop`
- `content/regulatory/qms/_template_ins.md` — appears only for pages starting with `ins`
- `content/regulatory/qms/_templatemeeting.md` — always appears in the picker as "meeting"

## 1. Using templates

When you navigate to a page that doesn't exist yet, the wiki collects every `_template*.md` that applies to that path (walking up the namespace tree) and filters the constrained ones by the filename-prefix rule.

- **One match** → applied silently (same as before).
- **Several matches** → a picker modal opens with one row per template, plus a **"Blank page"** row. Click a row to start the editor with that content.
- **No match** → you get a blank page, as before.

Resolution walks up the tree and takes the **closest** version of each slug. A default defined in `/foo/bar/_template.md` overrides the root `/_template.md` only for pages under `/foo/bar/`.

Templates are hidden from the sitemap, orphan detection, and recent changes. They **are** indexed by full-text search so you can still find them via the search bar.

## 1. Variables in templates

Templates can include **global variables** that are resolved at render time (not at creation time). These use the `{{NAME}}` syntax with ALL_CAPS names:

```markdown
Page: {{PAGE}}
Author: {{AUTHOR}}
Created: {{DATE}}
```

Global variables remain as `{{NAME}}` in the stored markdown and update dynamically every time the page is viewed.

A fallback value can be specified with a colon: `{{AUTHORMAIL:no email provided}}`.

### Global variable reference

**Page variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{ID}}` | Full page path | `/docs/setup/install` |
| `{{PATH}}` | Parent namespace path | `/docs/setup` |
| `{{PAGE}}` | Page name (last segment) | `install` |
| `{{TITLE}}` | Page title (from first heading) | `Installation Guide` |

**Link variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{SERVER}}` | Server hostname | `wiki.example.com` |
| `{{EXTID}}` | Full external URL to the page | `https://wiki.example.com/docs/install` |
| `{{EXTPATH}}` | Full external URL to the namespace | `https://wiki.example.com/docs/` |

**Version variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{VERSION}}` | Page version number | `42` |
| `{{VERSIONDATE}}` | Last modified date (YYYY-MM-DD) | `2026-03-18` |
| `{{VERSIONTAG}}` | Reviewflow version tag | `2.1` |
| `{{YEAR}}` | Year of last modification | `2026` |
| `{{MONTH}}` | Month (zero-padded) | `03` |
| `{{SMONTH}}` | Month (short, no padding) | `3` |
| `{{DAY}}` | Day (zero-padded) | `18` |
| `{{SDAY}}` | Day (short, no padding) | `18` |
| `{{CREATIONDATE}}` | Page creation date (YYYY-MM-DD) | `2026-01-15` |

**Author variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{AUTHOR}}` | Page creator (login) | `alice` |
| `{{AUTHORNAME}}` | Page creator (display name) | `Alice Martin` |
| `{{AUTHORMAIL}}` | Page creator (email) | `alice@example.com` |
| `{{LASTAUTHOR}}` | Last editor (login) | `bob` |
| `{{LASTAUTHORNAME}}` | Last editor (display name) | `Bob Wilson` |
| `{{LASTAUTHORMAIL}}` | Last editor (email) | `bob@example.com` |

**Wiki variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{WIKI}}` | Site title | `Acme Wiki` |
| `{{WIKIVERSION}}` | Gowiki software version | `0.9.5` |

## 1. Database-bound variables

Templates are especially powerful with database-bound pages. When a page is linked to a database table row, **lowercase** template variables like `{{fieldname}}` resolve from the database row's fields:

```markdown
Patient: {{patient_name}}
Visit date: {{visit_date}}
```

These are described in detail in [Database](./database).
