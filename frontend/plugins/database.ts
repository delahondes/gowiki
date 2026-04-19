import { Plugin as PMPlugin, PluginKey, NodeSelection, EditorState } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import type { Registry } from "../compiler/registry"
import { enablePropertiesPanel, requestInputFocus } from "../compiler/core_ui"
import { openMediaManager } from "../media_manager.js"

// ── Properties ──

const databaseQueryProperties = [
  {
    name: "table",
    label: "Table",
    default: "",
    parse: (raw: string) => raw.trim() || null,
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "fields",
    label: "Fields",
    default: "",
    parse: (raw: string) => raw.trim() || null,
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "filter",
    label: "Filter",
    default: "",
    parse: (raw: string) => raw.trim() || null,
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "sort",
    label: "Sort Field",
    default: "",
    parse: (raw: string) => raw.trim() || null,
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "order",
    label: "Sort Order",
    default: "asc",
    parse: (raw: string) => raw.trim() || "asc",
    serialize: (value: string | null) => String(value ?? "asc"),
    options: [
      { value: "asc", label: "Ascending" },
      { value: "desc", label: "Descending" },
    ],
  },
  {
    name: "limit",
    label: "Limit",
    default: "20",
    parse: (raw: string) => raw.trim() || "20",
    serialize: (value: string | null) => String(value ?? "20"),
  },
]

const databaseRowProperties = [
  {
    name: "table",
    label: "Table",
    default: "",
    parse: (raw: string) => raw.trim() || null,
    serialize: (value: string | null) => String(value ?? ""),
  },
]

const databaseNewRowProperties = [
  {
    name: "table",
    label: "Table",
    default: "",
    parse: (raw: string) => raw.trim() || null,
    serialize: (value: string | null) => String(value ?? ""),
  },
]

// ── Styles ──

const databaseStyles = `
.gowiki-database-query,
.gowiki-database-newrow,
.gowiki-database-row {
  margin: 0.5em 0;
  user-select: none;
  -webkit-user-select: none;
}

#app.gowiki-editing .gowiki-database-query,
#app.gowiki-editing .gowiki-database-newrow,
#app.gowiki-editing .gowiki-database-row {
  background: #f8f9fa;
  border: 1px solid #dee2e6;
  border-radius: 4px;
  padding: 8px;
}

#app.gowiki-editing .gowiki-database-query.ProseMirror-selectednode,
#app.gowiki-editing .gowiki-database-newrow.ProseMirror-selectednode,
#app.gowiki-editing .gowiki-database-row.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}

.gowiki-database-query-label,
.gowiki-database-newrow-label,
.gowiki-database-row-label {
  font-size: 11px;
  color: #636e72;
  margin-bottom: 4px;
  font-family: monospace;
}

.gowiki-database-loading {
  color: #636e72;
  font-style: italic;
  padding: 8px;
}

.gowiki-database-error {
  color: #d63031;
  font-style: italic;
  padding: 8px;
}

/* Query result table */
.gowiki-database-table {
  width: 100%;
  border-collapse: collapse;
  margin-top: 4px;
}

.gowiki-database-table th,
.gowiki-database-table td {
  border: 1px solid var(--gw-color-border);
  padding: 4px 8px;
  text-align: left;
  font-size: 13px;
}

.gowiki-database-table th {
  background: var(--gw-color-table-head-bg);
  color: var(--gw-color-table-head-fg);
  font-weight: 600;
  cursor: pointer;
  user-select: none;
}

.gowiki-database-table th:hover {
  background: var(--gw-color-table-head-bg);
  filter: brightness(1.1);
}

.gowiki-database-table tr:nth-child(even) {
  background: var(--gw-color-surface);
}

.gowiki-database-pagination {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: 4px;
  font-size: 12px;
  color: #636e72;
}

.gowiki-database-pagination button {
  padding: 2px 8px;
  font-size: 12px;
  cursor: pointer;
}

/* New row form */
.gowiki-database-form {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 4px;
}

.gowiki-database-form-field {
  display: flex;
  align-items: center;
  gap: 8px;
}

.gowiki-database-form-field label {
  min-width: 120px;
  font-size: 13px;
  font-weight: 500;
}

.gowiki-database-form-field input,
.gowiki-database-form-field select {
  flex: 1;
  padding: 4px 6px;
  font-size: 13px;
  border: 1px solid #ced4da;
  border-radius: 3px;
  user-select: text;
  -webkit-user-select: text;
}

.gowiki-database-form-actions {
  margin-top: 4px;
}

.gowiki-database-form-actions button {
  padding: 4px 12px;
  font-size: 13px;
  cursor: pointer;
  background: #228be6;
  color: white;
  border: 1px solid #1971c2;
  border-radius: 3px;
}

.gowiki-database-form-actions button:hover {
  background: #1c7ed6;
}

/* Inline editable values */
.gowiki-database-editable-value {
  cursor: pointer;
}

.gowiki-database-editable-value:hover {
  background: #fff3cd;
}

.gowiki-database-page-link,
.ProseMirror a.gowiki-database-page-link {
  color: var(--gw-color-link-internal);
  text-decoration: none;
  cursor: pointer;
}

.gowiki-database-page-link:hover {
  text-decoration: underline;
}

.db-image-cell {
  max-height: 24px;
  vertical-align: middle;
}

.db-image-input-wrap {
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
}

.db-image-input-wrap input {
  flex: 1;
  min-width: 0;
}

.db-image-input-wrap button {
  white-space: nowrap;
  padding: 2px 6px;
  font-size: 12px;
  cursor: pointer;
}

.db-image-preview {
  max-height: 24px;
  vertical-align: middle;
  margin-left: 4px;
}

.db-color-swatch {
  display: inline-block;
  width: 20px;
  height: 20px;
  border-radius: 3px;
  border: 1px solid #ccc;
  vertical-align: middle;
}

.db-tag-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 1px 8px;
  border-radius: 12px;
  font-size: 12px;
  line-height: 18px;
  vertical-align: middle;
  white-space: nowrap;
}

.db-tag-icon {
  width: 14px;
  height: 14px;
  vertical-align: middle;
}

/* Template variables — resolved styled in edit mode only */
#app.gowiki-editing .gowiki-template-var {
  background: #f0f4ff;
  border-radius: 3px;
  padding: 0 3px;
  font-style: normal;
}

/* Error state: unknown variable */
.gowiki-template-var-error {
  background: #fff3cd;
  color: #856404;
  border-radius: 3px;
  padding: 0 3px;
  font-weight: 600;
}
`

// ── Helpers ──

// Prevent ProseMirror from intercepting pointer/focus events on form inputs
// within the ProseMirror DOM tree (used for edit-mode inputs that live inside
// NodeViews and DON'T need cursor-click positioning — e.g. dropdowns).
function isolateInput(el: HTMLElement) {
  for (const evt of ["pointerdown", "mousedown", "pointerup", "mouseup", "click"] as const) {
    el.addEventListener(evt, (e) => e.stopPropagation())
  }
}

// ── Tag helpers ──

const tagTableCache = new Map<string, { data: Map<number, { label: string; icon: string; color: string }>; timestamp: number }>()
const tagTableInflight = new Map<string, Promise<Map<number, { label: string; icon: string; color: string }>>>()

async function getTagOptions(tableName: string): Promise<Map<number, { label: string; icon: string; color: string }>> {
  const cached = tagTableCache.get(tableName)
  if (cached && Date.now() - cached.timestamp < 30000) return cached.data

  // Deduplicate in-flight requests: if a fetch for this table is already
  // in progress, reuse its promise instead of firing another request.
  const inflight = tagTableInflight.get(tableName)
  if (inflight) return inflight

  const promise = (async () => {
    const resp = await fetch(`/api/database/${encodeURIComponent(tableName)}/rows?limit=500`)
    if (!resp.ok) return new Map<number, { label: string; icon: string; color: string }>()
    const body = await resp.json()
    const rows: any[] = body.rows || []
    const result = new Map<number, { label: string; icon: string; color: string }>()
    for (const r of rows) {
      result.set(r.id, {
        label: String(r.fields?.label ?? ""),
        icon: String(r.fields?.icon ?? ""),
        color: String(r.fields?.color ?? ""),
      })
    }
    tagTableCache.set(tableName, { data: result, timestamp: Date.now() })
    return result
  })()

  tagTableInflight.set(tableName, promise)
  promise.finally(() => tagTableInflight.delete(tableName))
  return promise
}

function isLightColor(hex: string): boolean {
  const c = hex.replace("#", "")
  if (c.length < 6) return true
  const r = parseInt(c.substring(0, 2), 16)
  const g = parseInt(c.substring(2, 4), 16)
  const b = parseInt(c.substring(4, 6), 16)
  return (r * 299 + g * 587 + b * 114) / 1000 > 150
}

function renderTagBadge(tag: { label: string; icon: string; color: string }): HTMLSpanElement {
  const span = document.createElement("span")
  span.className = "db-tag-badge"
  if (tag.color) {
    span.style.backgroundColor = tag.color
    span.style.color = isLightColor(tag.color) ? "#333" : "#fff"
  } else {
    span.style.backgroundColor = "#e9ecef"
    span.style.color = "#333"
  }
  if (tag.icon) {
    const img = document.createElement("img")
    img.src = tag.icon
    img.className = "db-tag-icon"
    span.appendChild(img)
  }
  const text = document.createTextNode(tag.label || "(no label)")
  span.appendChild(text)
  return span
}

// ── Lookup helpers ──

const lookupTableCache = new Map<string, { data: Map<number, string>; timestamp: number }>()
const lookupTableInflight = new Map<string, Promise<Map<number, string>>>()

async function getLookupOptions(tableName: string, displayColumn?: string): Promise<Map<number, string>> {
  const key = tableName + "\x00" + (displayColumn || "")
  const cached = lookupTableCache.get(key)
  if (cached && Date.now() - cached.timestamp < 30000) return cached.data

  const inflight = lookupTableInflight.get(key)
  if (inflight) return inflight

  const promise = (async () => {
    const resp = await fetch(`/api/database/${encodeURIComponent(tableName)}/rows?limit=500`)
    if (!resp.ok) return new Map<number, string>()
    const body = await resp.json()
    const rows: any[] = body.rows || []
    const result = new Map<number, string>()
    for (const r of rows) {
      const fields = r.fields || {}
      let display: unknown
      if (displayColumn && fields[displayColumn] != null && fields[displayColumn] !== "") {
        display = fields[displayColumn]
      } else {
        display = Object.values(fields).find(v => typeof v === "string" && v !== "") ?? String(r.id)
      }
      result.set(r.id, String(display))
    }
    lookupTableCache.set(key, { data: result, timestamp: Date.now() })
    return result
  })()

  lookupTableInflight.set(key, promise)
  promise.finally(() => lookupTableInflight.delete(key))
  return promise
}

// ── User helpers ──

let userListCache: { data: { username: string; display_name: string }[]; timestamp: number } | null = null
let userListInflight: Promise<{ username: string; display_name: string }[]> | null = null

