import type { Plugin as WikiPlugin } from "../compiler/registry"

const DEFAULT_COLOR = "yellow"

export const HIGHLIGHT_COLORS = [
  { name: "Yellow", value: "yellow" },
  { name: "Red", value: "#ffcccc" },
  { name: "Green", value: "#ccffcc" },
  { name: "Blue", value: "#cce5ff" },
  { name: "Orange", value: "#ffe0b2" },
  { name: "Pink", value: "#f8bbd0" },
  { name: "Cyan", value: "#b2ebf2" },
]

const highlightStyles = `
.ProseMirror mark[data-highlight] {
  border-radius: 2px;
  padding: 0 1px;
}
`

export const highlightPlugin: WikiPlugin = {
  register(reg) {
    // ── Schema ──
    reg.registerSchema({
      marks: {
        highlight: {
          attrs: { color: { default: DEFAULT_COLOR } },
          parseDOM: [
            {
              tag: "mark[data-highlight]",
              getAttrs(dom: HTMLElement) {
                return { color: dom.getAttribute("data-highlight") || DEFAULT_COLOR }
              },
            },
          ],
          toDOM(mark: any) {
            return [
              "mark",
              {
                "data-highlight": mark.attrs.color,
                style: `background-color: ${mark.attrs.color}`,
              },
              0,
            ]
          },
        },
      },
    })

    // ── Markdown-it inline rule: ==text== and =={color}text== ──
    reg.registerMarkdownItPlugin((md: any) => {
      md.inline.ruler.push("gowiki_highlight", (state: any, silent: boolean) => {
        const src = state.src
        const start = state.pos
        if (src.charCodeAt(start) !== 0x3D || src.charCodeAt(start + 1) !== 0x3D) return false // ==
        // Reject === (triple equals at the open position)
        if (start + 2 < state.posMax && src.charCodeAt(start + 2) === 0x3D) return false

        // Find closing ==
        let closePos = -1
        for (let i = start + 2; i < state.posMax - 1; i++) {
          if (src.charCodeAt(i) !== 0x3D) continue
          if (src.charCodeAt(i + 1) !== 0x3D) continue
          // Skip === (triple-or-more equals runs) in close search
          if (i + 2 < state.posMax && src.charCodeAt(i + 2) === 0x3D) continue
          closePos = i
          break
        }
        if (closePos === -1 || closePos <= start + 2) return false

        if (!silent) {
          // Check for optional {color=VALUE} prefix after ==
          let color = DEFAULT_COLOR
          let contentStart = start + 2
          if (src.charCodeAt(contentStart) === 0x7B) { // {
            const braceEnd = src.indexOf("}", contentStart + 1)
            if (braceEnd !== -1 && braceEnd < closePos) {
              const inner = src.slice(contentStart + 1, braceEnd).trim()
              const cm = inner.match(/^color=(.+)$/)
              if (cm && /^#?[a-zA-Z0-9]+$/.test(cm[1])) {
                color = cm[1]
                contentStart = braceEnd + 1
              }
            }
          }

          if (state.pending) state.pushPending()
          const tokenO = state.push("highlight_open", "mark", 1)
          tokenO.markup = "=="
          tokenO.meta = { color }

          // Recursively tokenize inner content
          const savedMax = state.posMax
          state.pos = contentStart
          state.posMax = closePos
          state.md.inline.tokenize(state)
          state.posMax = savedMax

          const tokenC = state.push("highlight_close", "mark", -1)
          tokenC.markup = "=="
        }
        state.pos = closePos + 2
        return true
      })
    })

    // ── Markdown → PM ──
    reg.registerNode("highlight_open", {
      open(ctx) {
        const color = ctx.token?.meta?.color || DEFAULT_COLOR
        ctx.pushMark(ctx.schema.marks.highlight.create({ color }))
      },
    })

    reg.registerNode("highlight_close", {
      close(ctx) {
        ctx.popMark()
      },
    })

    // ── PM → Markdown ──
    reg.registerPMMark("highlight", {
      open: (mark: any) => {
        const color = mark.attrs.color || DEFAULT_COLOR
        if (color === DEFAULT_COLOR) return "=="
        return `=={color=${color}}`
      },
      close: "==",
    })

    // ── Toolbar command ──
    reg.registerCommand("highlight", "Highlight", (state, dispatch) => {
      const markType = reg.schema.marks.highlight
      if (!markType) return false
      if (dispatch) {
        const { from, to } = state.selection
        if (from === to) return false
        const hasMark = state.doc.rangeHasMark(from, to, markType)
        if (hasMark) {
          dispatch(state.tr.removeMark(from, to, markType))
        } else {
          dispatch(state.tr.addMark(from, to, markType.create({ color: DEFAULT_COLOR })))
        }
      }
      return true
    })

    // ── Styles ──
    reg.registerStyle("highlight", highlightStyles)
  },
}
