import type { Plugin as WikiPlugin } from "../compiler/registry"

/**
 * Flow markers: inline position markers embedded in the document.
 *
 * Syntax:
 *   {#id}...{#/id}   — range marker (AI proposals)
 *   {#id/}           — point marker (self-closing)
 *   {#@user/}        — cursor marker (collab/cursor sync)
 *
 * Markers are invisible in visual mode (thin colored indicator),
 * and shown as literal text in raw mode. Ephemeral markers (all
 * except {#!...} bookmarks) are stripped on publish.
 */

const markerStyles = `
.gowiki-flow-marker {
  display: inline;
  position: relative;
  width: 0;
  overflow: visible;
  font-size: 0;
  line-height: 0;
  vertical-align: baseline;
  user-select: none;
}
.gowiki-flow-marker::before {
  content: "";
  display: inline-block;
  width: 2px;
  height: 1em;
  vertical-align: text-bottom;
  border-radius: 1px;
}
/* AI markers: indigo */
.gowiki-flow-marker[data-marker-prefix=""]::before {
  background: rgba(92, 107, 192, 0.6);
}
/* Cursor markers: green */
.gowiki-flow-marker[data-marker-prefix="@"]::before {
  background: rgba(76, 175, 80, 0.6);
}
/* Hover: highlight the marker */
.gowiki-flow-marker:hover::before {
  background: rgba(92, 107, 192, 1);
  width: 3px;
}
`

export const flowMarkerPlugin: WikiPlugin = {
  register(reg) {
    // ── Schema ──
    reg.registerSchema({
      nodes: {
        flow_marker: {
          inline: true,
          atom: true,
          group: "inline",
          selectable: false,
          attrs: {
            id: { default: "" },
            type: { default: "open" },   // "open", "close", "point"
            prefix: { default: "" },      // "", "@", "!"
          },
          toDOM(node: any) {
            return ["span", {
              class: "gowiki-flow-marker",
              "data-marker-id": node.attrs.id,
              "data-marker-type": node.attrs.type,
              "data-marker-prefix": node.attrs.prefix,
            }]
          },
          parseDOM: [{
            tag: "span.gowiki-flow-marker",
            getAttrs(dom: HTMLElement) {
              return {
                id: dom.getAttribute("data-marker-id") || "",
                type: dom.getAttribute("data-marker-type") || "open",
                prefix: dom.getAttribute("data-marker-prefix") || "",
              }
            },
          }],
        },
      },
    })

    // ── Markdown-it inline rule ──
    // Matches {#id}, {#/id}, {#id/}, {#@user/}, {#@user}, {#/@user}
    reg.registerMarkdownItPlugin((md: any) => {
      md.inline.ruler.push("gowiki_flow_marker", (state: any, silent: boolean) => {
        const src = state.src
        const start = state.pos
        if (src.charCodeAt(start) !== 0x7B) return false // {
        if (src.charCodeAt(start + 1) !== 0x23) return false // #

        // Find closing }
        const closeBrace = src.indexOf("}", start + 2)
        if (closeBrace === -1 || closeBrace > state.posMax) return false

        const inner = src.slice(start + 2, closeBrace) // everything between {# and }
        if (!inner) return false

        // Parse the marker
        let prefix = ""
        let id = inner
        let type = "open"

        // Self-closing: {#id/}
        if (id.endsWith("/")) {
          id = id.slice(0, -1)
          type = "point"
        }

        // Close marker: {#/id}
        if (id.startsWith("/")) {
          id = id.slice(1)
          type = "close"
        }

        // Prefix: @ or !
        if (id.startsWith("@") || id.startsWith("!")) {
          prefix = id[0]
          id = id.slice(1)
        }

        // Validate ID
        if (!id || !/^[A-Za-z0-9._-]+$/.test(id)) return false

        if (silent) return true

        const token = state.push("flow_marker", "span", 0)
        token.meta = { id, type, prefix }
        token.markup = src.slice(start, closeBrace + 1)

        state.pos = closeBrace + 1
        return true
      })
    })

    // ── Markdown → PM ──
    reg.registerText("flow_marker", {
      run(ctx: any, tok: any) {
        const { id, type, prefix } = tok.meta || {}
        ctx.push(ctx.schema.nodes.flow_marker.create({
          id: id || "",
          type: type || "open",
          prefix: prefix || "",
        }))
      },
    })

    // ── PM → Markdown ──
    reg.registerPMNode("flow_marker", {
      print(node: any) {
        const { id, type, prefix } = node.attrs
        const fullId = prefix + id
        if (type === "close") return `{#/${fullId}}`
        if (type === "point") return `{#${fullId}/}`
        return `{#${fullId}}`
      },
    })

    // ── Styles ──
    reg.registerStyle("flow_marker", markerStyles)
  },
}

/**
 * Strip ephemeral flow markers from markdown.
 * Preserves bookmark markers ({#!...}).
 */
export function stripFlowMarkers(markdown: string): string {
  // Match {#...} and {#/...} but NOT {#!...}
  return markdown.replace(/\{#\/?@?[A-Za-z0-9._-]+\/?\}/g, (match) => {
    // Preserve bookmark markers
    if (match.startsWith("{#!") || match.startsWith("{#/!")) return match
    return ""
  })
}
