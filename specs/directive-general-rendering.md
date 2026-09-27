# Directive rendering — general principles

Every `{directive}` in Gowiki has two visual surfaces: a **rendering box** (what the reader sees on the page) and a **property panel** (how the author edits the directive's attributes). This spec codifies how both should look and behave so every plugin — existing and future — presents a consistent, calm surface.

## 1. The rendering box

**Optional.** Some directives render nothing (e.g. `{tag=foo}` shows plain tag text; `{version-link ...}` shows just the link). When a directive DOES render a block, follow the rules below.

**Purpose.** The box tells the READER what the directive is doing on the page — in the reader's terms, not the directive's terms. It never exposes internal attribute names or template plumbing.

**Look.** Discreet. The wiki is the star; the box supports it. Use one of three status colours, chosen for the meaning of the current state:

| Colour | When to use | Header colour | Status-line colour |
|---|---|---|---|
| **Grey / dull** | Passive reporting; nothing has happened; informational | Same greyish tint | Muted grey |
| **Light pale red** | A negative state that warrants attention (rule fires, error, missing prerequisite) | Same red tint | Red |
| **Light pale green** | A positive state confirming the directive succeeded (validated, all good, complete) | Same green tint | Green |

**Anatomy.** Two elements, both terse:

1. A **short status line** in the header colour — one phrase, ≤~60 chars. Present tense. Reader-facing. Example: `All documents are within lifecycle rules` (green), `12 pages need review` (red), `Rule inactive — no matching pages` (grey).
2. An **optional black-on-neutral explanation** below the status — one or two lines of plain text explaining *why*. Kept humble; no bold, no emoji, no directive attributes echoed literally. Skip entirely when the status line is self-explanatory.

**Never.** Do not put the raw attributes in the box (that's the property panel's job). Do not use yellow — yellow is reserved for the property panel and mixing them confuses authors into thinking the read-only rendering is editable. Do not overload the box with icons, badges, or borders beyond the thin one-colour border that carries the status.

**Live data.** When the rendering needs backend context (a count of pages currently affected, a validation state, a fetched status), the NodeView fetches it on mount. Show `grey` while loading, then switch to the resolved colour once the response lands. A fetch failure falls back to `grey` with a status like `Status unavailable`.

## 2. The property panel

**Look.** Pale yellow background, thin subtle border. The yellow is the panel's semantic marker — nothing else in the editor uses that colour, so authors learn "yellow = edit this directive."

**When it appears.** NEVER in view mode. In visual edit mode, on any of these:

- The caret enters the directive's rendering box and the author clicks the properties icon (top-left icon in the edit toolbar).
- The directive is freshly inserted via the toolbar button (opens the panel automatically so the author can fill in required fields immediately).
- The author tries to type text while the caret is on the rendering box — since the box is atomic and non-editable, we interpret the keystroke as "I want to edit this" and open the panel.

The panel **disappears** as soon as the caret leaves the directive's node. Authors don't have to close it manually.

**Focus mode.** The panel shows only the attributes that currently have a non-default value — this keeps the panel short and directs attention to what's actually in use. A **`+` button** in the panel header expands to show every attribute the directive accepts, defaults included, so the author can add a new one. Freshly inserted directives are the one exception: they start expanded so nothing is hidden while the author is still deciding what to set.

**Help text.** Each attribute in the panel carries a one-line `helpText` describing the format (e.g. `"Comma-separated. Page must carry AT LEAST ONE"`). Concise, no examples beyond one, no marketing.

## 3. The insert-toolbar button

Every insertable directive registers one command (typically `<name>:insert`) that lands a fresh instance at the caret and opens the property panel. The toolbar renders these commands as icons in edit mode.

**Both modes.** The button must work in both visual and raw edit modes. In visual mode it inserts the PM node and opens the panel. In raw mode it inserts the equivalent Markdown directive text at the caret and (ideally) parks the caret on the required-attribute line so the author can start typing immediately.

**Grouping.** Related directives cluster (e.g. `template`, `template-title`, `template-stamp`, `template-reviewflow` share one toolbar slot). See `frontend/main.js`'s registration-capture pattern.

## 4. Rendering-box wording — the passive-first rule

Default to a **calm, passive status** when the rule/directive is doing its job. Only escalate to red when there's actually something wrong. This means most rendering boxes should be grey most of the time.

Examples:

| Directive | Grey (passive) | Red (something wrong) | Green (achieved) |
|---|---|---|---|
| `{reviewflow}` | `Draft — awaiting review` | `Overdue: reviewer, validator` | `Validated 1.2 · 2026-01-15` |
| `{lifecycle}` | `Rule active — no pages currently flagged` | `12 pages need review` | *(not typically green — a lifecycle rule doesn't have an "achieved" state)* |
| `{database-query}` | *(no rendering box — it draws the table itself)* | | |
| `{include}` | *(shows the included content directly)* | `Included page not found: /foo` | |

If in doubt: prefer grey over red. Red is a signal, and signals lose meaning when they're constant.

## 5. Round-trip preservation

Whatever the rendering box shows (or doesn't show), the underlying directive's markdown MUST round-trip losslessly through the visual editor. Rendering is a projection of the state; it never modifies the source. This is the same bijectivity invariant that governs the rest of the dialect — see `roundtrip.md`.
