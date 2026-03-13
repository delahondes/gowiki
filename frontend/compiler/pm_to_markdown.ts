import { Schema } from "prosemirror-model"

/* -----------------------------
 * PM → Markdown printer interfaces
 * ----------------------------- */

export class PrintContext {
  constructor(
    public readonly schema: Schema
  ) {}
}



import { Node as PMNode } from "prosemirror-model"
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
      const hasCodeMark = node.marks.some(m => m.type.name === "code")
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
