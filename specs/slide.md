# Slides Plugin — Specification (Phase 1)

## 1. Overview

The **Slides plugin** turns fenced markdown content into a fullscreen slideshow presentation. Slides are separated by `---` and contain standard Gowiki markdown. A "Present" button launches the presentation.

### Design Goals

- Simple syntax: fenced block with `---` separators, regular markdown inside each slide
- Lightweight custom presentation engine — zero external dependencies
- Consistent with Gowiki architecture: markdown is ground truth, rendering is a frontend concern
- Plugin boundary respected: disabling the plugin leaves the fenced block as a code block fallback
- Phase 1 scope: atomic node, basic presentation mode, no inline slide editing

### Non-Goals (Phase 1)

- Inline slide editing in the visual editor (data editing via property panel textarea)
- Slide reordering via drag-and-drop
- Speaker notes
- Slide transitions or animations
- Plugin-specific nodes (charts, includes, spoilers) rendered inside slides
- Export to PDF/PPTX
- Presenter view (current + next slide + timer)

---

## 2. Markdown Syntax

### Basic form

```
```slides [options...]
# Welcome

---

# Agenda
- Topic 1
- Topic 2

---

# Thank You
```
```

The opening fence is `` ```slides `` followed by optional parameters. The body contains slide content separated by `---` on its own line. Each slide's content is standard Gowiki markdown.

### Slide separator

- `---` on a line by itself (leading/trailing whitespace trimmed)
- Content before the first `---` is the first slide
- Content after the last `---` is the last slide
- Empty slides (no non-blank content between separators) are skipped
- A trailing `---` after the last slide is allowed and ignored

### Options

Options appear after `slides` on the opening fence line, in any order:

| Option | Syntax | Default | Description |
|---|---|---|---|
| Title | `"My Presentation"` | (none) | Shown in the placeholder card and as an overlay at presentation start |
| Theme | `dark` / `light` | `light` | Color theme for the presentation |
| Ratio | `16:9` / `4:3` | `16:9` | Aspect ratio of the slide area |
| Background | `background=./image.png` | (none) | Background image applied to all slides (cover, centered) |

### Examples

**Basic presentation:**
```
```slides "Q1 Review"
# Q1 Revenue Review
Finance Team — March 2026

---

# Highlights
- Revenue up 15%
- New markets opened
- Customer satisfaction at 92%

---

# Questions?
Thank you for your attention
```
```

**Dark theme, 4:3:**
```
```slides dark 4:3 "Technical Overview"
# System Architecture

---

# Components
- Frontend (TypeScript/ProseMirror)
- Backend (Go)
- Storage (Markdown)
```
```

**With background image:**
```
```slides "Q1 Review" dark background=./slide-bg.png
# Q1 Revenue Review

---

# Highlights
- Revenue up 15%
```
```

**Minimal (defaults to light, 16:9, no title):**
```
```slides
# Slide One

---

# Slide Two
```
```

### Slide content

Each slide supports standard Gowiki markdown:

- Headings (`#` through `######`)
- Paragraphs with hard line breaks
- Bold, italic, underline, strikethrough, inline code
- Unordered and ordered lists (including nested)
- Images (rendered at their natural or specified size)
- Code blocks with syntax highlighting
- Tables
- Blockquotes
- Horizontal rules (within a slide, `---` must be distinguished from the slide separator — see §4)

Plugin-specific nodes (charts, includes, spoilers) inside slides are **not rendered** in Phase 1. They appear as their raw fenced-block text.

---

## 3. ProseMirror Schema

### Node: `slides`

| Property | Value |
|---|---|
| `group` | `"block"` |
| `atom` | `true` |
| `attrs.title` | `string`, default `""` |
| `attrs.theme` | `string`, default `"light"` |
| `attrs.ratio` | `string`, default `"16:9"` |
| `attrs.background` | `string`, default `""` (path to background image) |
| `attrs.data` | `string`, default `""` (raw body: all slides separated by `---`) |

Atomic node — same pattern as chart. The slide content is stored as a raw string in the `data` attribute, not as ProseMirror content children.

