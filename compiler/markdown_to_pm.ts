import MarkdownIt from "markdown-it"
import { Schema, Node as PMNode } from "prosemirror-model"

import { CompileContext, run } from "./kernel"
import { Registry } from "./registry"
import { registerCoreNodes } from "./core_nodes"

/**
 * Convert Markdown text to a ProseMirror document.
 *
 * This is a frontend compiler pass:
 * - Markdown → tokens
 * - tokens → PM DocModel
 */
export function markdownToPM(
  markdown: string,
  schema: Schema
): PMNode {
  // 1. Parse Markdown
  const md = new MarkdownIt("commonmark", {
    html: false,
    linkify: false,
    typographer: false,
  })

  const tokens = md.parse(markdown, {})

  // 2. Setup registry and register semantics
  const registry = new Registry(schema)
  registerCoreNodes(registry)

  // 3. Run kernel
  const ctx = new CompileContext(schema)
  run(tokens, registry, ctx, { strict: true })

  // 4. Build final document
  return schema.nodes.doc.create(null, ctx.output)
}