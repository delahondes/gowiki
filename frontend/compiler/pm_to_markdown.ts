import { Schema } from "prosemirror-model"

/* -----------------------------
 * PM → Markdown printer interfaces
 * ----------------------------- */

export class PrintContext {
  constructor(
    public readonly schema: Schema
  ) {}
}



import { Fragment, Mark, Node as PMNode } from "prosemirror-model"
import { Registry } from "./registry"

function escapeMarkdownText(text: string): string {
  return text.replace(/[\\*_`>{}~^]/g, ch => "\\" + ch)
}

function serializePlainTextWithAutoLinks(text: string): string {
  // Match both URLs and bare email addresses
  const autoRe = /https?:\/\/[^\s<>()]+|[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}/g
  let out = ""
  let last = 0
  let m: RegExpExecArray | null
  while ((m = autoRe.exec(text)) !== null) {
    const start = m.index
    const match = m[0]
    out += escapeMarkdownText(text.slice(last, start))
    if (match.includes("@")) {
      out += `[](mailto:${match})`
    } else {
      out += `[](${match})`
    }
    last = start + match.length
  }
  out += escapeMarkdownText(text.slice(last))
  return out
}

function getMarkOpen(mark: Mark, registry: Registry): string {
  const printer = registry.getPMMark(mark.type.name)
  if (!printer) return ""
  return typeof printer.open === "function" ? printer.open(mark) : printer.open
}

function getMarkClose(mark: Mark, registry: Registry): string {
  const printer = registry.getPMMark(mark.type.name)
  if (!printer) return ""
  return typeof printer.close === "function" ? printer.close(mark) : printer.close
}

/**
 * Serialize inline content with mark-tracking: marks are opened/closed
 * at transitions rather than per text node. This prevents duplicate
 * delimiters like ==== when adjacent text nodes share the same mark.
 */
export function serializeInlineFragment(
  fragment: Fragment,
  registry: Registry,
  printNode: (node: PMNode) => string
): string {
  const nodes: PMNode[] = []
  fragment.forEach(n => nodes.push(n))

  let out = ""
  let activeMarks: readonly Mark[] = []

  function closeAllMarks() {
    for (let i = activeMarks.length - 1; i >= 0; i--) {
      out += getMarkClose(activeMarks[i], registry)
    }
    activeMarks = []
  }

  for (const node of nodes) {
    if (!node.isText) {
      // Non-text inline node (image, flow_marker, etc.):
      // close all active marks, emit node, reset marks.
      closeAllMarks()
      out += printNode(node)
      continue
    }

    // Link with autoText: self-contained serialization via printNode.
    if (node.marks.some(m => m.type.name === "link" && m.attrs.autoText)) {
      closeAllMarks()
      out += printNode(node)
      continue
    }

    // ProseMirror stores marks inner-first (em before highlight).
    // For serialization we need outermost-first so the prefix comparison
    // keeps outer marks open while toggling inner marks.
    const nodeMarks = [...node.marks].reverse()

    // Find the longest common prefix of active marks and this node's marks.
    let commonLen = 0
    while (commonLen < activeMarks.length && commonLen < nodeMarks.length &&
           activeMarks[commonLen].eq(nodeMarks[commonLen])) {
      commonLen++
    }

    // Close marks no longer active (innermost first = end of array first).
    for (let i = activeMarks.length - 1; i >= commonLen; i--) {
      out += getMarkClose(activeMarks[i], registry)
    }

    // Open new marks (outermost already open, add inner marks).
    for (let i = commonLen; i < nodeMarks.length; i++) {
      out += getMarkOpen(nodeMarks[i], registry)
    }

    activeMarks = nodeMarks

    // Emit text content.
    const hasCodeMark = node.marks.some(m => m.type.name === "code" || m.type.name === "code_expand")
    if (hasCodeMark) {
      out += node.text ?? ""
    } else if (nodeMarks.length === 0) {
      out += serializePlainTextWithAutoLinks(node.text ?? "")
    } else {
      out += escapeMarkdownText(node.text ?? "")
    }
  }

  // Close remaining marks.
  closeAllMarks()

  return out
}

/**
 * Convert a ProseMirror document to Markdown.
 */
export function pmToMarkdown(
  doc: PMNode,
  registry: Registry
): string {
  const ctx = new PrintContext(registry.schema)

  function printNode(node: PMNode): string {
    // Text node
    if (node.isText) {
      if (node.marks.length === 0) {
        return serializePlainTextWithAutoLinks(node.text ?? "")
      }
      const hasCodeMark = node.marks.some(m => m.type.name === "code" || m.type.name === "code_expand")
      let text = hasCodeMark ? (node.text ?? "") : escapeMarkdownText(node.text ?? "")
      for (const mark of node.marks) {
        const printer = registry.getPMMark(mark.type.name)
        if (!printer) {
          console.warn(`No Markdown printer for mark "${mark.type.name}", skipping`)
          continue
        }
        if (mark.type.name === "link" && mark.attrs.autoText) {
          const href = mark.attrs.href ?? ""
          const title = mark.attrs.title
          if (title) {
            text = `[](${href} "${String(title).replace(/"/g, '\\"')}")`
          } else {
            text = `[](${href})`
          }
          continue
        }
        const open =
          typeof printer.open === "function"
            ? printer.open(mark)
            : printer.open
        const close =
          typeof printer.close === "function"
            ? printer.close(mark)
            : printer.close
        text = open + text + close
      }
      return text
    }

    const printer = registry.getPMNode(node.type.name)
    if (!printer) {
      console.warn(`No Markdown printer for node "${node.type.name}", using text content`)
      return escapeMarkdownText(node.textContent)
    }

    return printer.print(node, ctx, printNode)
  }

  // Root doc node: print children only
  let out = ""
  doc.content.forEach(child => {
    out += printNode(child)
  })

  return out.trimEnd()
}
