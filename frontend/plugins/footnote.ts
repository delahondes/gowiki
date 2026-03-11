import { Plugin as PMPlugin, PluginKey } from "prosemirror-state"
import type { Node as PMNode } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"

/* ─── Footnote numbering state ─────────────────────────── */

const footnoteNumberingKey = new PluginKey<Map<number, number>>("gowiki.footnoteNumbering")

function buildFootnoteNumbering(doc: PMNode): Map<number, number> {
  const map = new Map<number, number>()
  let counter = 0
  doc.descendants((node, pos) => {
    if (node.type.name === "footnote") {
      counter++
      map.set(pos, counter)
    }
  })
  return map
}

/* ─── Footnote NodeView ────────────────────────────────── */

class FootnoteNodeView {
  dom: HTMLElement
  onDestroy: (() => void) | null = null
  private node: PMNode
  private outerView: EditorView
  private tooltip: HTMLElement | null = null

  constructor(node: PMNode, view: EditorView) {
    this.node = node
    this.outerView = view
    this.dom = document.createElement("sup")
    this.dom.className = "gowiki-footnote"
    this.dom.contentEditable = "false"

    this.dom.addEventListener("mouseenter", () => this.showTooltip())
    this.dom.addEventListener("mouseleave", () => this.hideTooltip())

    this.renderNumber(footnoteNumberingKey.getState(view.state))
  }

  private renderNumber(numbering: Map<number, number> | null | undefined) {
    // Find this node's position to look up its number
    // Since we can't easily get pos here, just use the numbering map via the outer view
    const doc = this.outerView.state.doc
    let myNumber = 0
    let counter = 0
    doc.descendants((n, _pos) => {
      if (n.type.name === "footnote") {
        counter++
        if (n === this.node) myNumber = counter
      }
    })
    this.dom.textContent = String(myNumber || "?")
  }

  refreshFromState(state: any) {
    this.renderNumber(footnoteNumberingKey.getState(state))
  }

  private showTooltip() {
    if (this.tooltip) return
    this.tooltip = document.createElement("div")
    this.tooltip.className = "gowiki-footnote-tooltip"
    this.tooltip.textContent = this.node.attrs.content
    document.body.appendChild(this.tooltip)
    const rect = this.dom.getBoundingClientRect()
    this.tooltip.style.left = rect.left + "px"
    this.tooltip.style.top = (rect.bottom + 4) + "px"
  }

