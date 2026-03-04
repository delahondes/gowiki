import { Registry } from "./registry"
import type { CompileContext } from "./kernel"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { addListNodes } from "prosemirror-schema-list"
import type { NodeSpec, MarkSpec } from "prosemirror-model"
import { Plugin as PMPlugin, PluginKey } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"
import { highlightPlugin, isKnownLanguage } from "../highlight"
import { slugify } from "./slugify"

/**
 * Core semantic nodes for the document language.
 *
 * This defines WHAT exists (paragraphs, lists, emphasis),
 * not HOW compilation works.
 */
export function registerCoreNodes(reg: Registry) {
  const baseNodes = addListNodes(
    basicSchema.spec.nodes,
    "paragraph block*",
    "block"
  )

  const nodes: Record<string, NodeSpec> = {}
  baseNodes.forEach((name: string, spec: NodeSpec) => {
    nodes[name] = spec
  })

  // Extend heading to carry a `numbered` attribute.
  nodes.heading = {
    ...nodes.heading,
    attrs: { level: { default: 1 }, numbered: { default: false } },
    toDOM(node) {
      return ["h" + node.attrs.level, node.attrs.numbered ? { class: "gowiki-heading-numbered" } : {}, 0]
    },
  }

  const marks: Record<string, MarkSpec> = {}
  basicSchema.spec.marks.forEach((name: string, spec: MarkSpec) => {
    marks[name] = spec
  })

  // Extend link mark to preserve whether label was explicit or auto-derived.
  if (marks.link) {
    const baseLink = marks.link
    const baseToDOM =
      typeof baseLink.toDOM === "function"
        ? baseLink.toDOM
        : (node: any, _inline: boolean) => ["a", node.attrs, 0]
    marks.link = {
      ...baseLink,
      attrs: {
        ...(baseLink.attrs ?? {}),
        autoText: { default: false },
      },
      toDOM(node) {
        const spec = baseToDOM(node, true)
        if (!Array.isArray(spec)) return spec
        const [tag, maybeAttrs, ...rest] = spec
        const hasAttrs =
          maybeAttrs &&
          typeof maybeAttrs === "object" &&
          !Array.isArray(maybeAttrs)
        const attrs = hasAttrs ? { ...maybeAttrs } : {}
        const href = String(node.attrs.href ?? "")
        if (/^https?:\/\//i.test(href)) {
          attrs.target = "_blank"
          attrs.rel = "noopener noreferrer"
          const prevClass = attrs.class ? String(attrs.class) : ""
          attrs.class = prevClass
            ? `${prevClass} gowiki-external-link`
            : "gowiki-external-link"
        }
        const children = hasAttrs ? rest : [maybeAttrs, ...rest]
        return [tag, attrs, ...children]
      },
    }
  }

  reg.registerSchema({ nodes, marks })

  registerParagraph(reg)
  registerEmphasis(reg)
  registerHeading(reg)
  registerLists(reg)
  registerCodeBlocks(reg)
  registerHorizontalRule(reg)

  registerMarkdownPrinters(reg)
  registerHeadingAnchors(reg)
  registerHeadingNumbers(reg)
}

/* --------------------------------------------------
 * Paragraphs + text
 * -------------------------------------------------- */

function registerParagraph(reg: Registry) {
  function writeTextWithExplicitBreaks(ctx: CompileContext, content: string) {
    const chunks = content.split("\\n")
    for (let i = 0; i < chunks.length; i++) {
      if (chunks[i].length > 0) {
        ctx.text(chunks[i])
      }
      if (i < chunks.length - 1) {
        ctx.push(ctx.schema.nodes.hard_break.create())
      }
    }
  }

  reg.registerNode("paragraph_open", {
    open(ctx) {
      ctx.open(ctx.schema.nodes.paragraph.create())
    },
  })

  reg.registerNode("paragraph_close", {
    close(ctx) {
      ctx.close()
    },
  })

  reg.registerText("text", {
    run(ctx, tok) {
      writeTextWithExplicitBreaks(ctx, tok.content ?? "")
    },
  })

  reg.registerText("softbreak", {
    run(ctx) {
      const inParagraph = ctx.currentNodeName() === "paragraph"
      const topLevelParagraph = ctx.openDepth() === 1
      const inListItem = ctx.hasOpenNode("list_item")
      const inTableCell =
        ctx.hasOpenNode("table_cell") || ctx.hasOpenNode("table_header")

      // In this dialect, plain newlines are hard breaks in top-level and list paragraphs.
      if (inParagraph && !inTableCell && (topLevelParagraph || inListItem)) {
        ctx.push(ctx.schema.nodes.hard_break.create())
        return
      }
      ctx.text(" ")
    },
  })

  reg.registerText("hardbreak", {
    run(ctx) {
      ctx.push(ctx.schema.nodes.hard_break.create())
    },
  })
}