async function getUserList(): Promise<{ username: string; display_name: string }[]> {
  if (userListCache && Date.now() - userListCache.timestamp < 30000) return userListCache.data
  if (userListInflight) return userListInflight

  const promise = (async () => {
    const resp = await fetch("/api/users/list")
    if (!resp.ok) return []
    const body = await resp.json()
    const users = body.users || []
    userListCache = { data: users, timestamp: Date.now() }
    return users
  })()

  userListInflight = promise
  promise.finally(() => { userListInflight = null })
  return promise
}

function renderColorSwatch(color: string): HTMLSpanElement {
  const span = document.createElement("span")
  span.className = "db-color-swatch"
  if (color) span.style.backgroundColor = color
  return span
}

// Creates an overlay select that uses {value, label} option objects.
function createOverlaySelectKeyed(
  anchor: HTMLElement,
  opts: {
    options: { value: string; label: string }[]
    value: string
    onSave: (newValue: string) => void
    onCancel: () => void
  },
) {
  const rect = anchor.getBoundingClientRect()
  const sel = document.createElement("select")
  sel.style.position = "fixed"
  sel.style.left = rect.left + "px"
  sel.style.top = rect.top + "px"
  sel.style.width = Math.max(rect.width, 150) + "px"
  sel.style.height = rect.height + "px"
  sel.style.boxSizing = "border-box"
  sel.style.fontSize = "13px"
  sel.style.border = "2px solid #228be6"
  sel.style.borderRadius = "2px"
  sel.style.outline = "none"
  sel.style.zIndex = "10000"
  sel.style.background = "#fff"

  const emptyOpt = document.createElement("option")
  emptyOpt.value = ""
  emptyOpt.textContent = "-- Select --"
  sel.appendChild(emptyOpt)
  for (const o of opts.options) {
    const opt = document.createElement("option")
    opt.value = o.value
    opt.textContent = o.label
    if (o.value === opts.value) opt.selected = true
    sel.appendChild(opt)
  }

  const cleanup = () => { if (sel.parentNode) sel.remove() }

  sel.addEventListener("change", () => {
    opts.onSave(sel.value)
    cleanup()
  })
  sel.addEventListener("blur", () => {
    cleanup()
    opts.onCancel()
  })

  document.body.appendChild(sel)
  sel.focus()
  return sel
}

// Preset pastel colors suitable for tag/badge backgrounds.
const colorPresets = [
  "#adb5bd", // gray
  "#ffa8a8", // red
  "#fcc2d7", // pink
  "#eebefa", // purple
  "#b197fc", // violet
  "#91a7ff", // indigo
  "#74c0fc", // blue
  "#66d9e8", // cyan
  "#63e6be", // teal
  "#8ce99a", // green
  "#c0eb75", // lime
  "#ffe066", // yellow
  "#ffc078", // orange
  "#e9ecef", // light gray
  "#868e96", // dark gray
  "#ff6b6b", // saturated red
  "#cc5de8", // saturated purple
  "#5c7cfa", // saturated indigo
  "#22b8cf", // saturated cyan
  "#51cf66", // saturated green
  "#fcc419", // saturated yellow
  "#ff922b", // saturated orange
  "#f06595", // saturated pink
  "#845ef7", // saturated violet
]

// Creates a color picker overlay with preset swatches and a custom color input.
function createOverlayColorPicker(
  anchor: HTMLElement,
  opts: {
    value: string
    onSave: (newValue: string) => void
    onCancel: () => void
  },
) {
  const rect = anchor.getBoundingClientRect()
  const wrap = document.createElement("div")
  wrap.style.position = "fixed"
  wrap.style.left = rect.left + "px"
  wrap.style.top = (rect.bottom + 2) + "px"
  wrap.style.zIndex = "10000"
  wrap.style.background = "#fff"
  wrap.style.border = "2px solid #228be6"
  wrap.style.borderRadius = "4px"
  wrap.style.padding = "8px"
  wrap.style.boxShadow = "0 2px 8px rgba(0,0,0,0.15)"
  wrap.style.width = "220px"

  // Preset grid.
  const grid = document.createElement("div")
  grid.style.display = "grid"
  grid.style.gridTemplateColumns = "repeat(8, 1fr)"
  grid.style.gap = "3px"
  grid.style.marginBottom = "6px"

  let saved = false
  const cleanup = () => { if (wrap.parentNode) wrap.remove() }

  for (const c of colorPresets) {
    const swatch = document.createElement("div")
    swatch.style.width = "22px"
    swatch.style.height = "22px"
    swatch.style.borderRadius = "3px"
    swatch.style.backgroundColor = c
    swatch.style.border = c === opts.value ? "2px solid #228be6" : "1px solid #ccc"
    swatch.style.cursor = "pointer"
    swatch.style.boxSizing = "border-box"
    swatch.addEventListener("click", () => {
      saved = true
      opts.onSave(c)
      cleanup()
    })
    grid.appendChild(swatch)
  }
  wrap.appendChild(grid)

  // Custom color row.
  const customRow = document.createElement("div")
  customRow.style.display = "flex"
  customRow.style.alignItems = "center"
  customRow.style.gap = "4px"

  const colorInput = document.createElement("input")
  colorInput.type = "color"
  colorInput.value = opts.value || "#adb5bd"
  colorInput.style.width = "30px"
  colorInput.style.height = "24px"
  colorInput.style.padding = "0"
  colorInput.style.border = "1px solid #ccc"
  colorInput.style.cursor = "pointer"
  customRow.appendChild(colorInput)

  const textInput = document.createElement("input")
  textInput.type = "text"
  textInput.value = opts.value || ""
  textInput.placeholder = "#rrggbb"
  textInput.style.flex = "1"
  textInput.style.fontSize = "12px"
  textInput.style.padding = "2px 4px"
  textInput.style.border = "1px solid #ccc"
  textInput.style.borderRadius = "2px"
  textInput.style.minWidth = "0"
  customRow.appendChild(textInput)

  const okBtn = document.createElement("button")
  okBtn.textContent = "OK"
  okBtn.style.fontSize = "12px"
  okBtn.style.padding = "2px 8px"
  okBtn.style.cursor = "pointer"
  customRow.appendChild(okBtn)

  colorInput.addEventListener("input", () => {
    textInput.value = colorInput.value
  })
  textInput.addEventListener("input", () => {
    if (/^#[0-9a-fA-F]{6}$/.test(textInput.value)) {
      colorInput.value = textInput.value
    }
  })
  okBtn.addEventListener("click", () => {
    saved = true
    opts.onSave(textInput.value || colorInput.value)
    cleanup()
  })
  textInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); saved = true; opts.onSave(textInput.value || colorInput.value); cleanup() }
    if (e.key === "Escape") { e.preventDefault(); saved = true; opts.onCancel(); cleanup() }
  })

  wrap.appendChild(customRow)

  // Close on outside click.
  const onOutside = (e: MouseEvent) => {
    if (!wrap.contains(e.target as Node)) {
      document.removeEventListener("mousedown", onOutside, true)
      if (!saved) { saved = true; opts.onCancel() }
      cleanup()
    }
  }
  setTimeout(() => document.addEventListener("mousedown", onOutside, true), 0)

  document.body.appendChild(wrap)
  return wrap
}

// Creates an inline edit overlay input OUTSIDE the ProseMirror DOM tree.
// ProseMirror's DOMObserver registers a document-level selectionchange handler
// that interferes with cursor positioning inside inputs that are descendants of
// view.dom. Rendering the input on document.body avoids this entirely.
function createOverlayInput(
  anchor: HTMLElement,
  opts: {
    type?: string
    value: string
    onSave: (newValue: string) => void
    onCancel: () => void
  },
) {
  const rect = anchor.getBoundingClientRect()
  const input = document.createElement("input")
  input.type = opts.type || "text"
  input.value = opts.value
  input.style.position = "fixed"
  input.style.left = rect.left + "px"
  input.style.top = rect.top + "px"
  input.style.width = rect.width + "px"
  input.style.height = rect.height + "px"
  input.style.boxSizing = "border-box"
  input.style.padding = "2px 4px"
  input.style.fontSize = "13px"
  input.style.border = "2px solid #228be6"
  input.style.borderRadius = "2px"
  input.style.outline = "none"
  input.style.zIndex = "10000"
  input.style.background = "#fff"

  let saved = false
  const cleanup = () => { if (input.parentNode) input.remove() }

  input.addEventListener("blur", () => {
    if (!saved) { saved = true; opts.onSave(input.value) }
    cleanup()
  })
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); saved = true; opts.onSave(input.value); cleanup(); input.blur() }
    if (e.key === "Escape") { e.preventDefault(); saved = true; opts.onCancel(); cleanup(); input.blur() }
  })

  document.body.appendChild(input)
  input.focus()
  input.select()
  return input
}

// Creates an inline edit overlay select OUTSIDE the ProseMirror DOM tree.
function createOverlaySelect(
  anchor: HTMLElement,
  opts: {
    options: string[]
    value: string
    onSave: (newValue: string) => void
    onCancel: () => void
  },
) {
  const rect = anchor.getBoundingClientRect()
  const sel = document.createElement("select")
  sel.style.position = "fixed"
  sel.style.left = rect.left + "px"
  sel.style.top = rect.top + "px"
  sel.style.width = rect.width + "px"
  sel.style.height = rect.height + "px"
  sel.style.boxSizing = "border-box"
  sel.style.fontSize = "13px"
  sel.style.border = "2px solid #228be6"
  sel.style.borderRadius = "2px"
  sel.style.outline = "none"
  sel.style.zIndex = "10000"
  sel.style.background = "#fff"

  const emptyOpt = document.createElement("option")
  emptyOpt.value = ""
  emptyOpt.textContent = "-- Select --"
  sel.appendChild(emptyOpt)
  for (const v of opts.options) {
    const opt = document.createElement("option")
    opt.value = v
    opt.textContent = v
    if (v === opts.value) opt.selected = true
    sel.appendChild(opt)
  }

  const cleanup = () => { if (sel.parentNode) sel.remove() }

  sel.addEventListener("change", () => {
    opts.onSave(sel.value)
    cleanup()
  })
  sel.addEventListener("blur", () => {
    cleanup()
    opts.onCancel()
  })

  document.body.appendChild(sel)
  sel.focus()
  return sel
}

