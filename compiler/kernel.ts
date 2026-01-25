import { Fragment, Mark, Node as PMNode, Schema } from "prosemirror-model"
import type { Registry } from "./registry"

/**
 * Minimal token interface for markdown-it tokens.
 */
export type MarkdownToken = {
  type: string
  tag?: string
  content?: string
  children?: MarkdownToken[]
  attrGet?(name: string): string | null
}

/**
 * Generic compilation context used by all handlers.
 *
 * Owns:
 * - node stack / tree construction
 * - mark stack
 * - output buffer
 *
 * Does NOT own:
 * - token parsing
 * - token traversal policy
 * - markdown semantics
 */
export class CompileContext {
  public readonly schema: Schema

  public token!: MarkdownToken

  private readonly stack: Array<{ node: PMNode; children: PMNode[] }> = []
  public readonly output: PMNode[] = []

  public readonly marks: Mark[] = []

  constructor(schema: Schema) {
    this.schema = schema
  }

  /* -----------------------------
   * Tree construction
   * ----------------------------- */

  open(node: PMNode) {
    this.stack.push({ node, children: [] })
  }

  close() {
    const frame = this.stack.pop()
    if (!frame) throw new Error("close(): unbalanced close")

    const built = frame.node.copy(Fragment.fromArray(frame.children))
    this.push(built)
  }

  push(node: PMNode) {
    const top = this.stack[this.stack.length - 1]
    if (top) top.children.push(node)
    else this.output.push(node)
  }

  /* -----------------------------
   * Marks
   * ----------------------------- */

  pushMark(mark: Mark) {
    this.marks.push(mark)
  }

  popMark() {
    if (this.marks.length === 0) throw new Error("popMark(): empty mark stack")
    this.marks.pop()
  }

  /* -----------------------------
   * Convenience
   * ----------------------------- */

  text(content: string, extraMarks: Mark[] = []) {
    const marks = extraMarks.length ? [...this.marks, ...extraMarks] : this.marks
    this.push(this.schema.text(content, marks))
  }
}

/**
 * Kernel runner options.
 * Strict means: any token that isn't handled triggers an error.
 */
export type RunOptions = {
  strict?: boolean
}

/**
 * Run the compiler kernel over a token stream using a Registry.
 *
 * Token protocol (minimum):
 * - token.type: string
 * - token.children?: token[]   (for container tokens like markdown-it "inline")
 */
export function run(tokens: MarkdownToken[], registry: Registry, ctx: CompileContext, opts: RunOptions = {}) {
  const strict = opts.strict ?? true
  for (const tok of tokens) {
    ctx.token = tok
    handleToken(tok, registry, ctx, strict)
  }
}

function handleToken(tok: MarkdownToken, registry: Registry, ctx: CompileContext, strict: boolean) {
  if (!tok || typeof tok.type !== "string") {
    if (strict) throw new Error(`Invalid token: ${String(tok)}`)
    return
  }

  // Container token (markdown-it uses "inline" with children)
  if (Array.isArray(tok.children)) {
    for (const child of tok.children) handleToken(child, registry, ctx, strict)
    return
  }

  const type: string = tok.type

  // text-like tokens (text, code_inline, etc.)
  const textHandler = registry.getText(type)
  if (textHandler) {
    textHandler.run(ctx, tok)
    return
  }

  // open/close tokens (markdown-it uses *_open / *_close for both nodes and marks)
  if (type.endsWith("_open")) {
    const nodeHandler = registry.getNode(type)
    if (nodeHandler?.open) {
      nodeHandler.open(ctx, tok)
      return
    }

    const markHandler = registry.getMark(type)
    if (markHandler?.open) {
      markHandler.open(ctx, tok)
      return
    }

    if (strict) throw new Error(`No handler for token: ${type}`)
    return
  }

  if (type.endsWith("_close")) {
    const nodeHandler = registry.getNode(type)
    if (nodeHandler?.close) {
      nodeHandler.close(ctx, tok)
      return
    }

    const markHandler = registry.getMark(type)
    if (markHandler?.close) {
      markHandler.close(ctx, tok)
      return
    }

    if (strict) throw new Error(`No handler for token: ${type}`)
    return
  }

  // Any other tokens must be explicitly registered as text-like or container-like.
  if (strict) throw new Error(`Unhandled token: ${type}`)
}