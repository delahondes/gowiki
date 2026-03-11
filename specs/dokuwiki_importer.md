# DokuWiki Importer

## Goal

Import the complete data folder of an existing DokuWiki site into a Gowiki site. The importer is a **one-time CLI tool** — not an ongoing service. It reads `backend/olddata/` and writes into `data/content/` and `data/meta/`.

The target is to convert **>=90% of all lines** of all documents automatically. Lines that cannot be converted are preserved as-is with a marker comment and reported in a summary.

## Scope exclusion

**Struct/data plugin migration is excluded.** The DokuWiki struct plugin stores structured data in SQLite (`meta/struct.sqlite3`) with 20+ schemas. This requires custom, manual migration outside the general importer. Struct-related syntax (`---- struct table ----`, `---- struct lookup ----`, `{{$schema.field}}`, `@@schema.field@@`) is flagged in the report but not converted.

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
- `{{page>ns:page&firstseconly&noreadmore}}` -> `{include path=/ns/page}` (firstseconly changes content scope -- **flag** for manual review)
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

Important: on pages that also contain a `{reviewflow}`, ACK todos (and any other todos) should remain **inactive until the reviewflow is fully validated**. This is a behavioral constraint for the Gowiki todo/reviewflow interaction, not something the importer itself enforces -- but the importer should preserve the co-location of reviewflow and ACK on the same page so this behavior can be applied.

### TODO (3 pages)

DokuWiki: `<todo @user #user:YYYY-MM-DD>Task description</todo>`

Gowiki: `{todo title="Task description" assign="user" due=YYYY-MM-DD}`

3 pages -- low volume. Parse the DokuWiki todo attributes and map to Gowiki's todo directive properties.

### WRAP (67 pages, ~220 occurrences)

DokuWiki's WRAP plugin provides styled containers. Most common patterns:

| Pattern | Count | Conversion |
|---|---|---|
| `<WRAP>...</WRAP>` (bare) | 61 | Drop wrapper, keep content |
| `<WRAP center round important 60%>` | 25 | Convert to blockquote: `> **Important:** content` |
| `<WRAP center round info 60%>` | ~15 | Convert to blockquote: `> **Info:** content` |
| `<WRAP center round tip 60%>` | 7 | Convert to blockquote: `> **Tip:** content` |
| `<WRAP group>` + `<WRAP half column>` with image | ~14 | Image+text side-by-side: convert to image with `wrap=left` or `wrap=right` property |
| `<WRAP group>` + `<WRAP half column>` without image | ~11 | Flatten to sequential blocks. Flag. |
| `<WRAP round white spacedx2>` | 18 | Drop wrapper, keep content |
| `<WRAP prewrap>` | 7 | Drop wrapper, keep content |

Strategy:
- **Admonition wraps** (info, important, tip): Convert to blockquote with bold prefix. Not perfect but preserves intent.
- **Image+text layout wraps** (group + half column containing an image): Convert to Gowiki image with `wrap` property for text wrapping.
- **Other layout wraps** (group, half column without image, flexcenter): Flatten to sequential content. Flag.
- **Bare wraps / styling wraps**: Drop `<WRAP>` / `</WRAP>` tags, keep content.
- **Wraps inside table cells**: Extract content only, strip tags.

### Figure (37 pages, ~121 occurrences)

DokuWiki:
```
<figure>
{{.:pasted:20250924-074417.png?700}}
<caption>Description text</caption>
</figure>
```

Gowiki: Convert to image with caption as alt text.
```
{image size=700x}
![Description text](./pasted/20250924-074417.png)
```

- Single image in figure: extract image + caption, convert to Gowiki image with alt text.
- Multiple images in figure (e.g. `{{img1.png}} {{img2.png}}`): convert each image separately. Flag for review.

### PDFNS (9 pages)

DokuWiki: `~~PDFNS>namespace:path|Display Name~~`

PDF export namespace targeting -- **no Gowiki equivalent**. Drop. Flag.

### NOCACHE / NOTOC (24 pages)

DokuWiki: `~~NOCACHE~~`, `~~NOTOC~~`

Rendering directives -- no Gowiki equivalent. Drop silently (no flag needed -- these have no content impact).

### Changes widget (1 page -- sidebar)

DokuWiki: `{{changes>ns=-sidebar}}`

Gowiki: `{changes}` (the Gowiki changes plugin). Direct mapping. The `ns=-sidebar` filter is DokuWiki-specific -- Gowiki excludes sidebar/footer by default.

### Struct/data (excluded from scope)

The following are **flagged but not converted**:
- `---- struct table ----` blocks (29 pages)
- `---- struct lookup ----` blocks (5 occurrences)
- `{{$schema.field}}` variable references (49 pages)
- `@@schema.field@@` bureaucracy variables (2 pages)

These require custom manual migration.

### Template variables

DokuWiki: `@!PAGE!@`

Found only in `templates/` namespace. Convert to Gowiki: `{{PAGE}}`.

## Conversion not attempted (flag only)

| Feature | Pages | Reason |
|---|---|---|
| Struct/data blocks and variables | ~50 | Excluded from scope -- custom migration |
| Interwiki links (`[[wp>...]]`) | rare | Not supported in Gowiki |
| `firstseconly` in includes | ~10 | Gowiki has no partial-include |
| PDFNS directives | 9 | No Gowiki equivalent |
| WRAP column layout (non-image) | ~11 | Flattened, no multi-column in Gowiki |

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
