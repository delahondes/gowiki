# Database Administration

Access: Admin > Database

## 1. Overview

Gowiki supports structured data tables backed by PostgreSQL. Tables are defined by admins and used by wiki pages to store, query, and display structured information alongside free-form content.

## 1. Connecting

Configure the PostgreSQL DSN in Admin > Configuration > Database, then click **Connect**. The connection status is shown in Admin > Database.

## 1. Creating a table

Click **Create Table** in Admin > Database. Each table has:

- **Name** — unique identifier used in directives (e.g. `test`, `server`, `interview`)
- **Label** — display name shown in the UI
- **Scope regexp** — optional page path pattern restricting where the table's directives are available
- **Default sort** — field and direction for default row ordering

## 1. Page-bound tables

Tables can be linked to wiki pages by setting:

- **Page folder** — a path pattern determining where new pages are created when a row is added. Supports tokens:
  - `@id` — the row's auto-incremented integer ID
  - `@field_name` — value of a field (e.g. `@name`, `@year`)
  - Example: `/regulatory/interviews/@name-@year` creates pages like `/regulatory/interviews/alice-2024`
- **Page template** — path to a template page used when creating page-bound rows. The template can include `{{field_name}}` variables that resolve from the row's data.

When a page-bound row is created, the wiki automatically generates a page from the template, pre-filled with the row's field values.

## 1. Field types

Add fields to a table via **Add Field**. Available types:

| Type | Description | SQL type |
| --- | --- | --- |
| Text | Single-line text | `TEXT` |
| Integer | Whole number | `BIGINT` |
| Float | Decimal number | `DOUBLE PRECISION` |
| Boolean | Yes/No | `BOOLEAN` |
| Date | Date without time | `DATE` |
| Datetime | Date with time | `TIMESTAMPTZ` |
| Page Link | Reference to a wiki page | `TEXT` |
| Enum | Single-select from a list | `TEXT` |
| Multi-enum | Multiple-select from a list | junction table |
| Tag | Reference to a row in another table | `BIGINT` FK |
| Lookup | Computed value from a related table | virtual |

Each field has:
- **Name** — column name (used in directives and template variables)
- **Label** — display name
- **Required** — whether the field must be filled
- **Default value** — pre-filled value for new rows
- **Enum values** — for enum/multi-enum types, the list of allowed values

## 1. Row identity

Every table has an auto-incrementing `id` column (system-managed, read-only). Rows also have `page_path`, `created_at`, and `updated_at` system columns.

## 1. Archiving fields

Fields can be archived (soft-deleted) instead of permanently removed. Archived fields are hidden from the UI but their data is preserved in the database.

## 1. Table history

Admin > Database shows the history of schema changes for each table (fields added, modified, archived).

## 1. CSV export

Each table can be exported as CSV via the admin panel or the API (`GET /api/database/{table}/export/csv`).
