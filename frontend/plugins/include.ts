import { Plugin as PMPlugin, PluginKey, NodeSelection, EditorState } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import type { Registry } from "../compiler/registry"
import { markdownToPM } from "../compiler/markdown_to_pm"
import { enablePropertiesPanel } from "../compiler/core_ui"
import { highlightCodeBlocks } from "../highlight"

const includeProperties = [
  {
    name: "path",
    label: "Path",
    default: null,
    parse: (raw: string) => {
      const trimmed = raw.trim()
      if (!trimmed) return null
      return trimmed
    },
    serialize: (value: string | null) => String(value ?? ""),
  },
]

const includeStyles = `
/* In edit mode: show a grey zone so the user can see the block is non-editable */
#app.gowiki-editing .gowiki-include {
  background: #f8f9fa;
  margin: 0.5em 0;
}

#app.gowiki-editing .gowiki-include-body {
  padding: 8px;
}

/* Yellow selection outline (edit mode only, implicitly) */
#app.gowiki-editing .gowiki-include.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}

.gowiki-include-body .ProseMirror {
  outline: none;
  padding: 0;
  min-height: 0;
  border: none;
}

.gowiki-include-loading {
  color: #636e72;
  font-style: italic;
  padding: 8px;
}

.gowiki-include-error {
  color: #d63031;
  font-style: italic;
  padding: 8px;
}
`

function encodePagePath(path: string): string {
  return path
    .split("/")
    .filter(Boolean)
    .map(part => encodeURIComponent(part))
    .join("/")
}

class IncludeNodeView {
  dom: HTMLElement
  private bodyEl: HTMLElement
  private node: PMNode
  private registry: Registry
  private innerView: EditorView | null = null

  constructor(
    node: PMNode,
    _outerView: EditorView,
    _getPos: () => number | undefined,
    registry: Registry
  ) {
    this.node = node
    this.registry = registry

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-include"
    this.dom.contentEditable = "false"

    this.bodyEl = document.createElement("div")
    this.bodyEl.className = "gowiki-include-body"

    this.dom.appendChild(this.bodyEl)

    if (node.attrs.path) {
      this.fetchAndRender(node.attrs.path)
    } else {
      this.showError("No path specified")
    }
  }

  private showError(message: string) {
    this.destroyInnerView()
    this.bodyEl.innerHTML = ""
    const msg = document.createElement("div")
    msg.className = "gowiki-include-error"
    msg.textContent = message
    this.bodyEl.appendChild(msg)
  }

  private destroyInnerView() {
    if (this.innerView) {
      this.innerView.destroy()
      this.innerView = null
    }
  }

  private async fetchAndRender(path: string) {
    this.destroyInnerView()
    this.bodyEl.innerHTML = ""
    const loading = document.createElement("div")
    loading.className = "gowiki-include-loading"
    loading.textContent = "Loading..."
    this.bodyEl.appendChild(loading)

    try {
      const cleanPath = path.replace(/^\/+/, "")
      const resp = await fetch(`/api/pages/${encodePagePath(cleanPath)}`)
      if (!resp.ok) {
        this.showError(
          resp.status === 404
            ? `Page not found: ${path}`
            : `Failed to load: ${path} (${resp.status})`
        )
        return
      }

      const data = await resp.json()
      const markdown = data.markdown ?? ""

      const doc = markdownToPM(markdown, this.registry)

      this.bodyEl.innerHTML = ""
      const state = EditorState.create({
        doc,
        schema: this.registry.schema,
        plugins: this.registry.getEditorPlugins(),
      })
      this.innerView = new EditorView(this.bodyEl, {
        state,
        editable: () => false,
      })
      highlightCodeBlocks(this.bodyEl)
    } catch (err) {
      this.showError(
        `Error loading include: ${err instanceof Error ? err.message : String(err)}`
      )
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    if (node.attrs.path !== this.node.attrs.path) {
      this.node = node
      if (node.attrs.path) {
        this.fetchAndRender(node.attrs.path)
      } else {
        this.showError("No path specified")
      }
      return true
    }
    this.node = node
    return true
  }

  stopEvent(): boolean {
    return true
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {
    this.destroyInnerView()
  }
}

export const includePlugin: WikiPlugin = {
  register(reg) {
    // Schema node
    reg.registerSchema({
      nodes: {
        include: {
          group: "block",
          atom: true,
          attrs: { path: { default: "" } },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-include",
                "data-include-path": node.attrs.path ?? "",
              },
              `Include: ${node.attrs.path || "(no path)"}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-include",
              getAttrs(dom: HTMLElement) {
                return {
                  path: dom.getAttribute("data-include-path") || "",
                }
              },
            },
          ],
        },
      },
    })

    // Self-contained directive: {include path=/path/to/page}
    reg.registerSelfContainedDirective("include", {
      tokenType: "include",
      nodeType: "include",
      properties: includeProperties,
    })

    // Markdown -> PM: handle the synthetic "include" token emitted by applyDirectives
    reg.registerText("include", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.include.create({ path: attrs.path ?? "" })
        )
      },
    })

    // PM -> Markdown: serialize include node back to directive syntax
    reg.registerPMNode("include", {
      print(node) {
        const path = node.attrs.path ?? ""
        return `{include path=${path}}\n\n`
      },
    })

    // Editor plugin: NodeView for live rendering of included content
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.include"),
        props: {
          nodeViews: {
            include(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new IncludeNodeView(node, view, getPos, reg)
            },
          },
        },
      })
    })

    // Command: insert an empty include node and open the properties panel
    reg.registerCommand("include", "insert", (state, dispatch) => {
      const includeType = reg.schema.nodes.include
      if (!includeType) return false
      if (dispatch) {
        const node = includeType.create({ path: "" })
        let tr = state.tr.replaceSelectionWith(node)
        // Find the freshly inserted include node near the original cursor position
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === includeType && insertedAt === null) {
              insertedAt = pos
              return false
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch {
            // Leave default selection if NodeSelection fails
          }
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    // Styles
    reg.registerStyle("include", includeStyles)
  },
}
