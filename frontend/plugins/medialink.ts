import { Plugin as PMPlugin, PluginKey } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import type { Node as PMNode } from "prosemirror-model"
import type { Plugin as WikiPlugin } from "../compiler/registry"

function normalizeMediaVersion(raw: string): string | null {
  const value = String(raw ?? "").trim().toLowerCase()
  if (!value) return null
  if (value === "latest") return "latest"
  const n = Number(value)
  if (Number.isInteger(n) && n >= 1) return String(n)
  throw new Error(`Invalid version "${raw}". Expected a positive integer or "latest".`)
}

const medialinkProperties = [
  {
    name: "label",
    label: "Label",
    default: null,
    parse: (raw: string) => {
      const trimmed = raw.trim()
      if (!trimmed) return null
      return trimmed
    },
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "version",
    label: "Version",
    default: null,
    parse: (raw: string) => normalizeMediaVersion(raw),
    serialize: (value: string | null) => String(value ?? ""),
    options: (attrs: Record<string, any>) => {
      const opts = [{ value: "", label: "(default)" }]
      const href = attrs.href || ""
      const cache = (window as any).__gowikiMediaVersions as Map<string, number> | undefined
      const maxVersion = cache?.get(href) ?? 0
      const currentV = attrs.version
      const currentN = currentV && currentV !== "latest" ? parseInt(currentV, 10) : 0
      const upper = Math.max(maxVersion, currentN || 0)
      for (let i = 2; i <= upper; i++) {
        opts.push({ value: String(i), label: `v=${i}` })
      }
      opts.push({ value: "latest", label: "latest" })
      return opts
    },
  },
]

const medialinkStyles = `
.gowiki-media-link {
  cursor: pointer;
  text-decoration: underline;
  color: #2b6cb0;
}

.gowiki-media-link::before {
  content: "📎 ";
  font-size: 0.85em;
}

.gowiki-media-link.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
  border-radius: 2px;
}
`

function escapeMarkdownText(text: string): string {
  return String(text ?? "").replace(/[\[\]]/g, ch => "\\" + ch)
}

class MedialinkNodeView {
  dom: HTMLElement
  private node: PMNode
  private outerView: EditorView
  private getPos: () => number | undefined

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.outerView = view
    this.getPos = getPos

    this.dom = document.createElement("a")
    this.dom.className = "gowiki-media-link"
    this.dom.addEventListener("click", this.onClick)
    this.applyAttrs()
  }

  private applyAttrs() {
    let href = this.node.attrs.href ?? ""
    const version = this.node.attrs.version ?? null
    if (version) {
      href += (href.includes("?") ? "&" : "?") + "v=" + version
    }
    this.dom.setAttribute("href", href)
    if (this.node.attrs.title) {
      this.dom.setAttribute("title", this.node.attrs.title)
    } else {
      this.dom.removeAttribute("title")
    }
    this.dom.textContent = this.node.attrs.label || this.node.attrs.href || "file"
  }

  private onClick = (event: MouseEvent) => {
    event.preventDefault()
    if (!this.outerView.editable) {
      // In view mode: navigate via /media/ prefix to avoid SPA routing
      const href = this.dom.getAttribute("href") || ""
      const resolved = new URL(href, window.location.href)
      const mediaUrl = "/media/" + resolved.pathname.replace(/^\/+/, "") + resolved.search
      window.open(mediaUrl, "_blank")
    }
    // In edit mode: ProseMirror handles selection
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.applyAttrs()
    return true
  }

  selectNode() {
    this.dom.classList.add("ProseMirror-selectednode")
  }

  deselectNode() {
    this.dom.classList.remove("ProseMirror-selectednode")
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {
    this.dom.removeEventListener("click", this.onClick)
  }
}

export const medialinkPlugin: WikiPlugin = {
  register(reg) {
    // Schema: inline atom node
    reg.registerSchema({
      nodes: {
        medialink: {
          group: "inline",
          inline: true,
          atom: true,
          attrs: {
            href: { default: "" },
            label: { default: "" },
            version: { default: null },
            title: { default: null },
            autoText: { default: false },
          },
          toDOM(node: PMNode) {
            let href = node.attrs.href ?? ""
            const version = node.attrs.version ?? null
            if (version) {
              href += (href.includes("?") ? "&" : "?") + "v=" + version
            }
            const attrs: Record<string, string> = {
              class: "gowiki-media-link",
              href,
            }
            if (node.attrs.title) {
              attrs.title = node.attrs.title
            }
            return ["a", attrs, node.attrs.label || node.attrs.href || "file"]
          },
          parseDOM: [
            {
              tag: "a.gowiki-media-link",
              getAttrs(dom: HTMLElement) {
                return {
                  href: dom.getAttribute("href") || "",
                  label: dom.textContent || "",
                  title: dom.getAttribute("title") || null,
                }
              },
            },
          ],
        },
      },
    })

    // Node properties for the property panel
    reg.registerNodeProperties("medialink", medialinkProperties)

    // Markdown -> PM: handle the synthetic "medialink" token
    reg.registerText("medialink", {
      run(ctx, tok) {
        let href = tok.attrGet?.("href") ?? tok.meta?.href ?? ""
        const title = tok.attrGet?.("title") ?? tok.meta?.title ?? null
        const autoText = tok.meta?.autoText ?? false
        let label = tok.meta?.label ?? tok.content ?? ""

        // Extract ?v=N from href
        let version: string | null = null
        const vMatch = href.match(/[?&]v=([^&]+)/)
        if (vMatch) {
          version = normalizeMediaVersion(vMatch[1])
          href = href.replace(/[?&]v=[^&]+/, "").replace(/\?$/, "")
        }

        if (!label) {
          label = href.split("/").pop() || "file"
        }

        ctx.push(
          ctx.schema.nodes.medialink.create({ href, label, version, title, autoText })
        )
      },
    })

    // PM -> Markdown: serialize back to standard link syntax [label](href?v=N)
    reg.registerPMNode("medialink", {
      print(node) {
        let href = String(node.attrs.href ?? "")
        const version = node.attrs.version ?? null
        if (version) {
          href += (href.includes("?") ? "&" : "?") + "v=" + version
        }
        const title = node.attrs.title
        const autoText = node.attrs.autoText
        const label = String(node.attrs.label ?? "")

        if (autoText) {
          if (title) {
            return `[](${href} "${String(title).replace(/"/g, '\\"')}")`
          }
          return `[](${href})`
        }

        const escapedLabel = escapeMarkdownText(label)
        if (title) {
          return `[${escapedLabel}](${href} "${String(title).replace(/"/g, '\\"')}")`
        }
        return `[${escapedLabel}](${href})`
      },
    })

    // NodeView for proper click handling and selection in edit mode
    reg.registerEditorPlugin(() => {
      return new PMPlugin({
        key: new PluginKey("gowiki.medialinkNodeView"),
        props: {
          nodeViews: {
            medialink(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new MedialinkNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Styles
    reg.registerStyle("medialink", medialinkStyles)
  },
}
