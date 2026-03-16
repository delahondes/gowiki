import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import type { Registry } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

const tagProperties = [
  {
    name: "values",
    label: "Tags",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
  },
]

const tagQueryProperties = [
  {
    name: "tag",
    label: "Tag",
    default: "",
    parse: (raw: string) => raw.trim(),
  },
  {
    name: "exclude",
    label: "Exclude tags",
    default: "",
    parse: (raw: string) => raw.trim(),
  },
  {
    name: "path",
    label: "Path prefix",
    default: "",
    parse: (raw: string) => raw.trim(),
  },
  {
    name: "render",
    label: "Render as",
    default: "table",
    parse: (raw: string) => {
      const v = raw.trim().toLowerCase()
      if (v === "list" || v === "table") return v
      return "table"
    },
    options: [
      { value: "table", label: "Table" },
      { value: "list", label: "List" },
    ],
  },
]

const tagStyles = `
.gowiki-tag-badges {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin: 8px 0;
}

.gowiki-tag-badge {
  display: inline-block;
  background: #e8f0fe;
  color: #1a56db;
  padding: 2px 10px;
  border-radius: 12px;
  font-size: 13px;
  font-weight: 500;
}

.gowiki-tag-query {
  margin: 12px 0;
}

.gowiki-tag-query-loading {
  color: #636e72;
  font-style: italic;
  padding: 8px;
}

.gowiki-tag-query-error {
  color: #d63031;
  font-style: italic;
  padding: 8px;
}

.gowiki-tag-query table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

.gowiki-tag-query th,
.gowiki-tag-query td {
  padding: 6px 10px;
  border: 1px solid #e0e0e0;
  text-align: left;
}

.gowiki-tag-query th {
  background: #f8f9fa;
  font-weight: 600;
}

.gowiki-tag-query a {
  color: #1e3f72;
  text-decoration: none;
}

.gowiki-tag-query a:hover {
  text-decoration: underline;
}

.gowiki-tag-query ul {
  margin: 0;
  padding-left: 20px;
}

.gowiki-tag-query li {
  margin: 4px 0;
}

#app.gowiki-editing .gowiki-tag-query {
  background: #f8f9fa;
  padding: 8px;
}

#app.gowiki-editing .gowiki-tag-query.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}

#app.gowiki-editing .gowiki-tag-badges.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
`

class TagQueryNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-tag-query"
    this.dom.contentEditable = "false"
    this.fetchAndRender()
  }

  private resolvePathPrefix(raw: string): string {
    if (!raw) return ""
    // Resolve relative paths (., ./, ../) against the current page's namespace.
    // "." means "same namespace as the current page".
    // Namespace index pages have a trailing slash in the URL; leaf pages don't.
    if (raw === "." || raw === ".." || raw.startsWith("./") || raw.startsWith("../")) {
      const pathname = window.location.pathname
      const loc = pathname.replace(/^\//, "").replace(/\/$/, "")
      const parts = loc ? loc.split("/") : []
      // For leaf pages (URL without trailing slash), drop the page name
      // to get the parent namespace. Namespace index pages (trailing slash)
      // already represent their namespace.
      if (!pathname.endsWith("/")) parts.pop()
      for (const seg of raw.split("/")) {
        if (seg === ".") continue
        else if (seg === "..") parts.pop()
        else if (seg) parts.push(seg)
      }
      return parts.join("/")
    }
    // Strip leading slash — backend paths don't use them.
    return raw.replace(/^\/+/, "")
  }

  private async fetchAndRender() {
    const tag = this.node.attrs.tag
    if (!tag) {
      this.dom.innerHTML = '<div class="gowiki-tag-query-error">No tag specified</div>'
      return
    }

    this.dom.innerHTML = '<div class="gowiki-tag-query-loading">Loading...</div>'

    try {
      const params = new URLSearchParams({ tag })
      const exclude = (this.node.attrs.exclude || "").trim()
      if (exclude) params.set("exclude", exclude)
      const path = this.resolvePathPrefix(this.node.attrs.path)
      if (path) params.set("path", path)

      const resp = await fetch(`/api/tags?${params}`)
      if (!resp.ok) throw new Error("Failed to fetch tags")
      const data = await resp.json()
      const pages = data.pages || []

      this.dom.innerHTML = ""

      if (pages.length === 0) {
        this.dom.textContent = `No pages tagged "${tag}"`
        return
      }

      const render = this.node.attrs.render || "table"
      if (render === "list") {
        const ul = document.createElement("ul")
        for (const p of pages) {
          const li = document.createElement("li")
          const a = document.createElement("a")
          a.href = p.path
          a.textContent = p.title || p.path
          li.appendChild(a)
          ul.appendChild(li)
        }
        this.dom.appendChild(ul)
      } else {
        const table = document.createElement("table")
        const thead = document.createElement("thead")
        const headerRow = document.createElement("tr")
        for (const h of ["Page", "Extract", "Version", "Author"]) {
          const th = document.createElement("th")
          th.textContent = h
          headerRow.appendChild(th)
        }
        thead.appendChild(headerRow)
        table.appendChild(thead)

        const tbody = document.createElement("tbody")
        for (const p of pages) {
          const row = document.createElement("tr")
          const tdTitle = document.createElement("td")
          const a = document.createElement("a")
          a.href = p.path
          a.textContent = p.title || p.path
          tdTitle.appendChild(a)
          row.appendChild(tdTitle)

          const tdExtract = document.createElement("td")
          tdExtract.textContent = p.extract || ""
          row.appendChild(tdExtract)

          const tdVersion = document.createElement("td")
          tdVersion.textContent = p.version ? String(p.version) : ""
          row.appendChild(tdVersion)

          const tdAuthor = document.createElement("td")
          tdAuthor.textContent = p.author || ""
          row.appendChild(tdAuthor)

          tbody.appendChild(row)
        }
        table.appendChild(tbody)
        this.dom.appendChild(table)
      }
    } catch {
      this.dom.innerHTML = '<div class="gowiki-tag-query-error">Failed to load tag results</div>'
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    if (
      node.attrs.tag !== this.node.attrs.tag ||
      node.attrs.exclude !== this.node.attrs.exclude ||
      node.attrs.path !== this.node.attrs.path ||
      node.attrs.render !== this.node.attrs.render
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

export const tagPlugin: WikiPlugin = {
  register(reg) {
    // Schema nodes
    reg.registerSchema({
      nodes: {
        tag: {
          group: "block",
          atom: true,
          attrs: { values: { default: "" } },
          toDOM(node: PMNode) {
            const badges = document.createElement("div")
            badges.className = "gowiki-tag-badges"
            const vals = (node.attrs.values || "").split(/\s+/).filter(Boolean)
            for (const v of vals) {
              const badge = document.createElement("span")
              badge.className = "gowiki-tag-badge"
              badge.textContent = v
              badges.appendChild(badge)
            }
            return { dom: badges }
          },
          parseDOM: [
            {
              tag: "div.gowiki-tag-badges",
              getAttrs(dom: HTMLElement) {
                const badges = dom.querySelectorAll(".gowiki-tag-badge")
                const values = Array.from(badges)
                  .map((b) => b.textContent?.trim() || "")
                  .filter(Boolean)
                  .join(" ")
                return { values }
              },
            },
          ],
        },
        tag_query: {
          group: "block",
          atom: true,
          attrs: {
            tag: { default: "" },
            exclude: { default: "" },
            path: { default: "" },
            render: { default: "table" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-tag-query",
                "data-tag": node.attrs.tag || "",
                "data-exclude": node.attrs.exclude || "",
                "data-path": node.attrs.path || "",
                "data-render": node.attrs.render || "table",
              },
              `Tag query: ${node.attrs.tag || "(no tag)"}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-tag-query",
              getAttrs(dom: HTMLElement) {
                return {
                  tag: dom.getAttribute("data-tag") || "",
                  exclude: dom.getAttribute("data-exclude") || "",
                  path: dom.getAttribute("data-path") || "",
                  render: dom.getAttribute("data-render") || "table",
                }
              },
            },
          ],
        },
      },
    })

    // Self-contained directives
    reg.registerSelfContainedDirective("tag", {
      tokenType: "tag",
      nodeType: "tag",
      properties: tagProperties,
    })

    reg.registerSelfContainedDirective("tag-query", {
      tokenType: "tag_query",
      nodeType: "tag_query",
      properties: tagQueryProperties,
    })

    // Markdown → PM: handle tokens
    reg.registerText("tag", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        // The _args field contains positional tag values
        const values = attrs._args || attrs.values || ""
        ctx.push(ctx.schema.nodes.tag.create({ values }))
      },
    })

    reg.registerText("tag_query", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.tag_query.create({
            tag: attrs.tag ?? "",
            exclude: attrs.exclude ?? "",
            path: attrs.path ?? "",
            render: attrs.render ?? "table",
          })
        )
      },
    })

    // PM → Markdown: serialize
    reg.registerPMNode("tag", {
      print(node) {
        const values = node.attrs.values || ""
        return `{tag ${values}}\n\n`
      },
    })

    reg.registerPMNode("tag_query", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.tag) parts.push(`tag=${node.attrs.tag}`)
        if (node.attrs.exclude) parts.push(`exclude=${node.attrs.exclude}`)
        if (node.attrs.path) parts.push(`path=${node.attrs.path}`)
        if (node.attrs.render && node.attrs.render !== "table") {
          parts.push(`render=${node.attrs.render}`)
        }
        return `{tag-query ${parts.join(" ")}}\n\n`
      },
    })

    // Editor plugin: NodeView for tag_query
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.tag"),
        props: {
          nodeViews: {
            tag_query(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TagQueryNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Command: insert tag node
    reg.registerCommand("tag", "insert", (state, dispatch) => {
      const tagType = reg.schema.nodes.tag
      if (!tagType) return false
      if (dispatch) {
        const node = tagType.create({ values: "" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === tagType && insertedAt === null) {
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

    // Command: insert tag_query node
    reg.registerCommand("tag-query", "insert", (state, dispatch) => {
      const queryType = reg.schema.nodes.tag_query
      if (!queryType) return false
      if (dispatch) {
        const node = queryType.create({ tag: "", exclude: "", path: "", render: "table" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === queryType && insertedAt === null) {
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
    reg.registerStyle("tag", tagStyles)
  },
}
