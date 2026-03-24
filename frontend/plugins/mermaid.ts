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

// ── NodeView ──

class MermaidNodeView {
  dom: HTMLElement
  contentDOM: undefined = undefined // no content hole — fully opaque
  private renderArea: HTMLElement
  node: PMNode
  view: EditorView
  getPos: () => number | undefined

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.getPos = getPos

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-mermaid"
    // Make the node selectable via NodeSelection on click (edit mode only).
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
      // Guard against stale renders.
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

  update(node: PMNode) {
    if (node.type !== this.node.type) return false
    if (node.attrs.data !== this.node.attrs.data) {
      this.node = node
      this.renderDiagram()
    }
    if (node.attrs.size !== this.node.attrs.size) {
      this.renderArea.style.maxWidth = node.attrs.size || ""
    }
    this.node = node
    return true
  }

  selectNode() {
    this.dom.classList.add("ProseMirror-selectednode")
    // Enable the properties panel via a proper transaction.
    setTimeout(() => {
      const tr = enablePropertiesPanel(this.view.state.tr)
      this.view.dispatch(tr)
      // Make the textarea bigger for diagram source editing.
      setTimeout(() => {
        const panel = document.querySelector(".gowiki-props-panel")
        if (panel) {
          const ta = panel.querySelector("textarea")
          if (ta) {
            ta.style.minHeight = "15em"
            ta.style.width = "30em"
            ta.style.fontFamily = "monospace"
          }
        }
      }, 10)
    }, 0)
  }

  deselectNode() {
    this.dom.classList.remove("ProseMirror-selectednode")
  }

  // Block all events — PM should not look inside this node.
  stopEvent(): boolean {
    return true
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {}
}

// ── Property definitions ──

const mermaidProperties: NodePropertySpec[] = [
  {
    name: "size",
    label: "Size",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => v ?? "",
  },
  {
    name: "data",
    label: "Diagram source",
    default: "",
    multiline: true,
    parse: (raw: string) => raw.replace(/\\n/g, "\n").replace(/\\\\/g, "\\"),
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
            data: { default: "" },
          },
          toDOM(node: any) {
            return ["div", {
              class: "gowiki-mermaid",
              "data-mermaid-data": node.attrs.data,
            }, "Mermaid diagram"]
          },
          parseDOM: [{
            tag: "div.gowiki-mermaid",
            getAttrs(dom: any) {
              return {
                data: dom.getAttribute("data-mermaid-data") || "",
              }
            },
          }],
        },
      },
    })

    // ── Self-contained directive ──
    reg.registerSelfContainedDirective("mermaid", {
      tokenType: "mermaid_diagram",
      nodeType: "mermaid_diagram",
      properties: mermaidProperties,
    })

    // ── Markdown → PM ──
    reg.registerText("mermaid_diagram", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(ctx.schema.nodes.mermaid_diagram.create({
          size: attrs.size ?? "",
          data: attrs.data ?? "",
        }))
      },
    })

    // ── PM → Markdown ──
    reg.registerPMNode("mermaid_diagram", {
      print(node) {
        const data = node.attrs.data || ""
        const escaped = data.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n")
        const parts: string[] = []
        if (node.attrs.size) parts.push(`size=${node.attrs.size}`)
        parts.push(`data="${escaped}"`)
        return `{mermaid ${parts.join(" ")}}\n\n`
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
        // Select the new node and open properties panel.
        const insertedAt = tr.doc.resolve(tr.selection.from).before(1)
        try {
          tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
          tr = enablePropertiesPanel(tr)
        } catch { /* leave default selection */ }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    // ── Styles ──
    reg.registerStyle("mermaid_diagram", mermaidStyles)
  },
}