  private hideTooltip() {
    if (this.tooltip) {
      this.tooltip.remove()
      this.tooltip = null
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.renderNumber(footnoteNumberingKey.getState(this.outerView.state))
    return true
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {
    this.hideTooltip()
    if (this.onDestroy) this.onDestroy()
  }
}

/* ─── CSS ──────────────────────────────────────────────── */

const footnoteStyles = `
.gowiki-footnote {
  cursor: pointer;
  color: #1a73e8;
  font-size: 0.75em;
  vertical-align: super;
  line-height: 0;
  padding: 0 1px;
}

.gowiki-footnote:hover {
  text-decoration: underline;
}

/* Selection highlight in edit mode */
#app.gowiki-editing .gowiki-footnote.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
  border-radius: 2px;
}

.gowiki-footnote-tooltip {
  position: fixed;
  background: #333;
  color: #fff;
  padding: 6px 10px;
  border-radius: 4px;
  font-size: 0.85em;
  max-width: 300px;
  z-index: 1000;
  pointer-events: none;
  line-height: 1.4;
  white-space: pre-wrap;
}

.gowiki-footnote-section {
  margin-top: 2em;
  font-size: 0.85em;
  color: #555;
  line-height: 1.6;
}

.gowiki-footnote-section hr {
  border: none;
  border-top: 1px solid #ccc;
  margin-bottom: 0.75em;
}

.gowiki-footnote-section ol {
  margin: 0;
  padding-left: 1.5em;
}

.gowiki-footnote-section li {
  margin-bottom: 0.25em;
}
`

/* ─── Plugin ───────────────────────────────────────────── */

export const footnotePlugin: WikiPlugin = {
  register(reg) {
    // Schema: inline atom node with content attribute
    reg.registerSchema({
      nodes: {
        footnote: {
          group: "inline",
          inline: true,
          atom: true,
          selectable: true,
          attrs: { content: { default: "" } },
          toDOM(node: PMNode) {
            return ["sup", {
              class: "gowiki-footnote",
              "data-footnote": node.attrs.content,
              contenteditable: "false",
            }, "0"]
          },
          parseDOM: [{
            tag: "sup.gowiki-footnote",
            getAttrs(dom: HTMLElement) {
              return { content: dom.getAttribute("data-footnote") || "" }
            },
          }],
        },
      },
    })

    // markdown-it inline rule: ^[content]
    reg.registerMarkdownItPlugin((md: any) => {
      md.inline.ruler.push("gowiki_footnote", (state: any, silent: boolean) => {
        const src = state.src
        const start = state.pos
        // Must start with ^[
        if (src.charCodeAt(start) !== 0x5E /* ^ */) return false
        if (start + 1 >= state.posMax || src.charCodeAt(start + 1) !== 0x5B /* [ */) return false
        // Find matching closing ] (handle nested brackets)
        let depth = 1
        let end = start + 2
        while (end < state.posMax && depth > 0) {
          const ch = src.charCodeAt(end)
          if (ch === 0x5C /* \ */) { end += 2; continue }
          if (ch === 0x5B /* [ */) depth++
          if (ch === 0x5D /* ] */) depth--
          end++
        }
        if (depth !== 0) return false
        const content = src.slice(start + 2, end - 1)
        if (content.length === 0) return false
        if (!silent) {
          if (state.pending) state.pushPending()
          const token = state.push("footnote", "", 0)
          token.content = content
          token.meta = { content }
        }
        state.pos = end
        return true
      })
    })

    // Markdown → PM: handle footnote token
    reg.registerText("footnote", {
      run(ctx, tok) {
        const content = tok.meta?.content ?? tok.content ?? ""
        ctx.push(ctx.schema.nodes.footnote.create({ content }))
      },
    })

    // PM → Markdown: serialize footnote node
    reg.registerPMNode("footnote", {
      print(node) {
        return `^[${node.attrs.content}]`
      },
    })

    // Node properties (editable via property panel)
    reg.registerNodeProperties("footnote", [
      {
        name: "content",
        label: "Footnote text",
        default: "",
        parse: (raw: string) => raw.trim() || null,
        serialize: (value: string | null) => String(value ?? ""),
      },
    ])

    // Numbering plugin
    reg.registerEditorPlugin(() => {
      return new PMPlugin({
        key: footnoteNumberingKey,
        state: {
          init(_, state) { return buildFootnoteNumbering(state.doc) },
          apply(tr, old) { return tr.docChanged ? buildFootnoteNumbering(tr.doc) : old },
        },
      })
    })

    // NodeView plugin with live renumbering
    reg.registerEditorPlugin(() => {
      const activeViews = new Set<FootnoteNodeView>()
      return new PMPlugin({
        key: new PluginKey("gowiki.footnoteNodeView"),
        view() {
          return {
            update(view) {
              for (const fv of activeViews) {
                fv.refreshFromState(view.state)
              }
            },
          }
        },
        props: {
          nodeViews: {
            footnote(node: PMNode, view: EditorView) {
              const nv = new FootnoteNodeView(node, view)
              activeViews.add(nv)
              nv.onDestroy = () => activeViews.delete(nv)
              return nv
            },
          },
        },
      })
    })

    // Footnote section at the bottom of the page
    reg.registerEditorPlugin(() => {
      return new PMPlugin({
        key: new PluginKey("gowiki.footnoteSection"),
        view(editorView) {
          const section = document.createElement("div")
          section.className = "gowiki-footnote-section"
          section.contentEditable = "false"
          editorView.dom.parentNode?.appendChild(section)

          function rebuild(doc: PMNode) {
            const footnotes: string[] = []
            doc.descendants((node) => {
              if (node.type.name === "footnote") {
                footnotes.push(node.attrs.content)
              }
            })
            if (footnotes.length === 0) {
              section.style.display = "none"
              return
            }
            section.style.display = ""
            section.innerHTML = ""
            const hr = document.createElement("hr")
            section.appendChild(hr)
            const ol = document.createElement("ol")
            for (const text of footnotes) {
              const li = document.createElement("li")
              li.textContent = text
              ol.appendChild(li)
            }
            section.appendChild(ol)
          }

          rebuild(editorView.state.doc)
          return {
            update(view) { rebuild(view.state.doc) },
            destroy() { section.remove() },
          }
        },
      })
    })

    // Command: insert footnote
    reg.registerCommand("footnote", "insert", (state, dispatch) => {
      const footnoteType = reg.schema.nodes.footnote
      if (!footnoteType) return false
      if (dispatch) {
        const content = window.prompt("Footnote text:")
        if (!content) return true
        const node = footnoteType.create({ content })
        dispatch(state.tr.replaceSelectionWith(node).scrollIntoView())
      }
      return true
    })

    // Styles
    reg.registerStyle("footnote", footnoteStyles)
  },
}
