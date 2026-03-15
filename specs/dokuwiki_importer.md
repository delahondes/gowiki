# DokuWiki Importer

## Goal

Import a complete DokuWiki installation into a Gowiki site. The importer is a **one-time CLI tool** — not an ongoing service. It reads from a DokuWiki export directory (`import/`) containing `data/` and `conf/` subdirectories, and writes into the Gowiki data directory (`backend/data/`).

The target is to convert **>=90% of all lines** of all documents automatically. Lines that cannot be converted are preserved as-is with a marker comment and reported in a summary.

## Migration approach

The import is a **two-phase hybrid**: a mechanical script for bulk conversion, followed by AI-assisted passes for tasks requiring judgment.

### Phase A: Mechanical script (importer CLI)

Handles >=90% of lines automatically:
- DokuWiki markup → Gowiki markdown conversion
- Media file copying
- Link rewriting
- Metadata extraction
- Struct syntax flagged but not converted

### Phase B: AI-assisted passes

After the script import, an interactive agent-assisted process handles:

1. **Namespace renaming and translation** — The old wiki uses French naming (e.g., `smq/` = Système de Management de la Qualité). Most content should be translated to English, with namespace renaming to match (e.g., `smq/` → `qms/`). Technical files (studies, technical file content) are left as-is. For each page to translate:
   - The French version is preserved as a prior version in page history
   - A new English version is created as the current page
   - Cross-references are updated to reflect renamed paths

2. **Struct/data migration** — Schema-by-schema, interactively (see struct section below). Each schema's columns, labels, and data values may need translation or renaming to match the English namespace structure.

3. **Naming consistency fixes** — The old wiki has inconsistent naming across namespaces. The agent reviews and proposes coherent naming during translation.

Scope exclusions from the script: struct variable references (`{{$schema.field}}`, `@@schema.field@@`) are flagged in the report but not converted — handled in Phase B. Struct block syntax (`---- struct table ----`, `---- struct lookup ----`, `<form>`) is converted in Phase A3 (see below).

## CLI usage

```bash
cd backend
go run ./cmd/import/ --src ../import --dest ./data
```

Both flags are **required** (no defaults — prevents accidental writes):

| Flag | Description |
|---|---|
| `-src` | DokuWiki import root (contains `data/` and `conf/` subdirectories) |
| `-dest` | Gowiki data directory (will contain `content/`, `meta/`) |
| `-dry-run` | Analyze and report without writing files |
| `-verbose` | Log each file being processed |

## Source data

| Item | Count |
|---|---|
| Pages (.txt files) | 659 |
| Total lines | ~51,000 |
| Media files | 1,129 |
| Namespaces (top-level) | 17 |
| Max namespace depth | 11 |
| Users | 16 |
| ACL rules | 66 (including 9 %USER% template rules → @self) |

Source layout:
```
import/                    DokuWiki import root (-src flag)
  data/
    pages/                 DokuWiki markup (.txt), namespace = directory
    media/                 Attachments (PNG, PDF, SVG, JPEG, XLSX, ZIP, …)
    meta/                  Serialized PHP metadata, .changes TSV changelogs
    attic/                 Gzipped historical page versions
    media_attic/           Gzipped historical media versions
  conf/
    users.auth.php         User accounts (login:hash:name:email:groups)
    acl.auth.php           ACL rules (path	subject	permission_level)
```

## Target layout

```
backend/data/              Gowiki data directory (-dest flag)
  content/                 Pages (.md) and media files, unified tree
  meta/
    *.json                 Page metadata, mirrors content/ structure
    users.json             Imported user accounts
    groups.json            Imported groups
    acl.json               Imported ACL rules
```

## Language choice

**Go.** The backend is Go, the importer needs no frontend, and Go has good stdlib support for file manipulation, text processing, and SQLite. The importer lives in `backend/cmd/import/` as a standalone binary.

## Path conversion

### Namespaces
DokuWiki uses `:` as namespace separator; Gowiki uses `/`.

| DokuWiki | Gowiki |
|---|---|
| `pages/ns/page.txt` | `content/ns/page.md` |
| `pages/ns/start.txt` | `content/ns/index.md` |
| `pages/start.txt` | `content/index.md` |
| `media/ns/image.png` | `content/ns/image.png` |

