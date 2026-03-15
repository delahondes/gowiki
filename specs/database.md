# Database Plugin — Specification

## 1. Overview

The database plugin provides structured data tables backed by PostgreSQL. Tables are defined via an admin UI, and data is accessed through three markdown directives:

- `{database-query table=name}` — renders a filterable, sortable table view of rows
- `{database-newrow table=name}` — renders an insert form
- `{database-row table=name}` — binds the current page to a row (followed by a field/value table)

Tables can be scoped to page paths via `scope_regexp` and optionally bound to a page folder (`page_folder`), in which case creating a row auto-generates a wiki page from a template (`page_template_path`).

### Template variables

Pages can embed field values using `{{field_name}}` syntax. Global variables use ALL_CAPS names: `{{AUTHOR}}`, `{{TITLE}}`, `{{CREATED}}`, `{{VERSIONDATE}}`, `{{VERSIONTAG}}`, `{{YEAR}}`.

## 2. Table definition

```go
type TableDef struct {
    ID                int
    Name              string    // unique identifier
    Label             string    // display label
    ScopeRegexp       string    // page path pattern for query/row binding
    PageFolder        string    // if set, new rows create pages here
    IndexField        string    // field used as page name when creating pages
    DefaultSortField  string
    DefaultSortOrder  string    // "asc" or "desc"
    PageTemplatePath  string    // template page for auto-created pages
}
```

## 3. Field definition

```go
type FieldDef struct {
    ID           int
    TableID      int
    Name         string    // column name (unique per table)
    Label        string    // display label
    Type         string    // one of the column types below
    Required     bool
    DefaultValue string
    DisplayOrder int
    Placeholder  string
    ForeignKey   string    // for tag type: name of the referenced table
    EnumValues   []string  // for enum/multi_enum: allowed values
}
```

## 4. Column types

### 4.1 Text

Basic string field. Rendered as a single-line text input.

| Property | Value |
|---|---|
| SQL type | `TEXT` |
| Input | `<input type="text">` |
| Display | Plain text |

### 4.2 Integer

Whole number field.

| Property | Value |
|---|---|
| SQL type | `BIGINT` |
| Input | `<input type="number">` |
| Display | Numeric text |

### 4.3 Float

Floating-point number field.

| Property | Value |
|---|---|
| SQL type | `DOUBLE PRECISION` |
| Input | `<input type="number" step="any">` |
| Display | Numeric text |

### 4.4 Boolean

True/false field.

| Property | Value |
|---|---|
| SQL type | `BOOLEAN` |
| Input | `<select>` with Yes/No options |
| Display | "Yes" or "No" |

### 4.5 Date

Date without time component.

| Property | Value |
|---|---|
| SQL type | `DATE` |
| Input | `<input type="date">` |
| Display | ISO date string |

### 4.6 Datetime

Date with time component.

| Property | Value |
|---|---|
| SQL type | `TIMESTAMPTZ` |
| Input | `<input type="datetime-local">` |
| Display | ISO datetime string |

### 4.7 Page Link

Reference to a wiki page. Stored as the page path string.

| Property | Value |
|---|---|
| SQL type | `TEXT` |
| Input | `<input type="text">` |
| Display | Clickable link to the referenced page |

### 4.8 Enum

Single-select from a predefined list of values. Values are defined per-field in `enum_values`.

| Property | Value |
|---|---|
| SQL type | `TEXT` |
| Input | `<select>` dropdown |
| Display | Plain text |

### 4.9 Multi-enum

Multi-select from a predefined list. Uses a junction table rather than a column in the data table.

| Property | Value |
|---|---|
| SQL type | Junction table |
| Input | Multi-select `<select>` |
| Display | Comma-separated values |

### 4.10 Auto-increment

System-generated incrementing ID. Read-only — the user cannot set or edit this field.

| Property | Value |
|---|---|
| SQL type | `BIGINT` |
| Input | None (read-only) |
| Display | Numeric text |

