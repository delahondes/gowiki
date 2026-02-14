import type { Plugin } from "../compiler/registry"

function escapeAltText(raw: string): string {
  return String(raw ?? "").replace(/]/g, "\\]")
}

export const imagePlugin: Plugin = {
  register(reg) {
    reg.registerText("image", {
      run(ctx, tok) {
        const src = tok.attrGet?.("src") ?? ""
        if (!src) return
        const title = tok.attrGet?.("title") ?? null
        const alt = tok.content ?? tok.attrGet?.("alt") ?? ""
        ctx.push(ctx.schema.nodes.image.create({ src, title, alt }))
      },
    })

    reg.registerPMNode("image", {
      print(node) {
        const src = String(node.attrs.src ?? "")
        const alt = escapeAltText(node.attrs.alt ?? "")
        const title = node.attrs.title
        if (title) {
          return "![" + alt + "](" + src + " \"" + String(title).replace(/"/g, "\\\"") + "\")"
        }
        return "![" + alt + "](" + src + ")"
      },
    })
  },
}
