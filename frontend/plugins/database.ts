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
`

// ── NodeViews ──

class DatabaseQueryNodeView {
  dom: HTMLElement
  private node: PMNode
  private currentSort: string
  private currentOrder: string
  private currentOffset: number

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.currentSort = node.attrs.sort || ""
    this.currentOrder = node.attrs.order || "asc"
    this.currentOffset = 0

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-database-query"
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
    const tbody = document.createElement("tbody")
    for (const row of rows) {
      const rtr = document.createElement("tr")
      for (const f of fields) {
        const td = document.createElement("td")
        const val = row.fields?.[f.name]
        if (f.type === "page_link" && val && schema.index_field) {
          const a = document.createElement("a")
          a.className = "gowiki-database-page-link"
          a.textContent = String(val)
          a.href = "/" + String(val)
          a.addEventListener("click", (e: Event) => {
            e.preventDefault()
            window.history.pushState({}, "", "/" + String(val))
            window.dispatchEvent(new PopStateEvent("popstate"))
          })
          td.appendChild(a)
        } else if (Array.isArray(val)) {
          td.textContent = val.join(", ")
        } else {
          td.textContent = val != null ? String(val) : ""
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

  stopEvent(): boolean { return true }
  ignoreMutation(): boolean { return true }
  destroy() {}
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
  private registry: Registry

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined, registry: Registry) {
    this.node = node
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

    // Render the field/value table from the node's content if we have it.
    this.renderFieldTable()
  }

  private renderFieldTable() {
    // In view mode: render field/value pairs as a table.
    // The actual data comes from the markdown table content that follows the directive.
    // Since database_row is an atom in edit mode, we show a summary.
    const tbl = document.createElement("table")
    tbl.className = "gowiki-database-table"

    const fields = this.node.attrs._fields || {}
    for (const [key, val] of Object.entries(fields)) {
      const tr = document.createElement("tr")

      const tdKey = document.createElement("td")
      tdKey.style.fontWeight = "600"
      tdKey.textContent = String(key)
      tr.appendChild(tdKey)

      const tdVal = document.createElement("td")
      tdVal.className = "gowiki-database-editable-value"
      tdVal.textContent = String(val)

      // Double-click inline editing.
      tdVal.addEventListener("dblclick", () => {
        this.inlineEdit(tdVal, String(key), String(val))
      })

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

  private async inlineEdit(td: HTMLElement, fieldName: string, currentValue: string) {
    const table = this.node.attrs.table
    if (!table) return

    const input = document.createElement("input")
    input.type = "text"
    input.value = currentValue
    input.style.width = "100%"
    input.style.boxSizing = "border-box"
    input.style.padding = "2px 4px"
    input.style.fontSize = "inherit"

    td.textContent = ""
    td.appendChild(input)
    input.focus()

    const save = async () => {
      const newValue = input.value
      td.textContent = newValue

      // Try to update via API if we have a row ID.
      try {
        // Get the row by page path (current page).
        const pagePath = window.location.pathname.replace(/^\/+/, "")
        const resp = await fetch(`/api/database/${encodeURIComponent(table)}/page/${encodeURIComponent(pagePath)}`, {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ fields: { [fieldName]: newValue } }),
        })
        if (!resp.ok) {
          td.style.color = "#c33"
          setTimeout(() => { td.style.color = "" }, 2000)
        }
      } catch {
        // Silently fail — data will be synced on next page save.
      }
    }

    input.addEventListener("blur", save)
    input.addEventListener("keydown", (e) => {
      if (e.key === "Enter") {
        e.preventDefault()
        input.blur()
      }
      if (e.key === "Escape") {
        td.textContent = currentValue
      }
    })
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.render()
    return true
  }

  stopEvent(event: Event): boolean {
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click" || type === "dblclick") {
      return false
    }
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

        // Must match {database-row table=...}
        const directiveMatch = line.match(/^\{database-row\s+table=(?:"([^"]+)"|'([^']+)'|(\S+?))\s*\}$/)
        if (!directiveMatch) return false
        if (silent) return true

        const tableName = directiveMatch[1] || directiveMatch[2] || directiveMatch[3]

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

    // ── Editor plugin: NodeViews ──

    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.database"),
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
          },
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

    // ── Node properties ──

    reg.registerNodeProperties("database_query", databaseQueryProperties)
    reg.registerNodeProperties("database_newrow", databaseNewRowProperties)
    reg.registerNodeProperties("database_row", databaseRowProperties)

    // ── Styles ──

    reg.registerStyle("database", databaseStyles)
  },
}
