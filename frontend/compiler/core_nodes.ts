import { Registry } from "./registry"
import type { CompileContext } from "./kernel"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { addListNodes } from "prosemirror-schema-list"
import type { NodeSpec, MarkSpec } from "prosemirror-model"

/**
 * Core semantic nodes for the document language.
 *
 * This defines WHAT exists (paragraphs, lists, emphasis),
 * not HOW compilation works.
 */
export function registerCoreNodes(reg: Registry) {
  // 1) Pull full base schema from prosemirror-schema-basic
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

  // 2) Restore core Markdown → PM semantics
  registerParagraph(reg)
  registerEmphasis(reg)
  registerHeading(reg)
  registerLists(reg)

  // 3) PM → Markdown printers (unchanged)
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
      // Single newline becomes a hard break only in top-level paragraphs.
      if (ctx.currentNodeName() === "paragraph" && ctx.openDepth() === 1) {
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
      const start = ctx.token.attrGet?.("start")
      ctx.open(
        ctx.schema.nodes.ordered_list.create(
          start ? { order: Number(start) } : null
        )
      )
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

/* --------------------------------------------------
 * PM → Markdown printers
 * -------------------------------------------------- */

function registerMarkdownPrinters(reg: Registry) {
  // Text: handled centrally in pm_to_markdown.ts

  reg.registerPMNode("paragraph", {
    print(node, ctx, recurse) {
      if (node.content.size === 0) {
        return "\n"
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
        out += recurse(child)
      })
      return out + "\n"
    },
  })

  reg.registerPMNode("ordered_list", {
    print(node, ctx, recurse) {
      let index = node.attrs.order ?? 1
      let out = ""
      node.content.forEach((child) => {
        out += `${index++}. ${recurse(child).trimEnd()}\n`
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
      return "- " + out.trimEnd() + "\n"
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

  reg.registerPMNode("hard_break", {
    print() {
      return "\\n"
    },
  })
}
