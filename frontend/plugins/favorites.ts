import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

// {favorites count=20}
//
// Renders the current user's favorite pages, most-recently-added first. Reuses
// the .gowiki-changes-* styles for visual parity with {changes} — a favorites
// list in the sidebar reads as the same kind of quick-nav block.

const favoritesProperties = [
  {
    name: "count",
    label: "Count",
    default: "20",
    parse: (raw: string) => {
      const n = parseInt(raw.trim(), 10)
      if (isNaN(n) || n < 1) return "20"
      if (n > 200) return "200"
      return String(n)
    },
  },
]

const favoritesStyles = `
.gowiki-favorites {
  margin: 4px 0;
  font-size: 13px;
  line-height: 1.3;
}

.gowiki-favorites-loading,
.gowiki-favorites-empty,
.gowiki-favorites-anon {
  color: var(--gw-color-muted);
  font-style: italic;
  padding: 2px 0;
}

.gowiki-favorites-error {
  color: var(--gw-color-error);
  font-style: italic;
  padding: 2px 0;
}

.gowiki-favorites-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.gowiki-favorites-item {
  padding: 3px 0;
  border-bottom: 1px solid var(--gw-color-border-soft);
}

.gowiki-favorites-item:last-child {
  border-bottom: none;
}

.gowiki-favorites-link {
  text-decoration: none;
}

.gowiki-favorites-link:hover {
  text-decoration: underline;
}

.gowiki-favorites-path {
  color: var(--gw-color-subtle);
  font-size: 11px;
  margin-left: 4px;
}

#app.gowiki-editing .gowiki-favorites {
  background: var(--gw-color-surface);
  padding: 6px;
}

#app.gowiki-editing .gowiki-favorites.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
`

interface FavoriteEntry {
  path: string
  added_at: string
  title?: string
  exists?: boolean
}

class FavoritesNodeView {
  dom: HTMLElement
  private node: PMNode
  private onFavoritesChanged: () => void

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-favorites"
    this.dom.contentEditable = "false"
    this.onFavoritesChanged = () => this.fetchAndRender()
    window.addEventListener("gowiki:favorites-changed", this.onFavoritesChanged)
    this.fetchAndRender()
  }

  private async fetchAndRender() {
    this.dom.innerHTML = '<div class="gowiki-favorites-loading">Loading…</div>'
    try {
      const resp = await fetch("/api/auth/me/favorites")
      if (resp.status === 401) {
        this.dom.innerHTML = '<div class="gowiki-favorites-anon">Sign in to see your favorites</div>'
        return
      }
      if (!resp.ok) throw new Error("Failed to fetch favorites")
      const data = await resp.json()
      const raw: FavoriteEntry[] = data.favorites || []
      const count = parseInt(this.node.attrs.count || "20", 10)
      const entries = raw.slice(0, isNaN(count) || count < 1 ? 20 : count)

      this.dom.innerHTML = ""
      if (entries.length === 0) {
        this.dom.innerHTML = '<div class="gowiki-favorites-empty">No favorites yet — click the star icon on a page to add it</div>'
        return
      }

      const ul = document.createElement("ul")
      ul.className = "gowiki-favorites-list"
      for (const entry of entries) {
        const li = document.createElement("li")
        li.className = "gowiki-favorites-item"

        const a = document.createElement("a")
        const linkCls = entry.exists === false ? "gowiki-link-missing" : "gowiki-link-exists"
        a.className = `gowiki-favorites-link ${linkCls}`
        a.href = entry.path
        a.textContent = entry.title || entry.path
        li.appendChild(a)

        // Show the path as a secondary label when we have a distinct title,
        // otherwise it would just duplicate the link text.
        if (entry.title && entry.title !== entry.path) {
          const meta = document.createElement("span")
          meta.className = "gowiki-favorites-path"
          meta.textContent = entry.path
          li.appendChild(meta)
        }

        ul.appendChild(li)
      }
      this.dom.appendChild(ul)
    } catch {
      this.dom.innerHTML = '<div class="gowiki-favorites-error">Failed to load favorites</div>'
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    if (node.attrs.count !== this.node.attrs.count) {
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

  destroy(): void {
    window.removeEventListener("gowiki:favorites-changed", this.onFavoritesChanged)
  }
}

export const favoritesPlugin: WikiPlugin = {
  register(reg) {
    reg.registerSchema({
      nodes: {
        favorites: {
          group: "block",
          atom: true,
          attrs: {
            count: { default: "20" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-favorites",
                "data-count": node.attrs.count || "20",
              },
              `Favorites${node.attrs.count !== "20" ? ` (${node.attrs.count})` : ""}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-favorites",
              getAttrs(dom: HTMLElement) {
                return {
                  count: dom.getAttribute("data-count") || "20",
                }
              },
            },
          ],
        },
      },
    })

    reg.registerSelfContainedDirective("favorites", {
      tokenType: "favorites",
      nodeType: "favorites",
      properties: favoritesProperties,
    })

    reg.registerText("favorites", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.favorites.create({
            count: attrs.count ?? "20",
          })
        )
      },
    })

    reg.registerPMNode("favorites", {
      print(node) {
        if (node.attrs.count && node.attrs.count !== "20") {
          return `{favorites count=${node.attrs.count}}\n\n`
        }
        return `{favorites}\n\n`
      },
    })

    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.favorites"),
        props: {
          nodeViews: {
            favorites(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new FavoritesNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    reg.registerCommand("favorites", "insert", (state, dispatch) => {
      const favType = reg.schema.nodes.favorites
      if (!favType) return false
      if (dispatch) {
        const node = favType.create({ count: "20" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === favType && insertedAt === null) {
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

    reg.registerStyle("favorites", favoritesStyles)
  },
}
