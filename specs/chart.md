# Chart Plugin — Specification

## 1. Overview

The **Chart plugin** renders simple data charts (pie, bar, line, etc.) directly in the wiki using [Chart.js](https://www.chartjs.org/). Data is authored as `name = value` pairs inside a fenced block, following the same pattern as code blocks and spoilers.

### Design Goals

- Simple syntax for common chart types — no JSON configuration, no external service
- Client-side rendering via Chart.js (no network dependency, no rate limits)
- Consistent with Gowiki's architecture: rendering is a frontend concern, markdown is the ground truth
- Plugin boundary respected: disabling the plugin leaves the fenced block intact (rendered as a code block fallback)
- Round-trip safe: the `name = value` syntax is plain text, not parsed as markdown

### Non-Goals

- Complex multi-series charts (use a dedicated tool and embed as image)
- Interactive drill-down, zoom, animations beyond Chart.js defaults
- Server-side rendering or image export (may be added in a future milestone)

---

## 2. Markdown Syntax

### Basic form

```
```chart <type> [options...]
Label 1 = 10
Label 2 = 25
Label 3 = 15
```
```

The opening fence is `` ```chart `` followed by the chart type and optional parameters. The body contains one `name = value` pair per line. Blank lines and lines starting with `#` are ignored.

### Chart types

| Type | Chart.js type | Description |
|---|---|---|
| `pie` | `pie` | 2D pie chart (default) |
| `doughnut` | `doughnut` | Doughnut (ring) chart |
| `bar` | `bar` | Vertical bar chart |
| `hbar` | `bar` (horizontal) | Horizontal bar chart |
| `line` | `line` | Line chart |
| `radar` | `radar` | Radar/spider chart |
| `polar` | `polarArea` | Polar area chart |

Default type: `pie`.

### Options

Options appear after the type on the opening fence line, in any order:

| Option | Syntax | Default | Description |
|---|---|---|---|
| Size | `<width>x<height>` | `400x250` | Canvas size in pixels |
| Title | `"My Title"` | (none) | Chart title, displayed above the chart |
| Legend | `legend` / `nolegend` | `legend` | Show or hide the legend |
| Values | `values` | (hidden) | Display data values on the chart |
| Align | `left` / `center` / `right` | `center` | Horizontal alignment of the chart block |
| Colors | `#rrggbb` or `#rgb` | (auto) | Custom color palette — multiple colors can be specified |

### Examples

**Pie chart with title:**
```
```chart pie 350x200 "Fruit Distribution"
Apples = 30
Peaches = 23
Strawberries = 25
Peanuts = 7
```
```

**Horizontal bar with values displayed:**
```
```chart hbar 500x300 values "Sales by Region"
Europe = 45
Americas = 38
Asia = 52
Africa = 12
```
```

**Line chart with custom colors:**
```
```chart line 600x300 #2563eb #10b981
Q1 = 100
Q2 = 150
Q3 = 130
Q4 = 200
```
```

**Minimal (type defaults to pie, size defaults to 400x250):**
```
```chart
Yes = 75
No = 25
```
```

### Data format

- One entry per line: `Label = Value`
- Value must be a number (integer or decimal, negative allowed)
- Label is trimmed whitespace; value is trimmed and parsed as float
- Lines that don't match `<text> = <number>` are ignored (with a console warning)
- Blank lines and lines starting with `#` are silently skipped
- Minimum 1 data entry required; maximum 50 (soft limit, for rendering sanity)

---

## 3. ProseMirror Schema

### Node: `chart`

| Property | Value |
|---|---|
| `group` | `"block"` |
| `atom` | `true` |
| `attrs.type` | `string`, default `"pie"` |
| `attrs.width` | `number`, default `400` |
| `attrs.height` | `number`, default `250` |
| `attrs.title` | `string`, default `""` |
| `attrs.legend` | `boolean`, default `true` |
| `attrs.values` | `boolean`, default `false` |
| `attrs.align` | `string \| null`, default `null` (= `center`) |
| `attrs.colors` | `string`, default `""` (comma-separated hex colors) |
| `attrs.data` | `string`, default `""` (raw body text: the `name=value` lines) |

The node is **atomic** — the chart body is not editable inline. The data is stored as a raw string in the `data` attribute, not as ProseMirror content children. This avoids the complexity of making the `name = value` lines into PM paragraphs and back.

### toDOM

Renders a `<div class="gowiki-chart">` wrapper containing a `<canvas>` element. Chart.js draws on the canvas. In edit mode, a placeholder with the chart type and title is shown until the NodeView renders the actual chart.

### parseDOM

Parses `<div class="gowiki-chart">` and reads attrs from `data-*` attributes.

---

## 4. Markdown-it Rule

A block rule registered **before** `fence` (same pattern as spoiler):

1. Match opening fence: `` ```chart `` with optional type and options
2. Find closing fence: matching `` ``` ``
3. Extract body text (the `name = value` lines) as raw string
4. Emit a single `chart` token (nesting: 0) with all parsed attributes in `token.meta`

Unlike the spoiler plugin, the chart body is **not tokenized** as markdown — it's opaque data, stored verbatim.

---

## 5. Serializer (PM to Markdown)

```
```chart <type> [options...]
<body>
```
```

Options are serialized in a canonical order: size, title, legend/nolegend, values, align, colors. Only non-default options are emitted.

Canonical order ensures deterministic round-trip. Example:

```
```chart hbar 500x300 "Sales" values right #2563eb
Europe = 45
Americas = 38
```
```

---

## 6. NodeView (Editor)

The chart node uses a custom NodeView (like the include plugin):

- **View mode:** renders the Chart.js canvas with the actual chart
- **Edit mode:** renders the Chart.js canvas as a preview, selected via NodeSelection
- **Property panel:** exposes type, size, title, legend, values, align as editable properties
- **Data editing:** clicking "Edit data" in the property panel opens a small textarea overlay for editing the raw `name = value` lines; on confirm, updates the node's `data` attr

The NodeView sets `contentEditable = false` on the entire DOM to prevent ProseMirror from trying to edit inside the canvas.

### Chart.js Integration

- Chart.js is imported dynamically (`import('chart.js/auto')`) on first render to avoid loading the library until a chart node actually exists
- Each NodeView instance creates a Chart.js instance on its canvas
- The chart is re-created when attrs change (via the `update()` method)
- On destroy, the Chart.js instance is destroyed to prevent memory leaks

---

## 7. Default Color Palette

When no custom colors are specified, the plugin uses a built-in palette of 12 distinct colors, cycling if more data points exist:

```
#4e79a7, #f28e2b, #e15759, #76b7b2,
#59a14f, #edc948, #b07aa1, #ff9da7,
#9c755f, #bab0ac, #d37295, #a0cbe8
```

(Tableau 12 palette — colorblind-friendly, good contrast.)

Custom colors from the fence line override this palette. If fewer custom colors than data points, they cycle.

---

## 8. Styles

```css
.gowiki-chart {
  margin: 0.5em 0;
  /* align via margin-left/margin-right based on align attr */
}
.gowiki-chart canvas {
  max-width: 100%;
}
/* Edit mode: show a subtle border so the chart block is visible */
#app.gowiki-editing .gowiki-chart {
  border: 1px dashed #ccc;
  border-radius: 4px;
  padding: 4px;
}
```

---

## 9. Property Panel

When a chart node is selected in the editor, the property panel shows:

| Property | Control | Notes |
|---|---|---|
| Type | dropdown | pie, doughnut, bar, hbar, line, radar, polar |
| Width | text input | pixels |
| Height | text input | pixels |
| Title | text input | |
| Legend | checkbox | |
| Values | checkbox | |
| Align | dropdown | left, center, right |
| Colors | text input | comma-separated hex colors |

Changes update the node attrs immediately and re-render the chart.

---

## 10. Toolbar

A toolbar button (chart icon) inserts a chart node with default attrs and sample data:

```
```chart pie
Item 1 = 30
Item 2 = 50
Item 3 = 20
```
```

In raw mode, the raw text is inserted at the cursor. In visual mode, a chart node is created with the sample data.

---

## 11. Plugin Boundary

### What the plugin owns

- Chart node schema, NodeView, and Chart.js rendering
- Markdown-it block rule for `` ```chart `` fences
- PM-to-markdown serializer
- Property panel integration
- Toolbar button
- CSS styles
- Chart.js dependency (bundled with the plugin, not with core)

### What the plugin does NOT own

- Storage, document identity, access control
- Export/print rendering (future: may add canvas-to-PNG for print CSS)

### Disabling the plugin

If the chart plugin is disabled:
- The `` ```chart `` fence is not intercepted by the spoiler rule (it doesn't match)
- The standard fence rule picks it up and renders it as a code block with language "chart"
- The `name = value` data is visible as plain text — no data loss
- Re-enabling the plugin restores chart rendering

---

## 12. Dependencies

| Dependency | Version | Size (gzipped) | Notes |
|---|---|---|---|
| chart.js | ^4.x | ~60 KB | Dynamically imported, loaded only when a chart node exists |

Chart.js is bundled as part of the chart plugin's independent bundle (not in the core bundle), following the plugin architecture.

---

## 13. Decisions

1. **Chart.js over QuickChart.io** — Client-side rendering avoids external service dependency, works offline, no rate limits. QuickChart.io is just Chart.js behind an HTTP API anyway.

2. **Atomic node, not container** — The chart body is opaque data (`name = value` pairs), not markdown content. Using `atom: true` with a raw `data` string attribute avoids the complexity of making data lines into PM paragraph nodes. The trade-off is that data editing requires a dedicated UI (textarea overlay), not inline editing.

3. **Fenced block syntax** — Consistent with code blocks and spoilers. The alternative (property/directive syntax like `{chart type=pie}`) would require storing data in the directive, which is awkward for multi-line data. The fenced approach keeps the data visually clear and naturally extends the existing fence pattern.

4. **No multi-series support** — Single data series keeps the syntax simple. Users needing complex charts should use a dedicated charting tool and embed the result as an image.

5. **Dynamic import for Chart.js** — The library is ~60 KB gzipped. Loading it only when a chart node exists avoids penalizing pages without charts.

6. **Soft limit of 50 data points** — Rendering more than 50 slices/bars is visually useless. The plugin warns but doesn't hard-reject.
