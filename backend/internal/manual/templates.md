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

Global variables remain as `{{NAME}}` in the stored markdown and update dynamically every time the page is viewed. See the full list in [Global Variables](#).

A fallback value can be specified with a colon: `{{AUTHORMAIL:no email provided}}`.

## 1. Database-bound variables

Templates are especially powerful with database-bound pages. When a page is linked to a database table row, **lowercase** template variables like `{{fieldname}}` resolve from the database row's fields:

```markdown
Patient: {{patient_name}}
Visit date: {{visit_date}}
```

These are described in detail in [Database](./database).