/* --------------------------------------------------
 * Emphasis / strong / code
 * -------------------------------------------------- */

function registerEmphasis(reg: Registry) {
  function isAllowedLinkTarget(href: string): boolean {
    if (/^https?:\/\//i.test(href)) return true
    // Allow #fragment (same-page anchor), and internal paths with optional fragment
    return /^(#\S+|(\/(?!\/)|\.\/|\.\.\/)\S*)$/.test(href)
  }

  reg.registerMark("em_open", {
    open(ctx) {
      ctx.pushMark(ctx.schema.marks.em.create())
    },
  })

  reg.registerMark("em_close", {
    close(ctx) {
      ctx.popMark()
    },
  })

  reg.registerMark("strong_open", {
    open(ctx) {
      ctx.pushMark(ctx.schema.marks.strong.create())
    },
  })

  reg.registerMark("strong_close", {
    close(ctx) {
      ctx.popMark()
    },
  })

  reg.registerText("code_inline", {
    run(ctx, tok) {
      ctx.text(tok.content ?? "", [ctx.schema.marks.code.create()])
    },
  })

  reg.registerMark("link_open", {
    open(ctx, tok) {
      const href = tok.attrGet?.("href") ?? ""
      if (!isAllowedLinkTarget(href)) {
        throw new Error(
          `Invalid link target "${href}". Expected http(s) URL or internal path starting with '/', './', or '../'.`
        )
      }
      const title = tok.attrGet?.("title") ?? null
      const autoText = Boolean(tok.meta?.autoText)
      ctx.pushMark(
        ctx.schema.marks.link.create({
          href,
          title,
          autoText,
        })
      )
    },
  })

  reg.registerMark("link_close", {
    close(ctx) {
      ctx.popMark()
    },
  })
}

/* --------------------------------------------------
 * Headings
 * -------------------------------------------------- */

function registerHeading(reg: Registry) {
  // markdown-it core rule: detect `1. ` prefix in heading inline content
  // and transfer it to the heading_open token as meta.numbered.
  reg.registerMarkdownItPlugin((md: any) => {
    md.core.ruler.push("gowiki_numbered_heading", (state: any) => {
      for (let i = 0; i < state.tokens.length; i++) {
        if (state.tokens[i].type === "heading_open" && i + 1 < state.tokens.length) {
          const inline = state.tokens[i + 1]
          if (inline.type === "inline" && inline.content.startsWith("1. ")) {
            state.tokens[i].meta = { ...(state.tokens[i].meta || {}), numbered: true }
            inline.content = inline.content.slice(3)
            if (inline.children?.length > 0 && inline.children[0].type === "text") {
              const t = inline.children[0]
              if (t.content.startsWith("1. ")) {
                t.content = t.content.slice(3)
              }
            }
          }
        }
      }
    })
  })

  reg.registerNode("heading_open", {
    open(ctx) {
      const tag = ctx.token.tag
      const level = tag ? Number(tag.slice(1)) : NaN
      if (!level || level < 1 || level > 6) {
        throw new Error(`Invalid heading token: ${tag}`)
      }
      const numbered = !!ctx.token.meta?.numbered
      ctx.open(ctx.schema.nodes.heading.create({ level, numbered }))
    },
  })

  reg.registerNode("heading_close", {
    close(ctx) {
      ctx.close()
    },
  })
}

/* --------------------------------------------------
 * Lists
 * -------------------------------------------------- */

function registerLists(reg: Registry) {
  reg.registerNode("bullet_list_open", {
    open(ctx) {
      ctx.open(ctx.schema.nodes.bullet_list.create())
    },
  })

  reg.registerNode("bullet_list_close", {
    close(ctx) {
      ctx.close()
    },
  })

  reg.registerNode("ordered_list_open", {
    open(ctx) {
      ctx.open(ctx.schema.nodes.ordered_list.create())
    },
  })

  reg.registerNode("ordered_list_close", {
    close(ctx) {
      ctx.close()
    },
  })

  reg.registerNode("list_item_open", {
    open(ctx) {
      ctx.open(ctx.schema.nodes.list_item.create())
    },
  })

  reg.registerNode("list_item_close", {
    close(ctx) {
      ctx.close()
    },
  })
}

function registerCodeBlocks(reg: Registry) {
  // Extend the code_block node spec to carry a language attribute.
  reg.extendSchemaNode("code_block", (spec: any) => ({
    ...spec,
    attrs: { ...(spec.attrs ?? {}), language: { default: "" } },
    toDOM(node: any) {
      return [
        "pre",
        ["code", { class: node.attrs.language ? "language-" + node.attrs.language : "" }, 0],
      ]
    },
    parseDOM: [
      {
        tag: "pre",
        getAttrs(dom: HTMLElement) {
          const code = dom.querySelector("code")
          const cls = code?.className?.match(/language-(\S+)/)
          return { language: cls ? cls[1] : "" }
        },
        contentElement: "code",
      },
    ],
  }))

  // Register language as a node property — uses the proven property panel system.
  reg.registerNodeProperties("code_block", [
    {
      name: "language",
      label: "Language:",
      default: "",
      parse(raw: string) {
        const lang = raw.trim().toLowerCase()
        if (lang === "") return ""
        if (!isKnownLanguage(lang)) {
          throw new Error(`Unknown language "${lang}"`)
        }
        return lang
      },
    },
  ])

  function pushCodeBlock(ctx: CompileContext, raw: string, language = "") {
    let content = String(raw ?? "")
    // markdown-it fence content usually ends with one trailing newline.
    if (content.endsWith("\n")) {
      content = content.slice(0, -1)
    }
    const textNode = content.length > 0 ? ctx.schema.text(content) : null
    const block = textNode
      ? ctx.schema.nodes.code_block.create({ language }, [textNode])
      : ctx.schema.nodes.code_block.create({ language })
    ctx.push(block)
  }

  // Triple-backtick fenced blocks from markdown-it.
  reg.registerText("fence", {
    run(ctx, tok) {
      pushCodeBlock(ctx, tok.content ?? "", (tok.info ?? "").trim())
    },
  })

  // Also accept indented code blocks when imported.
  reg.registerText("code_block", {
    run(ctx, tok) {
      pushCodeBlock(ctx, tok.content ?? "")
    },
  })

  // Syntax highlighting decorations in the editor.
  reg.registerEditorPlugin(() => highlightPlugin())
}

function registerHorizontalRule(reg: Registry) {
  reg.registerText("hr", {
    run(ctx) {
      ctx.push(ctx.schema.nodes.horizontal_rule.create())
    },
  })
}

/* --------------------------------------------------
 * PM → Markdown printers
 * -------------------------------------------------- */

function registerMarkdownPrinters(reg: Registry) {
  // Text: handled centrally in pm_to_markdown.ts

  function renderListItemText(
    itemNode: any,
    recurse: (node: any) => string
  ): string {
    const parts: string[] = []

    itemNode.content.forEach((child: any) => {
      let rendered = recurse(child).trimEnd()
      if (!rendered) return

      // Keep visual line breaks in list paragraphs as real newlines.
      if (child.type?.name === "paragraph") {
        rendered = rendered.replace(/\\n/g, "\n")
      }

      parts.push(rendered)
    })

    return parts.join("\n")
  }

  reg.registerPMNode("paragraph", {
    print(node, ctx, recurse) {
      if (node.content.size === 0) {
        return "\n"
      }

      if (node.childCount === 1 && node.firstChild?.type.name === "image") {
        const image = node.firstChild
        const size = image.attrs.size ?? null
        const version = image.attrs.version ?? null
        const body = recurse(image)
        const dirParts: string[] = []
        if (size) dirParts.push(`size=${size}`)
        if (version) dirParts.push(`version=${version}`)
        if (dirParts.length > 0) {
          return `{image ${dirParts.join(" ")}}\n${body}\n\n`
        }
        return body + "\n\n"
      }

      let out = ""
      node.content.forEach((child) => {
        out += recurse(child)
      })
      return out + "\n\n"
    },
  })

  reg.registerPMNode("heading", {
    print(node, ctx, recurse) {
      const level = node.attrs.level
      const numbered = node.attrs.numbered
      let out = ""
      node.content.forEach((child) => {
        out += recurse(child)
      })
      return "#".repeat(level) + " " + (numbered ? "1. " : "") + out + "\n\n"
    },
  })

  reg.registerPMNode("bullet_list", {
    print(node, ctx, recurse) {
      let out = ""
      node.content.forEach((child) => {
        const marker = "- "
        const continuationIndent = " ".repeat(marker.length)
        const item = renderListItemText(child, recurse)
        const formatted = item.replace(/\n/g, "\n" + continuationIndent)
        out += marker + formatted + "\n"
      })
      return out + "\n"
    },
  })

  reg.registerPMNode("ordered_list", {
    print(node, ctx, recurse) {
      let index = node.attrs.order ?? 1
      let out = ""
      node.content.forEach((child) => {
        const marker = String(index++) + ". "
        const continuationIndent = " ".repeat(marker.length)
        const item = renderListItemText(child, recurse)
        const formatted = item.replace(/\n/g, "\n" + continuationIndent)
        out += marker + formatted + "\n"
      })
      return out + "\n"
    },
  })

  reg.registerPMNode("list_item", {
    print(node, ctx, recurse) {
      let out = ""
      node.content.forEach((child) => {
        out += recurse(child)
      })
      return out
    },
  })

  reg.registerPMMark("em", { open: "*", close: "*" })
  reg.registerPMMark("strong", { open: "**", close: "**" })
  reg.registerPMMark("code", { open: "`", close: "`" })
  reg.registerPMMark("link", {
    open: mark => (mark.attrs.autoText ? "" : "["),
    close: mark => {
      const href = mark.attrs.href ?? ""
      const title = mark.attrs.title
      if (mark.attrs.autoText) {
        if (title) {
          return `[](${href} "${String(title).replace(/"/g, '\\"')}")`
        }
        return `[](${href})`
      }
      if (title) {
        return `](${href} "${String(title).replace(/"/g, '\\"')}")`
      }
      return `](${href})`
    },
  })

  reg.registerPMNode("code_block", {
    print(node) {
      const lang = node.attrs.language ?? ""
      const text = node.textContent ?? ""
      const body = text.endsWith("\n") ? text : text + "\n"
      return "```" + lang + "\n" + body + "```\n\n"
    },
  })

  reg.registerPMNode("hard_break", {
    print() {
      return "\\n"
    },
  })

  reg.registerPMNode("horizontal_rule", {
    print() {
      return "---\n\n"
    },
  })
}

/* --------------------------------------------------
 * Heading anchor IDs via decorations
 * -------------------------------------------------- */

const headingAnchorKey = new PluginKey("gowiki.headingAnchors")

function registerHeadingAnchors(reg: Registry) {
  reg.registerEditorPlugin(() => {
    return new PMPlugin({
      key: headingAnchorKey,
      state: {
        init(_, state) {
          return buildHeadingDecorations(state.doc)
        },
        apply(tr, old) {
          if (tr.docChanged) {
            return buildHeadingDecorations(tr.doc)
          }
          return old
        },
      },
      props: {
        decorations(state) {
          return headingAnchorKey.getState(state)
        },
      },
    })
  })
}

function buildHeadingDecorations(doc: any): DecorationSet {
  const decorations: Decoration[] = []
  const slugCounts = new Map<string, number>()

  doc.descendants((node: any, pos: number) => {
    if (node.type.name === "heading") {
      const text = node.textContent
      const base = slugify(text)
      const count = slugCounts.get(base) ?? 0
      slugCounts.set(base, count + 1)
      const id = count === 0 ? base : `${base}-${count}`
      decorations.push(Decoration.node(pos, pos + node.nodeSize, { id }))
      return false
    }
  })

  return DecorationSet.create(doc, decorations)
}

/* --------------------------------------------------
 * Heading numbering decorations
 * -------------------------------------------------- */

const headingNumberKey = new PluginKey("gowiki.headingNumbers")

function registerHeadingNumbers(reg: Registry) {
  reg.registerEditorPlugin(() => {
    return new PMPlugin({
      key: headingNumberKey,
      state: {
        init(_, state) {
          return computeHeadingNumbers(state.doc)
        },
        apply(tr, old) {
          if (tr.docChanged) {
            return computeHeadingNumbers(tr.doc)
          }
          return old
        },
      },
      props: {
        decorations(state) {
          return headingNumberKey.getState(state)
        },
      },
    })
  })
}

function computeHeadingNumbers(doc: any): DecorationSet {
  const counters = [0, 0, 0, 0, 0, 0]
  const decorations: Decoration[] = []

  doc.descendants((node: any, pos: number) => {
    if (node.type.name === "heading") {
      const level: number = node.attrs.level
      if (node.attrs.numbered) {
        counters[level - 1]++
        for (let i = level; i < 6; i++) counters[i] = 0
        // Build label: collect all active ancestor counters up to this level
        const parts: number[] = []
        for (let i = 0; i < level; i++) {
          if (counters[i] > 0) parts.push(counters[i])
        }
        const label = parts.join(".") + "."
        decorations.push(
          Decoration.node(pos, pos + node.nodeSize, {
            "data-heading-number": label,
          })
        )
      } else {
        // Non-numbered heading resets deeper counters
        for (let i = level; i < 6; i++) counters[i] = 0
      }
      return false
    }
  })

  return DecorationSet.create(doc, decorations)
}