### toDOM

Renders a `<div class="gowiki-slides">` wrapper containing a preview placeholder (title, slide count, "Present" button). Data stored in `data-*` attributes for parseDOM round-trip.

### parseDOM

Parses `<div class="gowiki-slides">` and reads attrs from `data-*` attributes.

---

## 4. Markdown-it Rule

A block rule registered **before** `fence` (same pattern as chart and spoiler):

1. Match opening fence: `` ```slides `` with optional options
2. Find closing fence: matching `` ``` ``
3. Extract body text as raw string (not tokenized as markdown)
4. Emit a single `slides` token (nesting: 0) with parsed attributes in `token.meta`

The body is opaque data, stored verbatim — same approach as chart.

### Slide separator vs. horizontal rule

Inside the fenced block, `---` on its own line is **always** a slide separator. There is no ambiguity because the body is not parsed as markdown by the block rule — parsing happens later, per-slide, at render time. If a user wants a horizontal rule within a slide, they can use `***` or `___` (though this is an edge case for Phase 1).

---

## 5. Serializer (PM to Markdown)

```
```slides [options...]
<body>
```
```

Options serialized in canonical order: title, theme, ratio, background. Only non-default options are emitted.

Canonical order: `"title"`, `dark`/`light` (omit if `light`), `4:3`/`16:9` (omit if `16:9`), `background=path` (omit if empty).

Examples:
- All defaults: `` ```slides ``
- Title only: `` ```slides "My Talk" ``
- Dark + 4:3: `` ```slides dark 4:3 ``
- Everything: `` ```slides "My Talk" dark 4:3 ``
- With background: `` ```slides "My Talk" background=./bg.png ``

---

## 6. NodeView (Editor)

### Placeholder card

In both edit and view mode, the slides node renders as a card:

```
┌──────────────────────────────────┐
│         🎬 Slides                │
│     "Q1 Revenue Review"         │
│         5 slides                 │
│                                  │
│       [ ▶ Present ]              │
└──────────────────────────────────┘
```

- Icon (slides icon) and label
- Title (if set) or "Untitled Presentation"
- Slide count (parsed from body)
- "Present" button — launches fullscreen presentation

In edit mode, selected via NodeSelection; property panel for editing attributes.

### Property panel

| Property | Control | Notes |
|---|---|---|
| Title | text input | |
| Theme | dropdown | light, dark |
| Ratio | dropdown | 16:9, 4:3 |
| Background | text input | Path to background image (e.g. `./slide-bg.png`) |
| Data | multiline textarea | Raw slide content with `---` separators |

Changes update the node attrs immediately.

---

## 7. Presentation Engine

A minimal custom engine, ~200 lines of JS/CSS. No external library. Activated by clicking the "Present" button.

### Lifecycle

1. Parse: split `data` on `---` lines, filter out empty slides
2. Render: each slide's markdown → HTML via a markdown-it instance
3. Build: create the fullscreen overlay DOM
4. Enter: `element.requestFullscreen()` (fallback: fixed-position overlay)
5. Navigate: keyboard/mouse events cycle through slides
6. Exit: leave fullscreen, remove overlay from DOM, clean up listeners

### Slide rendering

Each slide's markdown fragment is rendered to HTML using a **standalone markdown-it instance** configured with Gowiki's core dialect rules (hard breaks, ATX headings, etc.). Plugin-specific markdown-it rules are not loaded in Phase 1 — only standard block/inline elements render.

### Fullscreen overlay structure

```html
<div class="gowiki-slides-overlay theme-light">
  <div class="gowiki-slides-viewport">
    <div class="gowiki-slides-slide">
      <!-- rendered HTML for current slide -->
    </div>
  </div>
  <div class="gowiki-slides-progress"></div>
  <div class="gowiki-slides-counter">3 / 10</div>
