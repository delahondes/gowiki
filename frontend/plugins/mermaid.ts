import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import type { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin, NodePropertySpec, Registry } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

let mermaidPromise: Promise<any> | null = null
let renderCounter = 0

function ensureMermaid(): Promise<any> {
  if (!mermaidPromise) {
    mermaidPromise = new Promise((resolve, reject) => {
      if ((window as any).mermaid) {
        const api = (window as any).mermaid
        api.initialize({ startOnLoad: false, theme: "default", securityLevel: "strict" })
        resolve(api)
        return
      }
      const script = document.createElement("script")
      script.src = "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js"
      script.onload = () => {
        const api = (window as any).mermaid
        api.initialize({ startOnLoad: false, theme: "default", securityLevel: "strict" })
        resolve(api)
      }
      script.onerror = () => reject(new Error("Failed to load Mermaid library"))
      document.head.appendChild(script)
    })
  }
  return mermaidPromise
}

// ── Info string parsing ──
// ```mermaid [size=500px]

function parseInfoString(info: string): { size: string; caption: string } {
  const attrs = { size: "", caption: "" }
  const sizeMatch = info.match(/\bsize=(\S+)/)
  if (sizeMatch) attrs.size = sizeMatch[1]
  const captionMatch = info.match(/\bcaption="([^"]*)"/) || info.match(/\bcaption=(\S+)/)
  if (captionMatch) attrs.caption = captionMatch[1]
  return attrs
}

function serializeInfoString(attrs: { size: string; caption: string }): string {
  const parts = ["mermaid"]
  if (attrs.size) parts.push(`size=${attrs.size}`)
  if (attrs.caption) {
    if (attrs.caption.includes(" ")) {
      parts.push(`caption="${attrs.caption}"`)
    } else {
      parts.push(`caption=${attrs.caption}`)
    }
  }
  return parts.join(" ")
}

// ── NodeView ──

class MermaidNodeView {
  dom: HTMLElement
  contentDOM: undefined = undefined
  private renderArea: HTMLElement
  private captionEl: HTMLElement
  node: PMNode
  view: EditorView
  getPos: () => number | undefined

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.getPos = getPos

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-mermaid"

    this.dom.addEventListener("mousedown", (e) => {
      if (!view.editable) return
      e.preventDefault()
      const pos = this.getPos()
      if (pos != null) {
        const tr = view.state.tr.setSelection(NodeSelection.create(view.state.doc, pos))
        view.dispatch(tr)
        view.focus()
      }
    })

    const marker = document.createElement("div")
    marker.className = "gowiki-mermaid-marker"
    marker.textContent = "Mermaid diagram"
    this.dom.appendChild(marker)

    this.renderArea = document.createElement("div")
    this.renderArea.className = "gowiki-mermaid-render"
    if (node.attrs.size) {
      this.renderArea.style.maxWidth = node.attrs.size
    }
    this.dom.appendChild(this.renderArea)

    this.captionEl = document.createElement("div")
    this.captionEl.className = "gowiki-mermaid-caption"
    this.updateCaption()
    this.dom.appendChild(this.captionEl)

    this.renderDiagram()
  }

  private renderDiagram() {
    const data = (this.node.attrs.data || "").trim()
    if (!data) {
      this.renderArea.innerHTML = '<div class="gowiki-mermaid-empty">Empty diagram — click to edit</div>'
      return
    }

    this.renderArea.innerHTML = '<div style="color:#999;font-size:0.85em">Rendering...</div>'
    const currentData = data
    setTimeout(async () => {
      if ((this.node.attrs.data || "").trim() !== currentData) return
      try {
        const mermaid = await ensureMermaid()
        const id = `gowiki-mermaid-${++renderCounter}`
        const container = document.createElement("div")
        document.body.appendChild(container)
        const { svg } = await mermaid.render(id, currentData, container)
        document.body.removeChild(container)
        this.renderArea.innerHTML = svg
      } catch (err: any) {
        this.renderArea.innerHTML = `<div class="gowiki-mermaid-error">${(err.message || err).toString().substring(0, 300)}</div>`
      }
    }, 10)
  }

  private updateCaption() {
    const caption = this.node.attrs.caption || ""
    if (caption) {
      this.captionEl.textContent = caption
      this.captionEl.style.display = ""
    } else {
      this.captionEl.textContent = ""
      this.captionEl.style.display = "none"
    }
  }

  update(node: PMNode) {
    if (node.type !== this.node.type) return false
    if (node.attrs.data !== this.node.attrs.data) {
      this.node = node
      this.renderDiagram()
    }
    if (node.attrs.size !== this.node.attrs.size) {
      this.renderArea.style.maxWidth = node.attrs.size || ""
    }
    if (node.attrs.caption !== this.node.attrs.caption) {
      this.node = node
      this.updateCaption()
    }
    this.node = node
    return true
  }

  selectNode() {
    this.dom.classList.add("ProseMirror-selectednode")
    setTimeout(() => {
      const tr = enablePropertiesPanel(this.view.state.tr)
      this.view.dispatch(tr)
    }, 0)
  }

  deselectNode() {
    this.dom.classList.remove("ProseMirror-selectednode")
  }

  stopEvent(): boolean {
    return true
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {}
}

// ── Property definitions (size only — data is edited in raw mode) ──

const mermaidProperties: NodePropertySpec[] = [
  {
    name: "size",
    label: "Size",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => v ?? "",
  },
  {
    name: "caption",
    label: "Caption",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => v ?? "",
  },
  {
    name: "data",
    label: "Diagram source",
    default: "",
    multiline: true,
    parse: (raw: string) => raw,
    serialize: (v: string | null) => v ?? "",
  },
]