// Creates an inline edit overlay with text input + browse button for image fields.
function createOverlayImageInput(
  anchor: HTMLElement,
  opts: {
    value: string
    onSave: (newValue: string) => void
    onCancel: () => void
  },
) {
  const rect = anchor.getBoundingClientRect()
  const wrap = document.createElement("div")
  wrap.style.position = "fixed"
  wrap.style.left = rect.left + "px"
  wrap.style.top = rect.top + "px"
  wrap.style.width = Math.max(rect.width, 250) + "px"
  wrap.style.height = rect.height + "px"
  wrap.style.boxSizing = "border-box"
  wrap.style.display = "flex"
  wrap.style.gap = "4px"
  wrap.style.alignItems = "center"
  wrap.style.zIndex = "10000"
  wrap.style.background = "#fff"
  wrap.style.border = "2px solid #228be6"
  wrap.style.borderRadius = "2px"
  wrap.style.padding = "0 4px"

  const input = document.createElement("input")
  input.type = "text"
  input.value = opts.value
  input.placeholder = "/path/to/image.png"
  input.style.flex = "1"
  input.style.minWidth = "0"
  input.style.border = "none"
  input.style.outline = "none"
  input.style.fontSize = "13px"
  input.style.padding = "2px 4px"
  wrap.appendChild(input)

  const btn = document.createElement("button")
  btn.type = "button"
  btn.textContent = "Browse"
  btn.style.whiteSpace = "nowrap"
  btn.style.padding = "2px 6px"
  btn.style.fontSize = "12px"
  btn.style.cursor = "pointer"
  wrap.appendChild(btn)

  let saved = false
  const cleanup = () => { if (wrap.parentNode) wrap.remove() }

  btn.addEventListener("click", (e) => {
    e.stopPropagation()
    const ns = window.location.pathname.replace(/^\/+/, "").replace(/\/[^/]*$/, "") || ""
    openMediaManager(ns, () => {}, (_type: string, entry: any) => { const insertedPath = entry.path;
      input.value = insertedPath
      saved = true
      opts.onSave(input.value)
      cleanup()
    })
  })

  input.addEventListener("blur", () => {
    // Delay to allow browse button click to fire first.
    setTimeout(() => {
      if (!saved && wrap.parentNode) { saved = true; opts.onSave(input.value); cleanup() }
    }, 150)
  })
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); saved = true; opts.onSave(input.value); cleanup() }
    if (e.key === "Escape") { e.preventDefault(); saved = true; opts.onCancel(); cleanup() }
  })

  document.body.appendChild(wrap)
  input.focus()
  input.select()
}

// Custom event name for cross-NodeView communication.
const DATABASE_ROW_CREATED = "gowiki-database-row-created"

// ── NodeViews ──

class DatabaseQueryNodeView {
  dom: HTMLElement
  private node: PMNode
  private currentSort: string
  private currentOrder: string
  private initialLimit: number
  private displayLimit: number
  private refreshHandler: ((e: Event) => void) | null = null

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.currentSort = node.attrs.sort === "id" ? "__title__" : (node.attrs.sort || "")
    this.currentOrder = node.attrs.order || "asc"
    this.initialLimit = parseInt(node.attrs.limit) || 20
    this.displayLimit = this.initialLimit

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-database-query"
    this.dom.contentEditable = "false"

    // Listen for row creation events from DatabaseNewRowNodeView.
    this.refreshHandler = (e: Event) => {
      const detail = (e as CustomEvent).detail
      if (detail?.table === this.node.attrs.table) {
        this.fetchData()
      }
    }
    document.addEventListener(DATABASE_ROW_CREATED, this.refreshHandler)

    this.render()
  }

  private render() {
    this.dom.innerHTML = ""
    const table = this.node.attrs.table
    if (!table) {
      this.dom.innerHTML = '<div class="gowiki-database-error">No table specified</div>'
      return
    }

    const label = document.createElement("div")
    label.className = "gowiki-database-query-label"
    label.textContent = `Database: ${table}`
    this.dom.appendChild(label)

    this.fetchData()
  }

  private async fetchData() {
    const table = this.node.attrs.table
    const filter = this.node.attrs.filter || ""

    const params = new URLSearchParams()
    if (filter) {
      for (const f of filter.split("&")) {
        params.append("filter", f)
      }
    }
    if (this.currentSort) params.set("sort", this.currentSort === "__title__" ? "id" : this.currentSort)
    if (this.currentOrder) params.set("order", this.currentOrder)
    params.set("limit", String(this.displayLimit))
    params.set("offset", "0")

    try {
      const schemaResp = await fetch(`/api/database/${encodeURIComponent(table)}/schema`)
      if (!schemaResp.ok) {
        this.showError("Table not found: " + table)
        return
      }
      const schema = await schemaResp.json()

      const resp = await fetch(`/api/database/${encodeURIComponent(table)}/rows?${params}`)
      if (!resp.ok) {
        this.showError("Failed to load data")
        return
      }
      const data = await resp.json()
      this.renderTable(schema, data.rows || [], data.total || 0)
      document.dispatchEvent(new Event("gowiki:node-rendered"))
    } catch (err) {
      this.showError("Network error")
    }
  }

  private renderTable(schema: any, rows: any[], total: number) {
    // Remove loading, keep label.
    const existing = this.dom.querySelector(".gowiki-database-loading")
    if (existing) existing.remove()
    const existingTable = this.dom.querySelector(".gowiki-database-table")
    if (existingTable) existingTable.remove()
    const existingPag = this.dom.querySelector(".gowiki-database-pagination")
    if (existingPag) existingPag.remove()

    const allFields = (schema.fields || []).filter((f: any) => !f.archived_at)
    const idField = { name: "__title__", label: "ID", type: "text", _isTitle: true }

    // Apply fields filter: select and order columns by the fields attribute.
    // %title% is a special token: positions the ID column explicitly.
    // If %title% is not used, the ID column is always prepended as the first column.
    const fieldsAttr = this.node.attrs.fields || ""
    let fields: any[]
    let hasExplicitTitle = false
    if (fieldsAttr) {
      const colNames = fieldsAttr.split(",").map((c: string) => c.trim()).filter((c: string) => c)
      fields = []
      for (const col of colNames) {
        if (col === "%title%") {
          hasExplicitTitle = true
          fields.push(idField)
        } else {
          // Match by field name or label (case-insensitive).
          const lower = col.toLowerCase()
          const f = allFields.find((f: any) =>
            f.name === col || f.name === lower ||
            (f.label && f.label.toLowerCase() === lower)
          )
          if (f && !fields.some((ef: any) => ef.name === f.name)) {
            fields.push(f)
          }
        }
      }
      if (!hasExplicitTitle) fields.unshift(idField)
    } else {
      fields = [idField, ...allFields]
    }

    const tbl = document.createElement("table")
    tbl.className = "gowiki-database-table"

    // Header.
    const thead = document.createElement("thead")
    const tr = document.createElement("tr")
    for (const f of fields) {
      const th = document.createElement("th")
      th.textContent = f.label || f.name
      if (this.currentSort === f.name) {
        th.textContent += this.currentOrder === "asc" ? " \u25B2" : " \u25BC"
      }
      th.addEventListener("click", () => {
        if (this.currentSort === f.name) {
          this.currentOrder = this.currentOrder === "asc" ? "desc" : "asc"
        } else {
          this.currentSort = f.name
          this.currentOrder = "asc"
        }
        this.displayLimit = this.initialLimit
        this.fetchData()
      })
      tr.appendChild(th)
    }
    thead.appendChild(tr)
    tbl.appendChild(thead)

    // Body.
    const tableName = this.node.attrs.table
    const tbody = document.createElement("tbody")
    for (const row of rows) {
      const rtr = document.createElement("tr")
      for (const f of fields) {
        const td = document.createElement("td")
        const val = f.name === "__title__" ? row.id : row.fields?.[f.name]
        if (f._isTitle && row.page_path) {
          // %title%: row id as a clickable link to the page.
          const a = document.createElement("a")
          a.className = "gowiki-database-page-link"
          a.textContent = String(row.id)
          a.href = row.page_path
          td.appendChild(a)
        } else if (f.type === "page_link" && val) {
          const a = document.createElement("a")
          a.className = "gowiki-database-page-link"
          a.textContent = String(val)
          a.href = String(val)
          td.appendChild(a)
        } else if (f.type === "image" && val) {
          const img = document.createElement("img")
          img.src = String(val)
          img.className = "db-image-cell"
          td.appendChild(img)
        } else if (f.type === "color" && val) {
          td.appendChild(renderColorSwatch(String(val)))
        } else if (f.type === "tag" && f.foreign_key) {
          if (val && Number(val) !== 0) {
            td.textContent = "..."
            getTagOptions(f.foreign_key).then(tags => {
              td.textContent = ""
              const tag = tags.get(Number(val))
              if (tag) td.appendChild(renderTagBadge(tag))
              else td.textContent = String(val)
            })
          }
        } else if (f.type === "lookup" && f.foreign_key) {
          if (val && Number(val) !== 0) {
            td.textContent = "..."
            getLookupOptions(f.foreign_key, f.display_column).then(opts => {
              td.textContent = opts.get(Number(val)) || String(val)
            })
          }
        } else if (f.type === "user" && val) {
          td.textContent = String(val)
          fetchUserInfo(String(val)).then(info => {
            if (info.label) td.textContent = info.label
          })
        } else if ((f.type === "date" || f.type === "datetime") && val) {
          // Format ISO dates as YYYY-MM-DD.
          const s = String(val)
          td.textContent = s.length >= 10 ? s.substring(0, 10) : s
        } else if (Array.isArray(val)) {
          td.textContent = val.join(", ")
        } else {
          td.textContent = val != null ? String(val) : ""
        }

        // Double-click inline editing — forbidden for auto_increment and %title% synthetic fields.
        if (f.type !== "auto_increment" && !f._isTitle) {
          td.className = "gowiki-database-editable-value"
          td.addEventListener("dblclick", () => {
            this.inlineEditCell(td, tableName, row.id, f, val)
          })
        }

        rtr.appendChild(td)
      }
      tbody.appendChild(rtr)
    }
    tbl.appendChild(tbody)
    this.dom.appendChild(tbl)

    // Show more.
    if (total > rows.length) {
      const pag = document.createElement("div")
      pag.className = "gowiki-database-pagination"

      const info = document.createElement("span")
      info.textContent = `Showing ${rows.length} of ${total}`
      pag.appendChild(info)

      const more = document.createElement("button")
      more.textContent = "Show more"
      more.addEventListener("click", () => {
        this.displayLimit += this.initialLimit
        this.fetchData()
      })
      pag.appendChild(more)

      this.dom.appendChild(pag)
    }
  }

  private async saveInlineEdit(tableName: string, rowId: number, fieldName: string, newVal: string, force = false): Promise<boolean> {
    const url = `/api/database/${encodeURIComponent(tableName)}/rows/${rowId}${force ? "?force=true" : ""}`
    const resp = await fetch(url, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ fields: { [fieldName]: newVal } }),
    })
    if (resp.status === 409) {
      const body = await resp.json().catch(() => ({}))
      if (body.error === "page_draft_conflict") {
        const ok = confirm(`A draft by "${body.draft_owner}" exists for this row's page. Force the edit?`)
        if (ok) return this.saveInlineEdit(tableName, rowId, fieldName, newVal, true)
        return false
      }
    }
    return resp.ok
  }

  private inlineEditCell(td: HTMLElement, tableName: string, rowId: number, field: any, currentValue: any) {
    const displayValue = Array.isArray(currentValue) ? currentValue.join(", ") : (currentValue != null ? String(currentValue) : "")

    if (field.type === "enum") {
      createOverlaySelect(td, {
        options: field.enum_values || [],
        value: displayValue,
        onSave: async (newVal) => {
          td.textContent = newVal
          const ok = await this.saveInlineEdit(tableName, rowId, field.name, newVal)
          if (!ok) { td.textContent = displayValue }
        },
        onCancel: () => {},
      })
    } else if (field.type === "boolean") {
      const newVal = currentValue === true || currentValue === "true" ? "false" : "true"
      td.textContent = newVal
      this.saveInlineEdit(tableName, rowId, field.name, newVal).then(ok => {
        if (!ok) td.textContent = displayValue
      })
    } else if (field.type === "color") {
      createOverlayColorPicker(td, {
        value: displayValue || "#adb5bd",
        onSave: async (newVal) => {
          td.textContent = ""
          td.appendChild(renderColorSwatch(newVal))
          td.className = "gowiki-database-editable-value"
          const ok = await this.saveInlineEdit(tableName, rowId, field.name, newVal)
          if (!ok) {
            td.textContent = ""
            td.appendChild(renderColorSwatch(displayValue))
          }
        },
        onCancel: () => {},
      })
    } else if (field.type === "tag" && field.foreign_key) {
      getTagOptions(field.foreign_key).then(tags => {
        const options: { value: string; label: string }[] = []
        tags.forEach((t, id) => options.push({ value: String(id), label: t.label || String(id) }))
        createOverlaySelectKeyed(td, {
          options,
          value: displayValue,
          onSave: async (newVal) => {
            td.textContent = ""
            const tag = tags.get(Number(newVal))
            if (tag) td.appendChild(renderTagBadge(tag))
            else td.textContent = newVal
            td.className = "gowiki-database-editable-value"
            const ok = await this.saveInlineEdit(tableName, rowId, field.name, newVal)
            if (!ok) {
              td.textContent = ""
              const oldTag = tags.get(Number(displayValue))
              if (oldTag) td.appendChild(renderTagBadge(oldTag))
              else td.textContent = displayValue
            }
          },
          onCancel: () => {},
        })
      })
    } else if (field.type === "lookup" && field.foreign_key) {
      getLookupOptions(field.foreign_key, field.display_column).then(opts => {
        const options: { value: string; label: string }[] = []
        opts.forEach((label, id) => options.push({ value: String(id), label }))
        createOverlaySelectKeyed(td, {
          options,
          value: displayValue,
          onSave: async (newVal) => {
            td.textContent = opts.get(Number(newVal)) || newVal
            td.className = "gowiki-database-editable-value"
            const ok = await this.saveInlineEdit(tableName, rowId, field.name, newVal)
            if (!ok) td.textContent = opts.get(Number(displayValue)) || displayValue
          },
          onCancel: () => {},
        })
      })
    } else if (field.type === "user") {
      getUserList().then(users => {
        const options = users.map(u => ({ value: u.username, label: u.display_name || u.username }))
        createOverlaySelectKeyed(td, {
          options,
          value: displayValue,
          onSave: async (newVal) => {
            const u = users.find(u => u.username === newVal)
            td.textContent = u?.display_name || newVal
            td.className = "gowiki-database-editable-value"
            const ok = await this.saveInlineEdit(tableName, rowId, field.name, newVal)
            if (!ok) {
              const old = users.find(u => u.username === displayValue)
              td.textContent = old?.display_name || displayValue
            }
          },
          onCancel: () => {},
        })
      })
    } else if (field.type === "image") {
      createOverlayImageInput(td, {
        value: displayValue,
        onSave: async (newVal) => {
          td.textContent = ""
          if (newVal) {
            const img = document.createElement("img")
            img.src = newVal
            img.className = "db-image-cell"
            td.appendChild(img)
          }
          td.className = "gowiki-database-editable-value"
          const ok = await this.saveInlineEdit(tableName, rowId, field.name, newVal)
          if (!ok) {
            td.textContent = ""
            if (displayValue) {
              const img = document.createElement("img")
              img.src = displayValue
              img.className = "db-image-cell"
              td.appendChild(img)
            }
          }
        },
        onCancel: () => {},
      })
    } else {
      createOverlayInput(td, {
        type: field.type === "date" ? "date" : field.type === "integer" || field.type === "float" ? "number" : "text",
        value: displayValue,
        onSave: async (newVal) => {
          td.textContent = newVal
          td.className = "gowiki-database-editable-value"
          const ok = await this.saveInlineEdit(tableName, rowId, field.name, newVal)
          if (!ok) {
            td.textContent = displayValue
            td.style.color = "#c33"
            setTimeout(() => { td.style.color = "" }, 2000)
          }
        },
        onCancel: () => {},
      })
    }
  }

  private showError(message: string) {
    const el = document.createElement("div")
    el.className = "gowiki-database-error"
    el.textContent = message
    this.dom.appendChild(el)
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    if (node.attrs.table !== this.node.attrs.table ||
        node.attrs.fields !== this.node.attrs.fields ||
        node.attrs.filter !== this.node.attrs.filter) {
      this.node = node
      this.currentSort = node.attrs.sort === "id" ? "__title__" : (node.attrs.sort || "")
      this.currentOrder = node.attrs.order || "asc"
      this.initialLimit = parseInt(node.attrs.limit) || 20
      this.displayLimit = this.initialLimit
      this.render()
      return true
    }
    this.node = node
    return true
  }

  stopEvent(event: Event): boolean {
    // Stop mouse events to prevent ProseMirror from creating text selections
    // across the document when clicking inside database tables.
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click" || type === "dblclick") return true
    const tag = (event.target as HTMLElement)?.tagName
    if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return true
    return false
  }
  ignoreMutation(): boolean { return true }
  destroy() {
    if (this.refreshHandler) {
      document.removeEventListener(DATABASE_ROW_CREATED, this.refreshHandler)
      this.refreshHandler = null
    }
  }
}

class DatabaseNewRowNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-database-newrow"
    this.dom.contentEditable = "false"

    this.render()
  }

  private render() {
    this.dom.innerHTML = ""
    const table = this.node.attrs.table
    if (!table) {
      this.dom.innerHTML = '<div class="gowiki-database-error">No table specified</div>'
      return
    }

    const label = document.createElement("div")
    label.className = "gowiki-database-newrow-label"
    label.textContent = `New row: ${table}`
    this.dom.appendChild(label)

    this.fetchSchemaAndRenderForm()
  }

  private async fetchSchemaAndRenderForm() {
    const table = this.node.attrs.table
    try {
      const resp = await fetch(`/api/database/${encodeURIComponent(table)}/schema`)
      if (!resp.ok) {
        this.showError("Table not found: " + table)
        return
      }
      const schema = await resp.json()
      this.renderForm(schema)
      document.dispatchEvent(new Event("gowiki:node-rendered"))
    } catch {
      this.showError("Network error")
    }
  }

  private renderForm(schema: any) {
    const fields = (schema.fields || []).filter((f: any) => !f.archived_at && f.type !== "auto_increment")
    const form = document.createElement("div")
    form.className = "gowiki-database-form"

    const inputs: Map<string, HTMLInputElement | HTMLSelectElement | HTMLDivElement> = new Map()

    for (const f of fields) {
      const row = document.createElement("div")
      row.className = "gowiki-database-form-field"

      const lbl = document.createElement("label")
      lbl.textContent = f.label || f.name
      row.appendChild(lbl)

      if (f.type === "image") {
        const wrap = document.createElement("div")
        wrap.className = "db-image-input-wrap"
        const inp = document.createElement("input")
        inp.type = "text"
        inp.placeholder = f.placeholder || "/path/to/image.png"
        if (f.default_value) inp.value = f.default_value
        wrap.appendChild(inp)
        const btn = document.createElement("button")
        btn.type = "button"
        btn.textContent = "Browse"
        btn.addEventListener("click", () => {
          const ns = window.location.pathname.replace(/^\/+/, "").replace(/\/[^/]*$/, "") || ""
          openMediaManager(ns, () => {}, (_type: string, entry: any) => { const insertedPath = entry.path;
            inp.value = insertedPath
          })
        })
        wrap.appendChild(btn)
        Object.defineProperty(wrap, "value", {
          get: () => inp.value,
          set: (v: string) => { inp.value = v },
        })
        inputs.set(f.name, wrap)
        row.appendChild(wrap)
      } else if (f.type === "enum") {
        const sel = document.createElement("select")
        const empty = document.createElement("option")
        empty.value = ""
        empty.textContent = "-- Select --"
        sel.appendChild(empty)
        for (const v of (f.enum_values || [])) {
          const opt = document.createElement("option")
          opt.value = v
          opt.textContent = v
          sel.appendChild(opt)
        }
        inputs.set(f.name, sel)
        row.appendChild(sel)
      } else if (f.type === "color") {
        const inp = document.createElement("input")
        inp.type = "color"
        inp.value = f.default_value || "#adb5bd"
        inputs.set(f.name, inp)
        row.appendChild(inp)
      } else if (f.type === "tag" && f.foreign_key) {
        const sel = document.createElement("select")
        const empty = document.createElement("option")
        empty.value = ""
        empty.textContent = "-- Select --"
        sel.appendChild(empty)
        inputs.set(f.name, sel)
        row.appendChild(sel)
        // Populate async.
        getTagOptions(f.foreign_key).then(tags => {
          tags.forEach((t, id) => {
            const opt = document.createElement("option")
            opt.value = String(id)
            opt.textContent = t.label || String(id)
            sel.appendChild(opt)
          })
        })
      } else if (f.type === "lookup" && f.foreign_key) {
        const sel = document.createElement("select")
        const empty = document.createElement("option")
        empty.value = ""
        empty.textContent = "-- Select --"
        sel.appendChild(empty)
        inputs.set(f.name, sel)
        row.appendChild(sel)
        getLookupOptions(f.foreign_key, f.display_column).then(opts => {
          opts.forEach((label, id) => {
            const opt = document.createElement("option")
            opt.value = String(id)
            opt.textContent = label
            sel.appendChild(opt)
          })
        })
      } else if (f.type === "user") {
        const sel = document.createElement("select")
        const empty = document.createElement("option")
        empty.value = ""
        empty.textContent = "-- Select --"
        sel.appendChild(empty)
        inputs.set(f.name, sel)
        row.appendChild(sel)
        getUserList().then(users => {
          for (const u of users) {
            const opt = document.createElement("option")
            opt.value = u.username
            opt.textContent = u.display_name || u.username
            sel.appendChild(opt)
          }
        })
      } else if (f.type === "boolean") {
        const sel = document.createElement("select")
        for (const v of [
          { val: "", label: "-- Select --" },
          { val: "true", label: "Yes" },
          { val: "false", label: "No" },
        ]) {
          const opt = document.createElement("option")
          opt.value = v.val
          opt.textContent = v.label
          sel.appendChild(opt)
        }
        inputs.set(f.name, sel)
        row.appendChild(sel)
      } else {
        const inp = document.createElement("input")
        inp.type = f.type === "date" ? "date" : f.type === "datetime" ? "datetime-local" : f.type === "integer" || f.type === "float" ? "number" : "text"
        inp.placeholder = f.placeholder || ""
        if (f.default_value) inp.value = f.default_value
        inputs.set(f.name, inp)
        row.appendChild(inp)
      }

      form.appendChild(row)
    }

    const actions = document.createElement("div")
    actions.className = "gowiki-database-form-actions"

    const statusEl = document.createElement("span")
    statusEl.style.marginLeft = "8px"
    statusEl.style.fontSize = "12px"

    const createBtn = document.createElement("button")
    createBtn.textContent = "Create"
    createBtn.addEventListener("click", async () => {
      const fieldValues: Record<string, any> = {}
      for (const f of fields) {
        const el = inputs.get(f.name)
        if (el) fieldValues[f.name] = el.value
      }

      try {
        const resp = await fetch(`/api/database/${encodeURIComponent(this.node.attrs.table)}/rows`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ fields: fieldValues }),
        })
        if (resp.ok) {
          statusEl.textContent = "Created!"
          statusEl.style.color = "#155724"
          // Clear form.
          for (const el of inputs.values()) el.value = ""
          setTimeout(() => { statusEl.textContent = "" }, 3000)
          // Notify query NodeViews on the same page to refresh.
          document.dispatchEvent(new CustomEvent(DATABASE_ROW_CREATED, {
            detail: { table: this.node.attrs.table },
          }))
        } else {
          const err = await resp.json().catch(() => ({}))
          statusEl.textContent = err.error || "Failed"
          statusEl.style.color = "#c33"
        }
      } catch {
        statusEl.textContent = "Network error"
        statusEl.style.color = "#c33"
      }
    })

    actions.appendChild(createBtn)
    actions.appendChild(statusEl)
    form.appendChild(actions)
    this.dom.appendChild(form)
  }

  private showError(message: string) {
    const el = document.createElement("div")
    el.className = "gowiki-database-error"
    el.textContent = message
    this.dom.appendChild(el)
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    if (node.attrs.table !== this.node.attrs.table) {
      this.node = node
      this.render()
      return true
    }
    this.node = node
    return true
  }

  stopEvent(): boolean { return true }
  ignoreMutation(): boolean { return true }
  destroy() {}
}

