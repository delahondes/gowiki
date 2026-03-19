import { Registry } from "./registry"
import type { CompileContext } from "./kernel"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { addListNodes } from "prosemirror-schema-list"
import type { NodeSpec, MarkSpec, Node as PMNode } from "prosemirror-model"
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

  // Extend paragraph: empty paragraphs get a clearfix class to clear floated images.
  nodes.paragraph = {
    ...nodes.paragraph,
    toDOM(node) {
      if (node.content.size === 0) {
        return ["p", { class: "gowiki-clear" }, 0]
      }
      return ["p", 0]
    },
  }

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
        } else if (/^mailto:/i.test(href)) {
          const prevClass = attrs.class ? String(attrs.class) : ""
          attrs.class = prevClass
            ? `${prevClass} gowiki-mailto-link`
            : "gowiki-mailto-link"
        }
        const children = hasAttrs ? rest : [maybeAttrs, ...rest]
        return [tag, attrs, ...children]
      },
    }
  }

  // Extra marks: underline, strikethrough, subscript, superscript
  marks.underline = {
    parseDOM: [{ tag: "u" }, { style: "text-decoration=underline" }],
    toDOM() { return ["u", 0] },
  } as MarkSpec

  marks.strikethrough = {
    parseDOM: [{ tag: "s" }, { tag: "del" }, { style: "text-decoration=line-through" }],
    toDOM() { return ["s", 0] },
  } as MarkSpec

  marks.subscript = {
    parseDOM: [{ tag: "sub" }],
    toDOM() { return ["sub", 0] },
    excludes: "superscript",
  } as MarkSpec

  marks.superscript = {
    parseDOM: [{ tag: "sup" }],
    toDOM() { return ["sup", 0] },
    excludes: "subscript",
  } as MarkSpec

  // code_expand: like code but allows template variable expansion inside.
  // Syntax: @`text with {{VAR}}`
  marks.code_expand = {
    parseDOM: [{ tag: "code.gowiki-code-expand" }],
    toDOM() { return ["code", { class: "gowiki-code-expand" }, 0] },
    excludes: "code",
  } as MarkSpec

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
  registerCodeExpand(reg)
  registerLinkStatus(reg)

  reg.registerStyle("clearfix", `.gowiki-clear { clear: both; }`)
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
 * Emphasis / strong / code / underline / strikethrough / sub / sup
 * -------------------------------------------------- */

