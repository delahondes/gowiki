# DokuWiki Importer

## Goal

Import the complete data folder of an existing DokuWiki site into a Gowiki site. The importer is a **one-time CLI tool** — not an ongoing service. It reads `backend/olddata/` and writes into `data/content/` and `data/meta/`.

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

Scope exclusions from the script: struct-related syntax (`---- struct table ----`, `---- struct lookup ----`, `{{$schema.field}}`, `@@schema.field@@`) is flagged in the report but not converted — handled in Phase B.

## Source data

| Item | Count |
|---|---|
| Pages (.txt files) | 659 |
| Total lines | ~51,000 |
| Media files | 1,129 |
| Namespaces (top-level) | 17 |
| Max namespace depth | 11 |

Source layout:
```
backend/olddata/
  pages/        DokuWiki markup (.txt), namespace = directory
  media/        Attachments (PNG, PDF, SVG, JPEG, XLSX, ZIP, …)
  meta/         Serialized PHP metadata, .changes TSV changelogs
  attic/        Gzipped historical page versions
  media_attic/  Gzipped historical media versions
```

## Target layout

```
data/content/   Pages (.md) and media files, unified tree
data/meta/      Metadata (.json), mirrors content/ structure
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
- **Header column** (first column with `^`, rest with `|`): Convert to `{table headers=1st_col}` property + pipe table with all `|` cells.
- **Cell merge** (`:::`): 44 pages. DokuWiki uses `:::` for vertical merge. Convert to Gowiki's merge syntax: `<<` (colspan, merge left) and `^^` (rowspan, merge up).
- **Cell colors** (`@LightBlue:text`): 2 pages. Convert to Gowiki cell property syntax. DokuWiki uses standard CSS color names which Gowiki accepts as-is (e.g. `lightblue` -> `lightblue`). No color mapping needed.
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
- `<file>...</file>` -> ` ```\n...\n``` `
- `<file txt filename.txt>...</file>` -> ` ```\n...\n``` ` (filename info dropped, flagged)

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

### Struct/data (~50 pages)

DokuWiki struct binds structured fields to pages via schemas. The following syntax forms exist in the source wiki:
- `---- struct table ----` blocks (29 pages) — inline table rendering of struct data
- `---- struct lookup ----` blocks (5 occurrences) — aggregation/query views
- `{{$schema.field}}` variable references (49 pages) — inline field display
- `@@schema.field@@` bureaucracy variables (2 pages) — form-bound references

#### Export format

Each DokuWiki schema is exported as a pair of files in `olddata/struct/<schema_name>/`:
- `schema.json` — schema definition (columns, types, config, visibility)
- `data.csv` — row data with `pid` (DokuWiki page path) as key

#### Column type mapping

12 distinct column types found across 18 schemas (154 columns total). All types have Gowiki equivalents:

| DokuWiki type | Count | Gowiki type | Notes |
|---|---|---|---|
| `Text` | 54 | `text` | Direct |
| `Dropdown` | 27 | `enum` | Comma-separated `values` list |
| `Date` | 20 | `date` | Two format variants: `Y-m-d` and `Y/m/d`; `prefilltoday` config |
| `Decimal` | 11 | `integer` | Used exclusively as ID fields (`trimzeros=true`); map to `auto_increment` for IDs |
| `User` | 10 | `user` | DokuWiki usernames; select from user list via `/api/users/list` |
| `Checkbox` | 9 | `boolean` / `multi_enum` | Single value → `boolean`; multi-value (`ismulti=true`) → `multi_enum` |
| `Status` | 6 | `tag` | Foreign key with icon/color/label from reference table |
| `LongText` | 5 | `text` | Textarea variant; map to text |
| `DateTime` | 4 | `datetime` | Like Date but with time (`Y/m/d H:i`) |
| `Lookup` | 3 | `lookup` | General foreign key; self-referential supported |
| `Color` | 3 | `color` | Direct mapping |
| `Page` | 2 | `page_link` | DokuWiki namespace path → Gowiki page path |

#### Migration strategy

Struct migration is **agent-assisted, not scripted**. The data volume is small (~50 pages, handful of schemas) and each schema requires design decisions about how to represent it in Gowiki's structured data system. The process for each schema:

1. Read `schema.json` — review column definitions and types
2. Read `data.csv` — review actual data, identify edge cases
3. Propose Gowiki field definitions — interactive discussion to map column types, handle `User` references, resolve page paths
4. User confirms the mapping
5. Populate page metadata from CSV — attach structured fields to the corresponding converted Gowiki pages (identified by converting `pid` namespace paths)

This approach is preferred over a blind import script because:
- Column type mappings require semantic decisions, not mechanical mapping
- Each row is page-bound (`pid` = DokuWiki page path) — the target Gowiki page must exist and be correctly identified
- Schema-level config (visibility, validation, i18n labels) may not map 1:1

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

### Template variables

DokuWiki: `@!PAGE!@`

Found only in `templates/` namespace. Convert to Gowiki: `{{PAGE}}`.

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

`attic/` contains gzipped historical versions. **Out of scope for v1.** The importer only converts the current version (from `pages/`). Attic import can be added later if needed.

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

1. Copy all media files from `media/` to `content/` (preserving namespace structure)
2. Write converted `.md` files to `content/`
3. Parse DokuWiki metadata (serialized PHP in `.meta` files) and write `.json` to `meta/`
4. Generate conversion report

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
- **DokuWiki `templates/` namespace**: These are DokuWiki page templates. Convert to Gowiki `_template.md` convention: `templates/study.txt` -> `study/_template.md`. Template variables are converted (`@!PAGE!@` -> `{{PAGE}}`).

## Prerequisites

All inline marks needed for the import are implemented:
- `_underline_`, `~~strikethrough~~`, `~subscript~`, `^superscript^` -- all implemented and safe to import directly.
