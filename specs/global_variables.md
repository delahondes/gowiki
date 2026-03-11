# Global Variables — Specification

## 1. Overview

Global variables are inline placeholders written as `{{NAME}}` that resolve to live values at render time. Unlike database template variables (`{{fieldname}}` inside a `{database-row}` context), global variables use **ALL_CAPS** names and are available on every page, regardless of context.

Global variables are **not** template-creation substitutions — they remain as `{{NAME}}` in the stored markdown and are resolved every time the page is rendered. The same page viewed at different times may show different values (e.g., version info changes after a save).

### Design Goals

- Familiar syntax: reuses the existing `{{...}}` inline node mechanism
- Runtime resolution: values computed at render time, not stored
- No backend changes for page-scoped variables (ID, PATH, PAGE, TITLE)
- Backend provides version metadata and author identity via existing API endpoints
- All-caps convention cleanly separates global variables from database field variables

### Non-Goals

- Custom user-defined global variables (use database fields for that)
- Server-side rendering of variables (resolution is a frontend concern)
- Variable assignment or computation (`{{X + Y}}`)
- Current-user variables (see §8, decision 6)

---

## 2. Syntax

Same as existing template variables: `{{NAME}}` inline in any text context.

Global variables are distinguished from database template variables by their **ALL_CAPS** name. The resolver checks global names first; if not matched, falls back to database field resolution as today.

### Fallback syntax

`{{NAME:default text}}` — shell-like default value. If the variable resolves to an empty string, the fallback text is displayed instead. Works for both global and database variables.

```markdown
Contact: {{AUTHORMAIL:no email provided}}
```

The fallback is stored as a `fallback` attribute on the `template_var` node. Serialized back as `{{NAME:fallback}}`.

### Naming convention

| Pattern | Meaning |
|---|---|
| `{{ALLCAPS}}` | Global variable — resolved by the global variable system |
| `{{lowercase}}` or `{{mixed.case}}` | Database template variable — resolved from `{database-row}` field context |

---

## 3. Variable catalog

### Page variables

Derived from the current page path. No backend call needed — the frontend already knows the page path. All paths start with `/`.

| Variable | Description | Example value |
|---|---|---|
| `{{ID}}` | Full page ID (path) | `/docs/setup/install` |
| `{{PATH}}` | Namespace path (parent of the page) | `/docs/setup` |
| `{{PAGE}}` | Page name: last segment, underscores → spaces | `install` |
| `{{TITLE}}` | First heading of the current page | `Installation Guide` |

### Link variables

Derived from the current page path + server location.

| Variable | Description | Example value |
|---|---|---|
| `{{EXTID}}` | Full external URL to the current page | `https://wiki.example.com/docs/setup/install` |
| `{{EXTPATH}}` | Full external URL to the current namespace | `https://wiki.example.com/docs/setup/` |
| `{{SERVER}}` | Server hostname | `wiki.example.com` |

### Version variables

Derived from the page metadata returned by `GET /api/pages/{path}` (the `meta` object: `version`, `updated_at`, `author`).

| Variable | Description | Example value |
|---|---|---|
| `{{VERSION}}` | Current page version number | `42` |
| `{{VERSIONDATE}}` | Version date in `YYYY-MM-DD` format | `2026-03-11` |
| `{{VERSIONTAG}}` | Version tag from reviewflow (if any) | `v2.1` |
| `{{YEAR}}` | Version year (YYYY) | `2026` |
| `{{MONTH}}` | Version month (MM, zero-padded) | `03` |
| `{{SMONTH}}` | Version month (no leading zero) | `3` |
| `{{DAY}}` | Version day (DD, zero-padded) | `11` |
| `{{SDAY}}` | Version day (no leading zero) | `11` |

Note: DATE/MONTH/DAY are based on the **page version date** (last save), not the current wall-clock time. This is deliberate — the values are stable and reproducible, not time-dependent.

### Creation variables

Derived from page metadata. The creation date is the timestamp of the first version (version 1) of the page. Requires `created_at` to be stored in page metadata (alongside the existing `created_by`).

| Variable | Description | Example value |
|---|---|---|
| `{{CREATIONDATE}}` | Page creation date in `YYYY-MM-DD` format | `2026-01-15` |

### Author variables

Two sets: the page **creator** (first version) and the **last editor** (current version).

