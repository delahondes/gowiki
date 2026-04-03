# Flow Markers — Specification

## Motivation

Several features need precise, edit-resilient positioning within a document:
- **AI proposals**: referencing exact regions for replacement
- **Cursor restoration**: visual↔raw mode switch, draft resume
- **Collaborative editing**: remote cursor/selection indicators

Line numbers and character offsets break when the document is edited. Markers embedded in the markdown flow move with the text, surviving insertions and deletions around them.

## Syntax

### Point marker

```
{#id}
```

A zero-width position in the document. Used for cursor positions, insertion points.

### Range marker

```
{#id}content to mark{#/id}
```

A pair of markers delimiting a region. Used for AI proposals, selections.

### Subtypes

Markers use a prefix convention to distinguish use cases:

| Use case | Point (self-closing) | Range open/close | Prefix | Persisted on publish |
|---|---|---|---|---|
| AI proposal | `{#p1/}` | `{#p1}...{#/p1}` | bare | no |
| Cursor/caret | `{#@alice/}` | `{#@alice}...{#/@alice}` | `@` | no |
| Bookmark | `{#!intro/}` | — | `!` | **yes** |

- **Bare** (`{#id}`): ephemeral markers for AI proposals and internal use. Stripped on publish.
- **`@` prefix** (`{#@user}`): cursor/selection markers for collaborative editing and cursor sync. Stripped on publish.
- **`!` prefix** (`{#!name}`): user-facing bookmarks (anchors). Persisted on publish, renderable as link targets.

The self-closing form `{#id/}` is always a point marker (no matching close tag expected). The open form `{#id}` expects a matching `{#/id}`.

### Rules

- IDs must be alphanumeric + hyphens + dots: `[A-Za-z0-9._-]+` (after the optional prefix)
- IDs are unique within a document per subtype (no duplicate open/close pairs)
- Markers can appear anywhere inline content is valid (paragraphs, list items, table cells, headings)
- Markers cannot span across block boundaries (a range must start and end within the same block or set of contiguous inline content)
- Nested ranges are allowed: `{#a}text {#b}inner{#/b} text{#/a}`
- Unpaired open markers (open without close) are treated as point markers

## ProseMirror representation

Markers are **inline nodes** (not marks) with zero visual width:

```typescript
flow_marker: {
  inline: true,
  atom: true,
  attrs: {
    id: {},
    type: { default: "open" },  // "open", "close", or "point"
  },
  group: "inline",
  selectable: false,
  toDOM(node) {
    return ["span", {
      class: "gowiki-flow-marker",
      "data-marker-id": node.attrs.id,
      "data-marker-type": node.attrs.type,
    }]
  },
}
```

## Visual rendering

### Visual mode

A small colored indicator at the marker position:
- **AI markers**: thin vertical bar (2px wide, 1em tall) in indigo/purple, slightly translucent
- Hovering the marker highlights the full range (both markers + content between them)
- Clicking a marker scrolls the review panel to the corresponding proposal
- Markers do not affect text flow or layout (zero width in normal flow, the indicator is rendered via CSS `::before` pseudo-element or absolute positioning)

### Raw mode

Markers are shown as the literal `{#id}` / `{#/id}` text. They can be styled with CSS to appear as small colored badges, but remain editable text.

## AI integration

### Proposal flow

1. User triggers review mode
2. AI analyzes the page and returns proposals with `original` text (as today)
3. **Backend post-processing**: before sending proposals to the frontend, the backend locates each `original` in the markdown and wraps it with markers:
   ```
   {#p1}The datas shows{#/p1} the results
   ```
4. The modified markdown (with markers) is saved as the draft
5. Frontend receives proposals referencing marker IDs:
   ```json
   {"marker": "p1", "proposed": "The data show", "rationale": "..."}
   ```
6. Applying a proposal: replace content between `{#p1}` and `{#/p1}` with the proposed text, then remove the markers
7. Rejected proposals: markers are removed without changing content

### Review panel alignment

Each proposal in the review panel is vertically aligned with its corresponding marker in the editor:
- The panel scrolls in sync with the editor
- A thin connecting line (or just vertical alignment) links the proposal to its position
- Clicking a proposal scrolls the editor to the marker

### Verification

Since the backend places the markers by exact string match, verification happens at marker insertion time (server-side), not at apply time (client-side). If the `original` text is not found, no marker is placed and the proposal is flagged as unverifiable — same as today but earlier in the pipeline.

## Lifecycle

- **Created by**: AI backend (proposals), collab system (cursors), editor (cursor sync), user (bookmarks)
- **Ephemeral markers** (`{#id}`, `{#@user}`): persisted in draft only, stripped on publish
- **Persistent markers** (`{#!name}`): survive publish, stored in published content
- **Stripped on**: draft discard (all markers removed)

The publish path strips ephemeral markers only: `\{#/?[@]?[A-Za-z0-9._-]+/?\}` → empty string, but preserves `{#!...}` bookmark markers.

## Parser

A markdown-it inline rule that matches `{#id}` and `{#/id}`:

1. Detect `{#` at current position
2. Find closing `}`
3. Check if content after `#` starts with `/` (close marker) or not (open/point marker)
4. Emit a `flow_marker` inline token

The rule must be registered before the directive rule to avoid conflict (directives start with `{name` not `{#`).

## Serializer

The `flow_marker` node serializes back to `{#id}` or `{#/id}` based on the `type` attr. Point markers serialize as `{#id}`.

## Review persistence

When the user saves the draft (Ctrl+S), the review state is saved:
- The markdown (with markers) is saved as the draft content
- The proposal metadata (proposed text, rationale, accept/reject state per proposal) is saved as a JSON field alongside the draft

When the draft is resumed:
- Markers are parsed from the markdown → visual indicators appear
- Proposal metadata is loaded → review panel is restored with previous decisions

API change: `PUT /api/draft/{path}` accepts an optional `ai_review` JSON field. `GET /api/draft/{path}` returns it.

## Implementation plan

### Phase 1 — Parser + serializer + schema
- `flow_marker` inline node in ProseMirror schema
- Markdown-it inline rule for `{#id}` / `{#/id}`
- Serializer for `flow_marker` → `{#id}` / `{#/id}`
- Strip markers on publish
- Round-trip test

### Phase 2 — Visual rendering
- CSS for marker indicators (thin colored bars)
- Hover highlighting for ranges
- Click handler to scroll review panel

### Phase 3 — AI integration
- Backend: after AI returns proposals, locate `original` in markdown, insert markers
- Frontend: proposals reference marker IDs instead of original text
- Apply: replace content between markers, remove markers
- Reject: remove markers only

### Phase 4 — Review persistence
- Draft API: `ai_review` field for proposal metadata
- Frontend: save/restore review state on Ctrl+S / draft resume

### Phase 5 — Other uses (future)
- Cursor sync for visual↔raw switch
- Collab remote cursor positions
- Bookmarks
