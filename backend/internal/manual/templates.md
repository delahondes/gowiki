# Templates

Templates let you create new pages with pre-filled content.

## 1. Creating a template

A template is a regular page named `_template.md` inside a namespace. For example:

- `content/regulatory/qms/_template.md` — template for pages in `/regulatory/qms/`

## 1. Using a template

When creating a new page, if a `_template.md` exists in the same namespace (or a parent namespace), its content is used as the starting point for the new page.

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