### 4.11 Image

Stores a path to an image attachment. Supports browse via the media manager.

| Property | Value |
|---|---|
| SQL type | `TEXT` |
| Input | Text input + "Browse" button (opens media manager) |
| Display | Inline `<img>` thumbnail |

### 4.12 Color

Hex color value with visual picker.

| Property | Value |
|---|---|
| SQL type | `TEXT` |
| Input | Color input with preset pastel swatches + hex text input |
| Display | Colored swatch circle |

Preset swatches: gray (#adb5bd), red (#ffa8a8), pink (#fcc2d7), purple (#eebefa), indigo (#bac8ff), blue (#a5d8ff), cyan (#99e9f2), teal (#96f2d7), green (#b2f2bb), lime (#d8f5a2), yellow (#ffec99), orange (#ffd8a8).

### 4.13 Tag

Foreign key reference to another table, displayed as a colored badge with optional icon. The referenced table is expected to have `label`, `icon`, and `color` fields.

| Property | Value |
|---|---|
| SQL type | `BIGINT` (foreign key) |
| Config | `foreign_key` = name of the referenced table |
| Input | `<select>` dropdown populated from the referenced table's rows |
| Display | Colored badge with icon and label (`.db-tag-badge`) |

The tag type fetches all rows from the referenced table (cached for 30 seconds) and renders each value as a badge:
- Background color from the referenced row's `color` field
- Text color auto-computed (light/dark) for contrast
- Optional icon from the referenced row's `icon` field (rendered as `<img>`)
- Label from the referenced row's `label` field

This is used for status fields, categories, or any enumeration that needs rich visual rendering (icon + color + label) managed in a separate table.

**Example:** A "test" table with a "status" field of type `tag` referencing a "status" table. The status table has rows like:
- id=1, label="Open", icon="/icons/circle-outline.svg", color="#b2f2bb"
- id=2, label="Closed", icon="/icons/check-circle.svg", color="#adb5bd"

### 4.14 Lookup

A general foreign key to another table. Unlike `tag`, lookup displays the referenced row's value as plain text, without badge/icon/color styling. The display value is the first text field from the referenced row (heuristic — no explicit display field configuration needed).

| Property | Value |
|---|---|
| SQL type | `BIGINT` (foreign key) |
| Config | `foreign_key` = name of the referenced table |
| Input | `<select>` dropdown populated from the referenced table's rows |
| Display | Plain text (first text field value from the referenced row) |

Self-referential lookups (table references itself) are supported — used for linked change controls.

The lookup type fetches all rows from the referenced table (cached for 30 seconds) and uses the first string-valued field as the display label. Falls back to the row ID if no text field is found.

### 4.15 User

A field that references a Gowiki user account. Stored as the username string.

| Property | Value |
|---|---|
| SQL type | `TEXT` |
| Input | `<select>` dropdown populated from `/api/users/list` |
| Display | User's display name (falls back to username) |

The user list is fetched from the `/api/users/list` endpoint (cached for 30 seconds). Disabled users are excluded from the list.

## 5. DokuWiki struct type mapping

| DokuWiki type | Gowiki type | Notes |
|---|---|---|
| `Text` | `text` | Direct mapping |
| `LongText` | `text` | Textarea variant; use text |
| `Decimal` | `float` or `integer` | Depending on usage (`trimzeros=true` → integer) |
| `DateTime` | `datetime` | Direct mapping |
| `Date` | `date` | Direct mapping |
| `Checkbox` | `boolean` or `multi_enum` | Single → boolean, multi (`ismulti=true`) → multi_enum |
| `Color` | `color` | Direct mapping |
| `Status` | `tag` | Foreign key with icon/color/label |
| `User` | `user` | Direct mapping |
| `Lookup` | `lookup` | Direct mapping |
| `Page` | `page_link` | Direct mapping |
| `Media` | `image` | Direct mapping |

## 6. API

### User APIs

| Method | Path | Description |
|---|---|---|
| GET | `/api/database/{table}/schema` | Get table schema with fields |
| GET | `/api/database/{table}/rows` | Query rows (supports filter, sort, order, limit, offset) |
| GET | `/api/database/{table}/rows/{id}` | Get single row |
| GET | `/api/database/{table}/page/*` | Get row by page path |
| POST | `/api/database/{table}/rows` | Insert row |
| PUT | `/api/database/{table}/rows/{id}` | Update row fields |
| DELETE | `/api/database/{table}/rows/{id}` | Delete row |
| PUT | `/api/database/{table}/page/*` | Upsert row by page path |
| GET | `/api/database/{table}/export/csv` | CSV export |

### Admin APIs

| Method | Path | Description |
|---|---|---|
| GET | `/api/admin/database/status` | Connection status |
| POST | `/api/admin/database/test` | Test DSN connection |
| POST | `/api/admin/database/connect` | Establish connection |
| GET | `/api/admin/database/tables` | List all tables |
| POST | `/api/admin/database/tables` | Create table |
| GET | `/api/admin/database/tables/{id}` | Get table with fields |
| PUT | `/api/admin/database/tables/{id}` | Update table metadata |
| DELETE | `/api/admin/database/tables/{id}` | Delete table |
| POST | `/api/admin/database/tables/{id}/fields` | Add field |
| PUT | `/api/admin/database/tables/{id}/fields/{fid}` | Update field |
| DELETE | `/api/admin/database/tables/{id}/fields/{fid}` | Archive field |
| GET | `/api/admin/database/tables/{id}/history` | Schema change history |

## 7. Markdown syntax

### Query

```
{database-query table=test filter="status=1" sort=date order=desc limit=20}
```

Attributes:

| Attribute | Description | Default |
|---|---|---|
| `table` | Table name (required) | |
| `fields` | Comma-separated list of fields to display, in order | all fields |
| `filter` | Filter expression (see below) | |
| `sort` | Field name to sort by | table default |
| `order` | `asc` or `desc` | `asc` |
| `limit` | Rows per page | `20` |

#### Filter expressions (`filter`)

A filter is one or more conditions joined by `&` (logical AND). Each condition has the form `field<op>value`.

Supported operators:

| Operator | SQL equivalent | Description |
|---|---|---|
| `=` | `=` | Equal |
| `!=` | `!=` | Not equal |
| `<>` | `!=` | Not equal (SQL alias, normalized to `!=`) |
| `<` | `<` | Less than |
| `>` | `>` | Greater than |
| `<=` | `<=` | Less than or equal |
| `>=` | `>=` | Greater than or equal |
| `~` | `ILIKE` | Pattern match (case-insensitive). Uses `%` as wildcard. If no `%` is present in the value, the value is wrapped as `%value%` (substring match). |

Examples:

```
filter="status=1"
filter="status!=0&priority>2"
filter="archived<>Y&kpiid~K1%"
filter="name~alice"                 (substring: matches "Alice", "alice smith", etc.)
filter="code~PRJ%"                  (prefix: matches "PRJ001", "PRJ-alpha", etc.)
```

Multiple conditions are combined with AND. All filters apply to the SQL query — no client-side filtering.

#### Field selection (`fields`)

When `fields` is set, only the listed fields are displayed, in the specified order. Fields are matched by name or label (case-insensitive). The special token `%title%` refers to the index field (the field used as page name in page-bound tables); it renders as a clickable link to the page.

```
{database-query table=server fields="category, provider, %title%, description, ipv4"}
```

If `fields` is omitted, all non-archived fields are shown in their defined display order.

### New row form

```
{database-newrow table=test}
```

### Row binding

```
{database-row table=test}
| Field | Value |
| --- | --- |
| what | Some text |
| status | 1 |
```

The field/value table following `{database-row}` is parsed by the backend to sync row data. Template variables (`{{field_name}}`) in the page content are resolved from this row's fields.
