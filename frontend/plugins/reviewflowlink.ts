import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

// --- Properties ---

const reviewflowLinkProperties = [
  {
    name: "version",
    label: "Version",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "page",
    label: "Page",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "Target page path (empty = current page)",
  },
]

// --- API ---

const API_BASE = "/api/plugin/reviewflow/v1"

interface VersionRecord {
  page_version: number
  timestamp: string
  confirmed_by: Record<string, string>
  version_tag: string
}

interface ReviewflowStatus {
  version_history?: VersionRecord[]
}

// --- NodeView ---

class ReviewflowLinkNodeView {
  dom: HTMLElement
  private node: PMNode
  private resolved: { pageVersion: number; pagePath: string; title: string } | null = null
  private error: string | null = null
  private loading = true

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-reviewflow-link"
    this.dom.contentEditable = "false"
    this.render()
    this.resolve()
  }

  private getTargetPage(): string {
    const page = (this.node.attrs.page || "").trim()
    if (page) return page
    return window.location.pathname
  }

  private render() {
    this.dom.innerHTML = ""
    const version = this.node.attrs.version || ""
    const targetPage = this.getTargetPage()
    const isLocal = !(this.node.attrs.page || "").trim()

    if (this.loading) {
      const span = document.createElement("span")
      span.className = "gowiki-rfl-loading"
      span.textContent = `Resolving v${version}...`
      this.dom.appendChild(span)
      return
    }

    if (this.error || !this.resolved) {
      const span = document.createElement("span")
      span.className = "gowiki-rfl-error"
      span.textContent = `v${version}${isLocal ? "" : ` (${targetPage})`} — ${this.error || "not found"}`
      this.dom.appendChild(span)
      return
    }

    const a = document.createElement("a")
    a.className = "gowiki-rfl-link gowiki-link-exists"
    a.href = `${this.resolved.pagePath}?v=${this.resolved.pageVersion}`
    a.title = `Validated version ${version} of ${this.resolved.pagePath}`

    // Shield icon
    const icon = document.createElement("span")
    icon.className = "gowiki-rfl-icon"
    icon.textContent = "\u{1F6E1}\uFE0F"
    a.appendChild(icon)

    // Title + version badge
    const label = document.createElement("span")
    label.className = "gowiki-rfl-label"
    label.textContent = isLocal ? "This page" : this.resolved.title || this.resolved.pagePath
    a.appendChild(label)

    const badge = document.createElement("span")
    badge.className = "gowiki-rfl-version-badge"
    badge.textContent = `v${version}`
    a.appendChild(badge)

    this.dom.appendChild(a)
  }

  private async resolve() {
    const version = (this.node.attrs.version || "").trim()
    const targetPage = this.getTargetPage()

    if (!version) {
      this.loading = false
      this.error = "no version specified"
      this.render()
      return
    }

    try {
      const cleanPath = targetPage.replace(/^\/+/, "")
      const resp = await fetch(`${API_BASE}/status/${cleanPath}`)
      if (!resp.ok) {
        this.loading = false
        this.error = "could not load reviewflow status"
        this.render()
        return
      }

      const status: ReviewflowStatus = await resp.json()
      const history = status.version_history || []
      const match = history.find(vr => vr.version_tag === version)

      if (!match) {
        this.loading = false
        this.error = "version not found in history"
        this.render()
        return
      }

      // Resolve page title
      let title = targetPage
      try {
        const metaResp = await fetch(`/api/pages/${cleanPath}`)
        if (metaResp.ok) {
          const pageData = await metaResp.json()
          if (pageData.title) title = pageData.title
        }
      } catch { /* use path as fallback */ }

      this.resolved = {
        pageVersion: match.page_version,
        pagePath: targetPage.startsWith("/") ? targetPage : "/" + targetPage,
        title,
      }
      this.loading = false
      this.render()
    } catch {
      this.loading = false
      this.error = "failed to resolve"
      this.render()
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    const changed =
      node.attrs.version !== this.node.attrs.version ||
      node.attrs.page !== this.node.attrs.page
    this.node = node
    if (changed) {
      this.loading = true
      this.resolved = null
      this.error = null
      this.render()
      this.resolve()
    }
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

// --- Styles ---

const reviewflowLinkStyles = `
.gowiki-reviewflow-link {
  display: inline-block;
  margin: 4px 0;
}

.gowiki-rfl-loading {
  color: #999;
  font-size: 13px;
  font-style: italic;
}

.gowiki-rfl-error {
  color: #c62828;
  font-size: 13px;
  border-bottom: 2px dotted #c62828;
  padding-bottom: 1px;
}

.gowiki-rfl-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  text-decoration: none;
  padding: 2px 8px;
  border-radius: 4px;
  background: #e8f5e9;
  border: 1px solid #c8e6c9;
  font-size: 13px;
  color: #2e7d32;
}

.gowiki-rfl-link:hover {
  background: #c8e6c9;
}

.gowiki-rfl-icon {
  font-size: 12px;
}

.gowiki-rfl-label {
  font-weight: 500;
}

.gowiki-rfl-version-badge {
  background: #1565c0;
  color: white;
  font-size: 11px;
  font-weight: 600;
  padding: 0 6px;
  border-radius: 8px;
}

#app.gowiki-editing .gowiki-reviewflow-link.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
`

// --- Plugin ---

export const reviewflowLinkPlugin: WikiPlugin = {
  register(reg) {
    // Schema node
    reg.registerSchema({
      nodes: {
        reviewflow_link: {
          group: "block",
          atom: true,
          attrs: {
            version: { default: "" },
            page: { default: "" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-reviewflow-link",
                "data-version": node.attrs.version || "",
                "data-page": node.attrs.page || "",
              },
              `Reviewflow link: v${node.attrs.version || "?"}${node.attrs.page ? ` (${node.attrs.page})` : ""}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-reviewflow-link",
              getAttrs(dom: HTMLElement) {
                return {
                  version: dom.getAttribute("data-version") || "",
                  page: dom.getAttribute("data-page") || "",
                }
              },
            },
          ],
        },
      },
    })

    // Self-contained directive: {reviewflow-link version=1.0 page=/path}
    reg.registerSelfContainedDirective("reviewflow-link", {
      tokenType: "reviewflow_link",
      nodeType: "reviewflow_link",
      properties: reviewflowLinkProperties,
    })

    // Markdown → PM: handle the synthetic token
    reg.registerText("reviewflow_link", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.reviewflow_link.create({
            version: attrs.version ?? "",
            page: attrs.page ?? "",
          })
        )
      },
    })

    // PM → Markdown: serialize back to directive syntax
    reg.registerPMNode("reviewflow_link", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.version) {
          parts.push(`version=${node.attrs.version}`)
        }
        if (node.attrs.page) {
          parts.push(`page=${node.attrs.page}`)
        }
        return parts.length
          ? `{reviewflow-link ${parts.join(" ")}}\n\n`
          : `{reviewflow-link}\n\n`
      },
    })

    // Editor plugin: NodeView
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.reviewflow-link"),
        props: {
          nodeViews: {
            reviewflow_link(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new ReviewflowLinkNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Command: insert a reviewflow-link node and open properties panel
    reg.registerCommand("reviewflow-link", "insert", (state, dispatch) => {
      const nodeType = reg.schema.nodes.reviewflow_link
      if (!nodeType) return false
      if (dispatch) {
        const node = nodeType.create({ version: "", page: "" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === nodeType && insertedAt === null) {
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
    reg.registerStyle("reviewflow-link", reviewflowLinkStyles)
  },
}
