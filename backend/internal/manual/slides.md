# Slides

The Slides plugin turns markdown content into a fullscreen slideshow presentation. Slides are written inside a fenced block and separated by `---`.

## 1. Basic syntax

````
```slides "My Presentation"
# Welcome

---

# Agenda
- Topic 1
- Topic 2

---

# Thank You
```
````

The opening fence is `` ```slides `` followed by optional parameters. Each slide is separated by `---` on its own line. Slide content is standard Gowiki markdown (headings, lists, bold, italic, images, tables, code blocks, etc.).

## 1. Options

Options appear after `slides` on the opening fence line, in any order:

| Option | Syntax | Default | Description |
| --- | --- | --- | --- |
| Title | `"My Presentation"` | (none) | Displayed on the placeholder card and as an overlay at the start |
| Theme | `dark` or `light` | `light` | Color theme |
| Ratio | `16:9` or `4:3` | `16:9` | Aspect ratio |
| Background | `background=./image.png` | (none) | Background image for all slides (cover, centered) |

Only non-default options need to be specified. Order does not matter.

## 1. Examples

**Dark theme, 4:3 ratio:**

````
```slides "Technical Overview" dark 4:3
# Architecture

---

# Components
- Frontend (TypeScript)
- Backend (Go)
- Storage (Markdown)
```
````

**With background image:**

````
```slides "Q1 Review" dark background=./slide-bg.png
# Q1 Revenue

---

# Highlights
- Revenue up 15%
```
````

**Minimal (all defaults):**

````
```slides
# Slide One

---

# Slide Two
```
````

## 1. Presenting

In both view and edit mode, the slides block appears as a card with the title, slide count, and a **Present** button.

Click **Present** to enter fullscreen presentation mode.

### Keyboard navigation

| Key | Action |
| --- | --- |
| Right arrow, Space, Enter | Next slide |
| Left arrow, Backspace | Previous slide |
| Home | First slide |
| End | Last slide |
| Escape | Exit presentation |
| Click anywhere | Next slide |

A progress bar and slide counter appear at the bottom and fade after 2 seconds of inactivity.

## 1. Editing

In the visual editor, click the slides card to select it. The property panel lets you edit:
- **Title** — presentation title
- **Theme** — light or dark
- **Ratio** — 16:9 or 4:3
- **Background** — path to a background image
- **Data** — the raw slide content (markdown with `---` separators)

In raw mode, edit the fenced block directly.

## 1. Slide content

Each slide supports standard Gowiki markdown:

- Headings (`#` through `######`)
- Paragraphs with line breaks
- Bold, italic, underline, strikethrough, inline code
- Unordered and ordered lists (including nested)
- Images
- Code blocks with syntax highlighting
- Tables
- Blockquotes

{blockquote class=note}
> Keep slides concise. Content that overflows the slide area is hidden during presentation.

## 1. Toolbar

Use the slides toolbar button to insert a new presentation with sample content. In raw mode, the fenced block text is inserted at the cursor position.

## 1. Plugin behavior

If the slides plugin is disabled, the `` ```slides `` block falls back to a regular code block — the slide content is displayed as plain text. No data is lost. Re-enabling the plugin restores presentation functionality.
