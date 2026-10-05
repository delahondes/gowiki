# Templates

Three kinds of templates in Gowiki. They share the same `{template-*}` directive family and the same variable resolver, but they differ in how a new page gets *attached* to the template:

- **Implicit templates** — files named `_template*.md`. Picked up by filename convention when a new page is created in the namespace. No action required on the user's part.
- **Explicit templates** — any page carrying a `{template}` directive. Surfaced by a **Create document** button rendered on the template itself. The user clicks the button, names the destination, and the wiki copies the resolved payload into a new page.
- **Row-bound page templates** — pointed at by a database table's `page_folder` + `page_template_path`. Applied to the page auto-created on every row insert. Documented in detail in [Database](./database); the parts of this page about variables and `{template-*}` directives still apply.

The resolver that substitutes `{template-title}`, `{template-stamp}`, `{template-reviewflow}` and `{template-todo}` is the same in all three paths — one dialect rule across the whole system: a `{template-*}` directive has a defined meaning wherever it is parseable, whether a sibling `{template}` marker is present or not.

## 1. Implicit templates (`_template*.md`)

An implicit template is a regular page whose filename starts with `_template`, stored inside a namespace directory. The wiki pre-fills the editor with the template's content whenever a user starts a new page in that namespace.

Three naming shapes are supported:

| Filename | Behaviour |
| --- | --- |
| `_template.md` | **Default** template — applied to every new page in the namespace (or any sub-namespace) that has no closer default. |
| `_template1.md`, `_templatefoo.md` | **Unconstrained alternative** — always applies. The suffix after `_template` is just a label shown in the picker ("1", "foo"). |
| `_template_sop.md`, `_template_meeting.md` | **Constrained alternative** — applies only to pages whose name starts with the slug (case-insensitive). `_template_sop.md` matches `sop01`, `sopnew`, but not `ins01`. The underscore after `_template` is the constraint marker. |

The slug after `_template_` can contain further underscores (e.g. `_template_foo_bar.md` → constrained to pages starting with `foo_bar`).

Examples:

- `content/regulatory/qms/_template.md` — default template for `/regulatory/qms/` and everything under it
- `content/regulatory/qms/_template_sop.md` — appears only for pages starting with `sop`
- `content/regulatory/qms/_template_ins.md` — appears only for pages starting with `ins`
- `content/regulatory/qms/_templatemeeting.md` — always appears in the picker as "meeting"

### 1. Using an implicit template

When you navigate to a page that doesn't exist yet, the wiki collects every `_template*.md` that applies to that path (walking up the namespace tree) and filters the constrained ones by the filename-prefix rule.

- **One match** → applied silently (same as before).
- **Several matches** → a picker modal opens with one row per template, plus a **"Blank page"** row. Click a row to start the editor with that content.
- **No match** → you get a blank page, as before.

Resolution walks up the tree and takes the **closest** version of each slug. A default defined in `/foo/bar/_template.md` overrides the root `/_template.md` only for pages under `/foo/bar/`.

Templates are hidden from the sitemap, orphan detection, and recent changes. They **are** indexed by full-text search so you can still find them via the search bar.

### 1. Pre-fill content vs resolved directives

Two behaviours depending on whether the `_template*.md` carries any `{template-*}` directive:

- **No template directive** → the new page's draft starts out as a byte-for-byte copy of the template. This is the DokuWiki-compatible path and the right choice for a boilerplate snippet that just needs to be edited by hand.
- **At least one `{template-*}` directive** (title, stamp, reviewflow, todo) → the resolver runs the same way as on an explicit template: titles substituted, stamps frozen in, `{template-reviewflow}` becomes `{reviewflow}`, `{template-todo}` becomes `{todo}`. The template file itself stays untouched.

An implicit template with `{template-*}` directives is also the layout the **row-bound template** path expects. The row insertion flow treats the template file as a row-bound template whenever it lives at the table's `page_template_path`.

The reviewflow validation gate from the explicit-template path applies here too: if the implicit template carries a `{reviewflow}` of its own and it's not fully validated, the pre-fill is refused rather than silently issuing an un-reviewed document.

## 1. Explicit templates (`{template}` directive)

An explicit template is any page carrying a `{template}` directive on its own line. The directive renders as a **Create document** button; clicking the button opens a dialog that asks for a destination path and title, offers optional reviewflow overrides, and refuses upfront when the template's own reviewflow isn't fully validated.