class DatabaseRowNodeView {
  dom: HTMLElement
  private node: PMNode
  private view: EditorView
  private getPos: () => number | undefined
  private registry: Registry
  private schemaFields: any[] = []
  private selfUpdate = false // flag to skip re-render on our own attr changes

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined, registry: Registry) {
    this.node = node
    this.view = view
    this.getPos = getPos
    this.registry = registry

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-database-row"
    this.dom.contentEditable = "false"

    this.render()
  }

  private render() {
    this.dom.innerHTML = ""
    const table = this.node.attrs.table
    if (!table) {
      this.dom.innerHTML = '<div class="gowiki-database-error">No table specified</div>'
      return
    }

    const label = document.createElement("div")
    label.className = "gowiki-database-row-label"
    label.textContent = `Row: ${table}`
    this.dom.appendChild(label)

    this.fetchSchemaAndRender()
  }

  private async fetchSchemaAndRender() {
    const table = this.node.attrs.table
    try {
      const resp = await fetch(`/api/database/${encodeURIComponent(table)}/schema`)
      if (resp.ok) {
        const schema = await resp.json()
        this.schemaFields = (schema.fields || []).filter((f: any) => !f.archived_at)
      }
    } catch { /* ignore */ }

    if (this.view.editable) {
      this.renderEditMode()
    } else {
      this.renderViewMode()
    }
    document.dispatchEvent(new Event("gowiki:node-rendered"))
  }

  // ── Edit mode: inputs that update node attrs (synced to DB on publish) ──

  private renderEditMode() {
    const tbl = document.createElement("table")
    tbl.className = "gowiki-database-table"

    const fields = this.node.attrs._fields || {}
    const fieldMap = new Map<string, any>()
    for (const f of this.schemaFields) fieldMap.set(f.name, f)

    for (const [key, val] of Object.entries(fields)) {
      const tr = document.createElement("tr")
      const f = fieldMap.get(key)

      const tdKey = document.createElement("td")
      tdKey.style.fontWeight = "600"
      tdKey.textContent = f?.label || key
      tr.appendChild(tdKey)

      const tdVal = document.createElement("td")

      if ((f && f.type === "auto_increment") || key === "id") {
        // auto_increment / system id: read-only.
        tdVal.textContent = String(val)
      } else {
        // Editable input, type-aware.
        const input = this.createFieldInput(f, String(val))
        const commitChange = () => {
          const newVal = (input as any).value
          this.updateField(key, newVal)
        }
        if (input instanceof HTMLDivElement) {
          // Image wrapper: listen on the inner input.
          const innerInput = input.querySelector("input")
          if (innerInput) {
            innerInput.addEventListener("change", commitChange)
            innerInput.addEventListener("blur", commitChange)
          }
        } else {
          input.addEventListener("change", commitChange)
          if (input instanceof HTMLInputElement) {
            input.addEventListener("blur", commitChange)
          }
        }
        tdVal.appendChild(input)
      }

      tr.appendChild(tdVal)
      tbl.appendChild(tr)
    }

    if (Object.keys(fields).length === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-database-loading"
      empty.textContent = "No fields"
      this.dom.appendChild(empty)
    } else {
      this.dom.appendChild(tbl)
    }
  }

  private createFieldInput(f: any, value: string): HTMLInputElement | HTMLSelectElement | HTMLDivElement {
    if (f && f.type === "image") {
      const wrap = document.createElement("div")
      wrap.className = "db-image-input-wrap"
      const inp = document.createElement("input")
      inp.type = "text"
      inp.value = value
      inp.placeholder = "/path/to/image.png"
      inp.style.fontSize = "inherit"
      isolateInput(inp)
      wrap.appendChild(inp)
      if (value) {
        const preview = document.createElement("img")
        preview.src = value
        preview.className = "db-image-preview"
        wrap.appendChild(preview)
      }
      const btn = document.createElement("button")
      btn.type = "button"
      btn.textContent = "Browse"
      isolateInput(btn)
      btn.addEventListener("click", () => {
        const ns = window.location.pathname.replace(/^\/+/, "").replace(/\/[^/]*$/, "") || ""
        openMediaManager(ns, () => {}, (_type: string, entry: any) => { const insertedPath = entry.path;
          inp.value = insertedPath
          inp.dispatchEvent(new Event("change"))
          // Update or add preview.
          const existing = wrap.querySelector(".db-image-preview")
          if (existing) { (existing as HTMLImageElement).src = insertedPath }
          else {
            const img = document.createElement("img")
            img.src = insertedPath
            img.className = "db-image-preview"
            wrap.insertBefore(img, btn)
          }
        })
      })
      wrap.appendChild(btn)
      // Expose .value on the wrapper for the commitChange handler.
      Object.defineProperty(wrap, "value", {
        get: () => inp.value,
        set: (v: string) => { inp.value = v },
      })
      return wrap
    }
    if (f && f.type === "color") {
      const inp = document.createElement("input")
      inp.type = "color"
      inp.value = value || "#adb5bd"
      inp.style.width = "50px"
      inp.style.height = "30px"
      inp.style.padding = "0"
      inp.style.border = "1px solid #ccc"
      inp.style.cursor = "pointer"
      isolateInput(inp)
      return inp
    }
    if (f && f.type === "tag" && f.foreign_key) {
      const sel = document.createElement("select")
      sel.style.width = "100%"
      sel.style.fontSize = "inherit"
      const emptyOpt = document.createElement("option")
      emptyOpt.value = ""
      emptyOpt.textContent = "-- Select --"
      sel.appendChild(emptyOpt)
      isolateInput(sel)
      // Populate async.
      getTagOptions(f.foreign_key).then(tags => {
        tags.forEach((t, id) => {
          const opt = document.createElement("option")
          opt.value = String(id)
          opt.textContent = t.label || String(id)
          if (String(id) === value) opt.selected = true
          sel.appendChild(opt)
        })
      })
      return sel
    }
    if (f && f.type === "lookup" && f.foreign_key) {
      const sel = document.createElement("select")
      sel.style.width = "100%"
      sel.style.fontSize = "inherit"
      const emptyOpt = document.createElement("option")
      emptyOpt.value = ""
      emptyOpt.textContent = "-- Select --"
      sel.appendChild(emptyOpt)
      isolateInput(sel)
      getLookupOptions(f.foreign_key, f.display_column).then(opts => {
        opts.forEach((label, id) => {
          const opt = document.createElement("option")
          opt.value = String(id)
          opt.textContent = label
          if (String(id) === value) opt.selected = true
          sel.appendChild(opt)
        })
      })
      return sel
    }
    if (f && f.type === "user") {
      const sel = document.createElement("select")
      sel.style.width = "100%"
      sel.style.fontSize = "inherit"
      const emptyOpt = document.createElement("option")
      emptyOpt.value = ""
      emptyOpt.textContent = "-- Select --"
      sel.appendChild(emptyOpt)
      isolateInput(sel)
      getUserList().then(users => {
        for (const u of users) {
          const opt = document.createElement("option")
          opt.value = u.username
          opt.textContent = u.display_name || u.username
          if (u.username === value) opt.selected = true
          sel.appendChild(opt)
        }
      })
      return sel
    }
    if (f && (f.type === "enum" || f.type === "multi_enum")) {
      const sel = document.createElement("select")
      sel.style.width = "100%"
      sel.style.fontSize = "inherit"
      const emptyOpt = document.createElement("option")
      emptyOpt.value = ""
      emptyOpt.textContent = "-- Select --"
      sel.appendChild(emptyOpt)
      for (const v of (f.enum_values || [])) {
        const opt = document.createElement("option")
        opt.value = v
        opt.textContent = v
        if (v === value) opt.selected = true
        sel.appendChild(opt)
      }
      isolateInput(sel)
      return sel
    }
    if (f && f.type === "boolean") {
      const sel = document.createElement("select")
      sel.style.width = "100%"
      sel.style.fontSize = "inherit"
      for (const v of [
        { val: "", label: "-- Select --" },
        { val: "true", label: "Yes" },
        { val: "false", label: "No" },
      ]) {
        const opt = document.createElement("option")
        opt.value = v.val
        opt.textContent = v.label
        if (v.val === value) opt.selected = true
        sel.appendChild(opt)
      }
      isolateInput(sel)
      return sel
    }
    const inp = document.createElement("input")
    inp.style.width = "100%"
    inp.style.boxSizing = "border-box"
    inp.style.padding = "2px 4px"
    inp.style.fontSize = "inherit"
    inp.type = f?.type === "date" ? "date" : f?.type === "datetime" ? "datetime-local"
      : f?.type === "integer" || f?.type === "float" ? "number" : "text"
    inp.value = value
    isolateInput(inp)
    return inp
  }

  private updateField(fieldName: string, newValue: string) {
    const pos = this.getPos()
    if (pos === undefined) return
    const newFields = { ...this.node.attrs._fields, [fieldName]: newValue }
    this.selfUpdate = true
    const tr = this.view.state.tr.setNodeMarkup(pos, null, {
      ...this.node.attrs,
      _fields: newFields,
    })
    this.view.dispatch(tr)
  }

  // ── View mode: static text with dblclick inline edit (immediate API sync) ──

  private renderViewMode() {
    const tbl = document.createElement("table")
    tbl.className = "gowiki-database-table"

    const fields = this.node.attrs._fields || {}
    const fieldMap = new Map<string, any>()
    for (const f of this.schemaFields) fieldMap.set(f.name, f)

    for (const [key, val] of Object.entries(fields)) {
      const tr = document.createElement("tr")
      const f = fieldMap.get(key)

      const tdKey = document.createElement("td")
      tdKey.style.fontWeight = "600"
      tdKey.textContent = f?.label || key
      tr.appendChild(tdKey)

      const tdVal = document.createElement("td")
      if (f && f.type === "image" && val) {
        const img = document.createElement("img")
        img.src = String(val)
        img.className = "db-image-cell"
        tdVal.appendChild(img)
      } else if (f && f.type === "color" && val) {
        tdVal.appendChild(renderColorSwatch(String(val)))
      } else if (f && f.type === "tag" && f.foreign_key) {
        if (val && Number(val) !== 0) {
          tdVal.textContent = "..."
          getTagOptions(f.foreign_key).then(tags => {
            tdVal.textContent = ""
            const tag = tags.get(Number(val))
            if (tag) tdVal.appendChild(renderTagBadge(tag))
            else tdVal.textContent = String(val)
          })
        }
      } else if (f && f.type === "lookup" && f.foreign_key) {
        if (val && Number(val) !== 0) {
          tdVal.textContent = "..."
          getLookupOptions(f.foreign_key, f.display_column).then(opts => {
            tdVal.textContent = opts.get(Number(val)) || String(val)
          })
        }
      } else if (f && f.type === "user" && val) {
        tdVal.textContent = String(val)
        fetchUserInfo(String(val)).then(info => {
          if (info.label) tdVal.textContent = info.label
        })
      } else {
        tdVal.textContent = String(val)
      }

      // Inline editing forbidden for auto_increment and system id fields.
      if (!(f && f.type === "auto_increment") && key !== "id") {
        tdVal.className = "gowiki-database-editable-value"
        tdVal.addEventListener("dblclick", () => {
          this.inlineEdit(tdVal, f, String(key), String(val))
        })
      }

      tr.appendChild(tdVal)
      tbl.appendChild(tr)
    }

    if (Object.keys(fields).length === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-database-loading"
      empty.textContent = "No fields"
      this.dom.appendChild(empty)
    } else {
      this.dom.appendChild(tbl)
    }
  }

  /** Update _fields in PM state so template_var refresh plugin picks it up. */
  private syncFieldToState(fieldName: string, newValue: string) {
    const pos = this.getPos()
    if (pos === undefined) return
    const newFields = { ...this.node.attrs._fields, [fieldName]: newValue }
    this.selfUpdate = true
    const tr = this.view.state.tr.setNodeMarkup(pos, null, {
      ...this.node.attrs,
      _fields: newFields,
    })
    this.view.dispatch(tr)
  }

  private async inlineEdit(td: HTMLElement, fieldDef: any, fieldName: string, currentValue: string) {
    const table = this.node.attrs.table
    if (!table) return

    const saveToApi = async (newValue: string, force = false): Promise<boolean> => {
      try {
        const pagePath = window.location.pathname.replace(/^\/+/, "")
        const url = `/api/database/${encodeURIComponent(table)}/page/${pagePath}${force ? "?force=true" : ""}`
        const resp = await fetch(url, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ fields: { [fieldName]: newValue } }),
        })
        if (resp.status === 409) {
          const body = await resp.json().catch(() => ({}))
          if (body.error === "page_draft_conflict") {
            const ok = confirm(`A draft by "${body.draft_owner}" exists for this page. Force the edit?`)
            if (ok) return saveToApi(newValue, true)
            return false
          }
        }
        if (!resp.ok) {
          td.style.color = "#c33"
          setTimeout(() => { td.style.color = "" }, 2000)
          return false
        }
        this.syncFieldToState(fieldName, newValue)
        return true
      } catch {
        td.style.color = "#c33"
        setTimeout(() => { td.style.color = "" }, 2000)
        return false
      }
    }

    if (fieldDef && fieldDef.type === "enum") {
      createOverlaySelect(td, {
        options: fieldDef.enum_values || [],
        value: currentValue,
        onSave: async (newVal) => {
          td.textContent = newVal
          td.className = "gowiki-database-editable-value"
          await saveToApi(newVal)
        },
        onCancel: () => {},
      })
    } else if (fieldDef && fieldDef.type === "boolean") {
      const newVal = currentValue === "true" ? "false" : "true"
      td.textContent = newVal
      const ok = await saveToApi(newVal)
      if (!ok) { td.textContent = currentValue }
    } else if (fieldDef && fieldDef.type === "color") {
      createOverlayColorPicker(td, {
        value: currentValue || "#adb5bd",
        onSave: async (newVal) => {
          td.textContent = ""
          td.appendChild(renderColorSwatch(newVal))
          td.className = "gowiki-database-editable-value"
          const ok = await saveToApi(newVal)
          if (!ok) {
            td.textContent = ""
            td.appendChild(renderColorSwatch(currentValue))
          }
        },
        onCancel: () => {
          td.textContent = ""
          td.appendChild(renderColorSwatch(currentValue))
          td.className = "gowiki-database-editable-value"
        },
      })
    } else if (fieldDef && fieldDef.type === "tag" && fieldDef.foreign_key) {
      getTagOptions(fieldDef.foreign_key).then(tags => {
        const options: { value: string; label: string }[] = []
        tags.forEach((t, id) => options.push({ value: String(id), label: t.label || String(id) }))
        createOverlaySelectKeyed(td, {
          options,
          value: currentValue,
          onSave: async (newVal) => {
            td.textContent = ""
            const tag = tags.get(Number(newVal))
            if (tag) td.appendChild(renderTagBadge(tag))
            else td.textContent = newVal
            td.className = "gowiki-database-editable-value"
            await saveToApi(newVal)
          },
          onCancel: () => {},
        })
      })
    } else if (fieldDef && fieldDef.type === "lookup" && fieldDef.foreign_key) {
      getLookupOptions(fieldDef.foreign_key, fieldDef.display_column).then(opts => {
        const options: { value: string; label: string }[] = []
        opts.forEach((label, id) => options.push({ value: String(id), label }))
        createOverlaySelectKeyed(td, {
          options,
          value: currentValue,
          onSave: async (newVal) => {
            td.textContent = opts.get(Number(newVal)) || newVal
            td.className = "gowiki-database-editable-value"
            await saveToApi(newVal)
          },
          onCancel: () => {},
        })
      })
    } else if (fieldDef && fieldDef.type === "user") {
      getUserList().then(users => {
        const options = users.map(u => ({ value: u.username, label: u.display_name || u.username }))
        createOverlaySelectKeyed(td, {
          options,
          value: currentValue,
          onSave: async (newVal) => {
            const u = users.find(u => u.username === newVal)
            td.textContent = u?.display_name || newVal
            td.className = "gowiki-database-editable-value"
            await saveToApi(newVal)
          },
          onCancel: () => {},
        })
      })
    } else if (fieldDef && fieldDef.type === "image") {
      const restoreImage = (path: string) => {
        td.textContent = ""
        td.className = "gowiki-database-editable-value"
        if (path) {
          const img = document.createElement("img")
          img.src = path
          img.className = "db-image-cell"
          td.appendChild(img)
        }
      }
      createOverlayImageInput(td, {
        value: currentValue,
        onSave: async (newVal) => {
          restoreImage(newVal)
          const ok = await saveToApi(newVal)
          if (!ok) { restoreImage(currentValue) }
        },
        onCancel: () => { restoreImage(currentValue) },
      })
    } else {
      createOverlayInput(td, {
        type: fieldDef?.type === "date" ? "date"
          : fieldDef?.type === "integer" || fieldDef?.type === "float" ? "number" : "text",
        value: currentValue,
        onSave: async (newVal) => {
          td.textContent = newVal
          td.className = "gowiki-database-editable-value"
          const ok = await saveToApi(newVal)
          if (!ok) { td.textContent = currentValue }
        },
        onCancel: () => {
          td.textContent = currentValue
          td.className = "gowiki-database-editable-value"
        },
      })
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    if (this.selfUpdate) {
      // Our own attr change — don't re-render (would destroy inputs).
      this.selfUpdate = false
      return true
    }
    this.render()
    return true
  }

  stopEvent(event: Event): boolean {
    // Stop mouse events to prevent ProseMirror from creating text selections
    // across the document when clicking inside database tables.
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click" || type === "dblclick") return true
    const tag = (event.target as HTMLElement)?.tagName
    if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return true
    return false
  }

  ignoreMutation(): boolean { return true }
  destroy() {}
}

