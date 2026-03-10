import { Plugin as PMPlugin, PluginKey } from "prosemirror-state"
import { Decoration, DecorationSet, EditorView } from "prosemirror-view"
import type { Node as PMNode } from "prosemirror-model"
import type { Plugin as WikiPlugin } from "../compiler/registry"

// ─── Inline markdown renderer (small subset) ────────────

/**
 * Parse a small subset of inline Markdown into DOM nodes.
 * Supports: **bold**, *italic*, `code`, [text](url) links.
 * No nesting of bold/italic.
 */
export function renderInlineMarkdown(text: string, parent: HTMLElement) {
  // Regex matches **bold**, *italic*, `code`, or [text](url)
  const re = /(\*\*(.+?)\*\*|\*(.+?)\*|`(.+?)`|\[([^\]]*)\]\(([^)]+)\))/g
  let lastIndex = 0
  let m: RegExpExecArray | null

  while ((m = re.exec(text)) !== null) {
    // Text before the match
    if (m.index > lastIndex) {
      parent.appendChild(document.createTextNode(text.slice(lastIndex, m.index)))
    }

    if (m[2] !== undefined) {
      // **bold**
      const el = document.createElement("strong")
      el.textContent = m[2]
      parent.appendChild(el)
    } else if (m[3] !== undefined) {
      // *italic*
      const el = document.createElement("em")
      el.textContent = m[3]
      parent.appendChild(el)
    } else if (m[4] !== undefined) {
      // `code`
      const el = document.createElement("code")
      el.textContent = m[4]
      parent.appendChild(el)
    } else if (m[5] !== undefined && m[6] !== undefined) {
      // [text](url)
      const el = document.createElement("a")
      el.href = m[6]
      el.textContent = m[5] || m[6]
      if (/^https?:\/\//i.test(m[6])) {
        el.target = "_blank"
        el.rel = "noopener noreferrer"
      }
      parent.appendChild(el)
    }

    lastIndex = re.lastIndex
  }

  // Trailing text
  if (lastIndex < text.length) {
    parent.appendChild(document.createTextNode(text.slice(lastIndex)))
  }
}

// ─── Caption numbering state ─────────────────────────────

export type CaptionEntry = {
  kind: "figure" | "table"
  number: number
  caption: string
}

export type CaptionState = {
  map: Map<string, CaptionEntry>
  /** Map from node position to figure/table number */
  posnums: Map<number, number>
}

export const captionNumberingKey = new PluginKey<CaptionState>("gowiki.captionNumbering")

function buildCaptionState(doc: PMNode): CaptionState {
  const map = new Map<string, CaptionEntry>()
  const posnums = new Map<number, number>()
  let figCounter = 0
  let tabCounter = 0

  doc.descendants((node, pos) => {
    if (node.type.name === "image" && node.attrs.caption) {
      figCounter++
      posnums.set(pos, figCounter)
      const entry: CaptionEntry = {
        kind: "figure",
        number: figCounter,
        caption: String(node.attrs.caption),
      }
      const label = node.attrs.label
      if (label && !map.has(label)) {
        map.set(label, entry)
      }
      return false
    }
    if (node.type.name === "table" && node.attrs.caption) {
      tabCounter++
      posnums.set(pos, tabCounter)
      const entry: CaptionEntry = {
        kind: "table",
        number: tabCounter,
        caption: String(node.attrs.caption),
      }
      const label = node.attrs.label
      if (label && !map.has(label)) {
        map.set(label, entry)
      }
    }
  })

  return { map, posnums }
}

// ─── Table caption decorations ───────────────────────────