Explicit templates can live anywhere in the wiki — the filename convention of `_template*.md` is for implicit templates. An explicit template under `/qms/campaigns/ca-pattern` is a perfectly ordinary page that happens to carry a `{template}` directive; it is itself editable, versioned, and reviewable like any other page.

The five directives:

| Directive | Where it lives | Fate at creation |
| --- | --- | --- |
| `{template}` | Template only | Not copied. Marks where the copiable payload begins, and carries the **Create document** action. Above the marker: tracking metadata that stays on the template (its own `{reviewflow}`, its own tags). Below: the payload that gets copied. |
| `{template-title}` | Template and document | Prefixes the heading that becomes the document's title. Resolved to that heading, with the pattern replaced by the user's completed title. |
| `{template-stamp}` | Template and document | Replaced by the origin sentence (`Created from template [Title](/path?v=N), version 1.0`). |
| `{template-reviewflow …}` | Template only | Replaced by `{reviewflow …}` in the created document — actors default to the template's own `{reviewflow}`, version defaults to `1.0`. |
| `{template-todo …}` | Template only | Replaced by `{todo …}` in the created document — args carry through verbatim, with one creation-time rewrite: a relative `due=+N[d\|m\|y]` resolves to an absolute `YYYY-MM-DD` based on creation time (see below). The template itself never fires the task. Use it for distribution lists ("`{template-todo action=read assign=@ops}`") that must trigger on every derived document but stay inert on the pattern. |

Programmatic callers get the same guarantee via the MCP `create_page_from_template` tool.

**Pinned destination — `{template target=…}`.** A template can pin the namespace its instances land in by adding `target=/some/path/{{slug}}` to the marker. When set, the create dialog pre-fills the destination field with the pattern and locks it (read-only). `{{title}}` and `{{slug}}` expand at creation time against the title the author types; `{{slug}}` is the lowercased, ASCII-folded, hyphenated form (e.g. "Alpha Beta" → `alpha-beta`). The MCP `create_page_from_template` tool refuses any caller-supplied `path` that doesn't match the resolved target — so a template that decides "instances live under `/qms/campaigns/`" carries that rule with it, whether the caller is a human clicking the dialog or an agent calling the tool.

### 1. Behaviour we rely on

- **The version is frozen at creation.** The `?v=N` link in the stamp points at the template's page-version *at that moment*. A document from 2024 keeps announcing the form as it stood in 2024, no matter what the template does afterwards. The reviewflow VERSIONTAG is used when present (`, version 1.0`); a template without a reviewflow falls back to the raw page revision (`, revision N`) — the wording deliberately differs so the two numbers don't look alike.
- **Templates without a reviewflow are fine.** Some registers issue documents without a review-flow cycle; `{template}`, `{template-title}` and `{template-stamp}` work normally in that case, and `{template-reviewflow}` is simply omitted.
- **`{template-todo}` replaces the "escape and un-escape" trap for distribution lists.** A template that needs every derived document to notify a group (`{todo action=read assign=@ops}`) used to force the author into escaping the directive (`\{todo …\}`) so it wouldn't fire against the template itself, then remembering to un-escape it on every copy — which nobody did. Wrap it as `{template-todo action=read assign=@ops}` instead: the template stays inert, every derived document carries a live `{todo}` from the moment it's created. Several `{template-todo}` lines can coexist on one template (multi-step flow like read → acknowledge → validate), and their order is preserved end-to-end.
- **Relative `due=` dates resolve at creation time.** A `due=` value that starts with `+` is a creation-time offset, not a stored literal. The resolver computes `creation-time + offset` and writes the absolute `YYYY-MM-DD` into the created document — frozen, never re-computed on re-render. Syntax: `due=+N`, `due=+Nd` (same thing — days), `due=+Nm` (months), `due=+Ny` (years). Absolute dates like `due=2027-12-31` pass through unchanged; only the leading-`+` form triggers the rewrite. Example: `{template-todo action=review due=+365}` on a template, created on 2026-10-05, produces `{todo action=review due=2027-10-05}` in the new document.

### 1. Migrating an existing hand-copy template

Templates in a QMS that follow the hand-copy convention today can be converted mechanically:

