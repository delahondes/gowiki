import hljs from "highlight.js/lib/common"
import { Plugin, PluginKey } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"

// Suppress hljs warning about unescaped HTML in code blocks —
// our code block content is plain text from ProseMirror, not user HTML.
hljs.configure({ ignoreUnescapedHTML: true })

/**
 * Apply syntax highlighting to all <pre><code> elements in a container.
 * Only call this for view-mode / read-only views — never in the editable editor.
 */
export function highlightCodeBlocks(container: HTMLElement) {
  container.querySelectorAll<HTMLElement>("pre code").forEach((el) => {
    hljs.highlightElement(el)
  })
}

/**
 * Parse highlight.js HTML output into a list of {from, to, classes} spans
 * relative to the start of the text.
 */
function parseHighlightTokens(html: string): { from: number; to: number; classes: string }[] {
  const tokens: { from: number; to: number; classes: string }[] = []
  let pos = 0
  // Stack for nested spans.
  const stack: { start: number; classes: string }[] = []

  let i = 0
  while (i < html.length) {
    if (html[i] === "<") {
      const closeTag = html.indexOf(">", i)
      if (closeTag === -1) break
      const tag = html.slice(i, closeTag + 1)

      if (tag.startsWith("<span")) {
        const classMatch = tag.match(/class="([^"]*)"/)
        stack.push({ start: pos, classes: classMatch ? classMatch[1] : "" })
      } else if (tag === "</span>") {
        const opened = stack.pop()
        if (opened && opened.classes && pos > opened.start) {
          tokens.push({ from: opened.start, to: pos, classes: opened.classes })
        }
      }
      i = closeTag + 1
    } else if (html[i] === "&") {
      // Decode HTML entities produced by hljs.
      const semi = html.indexOf(";", i)
      if (semi !== -1 && semi - i < 8) {
        const entity = html.slice(i, semi + 1)
        if (entity === "&amp;") pos += 1
        else if (entity === "&lt;") pos += 1
        else if (entity === "&gt;") pos += 1
        else if (entity === "&quot;") pos += 1
        else if (entity === "&#x27;" || entity === "&#39;") pos += 1
        else pos += 1 // any other entity = 1 decoded char
        i = semi + 1
      } else {
        pos++
        i++
      }
    } else {
      pos++
      i++
    }
  }
  return tokens
}

const highlightKey = new PluginKey("gowiki.highlight")

/**
 * ProseMirror plugin that applies syntax highlighting decorations
 * to code_block nodes in the editor.
 */
export function highlightPlugin() {
  return new Plugin({
    key: highlightKey,
    state: {
      init(_, state) {
        return safeBuildDecorations(state)
      },
      apply(tr, oldSet, _oldState, newState) {
        if (!tr.docChanged) return oldSet
        return safeBuildDecorations(newState)
      },
    },
    props: {
      decorations(state) {
        return highlightKey.getState(state)
      },
    },
  })
}

function safeBuildDecorations(state: any): DecorationSet {
  try {
    return buildDecorations(state)
  } catch {
    return DecorationSet.empty
  }
}

function buildDecorations(state: any): DecorationSet {
  const decorations: Decoration[] = []

  state.doc.descendants((node: any, pos: number) => {
    if (node.type.name !== "code_block") return

    const text = node.textContent
    if (!text) return

    const lang = node.attrs.language || ""
    let result
    try {
      result = lang
        ? hljs.highlight(text, { language: lang, ignoreIllegals: true })
        : hljs.highlightAuto(text)
    } catch {
      return // unknown language or hljs error
    }

    if (!result.value || result.value === text) return

    // Position of the first text character inside the code_block.
    const basePos = pos + 1
    const textLen = text.length

    try {
      const tokens = parseHighlightTokens(result.value)
      for (const token of tokens) {
        // Guard against out-of-bounds tokens.
        if (token.from >= textLen || token.to > textLen) continue
        if (token.from >= token.to) continue
        decorations.push(
          Decoration.inline(basePos + token.from, basePos + token.to, {
            class: token.classes,
          })
        )
      }
    } catch {
      // Skip this code block if decoration creation fails.
    }
  })

  return DecorationSet.create(state.doc, decorations)
}
