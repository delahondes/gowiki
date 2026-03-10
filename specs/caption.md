# Caption Plugin — Specification

## Motivation

Academic and technical documents number their figures and tables ("Figure 1", "Table 3") and cross-reference them from the body text ("as shown in Figure 1"). Dokuwiki's caption plugin achieves this with XML-like `<figure>` / `<table>` wrapper elements — a syntax incompatible with Gowiki's Markdown dialect and its no-raw-HTML rule.

This plugin provides the same functionality through the existing property/directive system: a `caption` attribute on image and table nodes, automatic numbering, and an inline reference syntax for cross-links.

## Core Principles

1. **Captions are node properties, not wrapper elements.** A caption is an attribute on an image or table node, not a separate enclosing node. This avoids adding new block-level container types to the schema.

2. **Numbering is automatic and document-order.** Figures (images with captions) and tables (tables with captions) are numbered in two independent sequences. The numbers are computed at render time from document order — they are never stored.

3. **Labels are optional.** A captioned figure or table may have a `label` for cross-referencing. Labels must be unique within a page.

4. **Cross-references are inline.** A reference to a label renders as a clickable link showing the resolved number ("Figure 2", "Table 1").

5. **Round-trip safe.** Caption text is stored as a single-line property value. Inline formatting is supported via the dialect's inline syntax (`*italic*`, `**bold**`, `` `code` ``). Newlines within captions are not supported — captions are single-paragraph.

6. **Plugin boundary respected.** The plugin owns caption rendering, numbering, and reference resolution. Disabling it causes captions and references to disappear from rendering but never corrupts the Markdown source or breaks editing.

## Markdown Syntax

### Image caption

Captions are added via the image properties directive (the `{image ...}` line before the image paragraph):

```
{image caption="The ultimate pink cuddly shell." label=fig:kuschel}
![cuddle tanks](./kuschelpanzer.jpg)
```

The `caption` value is a quoted string. Inline Markdown is allowed inside:

```
{image caption="**Bold title.** Additional description with *emphasis*." label=fig:results}
![results chart](./results.png)
```

### Table caption

Captions are added via the existing `{table ...}` directive:

```
{table caption="Sales data by quarter, 2025." label=tab:sales}
| Quarter | Revenue | Growth |
|---------|---------|--------|
| Q1      | 120k    | +5%    |
| Q2      | 135k    | +12%   |
```

### Cross-references

A new inline syntax references a label:

```
As shown in {ref fig:kuschel}, the cuddly shell is pink.
See {ref tab:sales} for the quarterly breakdown.
```

`{ref label}` resolves to the figure/table number at render time.

### Counter control (optional, low priority)

To reset or set a counter:

```
{setcounter figure=5}
{setcounter table=0}
```

This is a standalone directive on its own line, like other `{...}` directives. Only `figure` and `table` counters are supported.

## Property Definitions

### On `image` node

| Property  | Type   | Default | Description |
|-----------|--------|---------|-------------|
| `caption` | string | `null`  | Caption text (inline Markdown allowed) |
| `label`   | string | `null`  | Cross-reference label, must be unique per page |

### On `table` node

| Property  | Type   | Default | Description |
|-----------|--------|---------|-------------|
| `caption` | string | `null`  | Caption text (inline Markdown allowed) |
| `label`   | string | `null`  | Cross-reference label, must be unique per page |

### `ref` inline node

| Attribute | Type   | Description |
|-----------|--------|-------------|
| `label`   | string | Target label to resolve |

### `setcounter` directive (optional)

| Attribute | Type | Description |
|-----------|------|-------------|
| `figure`  | int  | Reset figure counter to this value |
| `table`   | int  | Reset table counter to this value |

## Rendering

### Figures (images with captions)

An image with a `caption` attribute is wrapped in a `<figure>` element:

```html
<figure class="gowiki-figure" id="fig:kuschel">
  <img src="./kuschelpanzer.jpg" alt="cuddle tanks">
  <figcaption class="gowiki-caption">
    <span class="gowiki-caption-number">Figure 1:</span>
    <span class="gowiki-caption-text">The ultimate pink cuddly shell.</span>
  </figcaption>
</figure>
```

The `id` attribute is set to the label value, making the figure a URL anchor (e.g. `page#fig:kuschel`). This is best-effort — no collision checking with heading anchors.

The caption is placed **below** the image (standard convention for figures).

If the image has an `align` property (center, left, right), the `<figure>` inherits the same alignment.

### Tables with captions

A table with a `caption` attribute wraps the table in a `<figure>` and prepends a `<figcaption>`:

```html
<figure class="gowiki-table-figure" id="tab:sales">
  <figcaption class="gowiki-caption">
    <span class="gowiki-caption-number">Table 1:</span>
    <span class="gowiki-caption-text">Sales data by quarter, 2025.</span>
  </figcaption>
  <table>...</table>
</figure>
```

The caption is placed **above** the table (standard convention for tables).

### Cross-references

`{ref fig:kuschel}` renders as:

```html
<a class="gowiki-ref" href="#fig:kuschel" title="The ultimate pink cuddly shell.">Figure 1</a>
```

