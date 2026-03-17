# Templates

Templates let you create new pages with pre-filled content.

## 1. Creating a template

A template is a regular page named `_template.md` inside a namespace. For example:

- `content/regulatory/qms/_template.md` — template for pages in `/regulatory/qms/`

## 1. Using a template

When creating a new page, if a `_template.md` exists in the same namespace (or a parent namespace), its content is used as the starting point for the new page.

## 1. Template variables

Templates can include variables that are replaced when the page is created:

```
{{page_name}} — the new page's name
{{namespace}} — the namespace path
{{date}} — current date (YYYY-MM-DD)
{{author}} — the creating user's login
```

Variables use double-brace syntax: `{{variable_name}}`. A fallback value can be specified: `{{variable_name:default value}}`.