// ── Global Variable Resolution ──

const ALL_CAPS_RE = /^[A-Z_]+$/

// Cache for user display info: username -> { display_name, email, label }
const userInfoCache: Record<string, { display_name: string; email: string; label: string }> = {}

async function fetchUserInfo(username: string): Promise<{ display_name: string; email: string; label: string }> {
  if (!username) return { display_name: "", email: "", label: "" }
  if (userInfoCache[username]) return userInfoCache[username]
  try {
    const resp = await fetch(`/api/users/display?users=${encodeURIComponent(username)}`)
    if (resp.ok) {
      const data = await resp.json()
      const info = data.users?.[username]
      if (info) {
        userInfoCache[username] = {
          display_name: info.display_name || username,
          email: info.email || "",
          label: info.label || username,
        }
        return userInfoCache[username]
      }
    }
  } catch { /* best effort */ }
  return { display_name: username, email: "", label: username }
}

function extractTitle(view: EditorView): string {
  let title = ""
  view.state.doc.descendants((node) => {
    if (!title && node.type.name === "heading") {
      title = node.textContent
      return false
    }
  })
  return title
}

/**
 * Resolve a global variable synchronously from available context.
 *
 * Return values:
 *   null      — not an ALL_CAPS name → fall back to database template resolution
 *   undefined — ALL_CAPS but not a recognized global variable → ERROR
 *   ""        — recognized, but data unavailable → show fallback or nothing
 *   "value"   — resolved value
 */