function registerEmphasis(reg: Registry) {
  function isAllowedLinkTarget(href: string): boolean {
    if (/^https?:\/\//i.test(href)) return true
    if (/^mailto:/i.test(href)) return true
    // Allow #fragment (same-page anchor), and internal paths with optional fragment
    return /^(#\S+|(\/(?!\/)|\.\/|\.\.\/)\S*)$/.test(href)
  }

  // --- markdown-it plugin: remap _text_ from em to underline ---
  reg.registerMarkdownItPlugin((md: any) => {
    md.core.ruler.push("gowiki_underline", (state: any) => {
      function walk(tokens: any[]) {
        // Track which em_open/close pairs come from _ delimiters.
        // markdown-it sets markup = "_" on em tokens produced by underscore.
        for (const tok of tokens) {
          if ((tok.type === "em_open" || tok.type === "em_close") && tok.markup === "_") {
            tok.type = tok.type === "em_open" ? "underline_open" : "underline_close"
            tok.tag = "u"
          }
          if (tok.children) walk(tok.children)
        }
      }
      walk(state.tokens)
    })
  })

  // --- markdown-it plugin: ~~strikethrough~~ and ~subscript~ ---
  // We disable the built-in strikethrough (which uses a delimiter processor
  // that greedily consumes all ~ runs) and implement both ~~ and ~ ourselves
  // so they coexist cleanly — including when adjacent (e.g. ~~strike~~~sub~).
  reg.registerMarkdownItPlugin((md: any) => {
    md.disable("strikethrough")

    // ~subscript~ — registered first so single ~ is tried before ~~
    md.inline.ruler.push("gowiki_subscript", (state: any, silent: boolean) => {
      const src = state.src
      const start = state.pos
      if (src.charCodeAt(start) !== 0x7E) return false
      // Must be a lone ~ (not followed by another ~)
      if (start + 1 < state.posMax && src.charCodeAt(start + 1) === 0x7E) return false
      // Find closing single ~ (skip tilde runs of 2+)
      let end = -1
      for (let i = start + 1; i < state.posMax; i++) {
        if (src.charCodeAt(i) !== 0x7E) continue
        // Count tilde run length at this position
        let runEnd = i
        while (runEnd + 1 < state.posMax && src.charCodeAt(runEnd + 1) === 0x7E) runEnd++
        if (runEnd === i) { end = i; break } // single ~ → valid close
        i = runEnd // skip past the run
      }
      if (end === -1) return false
      const content = src.slice(start + 1, end)
      if (content.length === 0 || /[\s\n]/.test(content)) return false
      if (!silent) {
        if (state.pending) state.pushPending()
        const tokenO = state.push("subscript_open", "sub", 1)
        tokenO.markup = "~"
        const tokenT = state.push("text", "", 0)
        tokenT.content = content
        const tokenC = state.push("subscript_close", "sub", -1)
        tokenC.markup = "~"
      }
      state.pos = end + 1
      return true
    })

    // ~~strikethrough~~ — supports nested inline marks inside
    md.inline.ruler.push("gowiki_strikethrough", (state: any, silent: boolean) => {
      const src = state.src
      const start = state.pos
      if (src.charCodeAt(start) !== 0x7E || src.charCodeAt(start + 1) !== 0x7E) return false
      // Opening must be exactly ~~ (reject ~~~ at start)
      if (start + 2 < state.posMax && src.charCodeAt(start + 2) === 0x7E) return false
      // Also reject if preceded by ~ (tail of a longer run)
      if (start > 0 && src.charCodeAt(start - 1) === 0x7E) return false
      // Find closing ~~ (not preceded by ~; what follows is irrelevant)
      let closePos = -1
      for (let i = start + 2; i < state.posMax - 1; i++) {
        if (src.charCodeAt(i) !== 0x7E) continue
        if (src.charCodeAt(i + 1) !== 0x7E) continue // single ~, skip
        if (i > 0 && src.charCodeAt(i - 1) === 0x7E) continue // preceded by ~, skip
        closePos = i
        break
      }
      if (closePos === -1 || closePos <= start + 2) return false
      if (!silent) {
        if (state.pending) state.pushPending()
        const tokenO = state.push("s_open", "s", 1)
        tokenO.markup = "~~"
        // Recursively tokenize inner content (supports nested bold, italic, etc.)
        const savedMax = state.posMax
        state.pos = start + 2
        state.posMax = closePos
        state.md.inline.tokenize(state)
        state.posMax = savedMax
        const tokenC = state.push("s_close", "s", -1)
        tokenC.markup = "~~"
      }
      state.pos = closePos + 2
      return true
    })
  })

  // --- markdown-it plugin: ^superscript^ ---
  reg.registerMarkdownItPlugin((md: any) => {
    md.inline.ruler.push("gowiki_superscript", (state: any, silent: boolean) => {
      const src = state.src
      const start = state.pos
      if (src.charCodeAt(start) !== 0x5E /* ^ */) return false
      const end = src.indexOf("^", start + 1)
      if (end === -1 || end === start + 1) return false
      // No spaces or newlines inside superscript
      const content = src.slice(start + 1, end)
      if (/[\s\n]/.test(content)) return false
      if (!silent) {
        const tokenO = state.push("superscript_open", "sup", 1)
        tokenO.markup = "^"
        const tokenT = state.push("text", "", 0)
        tokenT.content = content
        const tokenC = state.push("superscript_close", "sup", -1)
        tokenC.markup = "^"
      }
      state.pos = end + 1
      return true
    })
  })

  // --- em (italic, *text*) ---
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

  // --- strong (bold, **text**) ---
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

  // --- underline (_text_) ---
  reg.registerMark("underline_open", {
    open(ctx) {
      ctx.pushMark(ctx.schema.marks.underline.create())
    },
  })

  reg.registerMark("underline_close", {
    close(ctx) {
      ctx.popMark()
    },
  })

  // --- strikethrough (~~text~~) ---
  reg.registerMark("s_open", {
    open(ctx) {
      ctx.pushMark(ctx.schema.marks.strikethrough.create())
    },
  })

  reg.registerMark("s_close", {
    close(ctx) {
      ctx.popMark()
    },
  })

  // --- subscript (~text~) ---
  reg.registerMark("subscript_open", {
    open(ctx) {
      ctx.pushMark(ctx.schema.marks.subscript.create())
    },
  })

  reg.registerMark("subscript_close", {
    close(ctx) {
      ctx.popMark()
    },
  })

  // --- superscript (^text^) ---
  reg.registerMark("superscript_open", {
    open(ctx) {
      ctx.pushMark(ctx.schema.marks.superscript.create())
    },
  })

  reg.registerMark("superscript_close", {
    close(ctx) {
      ctx.popMark()
    },
  })

  reg.registerText("code_inline", {
    run(ctx, tok) {
      ctx.text(tok.content ?? "", [ctx.schema.marks.code.create()])
    },
  })

  // @`...` — code with template variable expansion.
  reg.registerMarkdownItPlugin((md: any) => {
    md.inline.ruler.push("gowiki_code_expand", (state: any, silent: boolean) => {
      const src = state.src
      const pos = state.pos
      if (src.charCodeAt(pos) !== 0x40 /* @ */) return false
      if (pos + 1 >= state.posMax || src.charCodeAt(pos + 1) !== 0x60 /* ` */) return false
      // Find closing backtick
      let end = pos + 2
      while (end < state.posMax && src.charCodeAt(end) !== 0x60) end++
      if (end >= state.posMax) return false
      const content = src.slice(pos + 2, end)
      if (!content) return false
      if (silent) return true
      if (state.pending) state.pushPending()
      const token = state.push("code_expand", "", 0)
      token.content = content
      state.pos = end + 1
      return true
    })
  })

  reg.registerText("code_expand", {
    run(ctx, tok) {
      // Keep the raw content as text with code_expand mark.
      // Template variables inside ({{VAR}}) are resolved at render time
      // by the convertTemplateVarChildren pass, which runs on inline tokens.
      ctx.text(tok.content ?? "", [ctx.schema.marks.code_expand.create()])
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

  function imageDirectiveStr(image: PMNode): string {
    const size = image.attrs.size ?? null
    const version = image.attrs.version ?? null
    const align = image.attrs.align ?? null
    const wrap = image.attrs.wrap ?? null
    const caption = image.attrs.caption ?? null
    const label = image.attrs.label ?? null
    const dirParts: string[] = []
    if (size) dirParts.push(size.includes(";") ? `size="${size}"` : `size=${size}`)
    if (version) dirParts.push(`version=${version}`)
    if (align) dirParts.push(`align=${align}`)
    if (wrap) dirParts.push(`wrap=${wrap}`)
    if (caption) dirParts.push(`caption="${String(caption).replace(/"/g, '\\"')}"`)
    if (label) dirParts.push(`label=${label}`)
    if (dirParts.length > 0) {
      return `{image ${dirParts.join(" ")}}`
    }
    return ""
  }

  function printStandaloneImage(
    image: PMNode,
    recurse: (node: PMNode) => string
  ): string {
    const body = recurse(image)
    const dir = imageDirectiveStr(image)
    if (dir) {
      return `${dir}\n${body}\n\n`
    }
    return body + "\n\n"
  }

  function printInlineImage(
    image: PMNode,
    recurse: (node: PMNode) => string
  ): string {
    const body = recurse(image)
    const dir = imageDirectiveStr(image)
    if (dir) {
      return `${dir}${body}`
    }
    return body
  }

  reg.registerPMNode("paragraph", {
    print(node, ctx, recurse) {
      if (node.content.size === 0) {
        return "\n"
      }

      if (node.childCount === 1 && node.firstChild?.type.name === "image") {
        const image = node.firstChild
        return printStandaloneImage(image, recurse)
      }

      // Check if the paragraph contains an image with directive properties
      // alongside other content. Use inline directive syntax:
      // text {image size=500px}![alt](src) more text
      let hasSizedImage = false
      node.content.forEach((child) => {
        if (
          child.type.name === "image" &&
          (child.attrs.size || child.attrs.version || child.attrs.align || child.attrs.wrap || child.attrs.caption || child.attrs.label)
        ) {
          hasSizedImage = true
        }
      })

      if (hasSizedImage) {
        let out = ""
        node.content.forEach((child) => {
          if (child.type.name === "image" && imageDirectiveStr(child)) {
            out += printInlineImage(child, recurse)
          } else {
            out += recurse(child)
          }
        })
        return out + "\n\n"
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
        if (!item) return // skip empty list items (no content to serialize)
        const formatted = item.replace(/\n/g, "\n" + continuationIndent)
        out += marker + formatted + "\n"
      })
      // If all items were empty, return empty string to avoid orphan list markers.
      if (!out) return ""
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
        if (!item) return // skip empty list items
        const formatted = item.replace(/\n/g, "\n" + continuationIndent)
        out += marker + formatted + "\n"
      })
      if (!out) return ""
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
  reg.registerPMMark("code_expand", {
    open: "@`",
    close: "`",
  })

  // In the serializer, code_expand text should not be markdown-escaped
  // (same as regular code marks).
  // This is handled by the hasCodeMark check in pm_to_markdown.ts.
  reg.registerPMMark("underline", { open: "_", close: "_" })
  reg.registerPMMark("strikethrough", { open: "~~", close: "~~" })
  reg.registerPMMark("subscript", { open: "~", close: "~" })
  reg.registerPMMark("superscript", { open: "^", close: "^" })
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

/** Meta key used by include NodeViews to report their heading counts to the parent. */
export const INCLUDE_HEADING_META = "includeHeadingUpdate"

type HeadingNumberState = {
  decorations: DecorationSet
  includeCounts: Map<number, number[]>  // pos → final counter state of that include
}

function registerHeadingNumbers(reg: Registry) {
  reg.registerEditorPlugin(() => {
    return new PMPlugin({
      key: headingNumberKey,
      state: {
        init(_, state): HeadingNumberState {
          return { decorations: computeHeadingNumbers(state.doc), includeCounts: new Map() }
        },
        apply(tr, old: HeadingNumberState): HeadingNumberState {
          const meta = tr.getMeta(INCLUDE_HEADING_META)
          if (meta) {
            const newCounts = new Map(old.includeCounts)
            newCounts.set(meta.pos, meta.counters)
            return { decorations: computeHeadingNumbers(tr.doc, undefined, newCounts), includeCounts: newCounts }
          }
          if (tr.docChanged) {
            // Remap include positions after doc changes
            const newCounts = new Map<number, number[]>()
            for (const [pos, counts] of old.includeCounts) {
              newCounts.set(tr.mapping.map(pos), counts)
            }
            return { decorations: computeHeadingNumbers(tr.doc, undefined, newCounts), includeCounts: newCounts }
          }
          return old
        },
      },
      props: {
        decorations(state) {
          return (headingNumberKey.getState(state) as HeadingNumberState)?.decorations
        },
      },
    })
  })
}

export function computeHeadingNumbers(
  doc: any,
  initialCounters?: number[],
  includeCounts?: Map<number, number[]>,
): DecorationSet {
  const counters = initialCounters ? [...initialCounters] : [0, 0, 0, 0, 0, 0]
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
    // When encountering an include node, add its heading counts to our counters.
    if (node.type.name === "include" && includeCounts) {
      const counts = includeCounts.get(pos)
      if (counts) {
        for (let i = 0; i < 6; i++) counters[i] += counts[i]
      }
    }
    return false
  })

  return DecorationSet.create(doc, decorations)
}

/** Compute heading counter state at a given position in the doc. */
export function getHeadingCountersAt(doc: any, targetPos: number): number[] {
  const counters = [0, 0, 0, 0, 0, 0]
  let offset = 0
  for (let i = 0; i < doc.childCount; i++) {
    if (offset >= targetPos) break
    const child = doc.child(i)
    if (child.type.name === "heading") {
      const level: number = child.attrs.level
      if (child.attrs.numbered) {
        counters[level - 1]++
        for (let j = level; j < 6; j++) counters[j] = 0
      } else {
        for (let j = level; j < 6; j++) counters[j] = 0
      }
    }
    offset += child.nodeSize
  }
  return counters
}

export { headingNumberKey }

/* --------------------------------------------------
 * Internal link status decorations (exists / missing)
 * -------------------------------------------------- */

const linkStatusKey = new PluginKey("gowiki.linkStatus")

function resolveInternalHref(href: string, pageNamespace: string): string | null {
  if (!href || /^https?:\/\//i.test(href) || /^mailto:/i.test(href)) return null
  if (href.startsWith("#")) return null
  // Decode percent-encoded characters (match backend resolve.go)
  try { href = decodeURIComponent(href) } catch {}
  let resolved: string
  if (href.startsWith("/")) {
    resolved = href
  } else {
    // Relative: resolve against current page namespace
    const base = pageNamespace ? pageNamespace + "/" + href : href
    const parts = base.split("/")
    const out: string[] = []
    for (const p of parts) {
      if (p === "..") out.pop()
      else if (p !== "." && p !== "") out.push(p)
    }
    resolved = "/" + out.join("/")
  }
  // Strip trailing slash and query/hash
  resolved = resolved.split(/[?#]/)[0].replace(/\/+$/, "") || "/"
  return resolved
}

function collectLinkRanges(
  doc: any,
  pageNamespace: string
): { ranges: Array<{ from: number; to: number; path: string }>; paths: string[] } {
  const ranges: Array<{ from: number; to: number; path: string }> = []
  const pathSet = new Set<string>()

  doc.descendants((node: any, pos: number) => {
    if (!node.isInline) return
    for (const mark of node.marks) {
      if (mark.type.name === "link") {
        const href = String(mark.attrs.href ?? "")
        const path = resolveInternalHref(href, pageNamespace)
        if (path) {
          ranges.push({ from: pos, to: pos + node.nodeSize, path })
          pathSet.add(path)
        }
      }
    }
  })

  return { ranges, paths: Array.from(pathSet) }
}

function buildLinkDecorations(
  doc: any,
  pageNamespace: string,
  statusMap: Map<string, boolean>
): DecorationSet {
  if (statusMap.size === 0) return DecorationSet.empty
  const { ranges } = collectLinkRanges(doc, pageNamespace)
  const decorations: Decoration[] = []
  for (const { from, to, path } of ranges) {
    const exists = statusMap.get(path)
    if (exists === undefined) continue
    const cls = exists ? "gowiki-link-exists" : "gowiki-link-missing"
    decorations.push(Decoration.inline(from, to, { class: cls }))
  }
  return DecorationSet.create(doc, decorations)
}

/* --------------------------------------------------
 * code_expand: resolve {{VAR}} inside @`...` at render time
 * -------------------------------------------------- */

function registerCodeExpand(reg: Registry) {
  const codeExpandKey = new PluginKey("gowiki.codeExpand")

  function resolveVar(name: string, fallback: string): string {
    const ctx = (window as any).__gowikiGlobalVarContext?.()
    if (!ctx) return fallback || `{{${name}}}`
    const meta = ctx.pageMeta
    switch (name) {
      case "SERVER": return window.location.hostname
      case "ID": return ctx.pagePath || fallback
      case "PATH": return ctx.pageNamespace || fallback
      case "PAGE": return ctx.pageName || fallback
      case "TITLE": return meta?.title || fallback
      case "VERSION": return meta?.version != null ? String(meta.version) : fallback
      case "VERSIONDATE": {
        if (!meta?.updated_at) return fallback
        const d = new Date(meta.updated_at)
        return isNaN(d.getTime()) ? fallback : d.toISOString().slice(0, 10)
      }
      case "AUTHOR": return meta?.author || fallback
      case "CREATED": {
        if (!meta?.created_at) return fallback
        const d = new Date(meta.created_at)
        return isNaN(d.getTime()) ? fallback : d.toISOString().slice(0, 10)
      }
      default: return fallback || `{{${name}}}`
    }
  }

  reg.registerEditorPlugin(() => {
    return new PMPlugin({
      key: codeExpandKey,
      props: {
        decorations(state) {
          const decos: Decoration[] = []
          const varPattern = /\{\{([a-zA-Z_][a-zA-Z0-9_.]*)(?::([^}]*))?\}\}/g
          state.doc.descendants((node, pos) => {
            if (!node.isText) return
            if (!node.marks.some(m => m.type.name === "code_expand")) return
            const text = node.text ?? ""
            let m: RegExpExecArray | null
            while ((m = varPattern.exec(text)) !== null) {
              const resolved = resolveVar(m[1], m[2] ?? "")
              if (resolved === m[0]) continue
              const from = pos + m.index
              const to = from + m[0].length
              // Hide the {{VAR}} text
              decos.push(Decoration.inline(from, to, {
                class: "gowiki-code-expand-hidden",
              }))
              // Insert resolved value as a widget right before the hidden text
              decos.push(Decoration.widget(from, () => {
                const span = document.createElement("span")
                span.className = "gowiki-code-expand-resolved"
                span.textContent = resolved
                return span
              }, { side: -1 }))
            }
          })
          return decos.length > 0 ? DecorationSet.create(state.doc, decos) : DecorationSet.empty
        },
      },
    })
  })

  reg.registerStyle("code-expand", `
    .gowiki-code-expand-hidden {
      display: none;
    }
    .gowiki-code-expand-resolved {
      /* inherits code styling from parent <code> element */
    }
  `)
}

function registerLinkStatus(reg: Registry) {
  reg.registerEditorPlugin(() => {
    const loc = window.location.pathname
    const isNamespaceIndex = loc.endsWith("/")
    const curPage = loc === "/" ? "" : loc.replace(/^\/+|\/+$/g, "").replace(/\/index$/, "")
    // For namespace index pages (URL ends with /), the page IS the namespace.
    // For regular pages, the namespace is the parent directory.
    const pageNamespace = isNamespaceIndex
      ? curPage
      : curPage.includes("/")
        ? curPage.split("/").slice(0, -1).join("/")
        : ""

    const statusMap = new Map<string, boolean>()
    let timer: ReturnType<typeof setTimeout> | null = null
    let currentView: any = null

    async function checkLinks(doc: any) {
      const { paths } = collectLinkRanges(doc, pageNamespace)
      if (paths.length === 0) {
        if (statusMap.size > 0) {
          statusMap.clear()
          if (currentView) {
            currentView.dispatch(currentView.state.tr.setMeta(linkStatusKey, true))
          }
        }
        return
      }

      try {
        const resp = await fetch("/api/pages/check", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ paths }),
        })
        if (!resp.ok) return
        const data = await resp.json()
        const exists = data.exists as Record<string, boolean>
        statusMap.clear()
        for (const [p, v] of Object.entries(exists)) {
          statusMap.set(p, v)
        }
        if (currentView) {
          currentView.dispatch(currentView.state.tr.setMeta(linkStatusKey, true))
        }
      } catch {
        // Ignore network errors
      }
    }

    function scheduleCheck(doc: any, immediate?: boolean) {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => {
        timer = null
        checkLinks(doc)
      }, immediate ? 0 : 500)
    }

    return new PMPlugin({
      key: linkStatusKey,
      state: {
        init() {
          return DecorationSet.empty
        },
        apply(tr, old, _oldState, newState) {
          if (tr.getMeta(linkStatusKey)) {
            return buildLinkDecorations(newState.doc, pageNamespace, statusMap)
          }
          if (tr.docChanged) {
            return old.map(tr.mapping, tr.doc)
          }
          return old
        },
      },
      props: {
        decorations(state) {
          return linkStatusKey.getState(state)
        },
      },
      view(view) {
        currentView = view
        scheduleCheck(view.state.doc, true)
        return {
          update(view, prevState) {
            currentView = view
            if (view.state.doc !== prevState.doc) {
              scheduleCheck(view.state.doc)
            }
          },
          destroy() {
            currentView = null
            if (timer) clearTimeout(timer)
          },
        }
      },
    })
  })
}
