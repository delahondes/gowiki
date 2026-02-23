import { NodeSelection, Plugin as PMPlugin, PluginKey } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import type { Node as PMNode } from "prosemirror-model"
import type { Plugin as WikiPlugin } from "../compiler/registry"

const imageProperties = [
  {
    name: "size",
    label: "Size",
    default: null,
    parse: (raw: string) => normalizeImageSize(raw),
    serialize: (value: string | null) => String(value ?? ""),
  },
]

const imageStyles = `
.gowiki-image-wrapper {
  position: relative;
  display: inline-block;
  max-width: 100%;
  line-height: 0;
}

.gowiki-image-wrapper img {
  display: block;
  max-width: 100%;
  height: auto;
}

.gowiki-image-wrapper.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}

.gowiki-image-resize-handle {
  position: absolute;
  bottom: -3px;
  right: -3px;
  width: 10px;
  height: 10px;
  border: 1px solid #3f5f8f;
  background: #dbe8ff;
  border-radius: 2px;
  cursor: nwse-resize;
  z-index: 5;
}
`

const MIN_DRAG_SIZE_PX = 16
const MAX_DRAG_SIZE_PX = 4096

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

class ImageNodeView {
  dom: HTMLElement
  private imgEl: HTMLImageElement
  private handle: HTMLElement | null = null
  private node: PMNode
  private outerView: EditorView
  private getPos: () => number | undefined

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.outerView = view
    this.getPos = getPos

    this.dom = document.createElement("span")
    this.dom.className = "gowiki-image-wrapper"

    this.imgEl = document.createElement("img")
    this.applyAttrs()
    this.dom.appendChild(this.imgEl)
  }

  private applyAttrs() {
    this.imgEl.src = this.node.attrs.src ?? ""
    this.imgEl.alt = this.node.attrs.alt ?? ""
    if (this.node.attrs.title) {
      this.imgEl.title = this.node.attrs.title
    } else {
      this.imgEl.removeAttribute("title")
    }
    const sizeStyle = styleFromImageSize(this.node.attrs.size ?? null)
    this.imgEl.style.cssText = sizeStyle ?? ""
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.applyAttrs()
    return true
  }

  selectNode() {
    this.dom.classList.add("ProseMirror-selectednode")
    if (this.outerView.editable) {
      this.showHandle()
    }
  }

  deselectNode() {
    this.dom.classList.remove("ProseMirror-selectednode")
    this.hideHandle()
  }

  private showHandle() {
    if (this.handle) return
    this.handle = document.createElement("span")
    this.handle.className = "gowiki-image-resize-handle"
    this.handle.title = "Drag to resize (Shift keeps ratio)"
    this.handle.addEventListener("mousedown", this.onResizeStart)
    this.dom.appendChild(this.handle)
  }

  private hideHandle() {
    if (this.handle) {
      this.handle.remove()
      this.handle = null
    }
  }

  private onResizeStart = (event: MouseEvent) => {
    event.preventDefault()
    event.stopPropagation()

    const rect = this.imgEl.getBoundingClientRect()
    const startX = event.clientX
    const startY = event.clientY
    const startWidth = Math.max(1, rect.width)
    const startHeight = Math.max(1, rect.height)
    const ratio = startWidth / startHeight

    const oldCursor = document.body.style.cursor
    const oldSelect = document.body.style.userSelect
    document.body.style.cursor = "nwse-resize"
    document.body.style.userSelect = "none"

    const onMove = (moveEvent: MouseEvent) => {
      let width = Math.round(startWidth + moveEvent.clientX - startX)
      let height = Math.round(startHeight + moveEvent.clientY - startY)

      if (moveEvent.shiftKey) {
        const byWidth = Math.max(MIN_DRAG_SIZE_PX, width)
        const byHeight = Math.max(MIN_DRAG_SIZE_PX, height)
        const widthDrivenHeight = Math.round(byWidth / ratio)
        const heightDrivenWidth = Math.round(byHeight * ratio)
        const widthDelta = Math.abs(byWidth - startWidth)
        const heightDelta = Math.abs(byHeight - startHeight)
        if (widthDelta >= heightDelta) {
          width = byWidth
          height = widthDrivenHeight
        } else {
          width = heightDrivenWidth
          height = byHeight
        }
      }

      width = Math.max(MIN_DRAG_SIZE_PX, Math.min(MAX_DRAG_SIZE_PX, width))
      height = Math.max(MIN_DRAG_SIZE_PX, Math.min(MAX_DRAG_SIZE_PX, height))
      const size = `${width}px;${height}px`

      const pos = this.getPos()
      if (pos === undefined) return
      const state = this.outerView.state
      const node = state.doc.nodeAt(pos)
      if (!node || node.type.name !== "image") return

      let tr = state.tr.setNodeMarkup(pos, node.type, {
        ...node.attrs,
        size,
      })
      tr = tr.setSelection(NodeSelection.create(tr.doc, pos))
      this.outerView.dispatch(tr)
    }

    const onUp = () => {
      window.removeEventListener("mousemove", onMove)
      window.removeEventListener("mouseup", onUp)
      document.body.style.cursor = oldCursor
      document.body.style.userSelect = oldSelect
    }

    window.addEventListener("mousemove", onMove)
    window.addEventListener("mouseup", onUp)
  }

  stopEvent(event: Event): boolean {
    // Let the resize handle's mousedown through to our handler,
    // block other events on the handle from reaching ProseMirror
    if (event.target === this.handle) {
      return event.type === "mousedown"
    }
    return false
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {
    this.hideHandle()
  }
}

export const imagePlugin: WikiPlugin = {
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

    // NodeView-based image rendering with resize handle
    reg.registerEditorPlugin(() => {
      return new PMPlugin({
        key: new PluginKey("gowiki.imageNodeView"),
        props: {
          nodeViews: {
            image(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new ImageNodeView(node, view, getPos)
            },
          },
        },
      })
    })
    reg.registerStyle("image-resize", imageStyles)
  },
}
