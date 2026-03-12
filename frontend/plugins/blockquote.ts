import type { Plugin as WikiPlugin, NodePropertySpec } from "../compiler/registry"

const VALID_CLASSES = ["tip", "note", "important", "warning", "custom"]
const VALID_ICONS = ["lightbulb", "info", "warning", "important"]
const VALID_ALIGNS = ["left", "center", "right"]
const VALID_WRAPS = ["left", "right"]

function normalizeBlockquoteWidth(raw: string): string | null {
  const value = String(raw ?? "").trim().toLowerCase()
  if (!value) return null
  const pct = value.match(/^(\d+)%$/)
  if (pct) {
    const n = Number(pct[1])
    if (n > 0) return `${n}%`
    throw new Error("Width percent must be > 0")
  }
  const px = value.match(/^(\d+)px$/)
  if (px) {
    const n = Number(px[1])
    if (n > 0) return `${n}px`
    throw new Error("Width in px must be > 0")
  }
  throw new Error(`Invalid width "${raw}". Expected 80% or 400px.`)
}

function normalizeColor(raw: string): string | null {
  const trimmed = raw.trim()
  if (!trimmed) return null
  if (/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(trimmed)) return trimmed
  if (/^[a-zA-Z]+$/.test(trimmed)) return trimmed
  throw new Error(`Invalid color "${raw}". Use hex (#abc or #aabbcc) or a named color.`)
}

const isCustom = (attrs: Record<string, any>) => attrs.class === "custom"

const blockquoteProperties: NodePropertySpec[] = [
  {
    name: "class",
    label: "Class",
    default: null,
    parse: (raw: string) => {
      const trimmed = raw.trim().toLowerCase()
      if (!trimmed) return null
      if (!VALID_CLASSES.includes(trimmed)) {
        throw new Error(`Unknown class "${trimmed}". Use: ${VALID_CLASSES.join(", ")}`)
      }
      return trimmed
    },
    serialize: (value: string | null) => String(value ?? ""),
    options: [
      { value: "", label: "(none)" },
      { value: "tip", label: "Tip" },
      { value: "note", label: "Note" },
      { value: "important", label: "Important" },
      { value: "warning", label: "Warning" },
      { value: "custom", label: "Custom" },
    ],
  },
  {
    name: "color",
    label: "Color",
    default: null,
    parse: normalizeColor,
    serialize: (value: string | null) => String(value ?? ""),
    visible: isCustom,
  },
  {
    name: "icon",
    label: "Icon",
    default: null,
    parse: (raw: string) => {
      const trimmed = raw.trim().toLowerCase()
      if (!trimmed) return null
      if (!VALID_ICONS.includes(trimmed)) {
        throw new Error(`Unknown icon "${trimmed}". Use: ${VALID_ICONS.join(", ")}`)
      }
      return trimmed
    },
    serialize: (value: string | null) => String(value ?? ""),
    options: [
      { value: "", label: "(none)" },
      { value: "lightbulb", label: "Lightbulb" },
      { value: "info", label: "Info" },
      { value: "warning", label: "Warning" },
      { value: "important", label: "Important" },
    ],
    visible: isCustom,
  },
  {
    name: "width",
    label: "Width",
    default: null,
    parse: normalizeBlockquoteWidth,
    serialize: (value: string | null) => String(value ?? ""),
    visible: isCustom,
  },
  {
    name: "align",
    label: "Align",
    default: null,
    parse: (raw: string) => {
      const trimmed = raw.trim().toLowerCase()
      if (!trimmed) return null
      if (!VALID_ALIGNS.includes(trimmed)) {
        throw new Error(`Invalid alignment "${trimmed}". Use: ${VALID_ALIGNS.join(", ")}`)
      }
      return trimmed
    },
    serialize: (value: string | null) => String(value ?? ""),
    options: [
      { value: "", label: "(none)" },
      { value: "left", label: "Left" },
      { value: "center", label: "Center" },
      { value: "right", label: "Right" },
    ],
    visible: isCustom,
  },
  {
    name: "image-width",
    label: "Image width",
    default: null,
    parse: normalizeBlockquoteWidth,
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "wrap",
    label: "Wrap",
    default: null,
    parse: (raw: string) => {
      const trimmed = raw.trim().toLowerCase()
      if (!trimmed) return null
      if (!VALID_WRAPS.includes(trimmed)) {
        throw new Error(`Invalid wrap "${trimmed}". Use: ${VALID_WRAPS.join(", ")}`)
      }
      return trimmed
    },
    serialize: (value: string | null) => String(value ?? ""),
    options: [
      { value: "", label: "(none)" },
      { value: "left", label: "Left" },
      { value: "right", label: "Right" },
    ],
  },
]