The `href` targets the `id` anchor on the `<figure>` element, so clicking scrolls to the figure/table. The tooltip shows the caption text. If the label cannot be resolved (typo, deleted figure), the reference renders as:

```html
<span class="gowiki-ref gowiki-ref--broken">??</span>
```

### Numbering rules

1. Walk the document in node order.
2. Each image node with a non-null `caption` increments the figure counter.
3. Each table node with a non-null `caption` increments the table counter.
4. `{setcounter}` directives reset the relevant counter.
5. Numbering is recomputed on every render — never persisted.
6. References resolve by scanning all captioned nodes for the matching label.

### Abbreviation setting

A global configuration option controls whether captions use abbreviated or full labels:

| Setting          | Abbreviated | Full      |
|------------------|-------------|-----------|
| `caption_style`  | "Fig. 1:"  | "Figure 1:" |
|                  | "Tab. 1:"  | "Table 1:" |

Default: full. The setting applies page-wide (or site-wide via config).

## ProseMirror Integration

### Schema changes

**No new block nodes.** The `<figure>` wrapper is a rendering-time construct only. In the ProseMirror document model, the image and table nodes gain `caption` and `label` attributes — the schema is unchanged structurally.

**New inline node: `caption_ref`**

```
caption_ref: {
  attrs: { label: { default: "" } },
  inline: true,
  group: "inline",
  atom: true
}
```

Rendered as an inline pill/badge showing "Figure N" or "Table N" (or "??" if unresolved).

### Property panel

When an image or table has a `caption` attribute set, the property panel shows:

- **Caption**: text input (wide, single-line)
- **Label**: text input (short, for cross-reference ID)

These appear alongside existing properties (size, align, etc. for images; column settings for tables).

### Visual editor behavior

1. **Caption display**: In visual/view mode, captioned images show the `<figcaption>` below the image. Captioned tables show it above. The caption text is **read-only in view mode** (edited via the property panel in edit mode).

2. **Reference insertion**: A toolbar button or command inserts a `{ref ...}` node. A dropdown or autocomplete lists available labels in the document.

3. **Numbering preview**: In edit mode, figure/table numbers update live as nodes are reordered.

### Markdown parser

- `{image caption="..." label=...}` → sets `caption` and `label` attributes on the image node.
- `{table caption="..." label=...}` → sets `caption` and `label` attributes on the table node.
- `{ref label}` → creates a `caption_ref` inline node.
- `{setcounter figure=N table=M}` → handled as a render-time directive (no persistent node, or a lightweight atom node that serializes back identically).

### Markdown serializer

- Image with caption: `{image caption="..." label=...}` directive line before `![alt](src)`.
- Table with caption: `{table caption="..." label=...}` in the table directive.
- `caption_ref` node: serializes to `{ref label}`.
- `setcounter`: serializes to `{setcounter figure=N}`.

## CSS

```css
.gowiki-figure {
  margin: 1em 0;
}

.gowiki-table-figure {
  margin: 1em 0;
}

.gowiki-caption {
  font-size: 0.9em;
  color: #333;
  padding: 4px 0;
}

.gowiki-caption-number {
  font-weight: bold;
  margin-right: 0.3em;
}

.gowiki-ref {
  color: inherit;
  text-decoration: none;
  border-bottom: 1px dotted #666;
}

.gowiki-ref:hover {
  border-bottom-style: solid;
}

.gowiki-ref--broken {
  color: #c00;
  border-bottom-color: #c00;
}
```

## Escaping and edge cases

### Caption value escaping

Since caption text is a property value in `{...}` directives, double quotes inside the caption must be escaped:

```
{image caption="He said \"hello\" loudly."}
```

The existing property parser's string escaping rules apply.

### Caption without label

A figure/table can have a caption without a label. It still gets numbered but cannot be cross-referenced.

### Label without caption

A label without a caption is ignored — no numbering, no reference target.

### Duplicate labels

If two nodes share the same label, the first in document order wins. The second is treated as if it has no label. A warning may be shown in the property panel.

### Cross-page references

Out of scope for v1. References only resolve within the same page. Cross-page figure references may be added later.

### Include zones

Figures/tables inside included content participate in the numbering sequence of the host page. Their labels are resolvable from the host page. If the same included page appears in multiple host pages, the numbers will differ — this is correct and expected.

## Implementation order

1. **Schema attributes**: add `caption` and `label` to image and table node specs.
2. **Directive parsing**: extend `{image ...}` and `{table ...}` parsers to extract `caption` and `label`.
3. **Serialization**: emit `caption=` and `label=` in directives.
4. **Property panel**: add Caption and Label fields to image and table property definitions.
5. **Render-time numbering**: walk document, compute counters, inject `<figure>` + `<figcaption>` wrappers.
6. **`caption_ref` inline node**: schema, parser (`{ref label}`), serializer, render with resolved number.
7. **Reference autocomplete**: toolbar button / command for inserting references.
8. **`setcounter` directive** (optional): parse, serialize, apply during numbering walk.
9. **CSS and polish**.

## Out of scope

- Cross-page references
- List of figures / list of tables (could be a future query)
- Captions on code blocks (can be added later with the same pattern)
- Multi-paragraph captions (single-paragraph only)
- Export-specific numbering (PDF, print) — uses the same render-time numbering
