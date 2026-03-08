import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

const changesProperties = [
  {
    name: "count",
    label: "Count",
    default: "10",
    parse: (raw: string) => {
      const n = parseInt(raw.trim(), 10)
      if (isNaN(n) || n < 1) return "10"
      if (n > 100) return "100"
      return String(n)
    },
  },
  {
    name: "path",
    label: "Path filter",
    default: "",
    parse: (raw: string) => raw.trim(),
  },
  {
    name: "type",
    label: "Change types",
    default: "",
    parse: (raw: string) => raw.trim(),
    helpText: "Comma-separated: edit, create, delete, migrate, admin",
  },
  {
    name: "user",
    label: "Users",
    default: "",
    parse: (raw: string) => raw.trim(),
    helpText: "Comma-separated usernames",
  },
]

const changesStyles = `
.gowiki-changes {
  margin: 4px 0;
  font-size: 13px;
  line-height: 1.3;
}

.gowiki-changes-loading,
.gowiki-changes-empty {
  color: #636e72;
  font-style: italic;
  padding: 2px 0;
}

.gowiki-changes-error {
  color: #d63031;
  font-style: italic;
  padding: 2px 0;
}

.gowiki-changes-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.gowiki-changes-item {
  padding: 3px 0;
  border-bottom: 1px solid #f0f0f0;
}

.gowiki-changes-item:last-child {
  border-bottom: none;
}

.gowiki-changes-link {
  color: #1e3f72;
  text-decoration: none;
}

.gowiki-changes-link:hover {
  text-decoration: underline;
}

.gowiki-changes-meta {
  color: #999;
  font-size: 11px;
  margin-left: 4px;
}

.gowiki-changes-badge {
  display: inline-block;
  font-size: 10px;
  font-weight: 600;
  padding: 0 4px;
  border-radius: 6px;
  margin-left: 4px;
  vertical-align: middle;
}

.gowiki-changes-badge--create {
  background: #d4edda;
  color: #155724;
}

.gowiki-changes-badge--delete {
  background: #f8d7da;
  color: #721c24;
}

.gowiki-changes-badge--migrate {
  background: #e2e3e5;
  color: #383d41;
}

.gowiki-changes-badge--admin {
  background: #fff3cd;
  color: #856404;
}

#app.gowiki-editing .gowiki-changes {
  background: #f8f9fa;
  padding: 6px;
}

#app.gowiki-editing .gowiki-changes.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
`

function humanizePagePath(pagePath: string): string {
  // Get last segment, replace dashes/underscores with spaces, title case.
  const segments = pagePath.split("/").filter(Boolean)
  const last = segments[segments.length - 1] || pagePath
  return last
    .replace(/[-_]/g, " ")
    .replace(/\b\w/g, c => c.toUpperCase())
}

function relativeTime(timestamp: string): string {
  const now = Date.now()
  const then = new Date(timestamp).getTime()
  const diffMs = now - then
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)
  const diffMonth = Math.floor(diffDay / 30)
  const diffYear = Math.floor(diffDay / 365)

  if (diffSec < 60) return "now"
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHour < 24) return `${diffHour}h ago`
  if (diffDay < 30) return `${diffDay}d ago`
  if (diffMonth < 12) return `${diffMonth}mo ago`
  return `${diffYear}y ago`
}

interface ChangeEntryData {
  timestamp: string
  page: string
  version: number
  author: string
  summary: string
  type: string
}

class ChangesNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-changes"
    this.dom.contentEditable = "false"
    this.fetchAndRender()
  }

  private async fetchAndRender() {
    this.dom.innerHTML = '<div class="gowiki-changes-loading">Loading...</div>'

    try {
      const params = new URLSearchParams()
      const count = this.node.attrs.count || "10"
      if (count !== "10") params.set("count", count)

      const path = (this.node.attrs.path || "").trim()
      if (path) params.set("path", path)

      const type = (this.node.attrs.type || "").trim()
      if (type) params.set("type", type)

      const user = (this.node.attrs.user || "").trim()
      if (user) params.set("user", user)

      const qs = params.toString()
      const url = qs ? `/api/changes?${qs}` : "/api/changes"
      const resp = await fetch(url)
      if (!resp.ok) throw new Error("Failed to fetch changes")

      const data = await resp.json()
      const entries: ChangeEntryData[] = data.entries || []

      this.dom.innerHTML = ""

      if (entries.length === 0) {
        this.dom.innerHTML = '<div class="gowiki-changes-empty">No recent changes</div>'
        return
      }

      const ul = document.createElement("ul")
      ul.className = "gowiki-changes-list"

      for (const entry of entries) {
        const li = document.createElement("li")
        li.className = "gowiki-changes-item"

        const a = document.createElement("a")
        a.className = "gowiki-changes-link"
        a.href = "/" + entry.page
        a.textContent = humanizePagePath(entry.page)
        li.appendChild(a)

        // Change type badge (only for non-edit types).
        if (entry.type && entry.type !== "edit") {
          const badge = document.createElement("span")
          badge.className = `gowiki-changes-badge gowiki-changes-badge--${entry.type}`
          badge.textContent = entry.type
          li.appendChild(badge)
        }

        const meta = document.createElement("span")
        meta.className = "gowiki-changes-meta"
        meta.textContent = `${entry.author} \u00b7 ${relativeTime(entry.timestamp)}`
        li.appendChild(meta)

        ul.appendChild(li)
      }

      this.dom.appendChild(ul)
    } catch {
      this.dom.innerHTML = '<div class="gowiki-changes-error">Failed to load recent changes</div>'
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    if (
      node.attrs.count !== this.node.attrs.count ||
      node.attrs.path !== this.node.attrs.path ||
      node.attrs.type !== this.node.attrs.type ||
      node.attrs.user !== this.node.attrs.user
    ) {
      this.node = node
      this.fetchAndRender()
      return true
    }
    this.node = node
    return true
  }

  stopEvent(event: Event): boolean {
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click") return false
    return true
  }

  ignoreMutation(): boolean {
    return true
  }

}

export const changesPlugin: WikiPlugin = {
  register(reg) {
    // Schema node
    reg.registerSchema({
      nodes: {
        changes: {
          group: "block",
          atom: true,
          attrs: {
            count: { default: "10" },
            path: { default: "" },
            type: { default: "" },
            user: { default: "" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-changes",
                "data-count": node.attrs.count || "10",
                "data-path": node.attrs.path || "",
                "data-type": node.attrs.type || "",
                "data-user": node.attrs.user || "",
              },
              `Recent changes${node.attrs.count !== "10" ? ` (${node.attrs.count})` : ""}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-changes",
              getAttrs(dom: HTMLElement) {
                return {
                  count: dom.getAttribute("data-count") || "10",
                  path: dom.getAttribute("data-path") || "",
                  type: dom.getAttribute("data-type") || "",
                  user: dom.getAttribute("data-user") || "",
                }
              },
            },
          ],
        },
      },
    })

    // Self-contained directive: {changes count=10 path=/projects}
    reg.registerSelfContainedDirective("changes", {
      tokenType: "changes",
      nodeType: "changes",
      properties: changesProperties,
    })

    // Markdown → PM: handle the synthetic "changes" token
    reg.registerText("changes", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.changes.create({
            count: attrs.count ?? "10",
            path: attrs.path ?? "",
            type: attrs.type ?? "",
            user: attrs.user ?? "",
          })
        )
      },
    })

    // PM → Markdown: serialize changes node back to directive syntax
    reg.registerPMNode("changes", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.count && node.attrs.count !== "10") {
          parts.push(`count=${node.attrs.count}`)
        }
        if (node.attrs.path) parts.push(`path=${node.attrs.path}`)
        if (node.attrs.type) parts.push(`type=${node.attrs.type}`)
        if (node.attrs.user) parts.push(`user=${node.attrs.user}`)
        return parts.length ? `{changes ${parts.join(" ")}}\n\n` : `{changes}\n\n`
      },
    })

    // Editor plugin: NodeView
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.changes"),
        props: {
          nodeViews: {
            changes(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new ChangesNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Command: insert a changes node and open properties panel
    reg.registerCommand("changes", "insert", (state, dispatch) => {
      const changesType = reg.schema.nodes.changes
      if (!changesType) return false
      if (dispatch) {
        const node = changesType.create({ count: "10", path: "", type: "", user: "" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === changesType && insertedAt === null) {
              insertedAt = pos
              return false
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch { /* leave default selection */ }
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    // Styles
    reg.registerStyle("changes", changesStyles)
  },
}