1. Replace the horizontal rule that separates the tracking block from the payload with `{template}`.
2. Replace the hand-written "Source template: …" block with `{template-stamp}`.
3. Replace the escaped `\{reviewflow …\}` on the payload side with `{template-reviewflow}` (arguments are optional — defaults inherit the template's own reviewflow).
4. Add `{template-title}` on the line just above the payload's H1 heading.
5. Replace any escaped `\{todo …\}` (the distribution-list workaround) with `{template-todo …}` carrying the same args — the created document then gets a live `{todo}` without the template itself firing.

Nothing in the resulting template needs maintaining by hand: version numbers, actor lists, origin sentences and distribution tasks are all derived at creation time from the template's current state.

## 1. Row-bound page templates

A database table whose row-bound page definition points at a template file (via the table's `page_template_path` field) gets the resolver applied to every row-bound page the database auto-creates on insert. This is covered end-to-end in [Database](./database); the gotchas specific to templates in this role:

- **Do NOT put `{template}` on a row-bound template.** The marker would render a Create document button on the template page, which would mislead a user into clicking "Create" to add a row — but rows get created via the database UI, not via the button. The `{template-*}` directives still resolve without `{template}` thanks to the "any template-family directive engages the resolver" rule.
- **`{template-stamp}` works.** The row-bound page gets the standard origin sentence at insert time, with `?v=N` pointing at the template file's page-version at that moment — same freezing guarantee as the explicit-template path.
- **`{template-todo}` works.** Each row's page carries any `{template-todo}` from the template as a live `{todo}`.
- **`{template-title}` and `{template-reviewflow}` are generally ignored on this path.** The row supplies the title (via `page_title_field` on the table), and row-bound pages typically carry no reviewflow of their own — the table's own validation workflow owns review for row data.

## 1. Variables (shared across all three kinds)

Templates can include **global variables** that are resolved at render time (not at creation time). These use the `{{NAME}}` syntax with ALL_CAPS names:

```markdown
Page: {{PAGE}}
Author: {{AUTHOR}}
Created: {{DATE}}
```

Global variables remain as `{{NAME}}` in the stored markdown and update dynamically every time the page is viewed.

A fallback value can be specified with a colon: `{{AUTHORMAIL:no email provided}}`.

### 1. Global variable reference

**Page variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{ID}}` | Full page path | `/docs/setup/install` |
| `{{PATH}}` | Parent namespace path | `/docs/setup` |
| `{{PAGE}}` | Page name (last segment) | `install` |
| `{{TITLE}}` | Page title (from first heading) | `Installation Guide` |

**Link variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{SERVER}}` | Server hostname | `wiki.example.com` |
| `{{EXTID}}` | Full external URL to the page | `https://wiki.example.com/docs/install` |
| `{{EXTPATH}}` | Full external URL to the namespace | `https://wiki.example.com/docs/` |

**Version variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{VERSION}}` | Page version number | `42` |
| `{{VERSIONDATE}}` | Last modified date (YYYY-MM-DD) | `2026-03-18` |
| `{{VERSIONTAG}}` | Reviewflow version tag | `2.1` |
| `{{YEAR}}` | Year of last modification | `2026` |
| `{{MONTH}}` | Month (zero-padded) | `03` |
| `{{SMONTH}}` | Month (short, no padding) | `3` |
| `{{DAY}}` | Day (zero-padded) | `18` |
| `{{SDAY}}` | Day (short, no padding) | `18` |
| `{{CREATIONDATE}}` | Page creation date (YYYY-MM-DD) | `2026-01-15` |

**Author variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{AUTHOR}}` | Page creator (login) | `alice` |
| `{{AUTHORNAME}}` | Page creator (display name) | `Alice Martin` |
| `{{AUTHORMAIL}}` | Page creator (email) | `alice@example.com` |
| `{{LASTAUTHOR}}` | Last editor (login) | `bob` |
| `{{LASTAUTHORNAME}}` | Last editor (display name) | `Bob Wilson` |
| `{{LASTAUTHORMAIL}}` | Last editor (email) | `bob@example.com` |

**Wiki variables:**

| Variable | Description | Example |
| --- | --- | --- |
| `{{WIKI}}` | Site title | `Acme Wiki` |
| `{{WIKIVERSION}}` | Gowiki software version | `0.9.5` |

### 1. Database-bound variables

Templates are especially powerful with database-bound pages. When a page is linked to a database table row, **lowercase** template variables like `{{fieldname}}` resolve from the database row's fields:

```markdown
Patient: {{patient_name}}
Visit date: {{visit_date}}
```

These are described in detail in [Database](./database).
