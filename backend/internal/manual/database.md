# Database (User Guide)

The database plugin lets you embed structured data in wiki pages — query tables, insert rows, and bind pages to database records.

## 1. Querying data

Display a table of rows using `{database-query}`:

```markdown
{database-query table=server}
```

This renders an interactive table with all rows, sortable and filterable.

Optional parameters:

```markdown
{database-query table=server fields="category, provider, description" filter="status=active" sort=name order=asc limit=50}
```

| Parameter | Description |
| --- | --- |
| table | Table name (required) |
| fields | Comma-separated list of fields to display (default: all) |
| filter | Filter expression (e.g. `status=active`) |
| sort | Sort field |
| order | `asc` or `desc` |
| limit | Maximum rows to display |

Field names prefixed with `%` are displayed as page links: `fields="%title%, description"` makes the title column a clickable link to the row's bound page.

## 1. Inserting rows

Display an insert form using `{database-newrow}`:

```markdown
{database-newrow table=server}
```

This renders a form with fields matching the table definition. For page-bound tables, submitting the form also creates a new wiki page from the table's template.

## 1. Page-bound rows

A page can be linked to a database row using `{database-row}`:

```markdown
{database-row table=interview}

| Field | Value |
| --- | --- |
| name | Alice Martin |
| date | 2024-06-15 |
| status | completed |
```

The field/value table below the directive syncs the row's data with the database. Editing the page updates the database row; changing the row via the API updates the page.

## 1. Template variables

Inside a page bound to a database row, lowercase `{{field_name}}` variables resolve to the row's field values:

```markdown
{database-row table=interview}

| Field | Value |
| --- | --- |
| name | Alice Martin |
| position | QA Engineer |

## Interview: {{name}}

Position: {{position}}
```

The `{{name}}` and `{{position}}` variables display "Alice Martin" and "QA Engineer" respectively. They update automatically when the row data changes.

These are distinct from **global variables** which use ALL_CAPS names (e.g. `{{AUTHOR}}`, `{{DATE}}`) and are available on every page regardless of database binding. See [Templates](./templates) for details.

## 1. Creating page-bound rows

When a table has a **page folder** configured (set by admin), the `{database-newrow}` form creates both a database row and a wiki page:

1. The admin defines a page folder pattern (e.g. `/regulatory/interviews/@name-@year`)
2. The admin sets a page template (a wiki page with `{{field_name}}` variables)
3. A user fills in the `{database-newrow}` form and submits
4. The system creates the database row, generates the page path from the pattern, and creates the page from the template with field values filled in

## 1. Toolbar buttons

The editor toolbar provides buttons for inserting database directives:
- **Query** — insert a `{database-query}` block
- **New Row** — insert a `{database-newrow}` form
- **Row** — insert a `{database-row}` binding
