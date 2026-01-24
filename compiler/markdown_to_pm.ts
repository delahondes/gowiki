import MarkdownIt from "markdown-it"
import { Node } from "prosemirror-model"

import { CompileContext, run } from "./kernel"
import { Registry } from "./registry"


/**
 * Convert Markdown text to a ProseMirror document.
 *
 * This is a frontend compiler pass:
 * - Markdown → tokens
 * - tokens → PM DocModel
 */
export function markdownToPM(
  markdown: string,
  registry: Registry
): Node {
  const schema = registry.schema
  // 1. Parse Markdown
  const md = new MarkdownIt("commonmark", {
    html: false,
    linkify: false,
    typographer: false,
  })

  const tokens = md.parse(markdown, {})



  // 3. Run kernel
  const ctx = new CompileContext(schema)
  run(tokens, registry, ctx, { strict: true })

  // 4. Build final document
  return schema.nodes.doc.create(null, ctx.output)
}