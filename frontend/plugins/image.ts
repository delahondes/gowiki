import type { Plugin } from "../compiler/registry"

const imageProperties = [
  {
    name: "size",
    label: "Size",
    default: null,
    parse: (raw: string) => normalizeImageSize(raw),
    serialize: (value: string | null) => String(value ?? ""),
  },
]

function escapeAltText(raw: string): string {
  return String(raw ?? "").replace(/]/g, "\\]")
}

function normalizeImageSize(raw: string): string | null {
  const value = String(raw ?? "").trim().toLowerCase()
  if (!value) return null

  const pct = value.match(/^(\d+)%$/)
  if (pct) {
    const n = Number(pct[1])
    if (n > 0) return `${n}%`
    throw new Error("Image size percent must be > 0")
  }

  const px = value.match(/^(\d+)px$/)
  if (px) {
    const n = Number(px[1])
    if (n > 0) return `${n}px`
    throw new Error("Image size in px must be > 0")
  }

  const exact = value.match(/^(\d+)px;(\d+)px$/)
  if (exact) {
    const w = Number(exact[1])
    const h = Number(exact[2])
    if (w > 0 && h > 0) return `${w}px;${h}px`
    throw new Error("Image width and height must be > 0")
  }

  throw new Error(
    `Invalid image size "${raw}". Expected 50%, 200px, or 200px;100px.`
  )
}

function styleFromImageSize(size: string | null): string | null {
  if (!size) return null
  if (/^\d+%$/.test(size)) {
    return `width: ${size}; height: auto;`
  }
  if (/^\d+px$/.test(size)) {
    return `max-width: ${size}; max-height: ${size}; width: auto; height: auto;`
  }
  const exact = size.match(/^(\d+px);(\d+px)$/)
  if (exact) {
    return `width: ${exact[1]}; height: ${exact[2]};`
  }
  return null
}

function addStyleToDOMSpec(spec: any, style: string | null) {
  if (!style || !Array.isArray(spec)) return spec
  const [tag, maybeAttrs, ...rest] = spec
  const hasAttrs =
    maybeAttrs && typeof maybeAttrs === "object" && !Array.isArray(maybeAttrs)
  const attrs = hasAttrs ? maybeAttrs : {}
  const existing = attrs.style ? String(attrs.style) : ""
  const mergedStyle = existing
    ? `${existing}${existing.trim().endsWith(";") ? " " : "; "}${style}`
    : style
  const children = hasAttrs ? rest : [maybeAttrs, ...rest]
  return [tag, { ...attrs, style: mergedStyle }, ...children]
}

export const imagePlugin: Plugin = {
  register(reg) {
    reg.extendSchemaNode("image", spec => {
      const baseToDOM =
        typeof spec.toDOM === "function"
          ? spec.toDOM
          : (node: any) => ["img", node.attrs]
      return {
        ...spec,
        attrs: {
          ...(spec.attrs ?? {}),
          size: { default: null },
        },
        toDOM(node: any) {
          const domSpec = baseToDOM(node)
          const style = styleFromImageSize(node.attrs.size ?? null)
          return addStyleToDOMSpec(domSpec, style)
        },
      }
    })

    reg.registerDirective("image", {
      nodeType: "image",
      appliesTo: ["paragraph_open"],
      properties: imageProperties,
    })

    reg.registerText("image", {
      run(ctx, tok) {
        const src = tok.attrGet?.("src") ?? ""
        if (!src) return
        const title = tok.attrGet?.("title") ?? null
        const alt = tok.content ?? tok.attrGet?.("alt") ?? ""
        const directiveSize = ctx.findDirective("image")?.size ?? null
        const size = normalizeImageSize(directiveSize ?? "")
        ctx.push(ctx.schema.nodes.image.create({ src, title, alt, size }))
      },
    })

    reg.registerPMNode("image", {
      print(node) {
        const src = String(node.attrs.src ?? "")
        const alt = escapeAltText(node.attrs.alt ?? "")
        const title = node.attrs.title
        if (title) {
          return "![" + alt + "](" + src + ' "' + String(title).replace(/"/g, "\\\"") + '")'
        }
        return "![" + alt + "](" + src + ")"
      },
    })
  },
}
