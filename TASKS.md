# v0.1 Task Breakdown

Three capability areas remain to reach v0.1, plus one UX gap identified during planning. Areas 1, 2, 3, and 4 are independent of each other and can be worked in parallel within each area.

---

## Area 1: Sidebar/Footer Composition

Sidebar and footer are regular Markdown pages at well-known paths. The existing `GET /api/pages/{path}` endpoint serves them with no backend changes. The frontend fetches them separately and mounts read-only ProseMirror views.

### Task 1.1 — Establish sidebar/footer storage convention

- Canonical paths: `data/content/sidebar.md` and `data/content/footer.md`
- Confirm the existing backend GET/PUT works for these paths (no path sanitization that would block them)
- Create placeholder `sidebar.md` and `footer.md` files in `data/content/` as initial content for development

No code changes; convention and scaffold task.

### Task 1.2 — HTML/CSS layout

Files: `frontend/index.html`, `frontend/style.css`

- The existing `#left` div is the sidebar mount point — no new div needed
- Add a `#footer` mount point below `#content` if not already present
- Implement CSS layout: left sidebar column, main content area, footer strip below content
- Sidebar and footer should be visually distinct from main content (greyed background or border)
- Layout must not break the current editor/view in `#content`

### Task 1.3 — Fetch and render sidebar/footer as read-only views

Files: `frontend/main.js`

- At page bootstrap, after the main page fetch, make two additional `GET /api/pages/sidebar` and `GET /api/pages/footer` requests
- If 404, skip silently (zone stays hidden)
- For each: compile the returned Markdown using the existing registry pipeline and mount a ProseMirror `EditorView` with `editable: () => false`
- Read-only views must use the same schema (full plugin set) so tables, images, etc. render correctly
- These views are never saved; they are purely display

### Task 1.4 — Editing-mode visual treatment of sidebar/footer

Files: `frontend/style.css`, `frontend/main.js`

- When the main editor is in edit mode, sidebar and footer read-only zones show a greyed-out / reduced-opacity treatment clearly marking them as non-editable context
- When in view mode, they render normally
- Implement as a CSS class toggled on the layout root when edit mode is active

**Dependency order:** 1.1 → 1.2 → 1.3 → 1.4

---

## Area 2: Include Rendering as Read-Only Zones

The directive parser and properties system are already in place. An include block uses the existing `{name key=value}` directive syntax as a self-contained block (not a prefix to another block): `{include path=/path/to/page}`. The frontend fetches included content via the existing `GET /api/pages/{path}` endpoint — no backend changes needed.

### Task 2.1 — Define the include syntax in the dialect

Spec/design task:

- Canonical syntax: `{include path=/path/to/page}` on its own line (a self-contained directive block, not a prefix to another block)
- The `include` node has one required attribute: `path` (absolute or relative, following the same resolution rules as links)
- The serializer must produce exactly `{include path=...}` with no variation
- Update `markdown_dialect_spec.md` with this definition

### Task 2.2 — Include plugin: ProseMirror schema node

New file: `frontend/plugins/include.ts`

- Define an `include` block node: `atom: true` (leaf, no content), attrs `{ path: { default: "" } }`
- Register via the plugin registry pattern (reference `frontend/plugins/table.ts` as the canonical example)
- The node's `toDOM` renders a placeholder showing the include path when content is not yet loaded

### Task 2.3 — Include plugin: Markdown parser

In `frontend/plugins/include.ts`:

- The directive parser already handles `{name key=value}` lines. Hook into it: when directive name is `include`, emit an `include` PM node with the `path` attribute rather than applying it to the next block
- This requires understanding how `frontend/compiler/markdown_to_pm.ts` dispatches self-contained vs. prefix directives — may need a minor extension to the directive dispatch mechanism

### Task 2.4 — Include plugin: Markdown serializer

In `frontend/plugins/include.ts`:

- `include` node → `{include path=<path>}` followed by a blank line (block-level)
- Must be bijective: parse → serialize → parse must be identity

### Task 2.5 — Include plugin: content resolution and read-only rendering

In `frontend/plugins/include.ts`, implement a ProseMirror NodeView:

- On mount, fetch `GET /api/pages/{node.attrs.path}` to retrieve the included page's Markdown
- Compile with the registry pipeline and render as a nested, non-editable ProseMirror view (same pattern as sidebar/footer in Task 1.3)
- Show a loading state while fetching, and an error state if the page is not found
- Visual treatment: grey border/background marking it as included, non-editable content

### Task 2.6 — Include plugin: properties panel integration

In `frontend/plugins/include.ts`:

- Register a properties panel entry for the `include` node using the existing `frontend/compiler/core_ui.ts` properties system
- Expose `path` as an editable field in the panel
- On path change, re-fetch and re-render the included content

