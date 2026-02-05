import MarkdownIt from "markdown-it"
import { Node } from "prosemirror-model"

import { CompileContext, run } from "./kernel"
import { Registry } from "./registry"

type DirectiveToken = {
  name: string
  attrs: Record<string, string>
}

function parseDirective(line: string): DirectiveToken | null {
  const trimmed = line.trim()
  if (!trimmed.startsWith("{") || !trimmed.endsWith("}")) return null

  const inner = trimmed.slice(1, -1).trim()
  if (!inner) throw new Error("Empty directive")

  const parts = inner.split(/\s+/)
  const name = parts[0]
  if (!/^[a-zA-Z][\\w-]*$/.test(name)) {
    throw new Error(`Invalid directive name: ${name}`)
  }

  const attrs: Record<string, string> = {}
  for (const part of parts.slice(1)) {
    const eq = part.indexOf("=")
    if (eq <= 0 || eq === part.length - 1) {
      throw new Error(`Invalid directive attribute: ${part}`)
    }
    const key = part.slice(0, eq)
    const value = part.slice(eq + 1)
    attrs[key] = value
  }

  return { name, attrs }
}

function directivePlugin(md: MarkdownIt) {
  md.block.ruler.before("table", "directive", (state, startLine, endLine, silent) => {
    const start = state.bMarks[startLine] + state.tShift[startLine]
    const max = state.eMarks[startLine]
    const line = state.src.slice(start, max)

    const parsed = parseDirective(line)
    if (!parsed) return false
    if (silent) return true

    const token = state.push("directive", "", 0)
    token.block = true
    token.map = [startLine, startLine + 1]
    token.meta = parsed

    state.line = startLine + 1
    return true
  })
}

function applyDirectives(tokens: any[], registry: Registry, strict: boolean) {
  const out: any[] = []
  let pending: { name: string; attrs: Record<string, string> } | null = null
  let pendingSpec: ReturnType<Registry["getDirective"]> | null = null

  for (const token of tokens) {
    if (token.type === "directive") {
      const meta = token.meta as DirectiveToken | undefined
      if (!meta) {
        throw new Error("Invalid directive token")
      }
      if (pending && strict) {
        throw new Error(
          `Directive "${pending.name}" must apply to the next block`
        )
      }
      const spec = registry.getDirective(meta.name)
      if (!spec) {
        if (strict) throw new Error(`Unknown directive: ${meta.name}`)
        continue
      }
      pending = meta
      pendingSpec = spec
      continue
    }

    if (pending && token.block && token.nesting === 1) {
      if (!pendingSpec || !pendingSpec.appliesTo.includes(token.type)) {
        if (strict) {
          throw new Error(
            `Directive "${pending.name}" cannot apply to "${token.type}"`
          )
        }
        pending = null
        pendingSpec = null
      } else {
        const parsedAttrs: Record<string, string | null> = {}
        for (const [key, raw] of Object.entries(pending.attrs)) {
          const prop = pendingSpec.properties.find(p => p.name === key)
          if (!prop) {
            if (strict) {
              throw new Error(
                `Unknown property "${key}" for directive "${pending.name}"`
              )
            }
            continue
          }
          parsedAttrs[key] = prop.parse ? prop.parse(raw) : raw
        }
        token.meta = token.meta ?? {}
        token.meta.directives = token.meta.directives ?? {}
        token.meta.directives[pending.name] = parsedAttrs
        pending = null
        pendingSpec = null
      }
    }

    out.push(token)
  }

  if (pending && strict) {
    throw new Error(
      `Directive "${pending.name}" must be followed by a block`
    )
  }

  return out
}

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
  const md = new MarkdownIt({
    html: false,
    linkify: false,
    typographer: false,
  })

  for (const plugin of registry.getMarkdownItPlugins()) {
    plugin(md)
  }
  md.use(directivePlugin)
  const tokens = applyDirectives(md.parse(markdown, {}), registry, true)



  // 3. Run kernel
  const ctx = new CompileContext(schema)
  run(tokens, registry, ctx, { strict: true })

  // 4. Build final document
  return schema.nodes.doc.create(null, ctx.output)
}
