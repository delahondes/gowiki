import type { Plugin as WikiPlugin, NodePropertySpec } from "../compiler/registry"

const VALID_CLASSES = ["tip", "note", "warning", "important"]

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
      { value: "warning", label: "Warning" },
      { value: "important", label: "Important" },
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
.ProseMirror blockquote.gowiki-bq-warning::before,
.ProseMirror blockquote.gowiki-bq-important::before {
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

.ProseMirror blockquote.gowiki-bq-warning {
  border-left-color: #ef4444;
  background: #fef2f2;
}
.ProseMirror blockquote.gowiki-bq-warning::before {
  content: 'Warning';
  color: #dc2626;
  background-image: url(/icons/warning.svg);
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
`

export const blockquotePlugin: WikiPlugin = {
  register(reg) {
    // Extend blockquote schema node with class attribute
    reg.extendSchemaNode("blockquote", spec => ({
      ...spec,
      attrs: { ...(spec.attrs ?? {}), class: { default: null } },
      toDOM(node: any) {
        const cls = node.attrs.class
        const attrs: Record<string, string> = {}
        if (cls) {
          attrs.class = `gowiki-bq-${cls}`
        }
        return ["blockquote", attrs, 0]
      },
      parseDOM: [
        {
          tag: "blockquote",
          getAttrs(dom: HTMLElement) {
            const clsList = dom.className || ""
            const match = clsList.match(/gowiki-bq-(\S+)/)
            if (match) return { class: match[1] }
            return {}
          },
        },
      ],
    }))

    // Register directive for blockquote properties (class attr + property panel)
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
        const cls = node.attrs.class ?? null
        if (cls) {
          const classProp = blockquoteProperties.find(p => p.name === "class")
          const rendered = classProp?.serialize
            ? classProp.serialize(cls)
            : String(cls)
          out += `{blockquote class=${rendered}}\n`
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
