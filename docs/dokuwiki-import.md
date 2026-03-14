# Importing from DokuWiki

This guide covers migrating a complete DokuWiki installation into Gowiki. The process has two phases: an automated bulk import, then an AI-assisted refinement pass.

## Prerequisites

- A working Gowiki installation (see `deploy.md`)
- A copy of the DokuWiki `data/` and `conf/` directories
- PostgreSQL configured and running (for structured data import)
- Go toolchain installed (to build the import tools)

## 1. Prepare the DokuWiki export

Copy the DokuWiki data into an `import/` directory at the project root:

```
import/
  data/
    pages/          .txt page files
    media/          attachments (images, PDFs, etc.)
    meta/           metadata, changelogs, struct.sqlite3
    attic/          version history
  conf/
    users.auth.php  user accounts
    acl.auth.php    ACL rules
```

## 2. Phase A: Bulk content import

The content importer converts DokuWiki markup to Gowiki markdown, copies media files, and imports users/groups/ACL.

```bash
cd backend
go run ./cmd/import/ --src ../import --dest ./data
```

| Flag | Description |
|------|-------------|
| `-src` | DokuWiki import root (contains `data/` and `conf/`) |
| `-dest` | Gowiki data directory |
| `-dry-run` | Analyze without writing |
| `-verbose` | Log each file |

This handles ~90% of content automatically:
- Markup conversion (headings, bold, italic, links, images, tables, code blocks, lists)
- Media file copying into the unified `content/` tree
- Link rewriting (`:` to `/`, `start` to `index`)
- Plugin syntax conversion (includes, tags, WRAP, figures, reviewflow, footnotes)
- User and ACL import (bcrypt password hashes preserved)

A conversion report is written to `content/import_report.md`.

## 3. Phase A2: Structured data import

If the DokuWiki installation uses the struct plugin, export the schema definitions and data:

### Prepare struct export files

For each DokuWiki schema, create a directory under `import/struct/<schema_name>/` containing:
- `<schema_name>.struct.json` -- the schema definition
- `<schema_name>.csv` -- the row data (with `pid` column for page associations)

The `.struct.json` format:

```json
{
  "schema": "my_schema",
  "columns": [
    {
      "colref": 1,
      "ismulti": false,
      "isenabled": true,
      "sort": 10,
      "label": "Column Label",
      "class": "Text",
      "config": {}
    }
  ]
}
```

Supported DokuWiki column classes: `Text`, `LongText`, `Date`, `DateTime`, `Decimal`, `Dropdown`, `Checkbox`, `User`, `Page`, `Color`, `Status`, `Lookup`.

### Verify with the DokuWiki SQLite database

If the DokuWiki struct plugin's SQLite database is available at `import/data/meta/struct.sqlite3`, use it to verify:

- **Status/reference tables** have all rows (CSV exports sometimes omit deleted-then-recreated entries)
- **Row IDs** are contiguous (gaps cause broken foreign key references)

Example: check a status table for missing rows:

```bash
sqlite3 import/data/meta/struct.sqlite3 \
  "SELECT rid, col1, col2, col3 FROM data_server_provider ORDER BY rid"
```

If the CSV is missing rows that are referenced by other tables, add them.

### Handle namespace renames

If pages were reorganized after the content import (e.g., renaming `ps02` to `qara`), the `pid` paths in CSVs will be outdated. The import tool has a built-in namespace translation map. Edit `backend/cmd/import-struct/main.go` to update the `namespaceRenames` map:

```go
var namespaceRenames = map[string]string{
    "ps01": "dir",
    "ps02": "qara",
    // add your renames here
}
```

### Run the import

```bash
cd backend
go build -o import-struct ./cmd/import-struct/

# Dry run first
./import-struct -dir ../import/struct -dsn 'postgres://...' -dry-run

# Then import
./import-struct -dir ../import/struct -dsn 'postgres://...'
```

The tool imports reference/status tables first (to establish row IDs for foreign keys), then all remaining tables alphabetically. It is safe to re-run: existing tables are skipped.

### Verify

Check the imported data in the Gowiki admin UI (Database tab) or directly:

```bash
psql -U gowiki -d gowiki -c 'SELECT name, label FROM database_tables ORDER BY name'
```

## 4. Phase B: AI-assisted refinement

After the bulk import, use an AI coding agent (e.g., Claude Code) for tasks requiring judgment:

### Namespace renaming and translation

Many DokuWiki installations use non-English naming. An agent can:
- Translate page content from the source language to English
- Rename namespaces to match (e.g., `smq/` to `qms/`)
- Update all cross-references to reflect new paths
- Preserve the original version in page history

### Naming consistency

The agent reviews and proposes coherent naming across namespaces that may have evolved organically over time.

### Struct field label translation

If DokuWiki schema labels are in a non-English language, the agent can update field labels in the Gowiki database admin UI.

## 5. Post-import checklist

- [ ] Browse key pages and verify rendering
- [ ] Check that internal links resolve correctly
- [ ] Verify media files display (images, PDFs)
- [ ] Test user login (users with bcrypt passwords can log in directly; others need password reset or OAuth)
- [ ] Review ACL rules in admin UI
- [ ] Check structured data tables in admin Database tab
- [ ] Verify struct foreign key references (Status/Lookup fields point to correct rows)
- [ ] Review the conversion report at `/import_report`
- [ ] Delete `import/` directory when satisfied

## Troubleshooting

**Broken struct foreign keys**: If a Status or Lookup field shows the wrong value, the DokuWiki row IDs didn't match the import order. Check the DokuWiki SQLite database for the correct rid mapping and compare with Gowiki's row IDs.

**Missing pages for struct rows**: If `page_path` values in the database don't match any existing page, namespace renames may not be configured. Update the `namespaceRenames` map and re-import (delete the affected tables first via admin UI).

**Garbled characters**: Ensure the DokuWiki export is UTF-8. The importer assumes UTF-8 throughout.

**Large wikis**: The content importer processes all pages sequentially. For very large wikis (>10,000 pages), consider importing in batches by namespace.
