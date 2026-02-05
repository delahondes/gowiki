import { Registry } from "./registry"
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
      ctx.text(tok.content ?? "")
    },
  })

  reg.registerText("softbreak", {
    run(ctx) {
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
}