const blockquoteStyles = `
.ProseMirror blockquote {
  border-left: 4px solid #ddd;
  margin: 0.5em 0;
  padding: 0.5em 1em;
}

.ProseMirror blockquote.gowiki-bq-tip::before,
.ProseMirror blockquote.gowiki-bq-note::before,
.ProseMirror blockquote.gowiki-bq-important::before,
.ProseMirror blockquote.gowiki-bq-warning::before {
  display: block;
  font-weight: 600;
  font-size: 0.85em;
  margin-bottom: 0.3em;
  padding-left: 1.5em;
  background-size: 1.1em 1.1em;
  background-repeat: no-repeat;
  background-position: left center;
}

.ProseMirror blockquote.gowiki-bq-tip {
  border-left-color: #10b981;
  background: #ecfdf5;
}
.ProseMirror blockquote.gowiki-bq-tip::before {
  content: 'Tip';
  color: #059669;
  background-image: url(/icons/lightbulb.svg);
}

.ProseMirror blockquote.gowiki-bq-note {
  border-left-color: #3b82f6;
  background: #eff6ff;
}
.ProseMirror blockquote.gowiki-bq-note::before {
  content: 'Note';
  color: #2563eb;
  background-image: url(/icons/info.svg);
}

.ProseMirror blockquote.gowiki-bq-important {
  border-left-color: #f59e0b;
  background: #fffbeb;
}
.ProseMirror blockquote.gowiki-bq-important::before {
  content: 'Important';
  color: #d97706;
  background-image: url(/icons/important.svg);
}

.ProseMirror blockquote.gowiki-bq-warning {
  border-left-color: #ef4444;
  background: #fef2f2;
}
.ProseMirror blockquote.gowiki-bq-warning::before {
  content: 'Warning';
  color: #dc2626;
  background-image: url(/icons/warning.svg);
}

/* Custom class */
.ProseMirror blockquote.gowiki-bq-custom {
  background: #f8f9fa;
}

.ProseMirror blockquote.gowiki-bq-custom.gowiki-bq-icon-lightbulb::before,
.ProseMirror blockquote.gowiki-bq-custom.gowiki-bq-icon-info::before,
.ProseMirror blockquote.gowiki-bq-custom.gowiki-bq-icon-warning::before,
.ProseMirror blockquote.gowiki-bq-custom.gowiki-bq-icon-important::before {
  content: '';
  display: block;
  width: 1.2em;
  height: 1.2em;
  margin-bottom: 0.3em;
  background-size: contain;
  background-repeat: no-repeat;
}

.ProseMirror blockquote.gowiki-bq-custom.gowiki-bq-icon-lightbulb::before {
  background-image: url(/icons/lightbulb.svg);
}
.ProseMirror blockquote.gowiki-bq-custom.gowiki-bq-icon-info::before {
  background-image: url(/icons/info.svg);
}
.ProseMirror blockquote.gowiki-bq-custom.gowiki-bq-icon-warning::before {
  background-image: url(/icons/warning.svg);
}
.ProseMirror blockquote.gowiki-bq-custom.gowiki-bq-icon-important::before {
  background-image: url(/icons/important.svg);
}

/* wrap: float blockquotes for column layouts */
.ProseMirror blockquote.gowiki-bq-wrap-left {
  float: left;
  margin-right: 1em;
  margin-bottom: 0.5em;
}

.ProseMirror blockquote.gowiki-bq-wrap-right {
  float: right;
  margin-left: 1em;
  margin-bottom: 0.5em;
}

/* clearfix: clear floats after a sequence of wrapped blockquotes */
.ProseMirror blockquote.gowiki-bq-wrap-left + :not(blockquote.gowiki-bq-wrap-left):not(blockquote.gowiki-bq-wrap-right),
.ProseMirror blockquote.gowiki-bq-wrap-right + :not(blockquote.gowiki-bq-wrap-left):not(blockquote.gowiki-bq-wrap-right) {
  clear: both;
}

/* image-width: percentages are relative to the blockquote width */
.ProseMirror blockquote.gowiki-bq-img-width .gowiki-image-wrapper {
  width: var(--gowiki-bq-img-width) !important;
  max-width: none !important;
  display: inline-block;
}

.ProseMirror blockquote.gowiki-bq-img-width .gowiki-image-wrapper img {
  width: 100% !important;
  height: auto !important;
}
`