function buildCaptionDecorations(doc: PMNode) {
  const decorations: Decoration[] = []
  let figCounter = 0
  let tabCounter = 0

  doc.descendants((node, pos) => {
    if (node.type.name === "image" && node.attrs.caption) {
      figCounter++
      // Node decoration with data-figure-number triggers ImageNodeView.update()
      // when the number changes, enabling live re-numbering.
      decorations.push(Decoration.node(pos, pos + node.nodeSize, {
        "data-figure-number": String(figCounter),
      }))
      return false
    }

    if (node.type.name === "table" && node.attrs.caption) {
      tabCounter++
      const caption = String(node.attrs.caption)
      const label = node.attrs.label ?? null

      // Widget decoration: figcaption above the table
      const num = tabCounter
      const widget = Decoration.widget(pos, () => {
        const figcaption = document.createElement("figcaption")
        figcaption.className = "gowiki-caption"
        figcaption.contentEditable = "false"
        const numSpan = document.createElement("span")
        numSpan.className = "gowiki-caption-number"
        numSpan.textContent = `Table ${num}:`
        const textSpan = document.createElement("span")
        textSpan.className = "gowiki-caption-text"
        renderInlineMarkdown(caption, textSpan)
        figcaption.appendChild(numSpan)
        figcaption.appendChild(textSpan)
        return figcaption
      }, { side: -1 })
      decorations.push(widget)

      // Node decoration: add id and figure class
      const attrs: Record<string, string> = { class: "gowiki-table-figure" }
      if (label) attrs.id = label
      decorations.push(Decoration.node(pos, pos + node.nodeSize, attrs))
    }
  })

  return decorations
}

// ─── Caption ref NodeView ────────────────────────────────

class CaptionRefNodeView {
  dom: HTMLElement
  onDestroy: (() => void) | null = null
  private node: PMNode
  private outerView: EditorView

  constructor(node: PMNode, view: EditorView) {
    this.node = node
    this.outerView = view
    this.dom = document.createElement("a")
    this.dom.className = "gowiki-ref"
    this.dom.contentEditable = "false"
    this.dom.addEventListener("click", (e) => {
      e.preventDefault()
      const label = this.node.attrs.label || ""
      if (label) {
        const target = document.getElementById(label)
        if (target) target.scrollIntoView({ behavior: "smooth", block: "center" })
      }
    })
    this.renderLabel(captionNumberingKey.getState(view.state))
  }

  private renderLabel(captionState: { map: Map<string, CaptionEntry> } | null | undefined) {
    const label = this.node.attrs.label || ""
    const entry = captionState?.map.get(label)

    if (entry) {
      const prefix = entry.kind === "figure" ? "Figure" : "Table"
      this.dom.textContent = `${prefix} ${entry.number}`
      ;(this.dom as HTMLAnchorElement).href = "#" + label
      this.dom.title = entry.caption
      this.dom.classList.remove("gowiki-ref--broken")
    } else {
      this.dom.textContent = "??"
      ;(this.dom as HTMLAnchorElement).removeAttribute("href")
      this.dom.removeAttribute("title")
      this.dom.classList.add("gowiki-ref--broken")
    }
  }