- `start.txt` -> `index.md` (DokuWiki's convention for namespace index pages).
- Media files are copied into `content/` alongside pages (Gowiki's unified content tree).
- Namespace constraint: if `content/path/to/ns/` exists, `content/path/to/ns.md` must not exist.

### Links inside documents
DokuWiki link `[[ns:page|text]]` -> Gowiki `[text](/ns/page)`.
- `:` -> `/` in all internal link paths.
- `start` -> `index` when it is the last segment and refers to a namespace index.
- Relative links (`[[page]]` without leading `:`) are resolved relative to the source page's namespace, then converted to absolute Gowiki paths (simpler, no ambiguity).
- Section anchors: `[[page#section]]` -> `[](/page#section)` (preserved as-is after path conversion).
- External links: `[[https://url|text]]` -> `[text](https://url)` (standard Markdown).
- Bare URLs (auto-linked by DokuWiki): preserved as plain text (Gowiki auto-links URLs).

## Syntax conversion table

### Core syntax (universal -- covers ~80% of lines)

| DokuWiki | Gowiki Markdown | Pages | Notes |
|---|---|---|---|
| `====== H1 ======` | `# H1` | 542 | Level mapping: 6 `=` -> `#`, 5 `=` -> `##`, etc. |
| `**bold**` | `**bold**` | 177 | Same syntax -- no conversion needed |
| `//italic//` | `*italic*` | 426 | |
| `__underline__` | `_underline_` | 26 | |
| `''monospace''` | `` `monospace` `` | 142 | |
| `~~strikethrough~~` | `~~strikethrough~~` | rare | Same syntax -- no conversion needed |
| `[[ns:page\|text]]` | `[text](/ns/page)` | 413 | See link conversion rules above |
| `{{ns:image.png}}` | `![](/ns/image.png)` | 341 | See media conversion below |
| `  * item` | `- item` | ~200 | Indentation depth -> nesting |
| `  - item` | `1. item` | ~21 | Indentation depth -> nesting |
| `\| cell \| cell \|` | `\| cell \| cell \|` | 429 | Pipe tables -- mostly compatible |
| `^ header ^ header ^` | `\| header \| header \|` + separator row | 429 | See table conversion |
| `<code bash>...</code>` | `` ```bash ... ``` `` | 130 | Language specifier preserved |
| `<file>...</file>` | `` ```...``` `` | 29 | Treated as unnamed code block |
| `----` (4+ dashes, own line) | `---` | 68 | Horizontal rule |
| `\\` (line break) | newline (in paragraphs) or `\n` (in tables/lists) | 152 | Context-dependent |
| `<sub>text</sub>` | `~text~` | 5 | Subscript -- implemented |
| `<sup>text</sup>` | `^text^` | 5 | Superscript -- implemented |
| `<del>text</del>` | `~~text~~` | 3 | Strikethrough -- implemented |
| `@color:text` (in table cell) | `{color=color} text` | 27 | Cell background color |
| `!!text!!` (in table cell) | `{vtext=upward} text` | 9 | Vertical/rotated text in cells |
| Empty line | Empty line (paragraph break) | -- | Same |

### Media/images

DokuWiki: `{{ns:image.png?200}}` or `{{ ns:image.png?200x100 |caption}}`

Gowiki equivalent:
```
{image size=200x align=center}
![caption](/ns/image.png)
```

Conversion rules:
- `{{image.png}}` -> `![](/image.png)` (no size, no properties)
- `{{image.png?200}}` -> `{image size=200x}\n![](/image.png)` (width only)
- `{{image.png?200x100}}` -> `{image size=200x100}\n![](/image.png)` (width x height)
- `{{ image.png}}` -> left-aligned: `{image align=left}\n![](/image.png)`
- `{{ image.png }}` -> centered: `{image align=center}\n![](/image.png)`
- `{{image.png }}` -> right-aligned: `{image align=right}\n![](/image.png)`
- Size + alignment combine in a single property line: `{image size=200x align=center}`
- `|caption` after image -> alt text in `![caption](...)`
- Namespace path: `:` -> `/`, relative paths resolved to absolute

### Tables

DokuWiki tables use `^` for header cells and `|` for data cells. Gowiki uses standard pipe tables with a `---` separator row.

```
DokuWiki:
^ Name ^ Age ^
| Alice | 30 |
| Bob | 25 |

Gowiki:
| Name | Age |
| --- | --- |
| Alice | 30 |
| Bob | 25 |
```

Special cases:
- **Header column** (first column with `^`, rest with `|`): Convert to `{table headers=1c}` property + pipe table with all `|` cells.
- **Cell merge** (`:::`): 44 pages. DokuWiki uses `:::` for vertical merge. Convert to Gowiki's merge syntax: `<<` (colspan, merge left) and `^^` (rowspan, merge up).
- **Cell colors** (`@LightBlue:text`): Convert to Gowiki cell directive: `{color=lightblue} text`. DokuWiki uses standard CSS color names and `#hex` codes, both supported as-is by Gowiki's cell color system.
- **Vertical text** (`!!text!!`): Convert to Gowiki cell directive: `{vtext=upward} text`. DokuWiki renders `!!text!!` as rotated text in table cells (common for compact column headers).
- **Table formulas** (`~~=sum(...)~~`): 9 pages. Gowiki has a `table_formulas` plugin. Syntax may differ; convert where possible, flag for review.
- **WRAP inside table cells**: Extract WRAP content, keep text only. Strip `<WRAP>`/`</WRAP>` tags.
- **Multi-line cells**: DokuWiki allows `\\` for line breaks in cells -> convert to `\n`.

### Includes

DokuWiki: `{{page>ns:page}}` or `{{page>ns:page#section&noheader&nofooter}}`

Gowiki: `{include path=/ns/page}` or `{include path=/ns/page#section}` (section-targeted)

52 pages use includes. Conversion:
- `{{page>ns:page}}` -> `{include path=/ns/page}`
- `{{page>ns:page&nofooter}}` -> `{include path=/ns/page}` (nofooter/noheader are DokuWiki rendering hints -- drop them)
- `{{page>ns:page&firstseconly&noreadmore}}` -> `{include path=/ns/page#first-heading}` (resolve first heading anchor from target page content; drop `noreadmore`)
- `{{page>ns:page#section}}` -> `{include path=/ns/page#section}` (section-targeted include -- renders from the anchor heading to the next heading of same or higher level)
- `{{page>ns:page#section&link}}` -> `{include path=/ns/page#section}` (drop DokuWiki rendering hints)

### Tags

DokuWiki: `{{tag>label1 label2}}`

Gowiki: `{tag label1 label2}`

123 pages use tags. Direct mapping -- the Gowiki tag plugin uses `{tag values}` syntax where values are space-separated.

### Code blocks

- `<code>...</code>` -> ` ```\n...\n``` `
- `<code bash>...</code>` -> ` ```bash\n...\n``` `
- `<code python>...</code>` -> ` ```python\n...\n``` `
- `<code sql>...</code>` -> ` ```sql\n...\n``` `
- `<code java>...</code>` -> ` ```java\n...\n``` `
- `<code ->...</code>` -> ` ```\n...\n``` ` (`-` means "no syntax highlighting" in DokuWiki)
- `<file>...</file>` -> ` ```\n...\n``` `
- `<file txt filename.txt>...</file>` -> ` ```\n...\n``` ` (filename info dropped, flagged)
- Inline `<code>text</code>` (without language specifier, on a single line with other content) -> `` `text` `` (backtick code span)
- Multi-line `<code>` blocks nested inside blockquotes/notes are handled: the code block is extracted and emitted as a fenced block with `> ` prefix
- **Post-import fix (2026-03-15):** 172 code blocks across 40 files were not caught by the importer (mostly inside list items and `<codeprism>` tags). Fixed by `scripts/fix_code_blocks.py` which transforms remaining `<code LANG>...</code>` and `<codeprism lang=X>...</codeprism>` into fenced blocks, handles escaped variants (`\>`), re-indents content in list contexts, and normalizes language names (`sudo`→`bash`, `R`→`r`, etc.). `wiki/syntax.md` excluded (DokuWiki syntax doc).

### Footnotes

DokuWiki: `((footnote text))`

~100 pages use footnotes, mostly as inline glossary annotations (e.g. `CRC((ColoRectal Cancer))`, `KPI((Key Performance Indicators))`). These are genuine DokuWiki footnotes. Convert directly: `((text))` -> `^[text]` (Gowiki inline footnote, Pandoc-style). Renders as a superscript number with tooltip on hover.

### Nowiki

DokuWiki: `<nowiki>...</nowiki>`

39 pages. Convert to inline code (`` `...` ``) when inline, or code block when block-level. Flag for review.

## Plugin conversion

### REVIEWFLOW (23 pages)

DokuWiki:
```
~~REVIEWFLOW|
version=2.0
author=@alice.laporte
reviewer=@raynald.delahondes
validation=@etienne.formstecher
render=table
~~
```

Gowiki: `{reviewflow version=2.0 author=alice.laporte reviewer=raynald.delahondes validation=etienne.formstecher}`

Conversion:
- Parse the multi-line `~~REVIEWFLOW|...\n~~` block into key=value pairs.
- Drop `render=table` (Gowiki always renders as table).
- Strip `@` prefix from usernames.
- Emit as single-line `{reviewflow ...}` directive.

Note: `~~#REVIEWFLOW|...~~` (with `#`) appears in template examples where the reviewflow is meant to be customized -- the `#` disables it. Preserve as a code block or commented text.

### ACK / ACKNOWLEDGE (34 pages)

DokuWiki: `~~ACK:@managers,@regulatory~~` or `~~ACKNOWLEDGE~~`

Patterns found:
- `~~ACK:@managers,@regulatory~~` (12x)
- `~~ACK:@gmt_science~~` (7x)
- `~~ACK:@managers,@regulatory,@regulatory_devops~~` (6x)
- `~~ACK:@regulatory~~` (3x)
- `~~ACK:user1,user2~~` (2x)
- `~~ACKNOWLEDGE~~` (1x)

ACK is a todo with a `read` action targeting groups with `resolution=all` (every member of every listed group must acknowledge, not just one). Convert to Gowiki's todo plugin:
- `~~ACK:@managers,@regulatory~~` -> `{todo title="Acknowledge" assign="managers,regulatory" action="read" resolution=all}`
- `~~ACKNOWLEDGE~~` -> `{todo title="Acknowledge" action="read" resolution=all}`

Note: group-level reporting for read acknowledgements is not yet implemented in Gowiki but can be added later.

Important: on pages that also contain a `{reviewflow}`, ACK todos (and any other todos) are automatically **inactive until the reviewflow is fully validated**. This is implemented in the Gowiki todo/reviewflow interaction (see `specs/todo_plugin.md` §8.2 "Todo inactivation on pages with pending review"): the backend marks wiki-node tasks as `inactive: true` when their source page has an unvalidated reviewflow, and the complete endpoint rejects completion attempts. The importer should preserve the co-location of reviewflow and ACK on the same page so this behavior applies correctly after import.

### TODO (3 pages)

DokuWiki: `<todo @user #user:YYYY-MM-DD>Task description</todo>`

Gowiki: `{todo title="Task description" assign="user" due=YYYY-MM-DD}`

3 pages -- low volume. Parse the DokuWiki todo attributes and map to Gowiki's todo directive properties.

### WRAP (67 pages, ~220 occurrences)

DokuWiki's WRAP plugin provides styled containers. Most common patterns:

| Pattern | Count | Conversion |
|---|---|---|
| `<WRAP>...</WRAP>` (bare) | 61 | Drop wrapper, keep content |
| `<WRAP center round important 60%>` | 25 | `{blockquote class=important}` + `> content` |
| `<WRAP center round info 60%>` | ~15 | `{blockquote class=note}` + `> content` |
| `<WRAP center round tip 60%>` | 7 | `{blockquote class=tip}` + `> content` |
| `<WRAP center round warning 60%>` | rare | `{blockquote class=warning}` + `> content` |
| `<WRAP group>` + `<WRAP half column>` with image | ~14 | Image+text side-by-side: convert to image with `wrap=left` or `wrap=right` property |
| `<WRAP group>` + `<WRAP half column>` without image | ~11 | Convert to `{blockquote wrap=left width=49%}` columns |
| `<WRAP round white spacedx2>` | 18 | Drop wrapper, keep content |
| `<WRAP prewrap>` | 7 | Drop wrapper, keep content |

Strategy:
- **Admonition wraps** (info, important, tip, warning): Convert to Gowiki blockquote with the `{blockquote class=...}` directive. Gowiki's blockquote plugin supports built-in classes: `tip`, `note`, `important`, `warning` — each renders with a colored border, background, icon, and label. DokuWiki's `info` maps to Gowiki's `note`. If a WRAP has a custom width (e.g. `60%`), use `{blockquote class=custom color=... width=60% align=center}` with appropriate color.
- **Image+text layout wraps** (group + half column containing an image): Convert to Gowiki image with `wrap` property for text wrapping.
- **Column layout wraps** (group + half/third column without image): Convert each column to a `{blockquote wrap=left width=49%}` (for half) or `{blockquote wrap=left width=32%}` (for third). Adjacent wrapped blockquotes float side by side. Content after the column group is automatically cleared.
- **Bare wraps / styling wraps**: Drop `<WRAP>` / `</WRAP>` tags, keep content.
- **Wraps inside table cells**: Extract content only, strip tags.

DokuWiki WRAP class to Gowiki blockquote class mapping:

| DokuWiki WRAP class | Gowiki blockquote class |
|---|---|
| `important` | `important` |
| `info` | `note` |
| `tip` | `tip` |
| `warning` | `warning` |
| Other styled | `custom` (with `color`, `icon`, `width`, `align` as needed) |

### Figure (37 pages, ~121 occurrences)

DokuWiki:
```
<figure>
{{.:pasted:20250924-074417.png?700}}
<caption>Description text</caption>
</figure>
```

Gowiki: Convert to image with `caption` property. The caption supports inline markdown (**bold**, *italic*, `code`, [links](url)).
```
{image size=700x caption="Description text"}
![](./pasted/20250924-074417.png)
```

The `caption` property triggers auto-numbering ("Figure 1:", "Figure 2:", ...) and renders a styled figcaption below the image. An optional `label` property enables cross-references via `{ref label-name}`.

- **Single image in figure**: extract image + caption, convert to Gowiki image with `caption=` property. Alt text is left empty (caption serves as the visible label).
- **Multiple images in figure**: group inside a custom blockquote with `image-width` to constrain image sizes uniformly. Use `image-width=49%` for a 2-up grid with a thin gap:
```
{blockquote class=custom color=lightgrey width=70% image-width=49%}
> ![](./pasted/img1.png) ![](./pasted/img2.png) \n![](./pasted/img3.png) ![](./pasted/img4.png)
> **Panel 1**: In this panel we...
```

### PDFNS (9 pages)

DokuWiki: `~~PDFNS>namespace:path|Display Name~~`

PDF export namespace targeting -- **no Gowiki equivalent**. Drop. Flag.

### NOCACHE / NOTOC (24 pages)

DokuWiki: `~~NOCACHE~~`, `~~NOTOC~~`

Rendering directives -- no Gowiki equivalent. Drop silently (no flag needed -- these have no content impact).

### Changes widget (1 page -- sidebar)

DokuWiki: `{{changes>ns=-sidebar}}`

Gowiki: `{changes}` (the Gowiki changes plugin). Direct mapping. The `ns=-sidebar` filter is DokuWiki-specific -- Gowiki excludes sidebar/footer by default.

### Struct/data

DokuWiki struct binds structured fields to pages via schemas. The following syntax forms exist:
- `---- struct table ----` blocks — inline table rendering of struct data
- `---- struct lookup ----` blocks — aggregation/query views
- `{{$schema.field}}` variable references — inline field display
- `@@schema.field@@` bureaucracy variables — form-bound references

#### Export format

Each DokuWiki schema is exported as a pair of files in `import/struct/<schema_name>/`:
- `<schema_name>.struct.json` — schema definition (columns, types, config, visibility)
- `<schema_name>.csv` — row data with `pid` (DokuWiki page path) as first column

The DokuWiki struct SQLite database (`import/data/meta/struct.sqlite3`) can be used as a reference to verify row IDs and extract data missing from CSV exports (e.g., status tables with deleted-then-recreated rows).

#### Column type mapping

All DokuWiki struct column types have Gowiki equivalents:

| DokuWiki type | Gowiki type | Notes |
|---|---|---|
| `Text` | `text` | Direct |
| `LongText` | `text` | Textarea variant |
| `Dropdown` | `enum` / `multi_enum` | `multi_enum` when `ismulti=true` |
| `Date` | `date` | Already `YYYY-MM-DD` in CSV |
| `DateTime` | `datetime` | ISO format in CSV |
| `Decimal` | `integer` | Strip trailing `.0` zeros |
| `User` | `user` | DokuWiki usernames preserved as-is |
| `Checkbox` | `enum` / `multi_enum` / `boolean` | Multi-value → `multi_enum`; single with values → `enum`; valueless → `boolean` |
| `Status` | `tag` | Foreign key to reference table (icon/color/label) |
| `Lookup` | `lookup` | Foreign key; self-referential supported |
| `Color` | `color` | Direct mapping |
| `Page` | `page_link` | DokuWiki path converted via `dokuPathToGowiki()` |

#### Import tool: `import-struct`

Located at `backend/cmd/import-struct/main.go`. A standalone CLI that reads from `import/struct/` and writes to the Gowiki PostgreSQL database.

```bash
cd backend
go build -o import-struct ./cmd/import-struct/
./import-struct -dir ../import/struct -dsn 'postgres://...' [-dry-run]
```

| Flag | Description |
|---|---|
| `-dir` | Path to `import/struct/` directory |
| `-dsn` | PostgreSQL connection string |
| `-dry-run` | Show what would be imported without writing |

#### Import ordering

Reference/status tables (those referenced by Status or Lookup fields in other tables) must be imported first so their row IDs exist before dependent tables reference them. The tool has a hardcoded list of reference tables that are imported before all others. Edit the `referenceFirst` slice in `main()` to match your DokuWiki's status tables.

Remaining tables are imported alphabetically. The tool is idempotent: existing tables are skipped.

#### Foreign key handling (Status/Lookup)

DokuWiki CSV encodes Status and Lookup field values as JSON arrays: `["", <rid>]` where `rid` is the DokuWiki row ID in the referenced table. The import tool parses this to extract the integer and stores it as the Gowiki row ID.

This works because reference tables are imported first, and their CSV rows are ordered by DokuWiki `rid`. Since Gowiki auto-increments IDs starting from 1, the Gowiki row IDs match the DokuWiki rids — but only if the CSV contains all rows (no gaps from deleted entries). Verify against the DokuWiki SQLite database and add any missing rows to the CSV before importing.

#### Namespace path translation

If pages were reorganized after the initial content import (e.g., namespace renames), the `pid` paths in CSVs will be outdated. The tool has a `namespaceRenames` map that translates old path segments to new ones. Edit this map in `main.go` to match your renames.

The standard DokuWiki-to-Gowiki path conversion also applies: `:` → `/`, `start` → `index`, leading `/` added.

#### Field name sanitization

DokuWiki column labels (which may contain accented characters and spaces) are converted to valid Gowiki field names:

1. Transliterate common accented characters (`é`→`e`, `ç`→`c`, `ô`→`o`, `ñ`→`n`, etc.)
2. Lowercase
3. Replace non-alphanumeric runs with `_`
4. Trim leading/trailing `_`
5. Prefix with `f_` if starts with a digit

The original DokuWiki label is preserved in the field's `label` attribute for display.

#### Tag table field normalization

Gowiki's tag rendering expects reference tables (used by `tag`-type fields) to have fields named `label`, `icon`, and `color`. DokuWiki status schemas often use different names for the label field (e.g., `name_en`, `name`, `title`).

The import tool detects tag tables by convention: if a table has `icon` and `color` fields but no `label` field, it renames the first text field to `label`. This runs automatically during import — no manual configuration needed.

#### Struct block conversion: `convert_struct_blocks.py`

Located at `scripts/convert_struct_blocks.py`. A standalone Python script that converts DokuWiki struct/form syntax in already-imported Gowiki markdown files into native `{database-query}` and `{database-newrow}` directives. Run after the struct data import (Phase A2).

```bash
# Dry run
python3 scripts/convert_struct_blocks.py /path/to/data/content --dry-run

# Apply
python3 scripts/convert_struct_blocks.py /path/to/data/content
```

Converts three block types:

| DokuWiki syntax | Gowiki directive |
|---|---|
| `---- struct table ----` block | `{database-query table=... fields="..." ...}` |
| `---- struct lookup ----` block | `{database-query table=...}` |
| `<form>...</form>` block | `{database-newrow table=...}` |

Conversion details:
- `schema:` → `table=`
- `cols:` → `fields="..."` (with `%pageid%` converted to `%title%`, standalone `*` removed)
- `sort:` → `sort=` (first field only; `^` prefix → `order=desc`)
- `filter:` → `filter="..."` (multiple filters joined with `&`)
- Code fences (` ``` `) wrapping these blocks (import artifacts) are removed
- Closing `---` (3 dashes) and `----` (4 dashes) are both handled

The script also extracts table configuration from `<form>` blocks and prints them at the end:
- `page_folder` — where new row pages are created (page name = row id)
- `page_template_path` — the template page for new rows

These must be applied to the database tables via the admin UI or SQL after running the script. The extracted paths use DokuWiki namespace conventions and may need translation if namespaces were renamed.

#### Status/tag table icons

DokuWiki's structstatus plugin stores SVG icons at `/var/www/dokuwiki/lib/plugins/structstatus/svg/` on the DokuWiki server. These icons are used by Status-type columns to render colored badges with icons.

After importing tag tables, copy the relevant SVGs into Gowiki's content tree and update the icon field values:

1. Copy SVGs from the DokuWiki server:
   ```bash
   scp dokuwiki-server:/var/www/dokuwiki/lib/plugins/structstatus/svg/*.svg import/svg/
   ```

2. Upload to Gowiki content (so they're served as static files):
   ```bash
   cp import/svg/*.svg /path/to/data/content/icons/
   ```

3. Update icon field values from bare names to content paths:
   ```sql
   UPDATE _my_status_table SET icon = '/icons/' || icon || '.svg';
   ```

The icon field in tag tables must contain a content path (e.g., `/icons/ovh.svg`), not a bare name. Gowiki's tag badge renderer fetches the SVG from this path.

### Slider (1 page)

DokuWiki:
```
<slider slide1.png>
====== Title Slide ======
Content...

<slider slide2.png>
====== Second Slide ======
Content...
```

Each `<slider background.png>` tag acts as a slide separator with a per-slide background image. In the only usage found (`regulatory/smq/ps01/sop01/tpl01.txt`), all slides share the same background (`slide2.png`), with a different one for the title slide (`slide1confidential.png`).

Gowiki: Convert the entire page to a `{slides}` directive with `background` property. Each `<slider>` becomes an `---` separator. The most common background image becomes the presentation-level `background`:

```
{slides title="Revue de Direction" background=./slide2.png}

# Title Slide
Content...

---

# Second Slide
Content...
```

Conversion notes:
- Strip `<slider ...>` tags, replace with `---` separators
- Extract the most frequent background image, set as `background=` on the `{slides}` directive
- Per-slide background differences are lost (Phase 1 limitation -- flag for manual review)
- WRAP styling inside slides (flexcenter, halfhalfbigtext, etc.) is stripped; content is kept
- `<gchart>` blocks inside slides are not supported in Phase 1 -- flag

### Note blocks (DokuWiki note plugin)

DokuWiki: `<note>...</note>` or `<note important>...</note>`, `<note tip>`, `<note warning>`

Gowiki: `{blockquote class=...}` + `> content`

| DokuWiki | Gowiki |
|---|---|
| `<note>...</note>` | `{blockquote class=tip}` (bare note defaults to tip) |
| `<note important>...</note>` | `{blockquote class=important}` |
| `<note tip>...</note>` | `{blockquote class=tip}` |
| `<note warning>...</note>` | `{blockquote class=warning}` |

These are block-level constructs — the content between the tags is converted line by line with inline conversion, and each line is prefixed with `> `.

### NB blocks (custom wiki notation)

DokuWiki: `NB:: content ::NB` or `NB!:: content ::NB`

Gowiki: `{blockquote class=note}` / `{blockquote class=important}` + `> content`

Both single-line and multi-line NB blocks are supported. `NB::` maps to `note`, `NB!::` maps to `important`.

### Entity conversions

DokuWiki has built-in entity shortcuts that are converted to UTF-8 characters during import:

| DokuWiki | UTF-8 | Description |
|---|---|---|
| `=>` | ⇒ (U+21D2) | Double arrow right |
| `->` | → (U+2192) | Arrow right |
| `<-` | ← (U+2190) | Arrow left |
| `<>` | ☐ (U+2610) | Unchecked checkbox |
| `<x>` | ☒ (U+2612) | Checked checkbox |
| `\_` | (U+00A0) | Non-breaking space |

Entity conversion runs at the inline level, after code spans and links are protected. This ensures entities inside code spans or URLs are not converted. The conversion order matters: `<x>` must be checked before `<>` to prevent partial matches.

### DokuWiki icons

| DokuWiki | UTF-8 |
|---|---|
| `:!:` | ⚠️ |
| `:?:` | ℹ️ |
| `FIXME` | ⚠️ FIXME |
| `DELETEME` | ❌ DELETEME |

### Folded plugin (14 pages)

DokuWiki:
```
++++ Title |
Hidden content (paragraphs, tables, code blocks, etc.)
++++
```

Gowiki: `` ```spoiler Title `` ... `` ``` ``

The folded plugin creates collapsible content blocks. The importer initially stripped the `++++` markers but preserved the content inline.

**Post-import fix (2026-03-15):** `scripts/fix_folded_spoilers.py` reintroduced the folded blocks as Gowiki spoiler blocks. The script uses the original DokuWiki source (`import/data/pages/`) to identify which pages had folded blocks and their titles, then uses distinctive text anchors to locate the content in the current (potentially edited/translated) Gowiki pages and wraps it in spoiler fences. 20 spoiler blocks across 12 files. Pages `regulatory/smq/soft/sop06/rec01.md` and `regulatory/smq/qara/sop05/rec01.md` were already fixed manually.

### Template variables

DokuWiki uses two forms: `@!PAGE!@` and `@PAGE@`. Both are converted to Gowiki `{{PAGE}}`.

Template variables can appear in any page but are most commonly found in template files (see below).

## Users and ACL import

The importer reads `conf/users.auth.php` and `conf/acl.auth.php` from the DokuWiki export and generates Gowiki's `users.json`, `groups.json`, and `acl.json` in the destination `meta/` directory.

### Users (`conf/users.auth.php`)

DokuWiki format: `login:passwordhash:Real Name:email:group1,group2`

- **Passwords**: DokuWiki bcrypt hashes (`$2y$`) are converted to Go-compatible `$2a$` prefix (same algorithm, different version tag). Non-bcrypt hashes (MD5-crypt, phpass) are dropped — those users must reset their password or use OAuth.
- **Groups**: Imported as-is from the comma-separated list. This includes Azure AD group UUIDs synced by DokuWiki's authAD plugin — they are imported but only meaningful if OAuth group sync is configured in Gowiki.
- **Display name and email**: Preserved directly.

### Groups (`meta/groups.json`)

Groups are **collected** from two sources:
1. All groups referenced in user memberships
2. All groups referenced in ACL rules (after URL-decoding)

The `admin` and `editors` groups are always present (Gowiki defaults).

### ACL rules (`conf/acl.auth.php`)

DokuWiki format: `path	subject	permission_level` (tab-separated)

#### Path conversion

DokuWiki ACL paths use colon separators and `*` wildcards:

| DokuWiki | Gowiki regex pattern |
|---|---|
| `*` | `.*` |
| `ns:*` | `ns/.*` |
| `ns:sub:*` | `ns/sub/.*` |
| `ns:page` | `ns/page` (exact match, metacharacters escaped) |

#### Subject conversion

| DokuWiki | Gowiki |
|---|---|
| `@ALL` | `subject_type: "special"`, `subject: "@all"` |
| `@groupname` | `subject_type: "group"`, `subject: "groupname"` |
| `username` | `subject_type: "user"`, `subject: "username"` |

URL-encoded names (e.g., `gmt%5fscience` → `gmt_science`) are decoded.

#### Permission level mapping

DokuWiki uses a numeric bitmask; Gowiki uses named permissions:

| DokuWiki level | Gowiki permissions |
|---|---|
| 0 (none) | `[]` (deny) |
| 1 (read) | `["view"]` |
| 2 (edit) | `["view", "edit"]` |
| 4 (create) / 8 (upload) | `["view", "edit"]` |
| 16 (delete) | `["view", "edit", "delete"]` |
| 255 (admin) | `["view", "edit", "delete"]` |

#### Per-user ACL rules (`%USER%`)

DokuWiki's `%USER%` template rules (e.g., `regulatory:smq:ps07:annualinterview:%USER%-*	%USER%	16`) are converted to Gowiki `@self` rules. The `%USER%` placeholder becomes `@self` in the pattern and is substituted with the authenticated username at evaluation time. Example conversion:

| DokuWiki | Gowiki |
|---|---|
| `regulatory:smq:ps07:sop03:staff:%USER%:*	%USER%	16` | `pattern: "regulatory/smq/ps07/sop03/staff/@self/.*"`, `subject: "@self"`, `permissions: ["view","edit","delete"]` |

## Conversion not attempted (flag only)

| Feature | Pages | Reason |
|---|---|---|
| Interwiki links (`[[wp>...]]`) | rare | Not supported in Gowiki |
| PDFNS directives | 9 | No Gowiki equivalent |

## Metadata conversion

### Page metadata

DokuWiki stores metadata in `meta/ns/page.meta` as serialized PHP. Relevant fields:
- `date.created` -> `created_at`
- `date.modified` -> `updated_at`
- `creator` -> `created_by`
- `last_change.user` -> `updated_by`

Write to `data/meta/ns/page.json` in Gowiki's metadata format.

### Changelog

DokuWiki stores changelogs in `meta/ns/page.changes` as TSV:
```
timestamp  IP  type  page:id  user  summary  extra  sizechange  mode
```

Convert to Gowiki's changelog format if needed. Low priority -- the main value is preserving creation date and author.

### Attic (version history)

`data/attic/` contains gzipped historical versions. Imported in Phase 4 — each version is converted through the same markup converter and stored in Gowiki's attic format. Changelog metadata from `data/meta/*.changes` is preserved.

## Implementation plan

### Phase 1: Core syntax converter

A converter that handles:
1. Headings (level inversion: 6 `=` -> `#`, 5 `=` -> `##`, etc.)
2. Bold (no-op), italic (`//` -> `*`), underline (`__` -> `_`), monospace (`''` -> backtick), strikethrough (no-op)
3. Links (internal with namespace conversion, external, with anchors)
4. Images (with size and alignment properties)
5. Lists (ordered, unordered, nested -- indentation to nesting level)
6. Tables (header row detection, `^` -> `|` + separator, cell merge `:::` -> `<<`/`^^`, cell colors)
7. Code blocks (`<code>`, `<file>` -> fenced code blocks)
8. Horizontal rules
9. Line breaks (`\\` -> newline or `\n`)
10. Nowiki -> inline code or code block
11. Sub/sup (`<sub>` -> `~`, `<sup>` -> `^`)

This phase should cover ~85% of all lines.

### Phase 2: Plugin syntax

1. Includes (`{{page>...}}` -> `{include path=...}`)
2. Tags (`{{tag>...}}` -> `{tag ...}`)
3. WRAP blocks (admonition conversion, image layout, strip)
4. Figures -> image with caption
5. REVIEWFLOW -> `{reviewflow ...}`
6. ACK -> `{todo ... action="read"}` (mapped to todo with read action)
7. TODO -> `{todo ...}`
8. NOCACHE/NOTOC -> drop
9. Changes widget -> `{changes}`
10. Footnotes -> parenthesized text
11. Template variables (`@!PAGE!@` -> `{{PAGE}}`)
12. PDFNS -> drop + flag
13. Slider -> `{slides}` with `background=` + `---` separators

### Phase 3: File operations

1. Copy all media files from `data/media/` to `content/` (preserving namespace structure)
2. Write converted `.md` files to `content/`
3. Parse DokuWiki metadata (serialized PHP in `data/meta/*.meta` files) and write `.json` to `meta/`
4. Generate conversion report

### Phase 4: Version history (attic)

Import gzipped historical versions from `data/attic/`. Each version is converted through the same markup converter and stored in Gowiki's attic format with changelog metadata.

### Phase 5: Auth import

Import users, groups, and ACL rules from `conf/users.auth.php` and `conf/acl.auth.php`. See "Users and ACL import" section above for details.

## Conversion report

The tool produces a report listing:
1. **Summary**: total pages, total lines, lines converted, lines flagged, conversion percentage
2. **Per-page detail**: for each page with flagged lines, the line numbers and reason
3. **Unsupported features**: aggregate count of each unsupported feature encountered
4. **Media**: files copied, files missing (referenced but not found in `media/`)

Report format: Markdown file written to `data/content/import_report.md` (so it's viewable in the wiki itself).

## Edge cases

- **Encoding**: DokuWiki pages are UTF-8. Gowiki is UTF-8. No conversion needed.
- **Empty pages**: Skip (don't create empty `.md` files).
- **Namespace conflicts**: If both `pages/ns.txt` and `pages/ns/start.txt` exist, the importer must handle the conflict (Gowiki forbids both). Prefer the namespace index; rename the file page. Flag.
- **Case sensitivity**: DokuWiki page names are lowercase. Gowiki paths are case-sensitive. Preserve as-is.
- **Special pages**: `wiki/` namespace contains DokuWiki reference docs -- import but flag as potentially irrelevant.
- **DokuWiki template files**: DokuWiki has multiple template file conventions, all converted to Gowiki's `_template.md`:
  - `templates/study.txt` -> `study/_template.md` (templates namespace)
  - `ns/_template.txt` -> `ns/_template.md` (in-namespace, primary)
  - `ns/__template.txt` -> `ns/_template.md` (in-namespace, secondary)
  - `ns/c_template.txt` -> `ns/_template.md` (in-namespace, create variant — not used in our wiki)
  - `ns/i_template.txt` -> `ns/_template.md` (in-namespace, import variant — not used in our wiki)
  - Template variables are converted: `@!PAGE!@` and `@PAGE@` both become `{{PAGE}}`.

## Prerequisites

All inline marks needed for the import are implemented:
- `_underline_`, `~~strikethrough~~`, `~subscript~`, `^superscript^` -- all implemented and safe to import directly.
