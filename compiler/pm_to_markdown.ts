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
      let text = node.text ?? ""
      for (const mark of node.marks) {
        const printer = registry.getPMMark(mark.type.name)
        if (!printer) {
          throw new Error(`No Markdown printer for mark "${mark.type.name}"`)
        }
        text = printer.open + text + printer.close
      }
      return text
    }

    const printer = registry.getPMNode(node.type.name)
    if (!printer) {
      throw new Error(`No Markdown printer for node "${node.type.name}"`)
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