  refreshFromState(state: any) {
    this.renderLabel(captionNumberingKey.getState(state))
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.renderLabel(captionNumberingKey.getState(this.outerView.state))
    return true
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {
    if (this.onDestroy) this.onDestroy()
  }
}

// ─── CSS ─────────────────────────────────────────────────

const captionStyles = `
.gowiki-figure {
  margin: 1em 0;
}

.gowiki-table-figure {
  margin: 1em 0;
}

.gowiki-caption {
  font-size: 0.9em;
  color: #333;
  padding: 6px 0 4px 0;
  margin-top: 4px;
  line-height: 1.4;
  /* width:0 + min-width:100% — caption doesn't widen the inline-block
     parent beyond the image, then fills whatever width the image set. */
  display: block;
  width: 0;
  min-width: 100%;
  overflow-wrap: break-word;
}

.gowiki-caption-number {
  font-weight: bold;
  margin-right: 0.3em;
}

.gowiki-ref {
  color: inherit;
  text-decoration: none;
  border-bottom: 1px dotted #666;
  cursor: pointer;
}

.gowiki-ref:hover {
  border-bottom-style: solid;
}

.gowiki-ref--broken {
  color: #c00;
  border-bottom-color: #c00;
}
`

// ─── Plugin ──────────────────────────────────────────────

export const captionPlugin: WikiPlugin = {
  register(reg) {
    // Schema: caption_ref inline atom node
    reg.registerSchema({
      nodes: {
        caption_ref: {
          inline: true,
          atom: true,
          selectable: true,
          group: "inline",
          attrs: { label: { default: "" } },
          toDOM(node: PMNode) {
            return ["a", {
              class: "gowiki-ref",
              href: "#" + node.attrs.label,
              contenteditable: "false",
            }, "??"]
          },
          parseDOM: [{
            tag: "a.gowiki-ref",
            getAttrs(dom: HTMLElement) {
              return { label: dom.getAttribute("href")?.slice(1) || "" }
            },
          }],
        },
      },
    })

    // Markdown → PM: caption_ref token
    reg.registerText("caption_ref", {
      run(ctx, tok) {
        const label = tok.meta?.label ?? ""
        ctx.push(ctx.schema.nodes.caption_ref.create({ label }))
      },
    })

    // PM → Markdown: caption_ref serializer
    reg.registerPMNode("caption_ref", {
      print(node) {
        return "{ref " + node.attrs.label + "}"
      },
    })

    // Caption numbering plugin (state + decorations for tables)
    reg.registerEditorPlugin(() => {
      return new PMPlugin({
        key: captionNumberingKey,
        state: {
          init(_, state) {
            return buildCaptionState(state.doc)
          },
          apply(tr, old) {
            if (tr.docChanged) {
              return buildCaptionState(tr.doc)
            }
            return old
          },
        },
        props: {
          decorations(state) {
            const decos = buildCaptionDecorations(state.doc)
            return DecorationSet.create(state.doc, decos)
          },
        },
      })
    })

    // NodeView for caption_ref (reads numbering state to render live)
    // We track all active ref views and refresh them when caption state changes.
    reg.registerEditorPlugin(() => {
      const activeRefViews = new Set<CaptionRefNodeView>()
      return new PMPlugin({
        key: new PluginKey("gowiki.captionRefNodeView"),
        view() {
          return {
            update(view) {
              // Refresh all ref node views when the document changes
              for (const rv of activeRefViews) {
                rv.refreshFromState(view.state)
              }
            },
          }
        },
        props: {
          nodeViews: {
            caption_ref(node: PMNode, view: EditorView) {
              const nv = new CaptionRefNodeView(node, view)
              activeRefViews.add(nv)
              nv.onDestroy = () => activeRefViews.delete(nv)
              return nv
            },
          },
        },
      })
    })

    // Insert ref command
    reg.registerCommand("caption", "insertRef", (state, dispatch) => {
      const captionState = captionNumberingKey.getState(state)
      if (!captionState || captionState.map.size === 0) return false
      if (!dispatch) return true

      // Collect available labels
      const labels = Array.from(captionState.map.keys())

      // If only one label, insert it directly
      if (labels.length === 1) {
        const label = labels[0]
        const node = state.schema.nodes.caption_ref.create({ label })
        dispatch(state.tr.replaceSelectionWith(node).scrollIntoView())
        return true
      }

      // Show a simple prompt for label selection
      const labelList = labels.map(l => {
        const e = captionState.map.get(l)!
        const prefix = e.kind === "figure" ? "Figure" : "Table"
        return `${l} (${prefix} ${e.number}: ${e.caption})`
      })
      const choice = window.prompt(
        "Enter label to reference:\n\n" + labelList.join("\n"),
        labels[0]
      )
      if (!choice) return true

      // Match by label prefix
      const label = labels.find(l => choice.startsWith(l)) ?? choice.trim()
      const node = state.schema.nodes.caption_ref.create({ label })
      dispatch(state.tr.replaceSelectionWith(node).scrollIntoView())
      return true
    })

    reg.registerStyle("caption", captionStyles)
  },
}
