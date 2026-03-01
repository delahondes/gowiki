import { Plugin as PMPlugin, PluginKey, NodeSelection, EditorState } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import type { Registry } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

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
    name: "filter",
    label: "Filter",
    default: "",
    parse: (raw: string) => raw.trim() || null,
    serialize: (value: string | null) => {
      const v = String(value ?? "")
      return v.includes("=") || v.includes(">") || v.includes("<") ? `"${v}"` : v
    },
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
  border: 1px solid #dee2e6;
  padding: 4px 8px;
  text-align: left;
  font-size: 13px;
}

.gowiki-database-table th {
  background: #f1f3f5;
  font-weight: 600;
  cursor: pointer;
  user-select: none;
}

.gowiki-database-table th:hover {
  background: #e9ecef;
}

.gowiki-database-table tr:nth-child(even) {
  background: #f8f9fa;
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

.gowiki-database-page-link {
  color: #228be6;
  text-decoration: none;
  cursor: pointer;
}

.gowiki-database-page-link:hover {
  text-decoration: underline;
}

/* Template variables — only styled in edit mode */
#app.gowiki-editing .gowiki-template-var {
  background: #f0f4ff;
  border-radius: 3px;
  padding: 0 3px;
  font-style: normal;
}

#app.gowiki-editing .gowiki-template-var-unresolved {
  background: #fff3cd;
  color: #856404;
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

// Custom event name for cross-NodeView communication.
const DATABASE_ROW_CREATED = "gowiki-database-row-created"

// ── NodeViews ──

class DatabaseQueryNodeView {
  dom: HTMLElement
  private node: PMNode
  private currentSort: string
  private currentOrder: string
  private currentOffset: number
  private refreshHandler: ((e: Event) => void) | null = null

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.currentSort = node.attrs.sort || ""
    this.currentOrder = node.attrs.order || "asc"
    this.currentOffset = 0

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
    const limit = parseInt(this.node.attrs.limit) || 20