| Variable | Description | Example value |
|---|---|---|
| `{{AUTHOR}}` | Username of the page creator | `jdoe` |
| `{{AUTHORNAME}}` | Display name of the page creator | `Jane Doe` |
| `{{AUTHORMAIL}}` | Email address of the page creator | `jane@example.com` |
| `{{LASTAUTHOR}}` | Username of the last editor | `bsmith` |
| `{{LASTAUTHORNAME}}` | Display name of the last editor | `Bob Smith` |
| `{{LASTAUTHORMAIL}}` | Email address of the last editor | `bob@example.com` |

- **AUTHOR** = creator of the page (author of version 1). This is the "owner" — useful for responsibility markers, signatures, and footers.
- **LASTAUTHOR** = author of the current version. Useful for "last edited by" footers and audit trails.
- Display names and emails are resolved via the user display API (`/api/users/display`).

### Wiki variables

Derived from site configuration (`/api/site/info`).

| Variable | Description | Example value |
|---|---|---|
| `{{WIKI}}` | Wiki title as configured in `site.title` | `Acme Corp Wiki` |
| `{{WIKIVERSION}}` | Gowiki software version string | `0.4.0` |

---

## 4. Resolution

### Resolution order

When rendering a `{{NAME}}` node:

1. If `NAME` is all-uppercase (matches `/^[A-Z_]+$/`): look up in the global variable table
2. Otherwise: fall back to database template variable resolution (existing behavior)

### Resolution context

The resolver needs access to:

| Data | Source | Already available in frontend? |
|---|---|---|
| Page path | URL / navigation state | Yes |
| Page metadata (version, updated_at, author) | `GET /api/pages/{path}` response | Yes (`page.meta`) |
| Page creator (author of version 1) | Changelog or page metadata | **No — needs backend addition** |
| User display info (name, email for authors) | `/api/users/display` | Yes |
| Site config (title) | `GET /api/site/info` response | Yes (fetched at startup) |
| First heading | ProseMirror document | Yes (walk the doc) |
| Software version | Build-time constant or API | Needs to be exposed |
| Version tag | Reviewflow state | Needs API addition or inclusion in page meta |

### Backend additions needed

1. **Creator in page metadata**: add `created_by` field to `PageMetadata`. Populated from the first changelog entry, or stored directly at page creation time.
2. **Version tag in page metadata**: include the reviewflow version tag in the `meta` object of the `GET /api/pages/{path}` response.
3. **Software version in site info**: add `"version"` to the `/api/site/info` response.

### Missing data and error handling

Three resolution states for any variable:

| State | Meaning | Rendering |
|---|---|---|
| **Resolved** | Variable is known, has a non-empty value | Display the value (plain text) |
| **Empty** | Variable is known but has no value (e.g., no version tag, no email) | If fallback is set, display fallback text. Otherwise, display nothing (invisible) |
| **Error** | Variable name is unrecognized (unknown ALL_CAPS global, or unresolved database field with no fallback) | Yellow error chip: `ERR: {{NAME}}` |

The error state uses a `gowiki-template-var-error` CSS class (yellow background, bold text), visually matching the error rendering for broken formulas and other syntax errors.

---

## 5. Implementation approach

### Frontend: global variable resolver

A resolver function takes a variable name and the current context, returns the resolved string:

```typescript
interface GlobalVarContext {
  pagePath: string
  pageMeta: {
    version: number
    updated_at: string
    author: string        // last editor
    created_by: string    // page creator
    version_tag?: string  // from reviewflow
  } | null
  authorDisplay: { display_name: string; email: string } | null   // creator
  lastAuthorDisplay: { display_name: string; email: string } | null // last editor
  siteInfo: { title: string; version: string } | null
  doc: PMNode | null  // for TITLE extraction
}

function resolveGlobalVar(name: string, ctx: GlobalVarContext): string | null
```

Returns `null` if the name is not a recognized global variable (→ fall back to database resolution). Returns `""` if recognized but data unavailable.

### Integration with existing template_var

The existing `template_var` NodeView in `database.ts` currently resolves variables from the database row field context. The change:

1. In the NodeView's render method, check if the variable name is all-caps
2. If yes, call `resolveGlobalVar()` instead of looking up database fields
3. If `resolveGlobalVar()` returns `null` (not a global var), fall back to existing behavior

This keeps the change minimal — no new node type, no parser changes, no schema changes.

---

## 6. Rendering

### View mode

Global variables are resolved and displayed as plain text — indistinguishable from surrounding content. No special styling.

### Edit mode (visual)

