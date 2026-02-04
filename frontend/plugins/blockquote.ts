import { Registry } from "../compiler/registry"

export const blockquotePlugin = {
  register(reg: Registry) {
    // Markdown → PM
    reg.registerNode("blockquote_open", {
      open(ctx) {
        ctx.open(ctx.schema.nodes.blockquote.create())
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
  },
}