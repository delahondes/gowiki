import { Plugin as PMPlugin, PluginKey, NodeSelection, EditorState } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import type { Registry } from "../compiler/registry"
import { markdownToPM } from "../compiler/markdown_to_pm"
import { enablePropertiesPanel } from "../compiler/core_ui"
import { highlightCodeBlocks } from "../highlight"
import { slugify } from "../compiler/slugify"
import { headingNumberKey, computeHeadingNumbers, getHeadingCountersAt, INCLUDE_HEADING_META } from "../compiler/core_nodes"

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

const includeHeadingKey = new PluginKey("gowiki.includeHeadingNumbers")

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

/** Extract a section from a PM doc: from the anchor heading to the next heading of same/higher level. */
function extractSection(doc: PMNode, anchor: string, schema: Schema): PMNode {
  let startIdx = -1
  let startLevel = 0
  let endIdx = doc.childCount
  for (let i = 0; i < doc.childCount; i++) {
    const child = doc.child(i)
    if (child.type.name === "heading") {
      if (startIdx === -1) {
        if (slugify(child.textContent) === anchor) {
          startIdx = i
          startLevel = child.attrs.level
        }
      } else if (child.attrs.level <= startLevel) {
        endIdx = i
        break
      }
    }
  }
  if (startIdx === -1) return doc // anchor not found — show full page
  const nodes: PMNode[] = []
  for (let i = startIdx; i < endIdx; i++) {
    nodes.push(doc.child(i))
  }
  return schema.nodes.doc.create(null, nodes)
}

/** Resolve a possibly-relative include path against the current page. */
function resolveIncludePath(includePath: string): string {
  if (includePath.startsWith("/")) return includePath
  // Get current page path from location
  let current = window.location.pathname.replace(/^\/+|\/+$/g, "")
  if (!current || current === "index") current = ""
  const namespace = current.includes("/")
    ? current.slice(0, current.lastIndexOf("/"))
    : ""
  if (includePath.startsWith("./")) {
    const rel = includePath.slice(2)
    return namespace ? `/${namespace}/${rel}` : `/${rel}`
  }
  if (includePath.startsWith("../")) {
    let ns = namespace
    let p = includePath
    while (p.startsWith("../")) {
      p = p.slice(3)
      ns = ns.includes("/") ? ns.slice(0, ns.lastIndexOf("/")) : ""
    }
    return ns ? `/${ns}/${p}` : `/${p}`
  }
  // Bare relative
  return namespace ? `/${namespace}/${includePath}` : `/${includePath}`
}

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
  private outerView: EditorView
  private getPos: () => number | undefined
  private innerView: EditorView | null = null

  constructor(
    node: PMNode,
    outerView: EditorView,
    getPos: () => number | undefined,
    registry: Registry
  ) {
    this.node = node
    this.registry = registry
    this.outerView = outerView
    this.getPos = getPos

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

  private async fetchAndRender(fullPath: string) {
    this.destroyInnerView()
    this.bodyEl.innerHTML = ""
    const loading = document.createElement("div")
    loading.className = "gowiki-include-loading"
    loading.textContent = "Loading..."
    this.bodyEl.appendChild(loading)

    // Split path and optional #anchor
    let pagePath = fullPath
    let anchor = ""
    const hashIdx = fullPath.indexOf("#")
    if (hashIdx !== -1) {
      pagePath = fullPath.slice(0, hashIdx)
      anchor = fullPath.slice(hashIdx + 1)
    }

    // Resolve relative paths (./foo, ../foo, bare) against current page
    pagePath = resolveIncludePath(pagePath)

    try {
      const cleanPath = pagePath.replace(/^\/+/, "")
      const resp = await fetch(`/api/pages/${encodePagePath(cleanPath)}`)
      if (!resp.ok) {
        if (resp.status === 403) {
          // ACL-restricted: fail silently (render nothing).
          this.bodyEl.innerHTML = ""
          return
        }
        this.showError(`Page not found: ${pagePath}`)
        return
      }

      const data = await resp.json()
      const markdown = data.markdown ?? ""

      let doc = markdownToPM(markdown, this.registry)
      if (anchor) {
        doc = extractSection(doc, anchor, this.registry.schema)
      }

      this.bodyEl.innerHTML = ""
      // Compute heading counter state from the outer doc up to this include's position,
      // so numbered headings in the included content continue the parent's sequence.
      const pos = this.getPos()
      const initialCounters = pos !== undefined
        ? getHeadingCountersAt(this.outerView.state.doc, pos)
        : undefined
      // Replace the standard heading-numbers plugin with one seeded from parent counters.
      const plugins = this.registry.getEditorPlugins().filter(
        p => (p as any).key !== headingNumberKey.key
      )
      plugins.push(new PMPlugin({
        key: includeHeadingKey,
        state: {
          init(_, s) { return computeHeadingNumbers(s.doc, initialCounters) },
          apply(tr, old) { return tr.docChanged ? computeHeadingNumbers(tr.doc, initialCounters) : old },
        },
        props: {
          decorations(s) { return includeHeadingKey.getState(s) },
        },
      }))
      const state = EditorState.create({
        doc,
        schema: this.registry.schema,
        plugins,
      })
      this.innerView = new EditorView(this.bodyEl, {
        state,
        editable: () => false,
      })
      highlightCodeBlocks(this.bodyEl)

      // Report heading counts back to the parent so its numbering accounts for include content.
      const includePos = this.getPos()
      if (includePos !== undefined) {
        const headingCounts = [0, 0, 0, 0, 0, 0]
        doc.descendants((n: PMNode) => {
          if (n.type.name === "heading" && n.attrs.numbered) {
            headingCounts[n.attrs.level - 1]++
          }
          return false
        })
        const tr = this.outerView.state.tr
        tr.setMeta(INCLUDE_HEADING_META, { pos: includePos, counters: headingCounts })
        this.outerView.dispatch(tr)
      }
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

  stopEvent(event: Event): boolean {
    // Allow mouse events through so ProseMirror can create NodeSelection
    // (needed for property panel). Block keyboard/paste/input events
    // from reaching the inner read-only EditorView.
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click") {
      return false
    }
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