export const blockquotePlugin: WikiPlugin = {
  register(reg) {
    // Extend blockquote schema node with class + custom attrs
    reg.extendSchemaNode("blockquote", spec => ({
      ...spec,
      attrs: {
        ...(spec.attrs ?? {}),
        class: { default: null },
        color: { default: null },
        icon: { default: null },
        width: { default: null },
        align: { default: null },
        "image-width": { default: null },
        wrap: { default: null },
      },
      toDOM(node: any) {
        const cls = node.attrs.class
        const domAttrs: Record<string, string> = {}
        const styles: string[] = []
        const classes: string[] = []

        if (cls === "custom") {
          classes.push("gowiki-bq-custom")
          if (node.attrs.icon) classes.push(`gowiki-bq-icon-${node.attrs.icon}`)
          if (node.attrs.color) {
            styles.push(`border-left-color: ${node.attrs.color}`)
            styles.push(`background: color-mix(in srgb, ${node.attrs.color} 10%, transparent)`)
          }
          if (node.attrs.width) styles.push(`width: ${node.attrs.width}`)
          if (node.attrs.align) styles.push(`text-align: ${node.attrs.align}`)
        } else if (cls) {
          classes.push(`gowiki-bq-${cls}`)
        }

        // image-width applies to any blockquote (% is relative to blockquote width).
        const imgWidth = node.attrs["image-width"]
        if (imgWidth) {
          classes.push("gowiki-bq-img-width")
          styles.push(`--gowiki-bq-img-width: ${imgWidth}`)
        }

        // wrap: float the blockquote left or right
        const wrap = node.attrs.wrap
        if (wrap) {
          classes.push(`gowiki-bq-wrap-${wrap}`)
        }

        if (classes.length > 0) domAttrs.class = classes.join(" ")
        if (styles.length > 0) domAttrs.style = styles.join("; ") + ";"

        return ["blockquote", domAttrs, 0]
      },
      parseDOM: [
        {
          tag: "blockquote",
          getAttrs(dom: HTMLElement) {
            const clsList = dom.className || ""
            const result: Record<string, any> = {}

            if (clsList.includes("gowiki-bq-custom")) {
              result.class = "custom"
              const iconMatch = clsList.match(/gowiki-bq-icon-(\S+)/)
              if (iconMatch) result.icon = iconMatch[1]
              const style = dom.style
              if (style.borderLeftColor) result.color = style.borderLeftColor
              if (style.width) result.width = style.width
              if (style.textAlign) result.align = style.textAlign
            } else {
              const match = clsList.match(/gowiki-bq-(\w+)/)
              if (match && match[1] !== "img" && match[1] !== "wrap") result.class = match[1]
            }

            // wrap from class
            const wrapMatch = clsList.match(/gowiki-bq-wrap-(left|right)/)
            if (wrapMatch) result.wrap = wrapMatch[1]

            // image-width from CSS variable
            if (clsList.includes("gowiki-bq-img-width")) {
              const imgWidth = dom.style.getPropertyValue("--gowiki-bq-img-width").trim()
              if (imgWidth) {
                result["image-width"] = imgWidth
              }
            }

            return result
          },
        },
      ],
    }))

    // Register directive for blockquote properties (class + custom attrs)
    reg.registerDirective("blockquote", {
      nodeType: "blockquote",
      appliesTo: ["blockquote_open"],
      properties: blockquoteProperties,
    })

    // Markdown → PM
    reg.registerNode("blockquote_open", {
      open(ctx) {
        const dirAttrs = ctx.token?.meta?.directives?.blockquote ?? null
        ctx.open(ctx.schema.nodes.blockquote.create(dirAttrs))
      },
    })

    reg.registerNode("blockquote_close", {
      close(ctx) {
        ctx.close()
      },
    })

    // PM → Markdown
    reg.registerPMNode("blockquote", {
      print(node, ctx, recurse) {
        let out = ""

        // Serialize directive with all non-default properties
        const parts: string[] = []
        for (const prop of blockquoteProperties) {
          const val = node.attrs[prop.name] ?? null
          const def = prop.default ?? null
          if (val !== def) {
            const rendered = prop.serialize
              ? prop.serialize(val)
              : String(val)
            parts.push(`${prop.name}=${rendered}`)
          }
        }
        if (parts.length > 0) {
          out += `{blockquote ${parts.join(" ")}}\n`
        }

        node.content.forEach(child => {
          const rendered = recurse(child).trimEnd()
          const lines = rendered.split("\n")
          for (const line of lines) {
            if (line.length > 0) {
              out += "> " + line + "\n"
            }
          }
        })
        return out + "\n"
      },
    })

    // Styles
    reg.registerStyle("blockquote", blockquoteStyles)
  },
}