### Task 2.7 — Wire include plugin into the plugin loader

File: `frontend/plugins/index.ts`

- Import and export the include plugin alongside table, image, blockquote
- Confirm it loads cleanly with no schema conflicts

**Dependency order:** 2.1 → 2.2 → 2.3 → 2.4; then 2.2+2.3+2.4 → 2.5 → 2.6 → 2.7

---

## Area 3: New Page Creation UX

The current flow is: navigate to a URL → 404 → frontend silently loads hardcoded default Markdown → page is implicitly created on first save. There is no "you are creating a new page" affordance, no path validation, and no way to navigate to a new page path from within the UI.

### Task 3.1 — Backend: namespace constraint validation on PUT

File: `backend/internal/storage/pages.go`

- In the `Put()` method, before writing, check whether a directory at the resolved path already exists (which would produce `ns.md` alongside `ns/`, violating the spec)
- Return a 409 Conflict with a clear error message if the constraint is violated

### Task 3.2 — Frontend: "new page" visual state

File: `frontend/main.js`

- When the backend returns 404 for the current page path, show a clear UI indication that the page does not exist (e.g., a banner: "This page does not exist. Switch to Edit mode to create it.")
- The page title area should show the path with a visual "new" indicator
- Do not auto-enter edit mode; require explicit user action

### Task 3.3 — Frontend: "New page" navigation action

Files: `frontend/main.js`, `frontend/style.css`

- Add a "New page" button to the action bar
- On click, open a dialog prompting for the path of the new page
- Validate: path must not be empty, must not have an extension (pages are extension-free in the URL space)
- On confirm, navigate the browser to that path — the page load will trigger the 404 → new page state from Task 3.2

**Dependency order:** 3.1 independent; 3.2 → 3.3

---

## Area 4: Raw Mode Menubar

Visual mode has a full ProseMirror menubar. Raw mode is a bare `<textarea>` with no tooling. A user switching to raw mode loses all toolbar access. The menubar items in visual mode operate via ProseMirror commands; in raw mode, the equivalent action inserts or wraps Markdown syntax around the textarea selection.

### Task 4.1 — Audit the visual mode menubar

File: `frontend/main.js`

- Enumerate all current menu items: labels, keyboard shortcuts, and what they do in visual mode
- Identify which are meaningful in raw mode (most formatting actions, insert link, insert image, save) vs. which are not (property panel toggle; possibly table insertion)
- Produces the definitive list of actions to port

### Task 4.2 — Raw mode menubar: infrastructure

Files: `frontend/main.js`, `frontend/menu.css`

- When the editor switches to raw mode, mount a menubar above the textarea in the same position as the visual menubar
- Reuse `menu.css` structure to keep a consistent look
- Each button dispatches a "raw action" rather than a ProseMirror command
- The property panel toggle must not appear in the raw mode menubar

### Task 4.3 — Raw mode menubar: inline formatting actions

File: `frontend/main.js`

For each applicable inline action (bold, italic, inline code, and others as the dialect gains them):
- Wrap the current textarea selection with the canonical Markdown syntax (e.g., `**selected**` for bold)
- If selection is empty, insert the syntax with a cursor placeholder positioned inside it
- Preserve native textarea undo history (use `insertText` input event approach)

### Task 4.4 — Raw mode menubar: block-level actions

File: `frontend/main.js`

For headings, lists, blockquote, code block, horizontal rule:
- Operate on current line(s) rather than inline selection
- Prefix the current line with the appropriate Markdown syntax, or toggle it off if already present
- Multi-line selections for list items should prefix each selected line

### Task 4.5 — Raw mode menubar: insert link / insert image

File: `frontend/main.js`

- Reuse or adapt the existing link/image insertion dialogs from visual mode
- On confirm, insert canonical Markdown syntax (`[text](url)` or `![alt](url)`) at the cursor position in the textarea

### Task 4.6 — Raw mode menubar: save action

File: `frontend/main.js`

- The save button in raw mode triggers the same save flow as visual mode (read textarea content → PUT to backend)
- Confirm the save action already reads from the textarea directly; wire the button if needed

**Dependency order:** 4.1 → 4.2 → 4.3, 4.4, 4.5, 4.6 (4.3–4.6 parallel after 4.2)

---

## Out of scope for v0.1

- Per-namespace sidebar/footer inheritance (v0.2+)
- Page templates for new page content (v0.2+)
- Circular include detection
- Media thumbnails and file-type icons in the media manager (already flagged in CLAUDE.md)
- Strikethrough and underline marks (dialect compliance matrix: "planned", not v0.1 requirements)