</div>
```

### Slide layout

- The viewport is centered in the screen
- The slide area maintains the chosen aspect ratio (16:9 or 4:3)
- Content is sized to fill the slide area using CSS `transform: scale()` — compute scale factor from viewport dimensions vs. a reference content area (e.g. 960×540 for 16:9)
- Base font size is large for projection readability (~2.5em effective)
- Content overflow is hidden (slides should be concise)
- If a `background` image is set, it is applied to every slide via CSS `background-image` with `background-size: cover` and `background-position: center`

### Keyboard navigation

| Key | Action |
|---|---|
| Right arrow, Space, Enter | Next slide |
| Left arrow, Backspace | Previous slide |
| Escape | Exit presentation |
| Home | First slide |
| End | Last slide |
| Click (anywhere) | Next slide |

### Progress indicator

- Thin progress bar at the bottom edge (width = currentSlide / totalSlides)
- Slide counter "N / M" in the bottom-right corner
- Both fade after 2 seconds of inactivity, reappear on mouse movement or key press

### No animations

Slide transitions are instant. No fade, no slide-in. Consistent with Gowiki's approach (same decision as chart animations).

---

## 8. Styles

```css
/* --- Placeholder card --- */
.gowiki-slides {
  margin: 0.5em 0;
  border: 1px solid #ddd;
  border-radius: 6px;
  padding: 1.2em;
  text-align: center;
  background: #f8f9fa;
  cursor: default;
}
#app.gowiki-editing .gowiki-slides {
  border: 1px dashed #ccc;
}
.gowiki-slides-title {
  font-weight: 600;
  margin: 0.3em 0;
}
.gowiki-slides-info {
  color: #666;
  font-size: 0.9em;
}
.gowiki-slides-present-btn {
  display: inline-block;
  margin-top: 0.6em;
  padding: 0.4em 1.4em;
  border: none;
  border-radius: 4px;
  background: #4e79a7;
  color: #fff;
  font-size: 0.9em;
  cursor: pointer;
}
.gowiki-slides-present-btn:hover {
  background: #3d6a96;
}

/* --- Fullscreen overlay --- */
.gowiki-slides-overlay {
  position: fixed;
  inset: 0;
  z-index: 10000;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.gowiki-slides-overlay.theme-light {
  background: #ffffff;
  color: #1a1a1a;
}
.gowiki-slides-overlay.theme-dark {
  background: #1a1a1a;
  color: #f0f0f0;
}

/* --- Viewport and slide --- */
.gowiki-slides-viewport {
  position: relative;
  overflow: hidden;
}
.gowiki-slides-slide {
  padding: 2em 3em;
  font-size: 1.5em;
  line-height: 1.5;
}
.gowiki-slides-slide h1 { font-size: 2em; margin: 0.3em 0; }
.gowiki-slides-slide h2 { font-size: 1.5em; margin: 0.3em 0; }
.gowiki-slides-slide h3 { font-size: 1.2em; margin: 0.3em 0; }
.gowiki-slides-slide ul, .gowiki-slides-slide ol {
  margin: 0.5em 0;
  padding-left: 1.2em;
}
.gowiki-slides-slide li { margin: 0.3em 0; }
.gowiki-slides-slide img { max-width: 80%; max-height: 60vh; }
.gowiki-slides-slide pre {
  background: #f4f4f4;
  padding: 0.8em;
  border-radius: 4px;
  font-size: 0.7em;
  overflow-x: auto;
}
.gowiki-slides-overlay.theme-dark .gowiki-slides-slide pre {
  background: #2d2d2d;
}
.gowiki-slides-slide table {
  border-collapse: collapse;
  margin: 0.5em auto;
}
.gowiki-slides-slide th, .gowiki-slides-slide td {
  border: 1px solid #ccc;
  padding: 0.3em 0.6em;
}
.gowiki-slides-overlay.theme-dark .gowiki-slides-slide th,
.gowiki-slides-overlay.theme-dark .gowiki-slides-slide td {
  border-color: #555;
}

/* --- Progress and counter --- */
.gowiki-slides-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  height: 3px;
  background: #4e79a7;
  transition: width 0.15s ease;
}
.gowiki-slides-counter {
  position: absolute;
  bottom: 0.5em;
  right: 1em;
  font-size: 0.75em;
  opacity: 0.6;
  transition: opacity 0.3s;
}
.gowiki-slides-overlay.controls-hidden .gowiki-slides-counter {
  opacity: 0;
}
.gowiki-slides-overlay.controls-hidden .gowiki-slides-progress {
  opacity: 0;
}
```

---

## 9. Toolbar

A toolbar button (slides icon) inserts a slides node with sample content:

```
```slides
# Title Slide

---

# Slide 2

---

# Thank You
```
```

In raw mode, the raw text is inserted at the cursor. In visual mode, a slides node is created with the sample data.

---

## 10. Plugin Boundary

### What the plugin owns

- Slides node schema, NodeView, and presentation engine
- Markdown-it block rule for `` ```slides `` fences
- PM-to-markdown serializer
- Property panel integration
- Toolbar button
- CSS styles
- Presentation engine (zero external dependencies)