function resolveGlobalVar(name: string, view: EditorView): string | null | undefined {
  if (!ALL_CAPS_RE.test(name)) return null

  const ctx = (window as any).__gowikiGlobalVarContext?.()
  if (!ctx) return ""

  const meta = ctx.pageMeta

  switch (name) {
    // Page variables
    case "ID":    return ctx.pagePath || ""
    case "PATH":  return ctx.pageNamespace || ""
    case "PAGE":  return ctx.pageName || ""
    case "TITLE": return extractTitle(view)

    // Link variables
    case "EXTID":   return `${window.location.origin}${ctx.pagePath || ""}`
    case "EXTPATH": return `${window.location.origin}${ctx.pageNamespace || ""}${ctx.pageNamespace?.endsWith("/") ? "" : "/"}`
    case "SERVER":  return window.location.hostname

    // Version variables
    case "VERSION":     return meta?.version != null ? String(meta.version) : ""
    case "VERSIONDATE": {
      if (!meta?.updated_at) return ""
      const d = new Date(meta.updated_at)
      return isNaN(d.getTime()) ? "" : d.toISOString().slice(0, 10)
    }
    case "VERSIONTAG": return meta?.version_tag || ""
    case "YEAR": {
      if (!meta?.updated_at) return ""
      const d = new Date(meta.updated_at)
      return isNaN(d.getTime()) ? "" : String(d.getFullYear())
    }
    case "MONTH": {
      if (!meta?.updated_at) return ""
      const d = new Date(meta.updated_at)
      return isNaN(d.getTime()) ? "" : String(d.getMonth() + 1).padStart(2, "0")
    }
    case "SMONTH": {
      if (!meta?.updated_at) return ""
      const d = new Date(meta.updated_at)
      return isNaN(d.getTime()) ? "" : String(d.getMonth() + 1)
    }
    case "DAY": {
      if (!meta?.updated_at) return ""
      const d = new Date(meta.updated_at)
      return isNaN(d.getTime()) ? "" : String(d.getDate()).padStart(2, "0")
    }
    case "SDAY": {
      if (!meta?.updated_at) return ""
      const d = new Date(meta.updated_at)
      return isNaN(d.getTime()) ? "" : String(d.getDate())
    }

    // Creation date variable
    case "CREATIONDATE": {
      if (!meta?.created_at) return ""
      const d = new Date(meta.created_at)
      return isNaN(d.getTime()) ? "" : d.toISOString().slice(0, 10)
    }

    // Author variables (sync: return username, trigger async display name fetch)
    case "AUTHOR":         return meta?.created_by || ""
    case "AUTHORNAME":     return userInfoCache[meta?.created_by]?.display_name || meta?.created_by || ""
    case "AUTHORMAIL":     return userInfoCache[meta?.created_by]?.email || ""
    case "LASTAUTHOR":     return meta?.author || ""
    case "LASTAUTHORNAME": return userInfoCache[meta?.author]?.display_name || meta?.author || ""
    case "LASTAUTHORMAIL": return userInfoCache[meta?.author]?.email || ""

    // Wiki variables
    case "WIKI":        return ctx.siteTitle || ""
    case "WIKIVERSION": return ctx.siteVersion || ""
  }

  return undefined // ALL_CAPS but not a recognized global variable → ERROR
}

// ── Template Variable NodeView ──

// Cache table field types for template variable user resolution.
const tableFieldTypeCache: Map<string, Map<string, string>> = new Map()

async function getFieldTypes(tableName: string): Promise<Map<string, string>> {
  if (tableFieldTypeCache.has(tableName)) return tableFieldTypeCache.get(tableName)!
  try {
    const resp = await fetch(`/api/database/${encodeURIComponent(tableName)}/schema`)
    if (!resp.ok) return new Map()
    const schema = await resp.json()
    const types = new Map<string, string>()
    for (const f of (schema.fields || [])) {
      if (!f.archived_at) types.set(f.name, f.type)
    }
    tableFieldTypeCache.set(tableName, types)
    return types
  } catch { return new Map() }
}

function resolveTemplateFields(state: EditorState): { fields: Record<string, string>; table: string } {
  const fields: Record<string, string> = {}
  let table = ""
  state.doc.descendants((node) => {
    if (node.type.name === "database_row" && node.attrs._fields) {
      if (node.attrs.table && !table) table = node.attrs.table
      for (const [k, v] of Object.entries(node.attrs._fields as Record<string, any>)) {
        if (!(k in fields)) fields[k] = String(v)
      }
    }
  })
  return { fields, table }
}

class TemplateVarNodeView {
  dom: HTMLElement
  private node: PMNode
  private view: EditorView

  constructor(node: PMNode, view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.dom = document.createElement("span")
    this.dom.contentEditable = "false"
    const { fields, table } = resolveTemplateFields(view.state)
    this.renderResolved(fields, table)
  }

  private renderResolved(fields: Record<string, string>, table: string) {
    const name = this.node.attrs.name || ""
    const fallback = this.node.attrs.fallback || ""

    // Empty name = user is still typing — show placeholder, not error
    if (!name) {
      this.dom.className = "gowiki-template-var"
      this.dom.textContent = "{{…}}"
      this.dom.title = "Variable (empty name)"
      return
    }

    // Try global variable resolution first (ALL_CAPS names)
    const globalResolved = resolveGlobalVar(name, this.view)
    if (globalResolved === undefined) {
      // Unknown ALL_CAPS variable → error
      this.dom.className = "gowiki-template-var gowiki-template-var-error"
      this.dom.textContent = `ERR: {{${name}}}`
      this.dom.title = `Unknown variable: ${name}`
      return
    }
    if (globalResolved !== null) {
      // Known global variable
      if (globalResolved) {
        // Has a value → show it
        this.dom.className = "gowiki-template-var"
        this.dom.textContent = globalResolved
        this.dom.title = `{{${name}}}`
      } else if (fallback) {
        // Empty value, has fallback → show fallback
        this.dom.className = "gowiki-template-var"
        this.dom.textContent = fallback
        this.dom.title = `{{${name}}} (default)`
      } else {
        // Empty value, no fallback → invisible
        this.dom.className = "gowiki-template-var"
        this.dom.textContent = ""
        this.dom.title = `{{${name}}} (empty)`
      }
      this.resolveAuthorDisplayAsync(name)
      return
    }

    // Fall back to database template variable resolution
    const resolved = fields[name]
    if (resolved !== undefined) {
      this.dom.className = "gowiki-template-var"
      this.dom.textContent = resolved
      this.dom.title = `{{${name}}}`
      // If the field is a user type, resolve to display label
      this.resolveUserFieldAsync(table, name, resolved)
    } else if (fallback) {
      // Unresolved database var with fallback → show fallback
      this.dom.className = "gowiki-template-var"
      this.dom.textContent = fallback
      this.dom.title = `{{${name}}} (default)`
    } else {
      // Unresolved database var, no fallback → error
      this.dom.className = "gowiki-template-var gowiki-template-var-error"
      this.dom.textContent = `ERR: {{${name}}}`
      this.dom.title = `Unresolved variable: ${name}`
    }
  }

  private resolveUserFieldAsync(table: string, fieldName: string, username: string) {
    if (!table || !username) return
    getFieldTypes(table).then(types => {
      if (types.get(fieldName) !== "user") return
      fetchUserInfo(username).then(info => {
        if (info.label && info.label !== username) {
          this.dom.textContent = info.label
        }
      })
    })
  }

  private resolveAuthorDisplayAsync(name: string) {
    const ctx = (window as any).__gowikiGlobalVarContext?.()
    if (!ctx?.pageMeta) return

    let username = ""
    if (name === "AUTHORNAME" || name === "AUTHORMAIL") {
      username = ctx.pageMeta.created_by || ""
    } else if (name === "LASTAUTHORNAME" || name === "LASTAUTHORMAIL") {
      username = ctx.pageMeta.author || ""
    }
    if (!username || userInfoCache[username]) return

    // Fetch user info and re-render once available
    fetchUserInfo(username).then(() => {
      const val = resolveGlobalVar(name, this.view)
      if (val) {
        this.dom.textContent = val
      }
    })
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    const { fields, table } = resolveTemplateFields(this.view.state)
    this.renderResolved(fields, table)
    return true
  }

  ignoreMutation(): boolean { return true }
  destroy() {}
}

// ── Plugin Registration ──

