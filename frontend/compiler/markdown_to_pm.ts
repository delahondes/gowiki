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
  // {{...}} is template variable syntax, not a directive
  if (trimmed.startsWith("{{")) return null

  const inner = trimmed.slice(1, -1).trim()
  if (!inner) throw new Error("Empty directive")

  const parts = inner.split(/\s+/)
  const name = parts[0].replace(/\r/g, "")
  if (!/^[A-Za-z][A-Za-z0-9_-]*$/.test(name)) {
    throw new Error(`Invalid directive name: ${name}`)
  }

  const attrs: Record<string, string> = {}
  // Parse attributes, supporting quoted values: key="value with spaces or ="
  const attrStr = inner.slice(parts[0].length).trim()
  const attrRe = /([A-Za-z_][A-Za-z0-9_-]*)=(?:"([^"]*)"|'([^']*)'|(\S+))/g
  let m: RegExpExecArray | null
  while ((m = attrRe.exec(attrStr)) !== null) {
    const key = m[1]
    const value = m[2] ?? m[3] ?? m[4]
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
  let pending: {
    name: string
    attrs: Record<string, string>
    lineCount: number
  } | null = null
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

      // Check for self-contained directives first
      const selfContained = registry.getSelfContainedDirective(meta.name)
      if (selfContained) {
        const parsedAttrs: Record<string, string | null> = {}
        for (const [key, raw] of Object.entries(meta.attrs)) {
          const prop = selfContained.properties.find(p => p.name === key)
          if (!prop) {
            if (strict) {
              throw new Error(
                `Unknown property "${key}" for directive "${meta.name}"`
              )
            }
            continue
          }
          parsedAttrs[key] = prop.parse ? prop.parse(raw) : raw
        }
        out.push({
          type: selfContained.tokenType,
          tag: "",
          nesting: 0,
          block: true,
          level: 0,
          map: token.map,
          meta: { directiveName: meta.name, attrs: parsedAttrs },
        })
        continue
      }

      const spec = registry.getDirective(meta.name)
      if (!spec) {
        if (strict) throw new Error(`Unknown directive: ${meta.name}`)
        continue
      }
      const directiveLineCount = Array.isArray(token.map)
        ? Math.max(1, token.map[1] - token.map[0])
        : 1
      pending = { ...meta, lineCount: directiveLineCount }
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
        token.meta.directiveLineCount = pending.lineCount
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

function defaultLinkTextForTarget(target: string): string {
  if (/^https?:\/\//i.test(target)) return target
  const pathOnly = target.split(/[?#]/)[0]
  const clean = pathOnly.replace(/\/+$/, "")
  const parts = clean.split("/").filter(Boolean).filter(p => p !== "." && p !== "..")
  return parts[parts.length - 1] ?? "index"
}

function normalizeInlineLinkTokens(children: any[]) {
  const out: any[] = []
  for (let i = 0; i < children.length; i++) {
    const tok = children[i]
    if (tok?.type === "link_open") {
      const close = children[i + 1]
      if (close?.type === "link_close") {
        const href = tok.attrGet?.("href") ?? ""
        tok.meta = { ...(tok.meta ?? {}), autoText: true }
        out.push(tok)
        out.push({
          type: "text",
          content: defaultLinkTextForTarget(href),
        })
        out.push(close)
        i += 1
        continue
      }
    }
    out.push(tok)
  }
  return out
}

function normalizeEmptyLinkLabels(tokens: any[]) {
  for (const tok of tokens) {
    if (Array.isArray(tok.children)) {
      tok.children = normalizeInlineLinkTokens(tok.children)
    }
  }
  return tokens
}

/**
 * Detect whether a link href points to a media file (non-.md file with extension,
 * not an external URL).
 */
function isMediaLinkHref(href: string): boolean {
  if (!href) return false
  if (/^https?:\/\//i.test(href)) return false
  // Strip query/hash for extension check
  const pathOnly = href.split(/[?#]/)[0]
  const ext = pathOnly.match(/\.([a-zA-Z0-9]+)$/)
  if (!ext) return false
  // .md links are page links, not media
  if (ext[1].toLowerCase() === "md") return false
  return true
}

/**
 * Convert link_open + text + link_close sequences into synthetic medialink tokens
 * when the href points to a media file.
 */
function convertMediaLinkChildren(children: any[]): any[] {
  const out: any[] = []
  for (let i = 0; i < children.length; i++) {
    const tok = children[i]
    if (tok?.type === "link_open") {
      const href = tok.attrGet?.("href") ?? ""
      if (isMediaLinkHref(href)) {
        // Collect text content between link_open and link_close
        const textParts: string[] = []
        let j = i + 1
        while (j < children.length && children[j]?.type !== "link_close") {
          if (children[j]?.type === "text") {
            textParts.push(children[j].content ?? "")
          }
          j++
        }
        const label = textParts.join("")
        const title = tok.attrGet?.("title") ?? null
        const autoText = tok.meta?.autoText ?? false

        out.push({
          type: "medialink",
          content: label,
          meta: { href, label, title, autoText },
          attrGet(name: string) {
            if (name === "href") return href
            if (name === "title") return title
            return null
          },
        })
        // Skip past the link_close
        i = j
        continue
      }
    }
    out.push(tok)
  }
  return out
}

function convertMediaLinkTokens(tokens: any[]) {
  for (const tok of tokens) {
    if (Array.isArray(tok.children)) {
      tok.children = convertMediaLinkChildren(tok.children)
    }
  }
  return tokens
}

const templateVarRe = /\{\{([a-zA-Z_][a-zA-Z0-9_.]*)\}\}/

function convertTemplateVarChildren(children: any[]): any[] {
  const out: any[] = []
  for (const tok of children) {
    if (tok?.type !== "text" || !tok.content || !templateVarRe.test(tok.content)) {
      out.push(tok)
      continue
    }
    // Split text around {{varname}} occurrences
    const re = /\{\{([a-zA-Z_][a-zA-Z0-9_.]*)\}\}/g
    let lastIndex = 0
    let m: RegExpExecArray | null
    while ((m = re.exec(tok.content)) !== null) {
      if (m.index > lastIndex) {
        out.push({ type: "text", content: tok.content.slice(lastIndex, m.index) })
      }
      out.push({
        type: "template_var",
        content: "",
        meta: { name: m[1] },
      })
      lastIndex = re.lastIndex
    }
    if (lastIndex < tok.content.length) {
      out.push({ type: "text", content: tok.content.slice(lastIndex) })
    }
  }
  return out
}

function convertTemplateVarTokens(tokens: any[]) {
  for (const tok of tokens) {
    if (Array.isArray(tok.children)) {
      tok.children = convertTemplateVarChildren(tok.children)
    }
  }
  return tokens
}

function isTopLevelBlockStart(token: any) {
  if (!token?.block || token.level !== 0) return false
  return token.nesting === 1 || token.nesting === 0
}

function findBlockEndIndex(tokens: any[], startIndex: number) {
  const start = tokens[startIndex]
  if (!start || start.nesting !== 1) return startIndex

  let depth = 1
  for (let i = startIndex + 1; i < tokens.length; i++) {
    depth += tokens[i].nesting
    if (depth === 0) return i
  }
  return startIndex
}

function semanticBlockEnd(tokens: any[], startIndex: number, endIndex: number, fallbackEnd: number) {
  let inlineEnd: number | null = null
  for (let i = startIndex; i <= endIndex; i++) {
    const tok = tokens[i]
    if (tok.type === "inline" && Array.isArray(tok.map)) {
      inlineEnd = inlineEnd === null ? tok.map[1] : Math.max(inlineEnd, tok.map[1])
    }
  }
  if (inlineEnd !== null) return inlineEnd

  let leafEnd: number | null = null
  for (let i = startIndex; i <= endIndex; i++) {
    const tok = tokens[i]
    if (tok.nesting === 0 && Array.isArray(tok.map)) {
      leafEnd = leafEnd === null ? tok.map[1] : Math.max(leafEnd, tok.map[1])
    }
  }
  return leafEnd ?? fallbackEnd
}

function injectExtraBlankParagraphs(tokens: any[]) {
  const out: any[] = []
  let lastSemanticEnd: number | null = null

  for (let i = 0; i < tokens.length; i++) {
    const token = tokens[i]
    if (isTopLevelBlockStart(token) && Array.isArray(token.map)) {
      const startLine = token.map[0]
      const blockEndIndex = findBlockEndIndex(tokens, i)
      const semanticEnd = semanticBlockEnd(tokens, i, blockEndIndex, token.map[1])
      const directiveLineCount =
        token.meta && typeof token.meta.directiveLineCount === "number"
          ? token.meta.directiveLineCount
          : 0

      if (lastSemanticEnd !== null) {
        const effectiveGap = Math.max(
          0,
          startLine - lastSemanticEnd - directiveLineCount
        )
        const extraBlankParagraphs = Math.max(0, effectiveGap - 1)
        for (let j = 0; j < extraBlankParagraphs; j++) {
          out.push({ type: "paragraph_open", tag: "p", nesting: 1 })
          out.push({ type: "paragraph_close", tag: "p", nesting: -1 })
        }
      }

      for (let j = i; j <= blockEndIndex; j++) {
        out.push(tokens[j])
      }
      lastSemanticEnd = semanticEnd
      i = blockEndIndex
      continue
    }

    out.push(token)
    if (lastSemanticEnd === null && Array.isArray(token.map)) {
      lastSemanticEnd = token.map[1]
    }
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
  const tokens = injectExtraBlankParagraphs(
    convertTemplateVarTokens(
      convertMediaLinkTokens(
        normalizeEmptyLinkLabels(
          applyDirectives(md.parse(markdown, {}), registry, true)
        )
      )
    )
  )



  // 3. Run kernel
  const ctx = new CompileContext(schema)
  run(tokens, registry, ctx, { strict: true })

  // 4. Build final document
  return schema.nodes.doc.create(null, ctx.output)
}
