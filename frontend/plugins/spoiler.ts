import type { Plugin as WikiPlugin, NodePropertySpec } from "../compiler/registry"

const spoilerProperties: NodePropertySpec[] = [
  {
    name: "title",
    label: "Title",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
  },
]

const spoilerStyles = `
.ProseMirror details.gowiki-spoiler {
  border: 1px solid #ddd;
  border-radius: 4px;
  margin: 0.5em 0;
  padding: 0;
}
.ProseMirror details.gowiki-spoiler > summary {
  cursor: pointer;
  padding: 0.4em 0.8em;
  background: #f5f5f5;
  font-weight: 600;
  font-size: 0.95em;
  user-select: none;
  list-style: revert;
}
.ProseMirror details.gowiki-spoiler > summary:hover {
  background: #eee;
}
.ProseMirror details.gowiki-spoiler > .gowiki-spoiler-body {
  padding: 0.5em 0.8em;
}
/* In view mode, start folded (JS removes the open attribute) */
.gowiki-view details.gowiki-spoiler > summary {
  cursor: pointer;
}
`

/** Return the length of the longest consecutive backtick sequence in text. */
function longestBacktickRun(text: string): number {
  let max = 0
  let cur = 0
  for (let i = 0; i < text.length; i++) {
    if (text[i] === "`") {
      cur++
      if (cur > max) max = cur
    } else {
      cur = 0
    }
  }
  return max
}

export const spoilerPlugin: WikiPlugin = {
  register(reg) {
    // ── Schema ──
    reg.registerSchema({
      nodes: {
        spoiler: {
          group: "block",
          content: "block+",
          defining: true,
          attrs: { title: { default: "" } },
          toDOM(node: any) {
            const title = node.attrs.title || "Details"
            const summary = document.createElement("summary")
            summary.textContent = title
            summary.contentEditable = "false"
            const body = document.createElement("div")
            body.className = "gowiki-spoiler-body"
            const details = document.createElement("details")
            details.className = "gowiki-spoiler"
            details.setAttribute("open", "")
            details.appendChild(summary)
            details.appendChild(body)
            return { dom: details, contentDOM: body }
          },
          parseDOM: [
            {
              tag: "details.gowiki-spoiler",
              getAttrs(dom: HTMLElement) {
                const summary = dom.querySelector(":scope > summary")
                return { title: summary?.textContent || "" }
              },
              contentElement: "div.gowiki-spoiler-body",
            },
          ],
        },
      },
    })

    // ── Property panel for title editing ──
    reg.registerNodeProperties("spoiler", spoilerProperties)

    // ── markdown-it block rule ──
    // Matches ```spoiler <title>\n...\n```
    reg.registerMarkdownItPlugin((md: any) => {
      md.block.ruler.before("fence", "spoiler_fence", (state: any, startLine: number, endLine: number, silent: boolean) => {
        const startPos = state.bMarks[startLine] + state.tShift[startLine]
        const maxPos = state.eMarks[startLine]
        const firstLine = state.src.slice(startPos, maxPos)

        // Must start with ```spoiler
        if (!firstLine.match(/^`{3,}spoiler(?:\s|$)/)) return false
        if (silent) return true

        const backtickCount = firstLine.match(/^(`+)/)![1].length
        const title = firstLine.slice(backtickCount).replace(/^spoiler\s*/, "").trim()

        // Find closing fence
        let nextLine = startLine + 1
        let found = false
        for (; nextLine < endLine; nextLine++) {
          const lineStart = state.bMarks[nextLine] + state.tShift[nextLine]
          const lineEnd = state.eMarks[nextLine]
          const line = state.src.slice(lineStart, lineEnd)
          if (line.match(new RegExp("^`{" + backtickCount + ",}\\s*$"))) {
            found = true
            break
          }
        }
        if (!found) return false

        // Emit spoiler_open
        const openToken = state.push("spoiler_open", "details", 1)
        openToken.map = [startLine, nextLine + 1]
        openToken.meta = { title }

        // Tokenize inner content as block markdown
        const oldParentType = state.parentType
        const oldLineMax = state.lineMax
        state.parentType = "root"
        state.lineMax = nextLine

        const innerStart = startLine + 1
        const innerEnd = nextLine

        // Check if inner content has any non-blank lines
        let hasContent = false
        for (let l = innerStart; l < innerEnd; l++) {
          if (state.src.slice(state.bMarks[l], state.eMarks[l]).trim() !== "") {
            hasContent = true
            break
          }
        }

        if (hasContent) {
          const tokensBefore = state.tokens.length
          state.md.block.tokenize(state, innerStart, innerEnd)
          // Strip maps from inner tokens so that semanticBlockEnd (in
          // injectExtraBlankParagraphs) doesn't use the inner inline
          // token's map, which would under-count the spoiler's extent
          // and cause an extra blank paragraph on each round-trip.
          for (let t = tokensBefore; t < state.tokens.length; t++) {
            state.tokens[t].map = null
          }
        } else {
          // Empty body: inject an empty paragraph to satisfy block+ constraint
          state.push("paragraph_open", "p", 1)
          state.push("paragraph_close", "p", -1)
        }

        state.parentType = oldParentType
        state.lineMax = oldLineMax

        // Emit spoiler_close
        const closeToken = state.push("spoiler_close", "details", -1)
        closeToken.map = [nextLine, nextLine + 1]

        state.line = nextLine + 1
        return true
      })
    })

    // ── Markdown → PM ──
    reg.registerNode("spoiler_open", {
      open(ctx) {
        const title = ctx.token?.meta?.title ?? ""
        ctx.open(ctx.schema.nodes.spoiler.create({ title }))
      },
    })

    reg.registerNode("spoiler_close", {
      close(ctx) {
        ctx.close()
      },
    })

    // ── PM → Markdown ──
    // Children are full block nodes (paragraphs serialize with trailing \n\n).
    // We keep the full output so paragraph separators are preserved on re-parse.
    // Only strip trailing whitespace from the combined body before the closing fence.
    reg.registerPMNode("spoiler", {
      print(node, _ctx, recurse) {
        const title = node.attrs.title || ""
        let body = ""
        node.content.forEach(child => {
          body += recurse(child)
        })
        // Use enough backticks to avoid collision with inner fences
        const maxRun = longestBacktickRun(body)
        const ticks = "`".repeat(Math.max(3, maxRun + 1))
        let out = ticks + "spoiler" + (title ? " " + title : "") + "\n"
        out += body.trimEnd() + "\n"
        out += ticks + "\n\n"
        return out
      },
    })

    // ── Toolbar command ──
    reg.registerCommand("spoiler", "insert", (state, dispatch) => {
      const spoilerType = reg.schema.nodes.spoiler
      if (!spoilerType) return false
      if (dispatch) {
        const para = reg.schema.nodes.paragraph.create()
        const node = spoilerType.create({ title: "Details" }, para)
        const tr = state.tr.replaceSelectionWith(node).scrollIntoView()
        // Place cursor inside the empty paragraph within the spoiler
        const $pos = tr.doc.resolve(tr.mapping.map(state.selection.from))
        for (let d = $pos.depth; d > 0; d--) {
          if ($pos.node(d).type === spoilerType) {
            const insidePos = $pos.start(d) + 1
            tr.setSelection(state.selection.constructor.near(tr.doc.resolve(insidePos)))
            break
          }
        }
        dispatch(tr)
      }
      return true
    })

    // ── Styles ──
    reg.registerStyle("spoiler", spoilerStyles)
  },
}