The `template_var` NodeView shows the variable name in a styled chip (existing behavior: `{{NAME}}`). Global variables could optionally show their resolved value as a tooltip.

### Edit mode (raw)

Variables appear as literal `{{NAME}}` text. No resolution in raw mode.

### Print / PDF

Variables are resolved at render time, so printed output shows resolved values.

---

## 7. Examples

### Footer with version info

```markdown
Last updated: {{VERSIONDATE}} by {{LASTAUTHORNAME}} (v{{VERSION}})
```

Renders as: *Last updated: 2026-03-11 by Bob Smith (v42)*

### Page ownership

```markdown
Author: {{AUTHORNAME}} ({{AUTHORMAIL}})
Last edited by: {{LASTAUTHORNAME}}
```

Renders as:
*Author: Jane Doe (jane@example.com)*
*Last edited by: Bob Smith*

### Page header with breadcrumb

```markdown
{{PATH}} / **{{PAGE}}**
```

Renders as: */docs/setup / **install***

### Wiki branding

```markdown
Powered by {{WIKI}} — Gowiki {{WIKIVERSION}}
```

Renders as: *Powered by Acme Corp Wiki — Gowiki 0.4.0*

### Fallback values

```markdown
Contact: {{AUTHORMAIL:no email on file}}
Tagged: {{VERSIONTAG:untagged}}
```

If the author has no email configured, renders as: *Contact: no email on file*
If no version tag exists, renders as: *Tagged: untagged*

### Error on unknown variable

```markdown
Page created by {{AUHTOR}}
```

Renders as: *Page created by* **ERR: \{\{AUHTOR\}\}** (yellow chip — typo detected)

---

## 8. Decisions

1. **All-caps convention** — Cleanly separates global variables from database field variables without a syntax change. Database fields are typically lowercase or camelCase (`{{email}}`, `{{start_date}}`). Global variables are `{{ID}}`, `{{VERSION}}`, etc. The resolver checks caps first.

2. **Version date, not current date** — Dokuwiki's `@DATE@` returns the current wall-clock date, which makes pages non-reproducible (different output each day). Using the version date is stable and meaningful — "when was this page last saved?" If users need today's date, that's a different feature (dynamic content, out of scope).

3. **Frontend-only resolution** — All data needed for global variables is already available or easily added to existing frontend state (page path, page meta, user display info, site info, document). No new backend endpoint is needed, only minor enrichment of existing responses. This keeps the backend clean of editor semantics.

4. **Three-state resolution** — Unknown variables show a yellow `ERR` chip (syntax error). Known variables with no data render invisibly (or show a fallback if `{{NAME:default}}` syntax is used). This distinguishes typos from legitimately empty values.

5. **No new node type** — Global variables reuse the existing `template_var` inline node. The only change is in the NodeView resolution logic. This avoids schema changes, parser changes, and serializer changes.

6. **No current-user variables** — Dokuwiki's `@USER@`, `@NAME@`, `@MAIL@`, `@ALIAS@` return the currently logged-in user. This made sense for template-based page creation (stamp the creator at creation time). In Gowiki's runtime-replacement model, these would show a different value to every reader, which is rarely useful ("Hello {{NAME}}" is awkward). Instead, author variables (`AUTHOR`/`LASTAUTHOR`) answer the meaningful questions: "who created this page?" and "who last edited it?" Current-user variables can be added later if a concrete use case emerges.

7. **AUTHOR vs LASTAUTHOR** — Two distinct author identities mapped to the page's lifecycle. AUTHOR is the creator (version 1 author) — the page's "owner." LASTAUTHOR is the current version's author — the last editor. Both have NAME and MAIL variants resolved via the user display API. This requires a small backend addition: storing `created_by` in page metadata.

8. **Consistent naming** — Dokuwiki's `@NS@`/`@NSL@`/`@PAGEL@` are inconsistently named. Renamed to `PATH` (namespace path), `EXTPATH` (external URL to namespace), `EXTID` (external URL to page) for clarity. The `EXT` prefix consistently means "full external URL."

9. **Paths include leading slash** — All path variables (`ID`, `PATH`, `EXTID`, `EXTPATH`) include the leading `/`, consistent with Gowiki's path convention where paths always start with `/`.

10. **Dropped from Dokuwiki** — `@ALIAS_@`/`@ALIAS-@` (custom separator variants), `@VERR@`/`@VERN@` (mapped to clearer names `VERSIONDATE`/`VERSIONTAG`).