export const databasePlugin: WikiPlugin = {
  register(reg) {
    // ── Schema nodes ──

    reg.registerSchema({
      nodes: {
        database_query: {
          group: "block",
          atom: true,
          attrs: {
            table: { default: "" },
            fields: { default: "" },
            filter: { default: "" },
            sort: { default: "" },
            order: { default: "asc" },
            limit: { default: "20" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-database-query",
                "data-table": node.attrs.table ?? "",
              },
              `Database query: ${node.attrs.table || "(no table)"}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-database-query",
              getAttrs(dom: HTMLElement) {
                return { table: dom.getAttribute("data-table") || "" }
              },
            },
          ],
        },
        database_newrow: {
          group: "block",
          atom: true,
          attrs: {
            table: { default: "" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-database-newrow",
                "data-table": node.attrs.table ?? "",
              },
              `New row form: ${node.attrs.table || "(no table)"}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-database-newrow",
              getAttrs(dom: HTMLElement) {
                return { table: dom.getAttribute("data-table") || "" }
              },
            },
          ],
        },
        database_row: {
          group: "block",
          atom: true,
          attrs: {
            table: { default: "" },
            _fields: { default: {} },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-database-row",
                "data-table": node.attrs.table ?? "",
              },
              `Database row: ${node.attrs.table || "(no table)"}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-database-row",
              getAttrs(dom: HTMLElement) {
                return { table: dom.getAttribute("data-table") || "" }
              },
            },
          ],
        },
        template_var: {
          group: "inline",
          inline: true,
          atom: true,
          attrs: {
            name: { default: "" },
            fallback: { default: "" },
          },
          toDOM(node: PMNode) {
            const fb = node.attrs.fallback ? `:${node.attrs.fallback}` : ""
            return [
              "span",
              {
                class: "gowiki-template-var",
                "data-var": node.attrs.name,
                "data-var-fallback": node.attrs.fallback || "",
              },
              `{{${node.attrs.name}${fb}}}`,
            ]
          },
          parseDOM: [
            {
              tag: "span.gowiki-template-var",
              getAttrs(dom: HTMLElement) {
                return {
                  name: dom.getAttribute("data-var") || "",
                  fallback: dom.getAttribute("data-var-fallback") || "",
                }
              },
            },
          ],
        },
      },
    })

    // ── Self-contained directives ──

    reg.registerSelfContainedDirective("database-query", {
      tokenType: "database_query",
      nodeType: "database_query",
      properties: databaseQueryProperties,
    })

    reg.registerSelfContainedDirective("database-newrow", {
      tokenType: "database_newrow",
      nodeType: "database_newrow",
      properties: databaseNewRowProperties,
    })

    // database-row is handled by a custom markdown-it block rule (see below)
    // because it needs to consume the following table block.

    // ── Markdown-it plugin for {database-row ...} + table ──

    reg.registerMarkdownItPlugin((md: any) => {
      md.block.ruler.before("table", "database_row_block", (state: any, startLine: number, endLine: number, silent: boolean) => {
        const start = state.bMarks[startLine] + state.tShift[startLine]
        const max = state.eMarks[startLine]
        const line = state.src.slice(start, max).trim()

        // Match {database-row table=...} or bare {database-row} (template placeholder)
        const fullMatch = line.match(/^\{database-row\s+table=(?:"([^"]+)"|'([^']+)'|(\S+?))\s*\}$/)
        const bareMatch = !fullMatch && /^\{database-row\s*\}$/.test(line)
        if (!fullMatch && !bareMatch) return false
        if (silent) return true

        const tableName = fullMatch ? (fullMatch[1] || fullMatch[2] || fullMatch[3]) : ""

        // Bare placeholder — just emit the node, don't consume a following table.
        if (bareMatch) {
          const token = state.push("database_row_block", "", 0)
          token.block = true
          token.map = [startLine, startLine + 1]
          token.meta = { tableName: "", fields: {} }
          state.line = startLine + 1
          return true
        }

        // Look ahead for a 2-column table (Field | Value).
        let nextLine = startLine + 1
        // Skip blank lines.
        while (nextLine < endLine && state.src.slice(state.bMarks[nextLine], state.eMarks[nextLine]).trim() === "") {
          nextLine++
        }

        const fields: Record<string, string> = {}
        const tableRowRe = /^\s*\|(.+)\|(.+)\|\s*$/
        const tableSepRe = /^\s*\|[\s:|-]+\|\s*$/

        // Try to parse header row.
        if (nextLine < endLine && tableRowRe.test(state.src.slice(state.bMarks[nextLine], state.eMarks[nextLine]))) {
          nextLine++ // skip header
        }
        // Try to parse separator row.
        if (nextLine < endLine && tableSepRe.test(state.src.slice(state.bMarks[nextLine], state.eMarks[nextLine]))) {
          nextLine++ // skip separator
        }
        // Parse data rows.
        while (nextLine < endLine) {
          const rowLine = state.src.slice(state.bMarks[nextLine], state.eMarks[nextLine])
          const rowMatch = tableRowRe.exec(rowLine)
          if (!rowMatch) break
          const key = rowMatch[1].trim()
          const val = rowMatch[2].trim()
          if (key && key !== "---" && key !== "Field") {
            fields[key] = val
          }
          nextLine++
        }

        const token = state.push("database_row_block", "", 0)
        token.block = true
        token.map = [startLine, nextLine]
        token.meta = { tableName, fields }

        state.line = nextLine
        return true
      })
    })

    // ── Markdown → PM: handle synthetic tokens ──

    reg.registerText("database_query", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.database_query.create({
            table: attrs.table ?? "",
            filter: attrs.filter ?? "",
            sort: attrs.sort ?? "",
            order: attrs.order ?? "asc",
            limit: attrs.limit ?? "20",
          })
        )
      },
    })

    reg.registerText("database_newrow", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.database_newrow.create({
            table: attrs.table ?? "",
          })
        )
      },
    })

    reg.registerText("database_row_block", {
      run(ctx, tok) {
        const meta = tok.meta ?? {}
        ctx.push(
          ctx.schema.nodes.database_row.create({
            table: meta.tableName ?? "",
            _fields: meta.fields ?? {},
          })
        )
      },
    })

    reg.registerText("template_var", {
      run(ctx, tok) {
        ctx.push(
          ctx.schema.nodes.template_var.create({
            name: tok.meta?.name ?? "",
            fallback: tok.meta?.fallback ?? "",
          })
        )
      },
    })

    // ── PM → Markdown: serialize back ──

    reg.registerPMNode("database_query", {
      print(node) {
        const parts = [`table=${node.attrs.table}`]
        if (node.attrs.fields) {
          const c = node.attrs.fields
          parts.push(c.includes(" ") ? `fields="${c}"` : `fields=${c}`)
        }
        if (node.attrs.filter) {
          const f = node.attrs.filter
          parts.push(f.includes("=") || f.includes(">") || f.includes("<") ? `filter="${f}"` : `filter=${f}`)
        }
        if (node.attrs.sort) parts.push(`sort=${node.attrs.sort}`)
        if (node.attrs.order && node.attrs.order !== "asc") parts.push(`order=${node.attrs.order}`)
        if (node.attrs.limit && node.attrs.limit !== "20") parts.push(`limit=${node.attrs.limit}`)
        return `{database-query ${parts.join(" ")}}\n\n`
      },
    })

    reg.registerPMNode("database_newrow", {
      print(node) {
        return `{database-newrow table=${node.attrs.table}}\n\n`
      },
    })

    reg.registerPMNode("database_row", {
      print(node) {
        const table = node.attrs.table || ""
        const fields = node.attrs._fields || {}
        const entries = Object.entries(fields)

        // Bare placeholder (template) — no table attribute, no field table
        if (!table) {
          return `{database-row}\n\n`
        }

        let md = `{database-row table=${table}}\n`
        if (entries.length > 0) {
          md += `| Field | Value |\n`
          md += `| --- | --- |\n`
          for (const [k, v] of entries) {
            md += `| ${k} | ${v} |\n`
          }
        }
        md += "\n"
        return md
      },
    })

    reg.registerPMNode("template_var", {
      print(node) {
        const name = node.attrs.name || "NAME"
        const fb = node.attrs.fallback ? `:${node.attrs.fallback}` : ""
        return `{{${name}${fb}}}`
      },
    })

    // ── Editor plugin: NodeViews ──

    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.database"),
        filterTransaction(tr, state) {
          if (!tr.docChanged) return true
          let oldBound = 0
          state.doc.descendants(n => { if (n.type.name === "database_row" && n.attrs.table) oldBound++ })
          if (oldBound === 0) return true
          // A bound row can become a placeholder (table cleared), but must not be deleted.
          let newTotal = 0
          tr.doc.descendants(n => { if (n.type.name === "database_row") newTotal++ })
          return newTotal >= oldBound
        },
        props: {
          nodeViews: {
            database_query(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new DatabaseQueryNodeView(node, view, getPos)
            },
            database_newrow(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new DatabaseNewRowNodeView(node, view, getPos)
            },
            database_row(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new DatabaseRowNodeView(node, view, getPos, reg)
            },
            template_var(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TemplateVarNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Refresh all template_var NodeViews when database_row fields change.
    reg.registerEditorPlugin((_schema: Schema) => {
      let lastFields: Record<string, string> = {}
      return new PMPlugin({
        key: new PluginKey("gowiki.templateVarRefresh"),
        view() {
          return {
            update(view: EditorView) {
              const { fields, table } = resolveTemplateFields(view.state)
              const changed = Object.keys(fields).length !== Object.keys(lastFields).length ||
                Object.entries(fields).some(([k, v]) => lastFields[k] !== v)
              if (!changed) return
              lastFields = fields
              // Re-render all template_var NodeViews by updating their DOM
              view.state.doc.descendants((node, pos) => {
                if (node.type.name === "template_var") {
                  const domNode = view.nodeDOM(pos)
                  if (domNode instanceof HTMLElement) {
                    const name = node.attrs.name || ""
                    const fallback = node.attrs.fallback || ""

                    // Empty name = placeholder state
                    if (!name) return

                    // Global variables: three-state resolution
                    const globalVal = resolveGlobalVar(name, view)
                    if (globalVal === undefined) {
                      // Unknown ALL_CAPS variable → error
                      domNode.className = "gowiki-template-var gowiki-template-var-error"
                      domNode.textContent = `ERR: {{${name}}}`
                      domNode.title = `Unknown variable: ${name}`
                      return
                    }
                    if (globalVal !== null) {
                      if (globalVal) {
                        domNode.className = "gowiki-template-var"
                        domNode.textContent = globalVal
                        domNode.title = `{{${name}}}`
                      } else if (fallback) {
                        domNode.className = "gowiki-template-var"
                        domNode.textContent = fallback
                        domNode.title = `{{${name}}} (default)`
                      } else {
                        domNode.className = "gowiki-template-var"
                        domNode.textContent = ""
                        domNode.title = `{{${name}}} (empty)`
                      }
                      return
                    }

                    // Database template variables
                    const resolved = fields[name]
                    if (resolved !== undefined) {
                      domNode.className = "gowiki-template-var"
                      domNode.textContent = resolved
                      domNode.title = `{{${name}}}`
                      // Resolve user fields to display label
                      if (table && resolved) {
                        getFieldTypes(table).then(types => {
                          if (types.get(name) !== "user") return
                          fetchUserInfo(resolved).then(info => {
                            if (info.label && info.label !== resolved) {
                              domNode.textContent = info.label
                            }
                          })
                        })
                      }
                    } else if (fallback) {
                      domNode.className = "gowiki-template-var"
                      domNode.textContent = fallback
                      domNode.title = `{{${name}}} (default)`
                    } else {
                      domNode.className = "gowiki-template-var gowiki-template-var-error"
                      domNode.textContent = `ERR: {{${name}}}`
                      domNode.title = `Unresolved variable: ${name}`
                    }
                  }
                }
              })
            },
          }
        },
      })
    })

    // ── Commands ──

    reg.registerCommand("database", "insertQuery", (state, dispatch) => {
      const type = reg.schema.nodes.database_query
      if (!type) return false
      if (dispatch) {
        requestInputFocus("table")
        const node = type.create({ table: "" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === type && insertedAt === null) {
              insertedAt = pos
              return false
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch {}
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    reg.registerCommand("database", "insertNewRow", (state, dispatch) => {
      const type = reg.schema.nodes.database_newrow
      if (!type) return false
      if (dispatch) {
        requestInputFocus("table")
        const node = type.create({ table: "" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === type && insertedAt === null) {
              insertedAt = pos
              return false
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch {}
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    reg.registerCommand("database", "insertRow", (state, dispatch) => {
      const type = reg.schema.nodes.database_row
      if (!type) return false
      // Disallow if a bound database_row (with table attr) already exists.
      let hasBoundRow = false
      state.doc.descendants(n => {
        if (n.type.name === "database_row" && n.attrs.table) hasBoundRow = true
      })
      if (hasBoundRow) return false
      if (dispatch) {
        requestInputFocus("table")
        const node = type.create({ table: "", _fields: {} })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === type && insertedAt === null) {
              insertedAt = pos
              return false
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch {}
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    reg.registerCommand("database", "insertVar", (state, dispatch) => {
      const type = reg.schema.nodes.template_var
      if (!type) return false
      if (dispatch) {
        const node = type.create({ name: "" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        // Find the freshly inserted node: it has name="" which is unique to a new insert.
        let insertedAt: number | null = null
        let bestDist = Infinity
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 10),
          Math.min(tr.doc.content.size, approxPos + 10),
          (n, pos) => {
            if (n.type === type && n.attrs.name === "") {
              const dist = Math.abs(pos - approxPos)
              if (dist < bestDist) {
                bestDist = dist
                insertedAt = pos
              }
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
            requestInputFocus("name")
          } catch {}
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    // ── Node properties ──

    // database_query and database_newrow properties are already registered
    // by registerSelfContainedDirective above. Only database_row needs
    // explicit registration since it uses a custom markdown-it block rule.
    reg.registerNodeProperties("database_row", databaseRowProperties)

    reg.registerNodeProperties("template_var", [
      {
        name: "name",
        label: "Variable",
        default: "",
        parse: (raw: string) => raw.trim(),
        serialize: (value: string | null) => String(value ?? ""),
      },
      {
        name: "fallback",
        label: "Default",
        default: "",
        parse: (raw: string) => raw,
        serialize: (value: string | null) => String(value ?? ""),
      },
    ])

    // ── Styles ──

    reg.registerStyle("database", databaseStyles)
  },
}
