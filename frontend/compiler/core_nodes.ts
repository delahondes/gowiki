import { Registry } from "./registry"
import type { CompileContext } from "./kernel"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { addListNodes } from "prosemirror-schema-list"
import type { NodeSpec, MarkSpec } from "prosemirror-model"
import { highlightPlugin, isKnownLanguage } from "../highlight"

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
    return /^(\/(?!\/)|\.\/|\.\.\/)\S*$/.test(href)
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
  reg.registerNode("heading_open", {
    open(ctx) {
      const tag = ctx.token.tag
      const level = tag ? Number(tag.slice(1)) : NaN
      if (!level || level < 1 || level > 6) {
        throw new Error(`Invalid heading token: ${tag}`)
      }
      ctx.open(ctx.schema.nodes.heading.create({ level }))
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
        const body = recurse(image)
        if (size) {
          return `{image size=${size}}\n${body}\n\n`
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
      let out = ""
      node.content.forEach((child) => {
        out += recurse(child)
      })
      return "#".repeat(level) + " " + out + "\n\n"
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