### What the plugin does NOT own

- Storage, document identity, access control
- Rendering of plugin-specific nodes inside slides (Phase 2)
- Export to PDF/PPTX

### Disabling the plugin

If the slides plugin is disabled:

- The `` ```slides `` fence is not intercepted by the slides rule
- The standard fence rule picks it up and renders it as a code block with language "slides"
- The slide content (markdown with `---` separators) is visible as plain text — no data loss
- Re-enabling the plugin restores presentation functionality

---

## 11. Dependencies

None. The presentation engine is pure JS/CSS.

| Dependency | Version | Size | Notes |
|---|---|---|---|
| (none) | — | — | Custom engine, no external library |

---

## 12. Phase 2 (Future)

- **Rich editor:** inline slide preview, per-slide editing panel, slide reordering
- **Plugin nodes:** charts, tables, spoilers rendered inside slides using the full Gowiki rendering pipeline
- **Speaker notes:** syntax TBD (possibly `???` or `Notes:` separator within a slide), separate presenter view
- **Presenter view:** dual-screen — current slide on projector, next slide + notes + timer on presenter screen
- **Custom styling:** per-presentation CSS overrides or named themes beyond light/dark
- **PDF export:** render slides to paginated PDF
- **Slide transitions:** optional, subtle (fade or instant, no flying animations)
- **Incremental reveal:** syntax for bullet-by-bullet reveal (e.g. `>- item`)

---

## 13. Decisions

1. **Custom engine over reveal.js** — reveal.js is ~300 KB gzipped, opinionated about DOM structure and CSS, and pulls in styles that would conflict with Gowiki. A custom engine is ~200 lines of JS, zero dependencies, and gives full control over the presentation UX. The trade-off is fewer features, but Phase 1 only needs basic slide navigation.

2. **Atomic node, not container** — The body contains multiple slides, each an independent markdown fragment. Making each slide a separate PM node would require a complex nested schema (`slides > slide > block+`), with the `---` separator needing its own schema treatment. Storing as raw text in a `data` attr keeps it simple and avoids schema complexity. The trade-off is no inline editing — data editing is via property panel textarea (Phase 1).

3. **Fenced block syntax** — Consistent with chart and spoiler. The `---` separator is unambiguous inside the fenced block because the body is opaque text (not parsed as markdown at the block-rule level).

4. **Standalone markdown-it for slide rendering** — Each slide's content is rendered to HTML via a markdown-it instance at presentation time, not via ProseMirror. Creating PM editor instances per slide would be heavyweight and unnecessary for read-only presentation. The trade-off is that plugin-specific nodes don't render in Phase 1.

5. **No animations** — Instant slide transitions. Consistent with the chart plugin's `animation: false` decision. Animations add complexity, cause rendering timing issues (as seen with Chart.js PDF), and don't suit a wiki presentation tool.

6. **Fullscreen API with fallback** — Primary mode uses the browser Fullscreen API for true fullscreen. Falls back to a fixed-position overlay for browsers that block fullscreen (e.g. iframe embeds). Both paths use the same DOM structure and styles.