    const params = new URLSearchParams()
    if (filter) {
      for (const f of filter.split("&")) {
        params.append("filter", f)
      }
    }
    if (this.currentSort) params.set("sort", this.currentSort)
    if (this.currentOrder) params.set("order", this.currentOrder)
    params.set("limit", String(limit))
    params.set("offset", String(this.currentOffset))

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
      this.renderTable(schema, data.rows || [], data.total || 0, limit)
    } catch (err) {
      this.showError("Network error")
    }
  }

  private renderTable(schema: any, rows: any[], total: number, limit: number) {
    // Remove loading, keep label.
    const existing = this.dom.querySelector(".gowiki-database-loading")
    if (existing) existing.remove()
    const existingTable = this.dom.querySelector(".gowiki-database-table")
    if (existingTable) existingTable.remove()
    const existingPag = this.dom.querySelector(".gowiki-database-pagination")
    if (existingPag) existingPag.remove()

    const fields = (schema.fields || []).filter((f: any) => !f.archived_at)

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
        this.currentOffset = 0
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
        const val = row.fields?.[f.name]
        const isIndexField = schema.index_field && f.name === schema.index_field && row.page_path
        if (isIndexField) {
          // Index field links to the page.
          const a = document.createElement("a")
          a.className = "gowiki-database-page-link"
          a.textContent = val != null ? String(val) : row.page_path
          a.href = "/" + row.page_path
          td.appendChild(a)
        } else if (f.type === "page_link" && val) {
          const a = document.createElement("a")
          a.className = "gowiki-database-page-link"
          a.textContent = String(val)
          a.href = "/" + String(val)
          td.appendChild(a)
        } else if (Array.isArray(val)) {
          td.textContent = val.join(", ")
        } else {
          td.textContent = val != null ? String(val) : ""
        }

        // Double-click inline editing — forbidden for auto_increment and index fields.
        if (f.type !== "auto_increment" && !(schema.index_field && f.name === schema.index_field)) {
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

    // Pagination.
    if (total > limit) {
      const pag = document.createElement("div")
      pag.className = "gowiki-database-pagination"

      const prev = document.createElement("button")
      prev.textContent = "\u2190 Previous"
      prev.disabled = this.currentOffset === 0
      prev.addEventListener("click", () => {
        this.currentOffset = Math.max(0, this.currentOffset - limit)
        this.fetchData()
      })
      pag.appendChild(prev)

      const info = document.createElement("span")
      const start = this.currentOffset + 1
      const end = Math.min(this.currentOffset + limit, total)
      info.textContent = `${start}\u2013${end} of ${total}`
      pag.appendChild(info)

      const next = document.createElement("button")
      next.textContent = "Next \u2192"
      next.disabled = this.currentOffset + limit >= total
      next.addEventListener("click", () => {
        this.currentOffset += limit
        this.fetchData()
      })
      pag.appendChild(next)

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
        node.attrs.filter !== this.node.attrs.filter) {
      this.node = node
      this.currentSort = node.attrs.sort || ""
      this.currentOrder = node.attrs.order || "asc"
      this.currentOffset = 0
      this.render()
      return true
    }
    this.node = node
    return true
  }

  stopEvent(event: Event): boolean {
    // If the event targets an input/select, stop ProseMirror from stealing focus.
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
    } catch {
      this.showError("Network error")
    }
  }

  private renderForm(schema: any) {
    const fields = (schema.fields || []).filter((f: any) => !f.archived_at && f.type !== "auto_increment")
    const form = document.createElement("div")
    form.className = "gowiki-database-form"

    const inputs: Map<string, HTMLInputElement | HTMLSelectElement> = new Map()

    for (const f of fields) {
      const row = document.createElement("div")
      row.className = "gowiki-database-form-field"

      const lbl = document.createElement("label")
      lbl.textContent = f.label || f.name
      row.appendChild(lbl)

      if (f.type === "enum") {
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
  private indexField: string = ""
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
        this.indexField = schema.index_field || ""
        this.schemaFields = (schema.fields || []).filter((f: any) => !f.archived_at)
      }
    } catch { /* ignore */ }

    if (this.view.editable) {
      this.renderEditMode()
    } else {
      this.renderViewMode()
    }
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

      if (key === this.indexField || (f && f.type === "auto_increment")) {
        // Index / auto_increment: read-only.
        tdVal.textContent = String(val)
      } else {
        // Editable input, type-aware.
        const input = this.createFieldInput(f, String(val))
        const commitChange = () => {
          const newVal = input instanceof HTMLSelectElement ? input.value : (input as HTMLInputElement).value
          this.updateField(key, newVal)
        }
        input.addEventListener("change", commitChange)
        if (input instanceof HTMLInputElement) {
          input.addEventListener("blur", commitChange)
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

  private createFieldInput(f: any, value: string): HTMLInputElement | HTMLSelectElement {
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
      tdVal.textContent = String(val)

      // Inline editing forbidden for index and auto_increment fields.
      if (key !== this.indexField && !(f && f.type === "auto_increment")) {
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
    // If the event targets an input/select, stop ProseMirror from stealing focus.
    const tag = (event.target as HTMLElement)?.tagName
    if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return true
    return false
  }

  ignoreMutation(): boolean { return true }
  destroy() {}
}

// ── Template Variable NodeView ──

function resolveTemplateFields(state: EditorState): Record<string, string> {
  const fields: Record<string, string> = {}
  state.doc.descendants((node) => {
    if (node.type.name === "database_row" && node.attrs._fields) {
      for (const [k, v] of Object.entries(node.attrs._fields as Record<string, any>)) {
        if (!(k in fields)) fields[k] = String(v)
      }
    }
  })
  return fields
}

class TemplateVarNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("span")
    this.dom.contentEditable = "false"
    this.renderResolved(resolveTemplateFields(view.state))
  }

  private renderResolved(fields: Record<string, string>) {
    const name = this.node.attrs.name
    const resolved = fields[name]
    if (resolved !== undefined) {
      this.dom.className = "gowiki-template-var"
      this.dom.textContent = resolved
      this.dom.title = `{{${name}}}`
    } else {
      this.dom.className = "gowiki-template-var gowiki-template-var-unresolved"
      this.dom.textContent = `{{${name}}}`
    }
  }

  update(node: PMNode, _decorations: any, _innerDecorations: any, view?: EditorView): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    // view is not passed by ProseMirror's standard update(), so we may not
    // be able to re-resolve here. The editor plugin below handles re-renders.
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
          },
          toDOM(node: PMNode) {
            return [
              "span",
              {
                class: "gowiki-template-var",
                "data-var": node.attrs.name,
              },
              `{{${node.attrs.name}}}`,
            ]
          },
          parseDOM: [
            {
              tag: "span.gowiki-template-var",
              getAttrs(dom: HTMLElement) {
                return { name: dom.getAttribute("data-var") || "" }
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
          ctx.schema.nodes.template_var.create({ name: tok.meta?.name ?? "" })
        )
      },
    })

    // ── PM → Markdown: serialize back ──

    reg.registerPMNode("database_query", {
      print(node) {
        const parts = [`table=${node.attrs.table}`]
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
        return `{{${node.attrs.name}}}`
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
              const fields = resolveTemplateFields(view.state)
              const changed = Object.keys(fields).length !== Object.keys(lastFields).length ||
                Object.entries(fields).some(([k, v]) => lastFields[k] !== v)
              if (!changed) return
              lastFields = fields
              // Re-render all template_var NodeViews by updating their DOM
              view.state.doc.descendants((node, pos) => {
                if (node.type.name === "template_var") {
                  const domNode = view.nodeDOM(pos)
                  if (domNode instanceof HTMLElement) {
                    const name = node.attrs.name
                    const resolved = fields[name]
                    if (resolved !== undefined) {
                      domNode.className = "gowiki-template-var"
                      domNode.textContent = resolved
                      domNode.title = `{{${name}}}`
                    } else {
                      domNode.className = "gowiki-template-var gowiki-template-var-unresolved"
                      domNode.textContent = `{{${name}}}`
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
        parse: (raw: string) => raw.trim() || null,
        serialize: (value: string | null) => String(value ?? ""),
      },
    ])

    // ── Styles ──

    reg.registerStyle("database", databaseStyles)
  },
}
