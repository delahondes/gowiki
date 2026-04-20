# Database (User Guide)

The database plugin lets you embed structured data in wiki pages — query tables, insert rows, and bind pages to database records.

{blockquote class=important}
> Before using any database directive on a wiki page, an administrator must first create the table in [Admin > Database](./admin-database). The directives reference tables by name — if the table doesn't exist, the directive will show an error.
> For a table to be fully functional, you need at minimum:
> - A `{database-query}` node on a page — to display the data
> - A `{database-newrow}` node on a page — to let users add rows
> A page template for row-bound pages is optional but recommended for tables with a **Page folder** configured.

## 1. Querying data

Display a table of rows using `{database-query}`:

```markdown
{database-query table=status}
```

This renders an interactive table with all rows, sortable and filterable.

![Creating a database-query node — use the toolbar button and type the table name](./screenshots/26.png)

The rendered result shows a fully interactive table:

![Status table with icons and colors](./screenshots/28.png)

Optional parameters:

```markdown
{database-query table=server fields="category, provider, description" filter="status=active" sort=name order=asc limit=50}
```

| Parameter | Description |
| --- | --- |
| table | Table name (required) |
| fields | Comma-separated list of fields to display (default: all) |
| filter | Filter expression (e.g. `status=active`) — see below |
| sort | Sort field |
| order | `asc` or `desc` |
| limit | Maximum rows to display |

Field names prefixed with `%` are displayed as page links: `fields="%title%, description"` makes the title column a clickable link to the row's bound page.

### Filter syntax

A filter is one or more conditions joined by `&` (logical AND). Each
condition has the form `field<op>value`.

Operators: `=`, `!=`, `<>` (alias for `!=`), `<`, `>`, `<=`, `>=`, and `~`
(case-insensitive pattern match; `%` is the wildcard, and a value without
any `%` is wrapped as `%value%` for substring match).

```markdown
filter="status=active"
filter="priority>=2&archived!=true"
filter="name~alice"
```

#### Joining through a lookup or tag field

A filter may reach into a related table via dotted notation
`parent_field.child_field<op>value`. The parent must be a `lookup` or `tag`
field pointing at another table; the child is resolved in that table by
name or label.

```markdown
filter="assignee.email~@gmt.bio"
filter="category.color=#ff0000"
filter="owner.page_path=/team/alice"
```

One level of indirection is supported (no chained `a.b.c`). `multi_enum`
children are not joinable. Unresolvable filters (typos, wrong types) are
silently dropped rather than raising an error.

## 1. Inserting rows

Display an insert form using `{database-newrow}`:

```markdown
{database-newrow table=status}
```

This renders a form with fields matching the table definition:

![Creating a database-newrow node — use the toolbar button](./screenshots/27.png)

Here's an example with a query table and a new row form together, for managing customer complaints:

![Customer complaints page with query and new row form in edit mode](./screenshots/29.png)

After filling in the form and clicking **Create**, the row appears in the query table above:

![Filled form ready to submit, with existing row in the query table](./screenshots/30.png)

For page-bound tables, submitting the form also creates a new wiki page from the table's template.

## 1. Page-bound rows

A page can be linked to a database row using `{database-row}`:

```markdown
{database-row table=customer_complaints}

| Field | Value |
| --- | --- |
| id | 1 |
| customer | UGC Corp |
| severity | Minor |
| status | 1 |
```

The field/value table below the directive syncs the row's data with the database. Here's what a default row-bound page looks like:

![Default row-bound page showing the data table](./screenshots/31.png)

Editing the page updates the database row; changing the row via the API or inline edit updates the page.

## 1. Customizing row-bound pages with templates

Instead of the default page layout, you can create a **page template** that uses template variables to display the data in a custom format.

The template uses `{{field_name}}` variables (lowercase) that resolve from the row's fields, and `{{GLOBALVAR}}` variables (uppercase) for page metadata. The template includes a `{database-row}` placeholder (with no table name) that gets replaced with the actual data block:

![Template with variables and database-row placeholder — note the CREATIONDATE global variable](./screenshots/32.png)

The `{database-row}` placeholder must be on its own line with a blank line before it. Leave the Table property blank — it will be filled automatically:

![Template with blank database-row placeholder](./screenshots/33.png)

When a row is created via the `{database-newrow}` form, the system generates a page from this template with all variables resolved:

![Generated page from template — id, customer name, and date are resolved](./screenshots/34.png)

## 1. Template variables

Inside a page bound to a database row, lowercase `{{field_name}}` variables resolve to the row's field values:

```markdown
## Customer complaint {{id}}

Customer: {{customer}}
Severity: {{severity}}
```

The `{{id}}` variable resolves to the database row's auto-incremented ID. All field names from the Field/Value table are available.

These are distinct from **global variables** which use ALL_CAPS names (e.g. `{{AUTHOR}}`, `{{CREATIONDATE}}`) and are available on every page regardless of database binding. See [Templates](./templates) for details.

## 1. Toolbar buttons

The editor toolbar provides buttons for inserting database directives:
- **Query** — insert a `{database-query}` block
- **New Row** — insert a `{database-newrow}` form
- **Row** — insert a `{database-row}` binding

When inserting any of these, the property panel opens automatically with focus on the table name field.