// ── Styles ──

const mermaidStyles = `
.gowiki-mermaid {
  margin: 0.5em 0;
}
.gowiki-mermaid-marker {
  font-size: 0.85em;
  font-weight: 600;
  color: #666;
  margin-bottom: 4px;
  display: none;
}
#app.gowiki-editing .gowiki-mermaid-marker {
  display: block;
}
.gowiki-mermaid-render {
  text-align: center;
}
.gowiki-mermaid-render svg {
  max-width: 100%;
  height: auto;
}
#app.gowiki-editing .gowiki-mermaid {
  border: 1px dashed #ccc;
  border-radius: 6px;
  padding: 0.5em;
  cursor: default;
}
#app.gowiki-editing .gowiki-mermaid.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
.gowiki-mermaid-caption {
  text-align: center;
  font-size: 0.9em;
  color: #555;
  font-style: italic;
  margin-top: 4px;
}
.gowiki-mermaid-empty {
  color: #999;
  font-style: italic;
  padding: 1em;
  text-align: center;
}
.gowiki-mermaid-error {
  color: #c62828;
  background: #fce4ec;
  border: 1px solid #ef9a9a;
  border-radius: 4px;
  padding: 0.5em 1em;
  font-size: 0.85em;
  font-family: monospace;
  white-space: pre-wrap;
  text-align: left;
}
`

// ── Plugin ──

export const mermaidPlugin: WikiPlugin = {
  register(reg: Registry) {
    // ── Schema ──
    reg.registerSchema({
      nodes: {
        mermaid_diagram: {
          group: "block",
          atom: true,
          attrs: {
            size: { default: "" },
            caption: { default: "" },
            data: { default: "" },
          },
          toDOM(node: any) {
            return ["div", {
              class: "gowiki-mermaid",
              "data-mermaid-size": node.attrs.size,
              "data-mermaid-caption": node.attrs.caption,
              "data-mermaid-data": node.attrs.data,
            }, "Mermaid diagram"]
          },
          parseDOM: [{
            tag: "div.gowiki-mermaid",
            getAttrs(dom: any) {
              return {
                size: dom.getAttribute("data-mermaid-size") || "",
                caption: dom.getAttribute("data-mermaid-caption") || "",
                data: dom.getAttribute("data-mermaid-data") || "",
              }
            },
          }],
        },
      },
    })

    // ── Markdown-it: fenced block ```mermaid [size=...] ──
    reg.registerMarkdownItPlugin((md: any) => {
      md.block.ruler.before("fence", "mermaid_fence", (state: any, startLine: number, endLine: number, silent: boolean) => {
        const start = state.bMarks[startLine] + state.tShift[startLine]
        const max = state.eMarks[startLine]
        const line = state.src.slice(start, max)

        if (!line.startsWith("```mermaid")) return false
        if (silent) return true

        // Parse info string for attributes (size etc.)
        const infoStr = line.slice(3).trim() // "mermaid size=500px"
        const attrs = parseInfoString(infoStr)

        // Find closing fence
        let nextLine = startLine + 1
        while (nextLine < endLine) {
          const nStart = state.bMarks[nextLine] + state.tShift[nextLine]
          const nMax = state.eMarks[nextLine]
          const nLine = state.src.slice(nStart, nMax)
          if (nLine.startsWith("```") && nLine.trim() === "```") break
          nextLine++
        }

        // Extract body
        const bodyStart = state.bMarks[startLine + 1]
        const bodyEnd = nextLine < endLine ? state.bMarks[nextLine] : state.eMarks[endLine - 1]
        const body = state.src.slice(bodyStart, bodyEnd).trim()

        const token = state.push("mermaid_diagram", "", 0)
        token.meta = { data: body, size: attrs.size, caption: attrs.caption }
        token.map = [startLine, nextLine + 1]
        token.block = true

        state.line = nextLine + 1
        return true
      })
    })

    // ── Markdown → PM ──
    reg.registerText("mermaid_diagram", {
      run(ctx, tok) {
        const meta = tok.meta ?? {}
        ctx.push(ctx.schema.nodes.mermaid_diagram.create({
          size: meta.size ?? "",
          caption: meta.caption ?? "",
          data: meta.data ?? "",
        }))
      },
    })

    // ── PM → Markdown ──
    reg.registerPMNode("mermaid_diagram", {
      print(node) {
        const infoStr = serializeInfoString({ size: node.attrs.size || "", caption: node.attrs.caption || "" })
        const data = node.attrs.data || ""
        return "```" + infoStr + "\n" + data + "\n```\n\n"
      },
    })

    // ── NodeView ──
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.mermaid"),
        props: {
          nodeViews: {
            mermaid_diagram: (node, view, getPos) =>
              new MermaidNodeView(node, view, getPos as () => number | undefined),
          },
        },
      })
    })

    // ── Toolbar command ──
    reg.registerCommand("mermaid_diagram", "insert", (state, dispatch) => {
      const mermaidType = reg.schema.nodes.mermaid_diagram
      if (!mermaidType) return false
      if (dispatch) {
        const node = mermaidType.create({
          data: "graph TD\n    A[Start] --> B{Decision}\n    B -->|Yes| C[Result 1]\n    B -->|No| D[Result 2]",
        })
        let tr = state.tr.replaceSelectionWith(node)
        const insertedAt = tr.doc.resolve(tr.selection.from).before(1)
        try {
          tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
          tr = enablePropertiesPanel(tr)
        } catch { /* leave default selection */ }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    // ── Node properties (for property panel) ──
    reg.registerNodeProperties("mermaid_diagram", mermaidProperties)

    // ── Styles ──
    reg.registerStyle("mermaid_diagram", mermaidStyles)
  },
}
