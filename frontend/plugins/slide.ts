import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import type { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin, NodePropertySpec, Registry } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"
import MarkdownIt from "markdown-it"

const VALID_THEMES = ["light", "dark"]
const VALID_RATIOS = ["16:9", "4:3"]

// ── Info string parsing ──

interface SlidesAttrs {
  title: string
  theme: string
  ratio: string
}

function parseSlidesInfo(info: string): SlidesAttrs {
  const attrs: SlidesAttrs = {
    title: "",
    theme: "light",
    ratio: "16:9",
  }

  // Extract quoted title first
  const titleMatch = info.match(/"([^"]*)"/)
  if (titleMatch) {
    attrs.title = titleMatch[1]
    info = info.slice(0, titleMatch.index) + info.slice(titleMatch.index! + titleMatch[0].length)
  }

  const tokens = info.trim().split(/\s+/).filter(Boolean)
  for (const tok of tokens) {
    const lower = tok.toLowerCase()
    if (lower === "dark" || lower === "light") {
      attrs.theme = lower
    } else if (lower === "16:9" || lower === "4:3") {
      attrs.ratio = lower
    }
  }

  return attrs
}

// ── Serialization ──

function serializeSlidesHeader(attrs: Record<string, any>): string {
  const parts: string[] = []

  // Canonical order: title, theme (if not light), ratio (if not 16:9)
  if (attrs.title) parts.push(`"${attrs.title}"`)
  if (attrs.theme && attrs.theme !== "light") parts.push(attrs.theme)
  if (attrs.ratio && attrs.ratio !== "16:9") parts.push(attrs.ratio)

  if (parts.length > 0) {
    return "```slides " + parts.join(" ")
  }
  return "```slides"
}

// ── Slide splitting ──

function splitSlides(data: string): string[] {
  const parts = data.split(/^---$/m)
  return parts
    .map(s => s.trim())
    .filter(s => s.length > 0)
}

function countSlides(data: string): number {
  if (!data.trim()) return 0
  return splitSlides(data).length
}

// ── Markdown rendering for slides ──

const slideMd = new MarkdownIt({
  html: false,
  breaks: true,
  linkify: false,
})

// ── Presentation engine ──

class PresentationEngine {
  private overlay: HTMLElement
  private slides: string[] // raw HTML per slide
  private current = 0
  private hideTimer: ReturnType<typeof setTimeout> | null = null
  private progressBar: HTMLElement
  private counter: HTMLElement

  constructor(slidesHtml: string[], theme: string, ratio: string) {
    this.slides = slidesHtml

    // Build overlay DOM
    this.overlay = document.createElement("div")
    this.overlay.className = `gowiki-slides-overlay theme-${theme}`

    const viewport = document.createElement("div")
    viewport.className = "gowiki-slides-viewport"
    this.overlay.appendChild(viewport)

    // Size viewport to aspect ratio
    const [rw, rh] = ratio === "4:3" ? [4, 3] : [16, 9]
    viewport.dataset.ratioW = String(rw)
    viewport.dataset.ratioH = String(rh)

    const slideEl = document.createElement("div")
    slideEl.className = "gowiki-slides-slide"
    viewport.appendChild(slideEl)

    this.progressBar = document.createElement("div")
    this.progressBar.className = "gowiki-slides-progress"
    this.overlay.appendChild(this.progressBar)

    this.counter = document.createElement("div")
    this.counter.className = "gowiki-slides-counter"
    this.overlay.appendChild(this.counter)

    this.overlay.addEventListener("click", (e) => {
      // Don't advance on button clicks, links, etc.
      if ((e.target as HTMLElement).closest("a, button")) return
      this.next()
    })

    this.handleKey = this.handleKey.bind(this)
    this.handleMouseMove = this.handleMouseMove.bind(this)
    this.handleFullscreenChange = this.handleFullscreenChange.bind(this)
    this.resizeViewport = this.resizeViewport.bind(this)
  }

  start() {
    document.body.appendChild(this.overlay)
    document.addEventListener("keydown", this.handleKey)
    document.addEventListener("mousemove", this.handleMouseMove)
    document.addEventListener("fullscreenchange", this.handleFullscreenChange)
    window.addEventListener("resize", this.resizeViewport)

    // Try fullscreen
    if (this.overlay.requestFullscreen) {
      this.overlay.requestFullscreen().catch(() => {
        // Fallback: stay as fixed overlay
      })
    }

    this.showSlide(0)
    this.resizeViewport()
    this.startHideTimer()
  }

  private handleKey(e: KeyboardEvent) {
    switch (e.key) {
      case "ArrowRight":
      case " ":
      case "Enter":
        e.preventDefault()
        this.next()
        break
      case "ArrowLeft":
      case "Backspace":
        e.preventDefault()
        this.prev()
        break
      case "Escape":
        e.preventDefault()
        this.exit()
        break
      case "Home":
        e.preventDefault()
        this.showSlide(0)
        break
      case "End":
        e.preventDefault()
        this.showSlide(this.slides.length - 1)
        break
    }
    this.showControls()
  }

  private handleMouseMove() {
    this.showControls()
  }

  private handleFullscreenChange() {
    if (!document.fullscreenElement && document.body.contains(this.overlay)) {
      this.cleanup()
    }
  }

  private next() {
    if (this.current < this.slides.length - 1) {
      this.showSlide(this.current + 1)
    }
  }

  private prev() {
    if (this.current > 0) {
      this.showSlide(this.current - 1)
    }
  }

  private showSlide(index: number) {
    this.current = index
    const slideEl = this.overlay.querySelector(".gowiki-slides-slide")!
    slideEl.innerHTML = this.slides[index]

    // Update progress
    const pct = this.slides.length > 1
      ? ((index + 1) / this.slides.length) * 100
      : 100
    this.progressBar.style.width = `${pct}%`
    this.counter.textContent = `${index + 1} / ${this.slides.length}`
  }

  private resizeViewport() {
    const viewport = this.overlay.querySelector(".gowiki-slides-viewport") as HTMLElement
    if (!viewport) return

    const rw = parseInt(viewport.dataset.ratioW || "16")
    const rh = parseInt(viewport.dataset.ratioH || "9")

    const vw = this.overlay.clientWidth
    const vh = this.overlay.clientHeight
    const margin = 0

    let width = vw - margin * 2
    let height = width * (rh / rw)
    if (height > vh - margin * 2) {
      height = vh - margin * 2
      width = height * (rw / rh)
    }

    viewport.style.width = `${width}px`
    viewport.style.height = `${height}px`
  }

  private showControls() {
    this.overlay.classList.remove("controls-hidden")
    this.startHideTimer()
  }

  private startHideTimer() {
    if (this.hideTimer) clearTimeout(this.hideTimer)
    this.hideTimer = setTimeout(() => {
      this.overlay.classList.add("controls-hidden")
    }, 2000)
  }

  exit() {
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {})
    }
    this.cleanup()
  }

  private cleanup() {
    document.removeEventListener("keydown", this.handleKey)
    document.removeEventListener("mousemove", this.handleMouseMove)
    document.removeEventListener("fullscreenchange", this.handleFullscreenChange)
    window.removeEventListener("resize", this.resizeViewport)
    if (this.hideTimer) clearTimeout(this.hideTimer)
    if (this.overlay.parentNode) {
      this.overlay.parentNode.removeChild(this.overlay)
    }
  }
}

function launchPresentation(data: string, theme: string, ratio: string) {
  const rawSlides = splitSlides(data)
  if (rawSlides.length === 0) return

  const slidesHtml = rawSlides.map(s => slideMd.render(s))
  const engine = new PresentationEngine(slidesHtml, theme, ratio)
  engine.start()
}

// ── NodeView ──

class SlidesNodeView {
  dom: HTMLElement
  private node: PMNode
  private presentBtn: HTMLButtonElement

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-slides"
    this.dom.contentEditable = "false"

    this.buildCard()

    this.presentBtn = this.dom.querySelector(".gowiki-slides-present-btn") as HTMLButtonElement
    this.presentBtn.addEventListener("click", (e) => {
      e.preventDefault()
      e.stopPropagation()
      launchPresentation(this.node.attrs.data, this.node.attrs.theme, this.node.attrs.ratio)
    })
  }

  private buildCard() {
    const count = countSlides(this.node.attrs.data)
    const title = this.node.attrs.title || "Untitled Presentation"
    const theme = this.node.attrs.theme || "light"
    const ratio = this.node.attrs.ratio || "16:9"

    this.dom.innerHTML = `
      <div class="gowiki-slides-icon">&#9654;</div>
      <div class="gowiki-slides-title">${this.escHtml(title)}</div>
      <div class="gowiki-slides-info">${count} slide${count !== 1 ? "s" : ""} &middot; ${theme} &middot; ${ratio}</div>
      <button class="gowiki-slides-present-btn">&#9654; Present</button>
    `
  }

  private escHtml(s: string): string {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;")
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.buildCard()

    // Re-attach click handler
    this.presentBtn = this.dom.querySelector(".gowiki-slides-present-btn") as HTMLButtonElement
    this.presentBtn.addEventListener("click", (e) => {
      e.preventDefault()
      e.stopPropagation()
      launchPresentation(this.node.attrs.data, this.node.attrs.theme, this.node.attrs.ratio)
    })

    return true
  }

  stopEvent(event: Event): boolean {
    if (event.type === "mousedown" || event.type === "mouseup" || event.type === "click") return false
    return true
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {}
}

// ── Property definitions ──

const slidesProperties: NodePropertySpec[] = [
  {
    name: "title",
    label: "Title",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
  },
  {
    name: "theme",
    label: "Theme",
    default: "light",
    parse: (raw: string) => {
      const t = raw.trim().toLowerCase()
      if (VALID_THEMES.includes(t)) return t
      throw new Error(`Invalid theme "${raw}". Use: ${VALID_THEMES.join(", ")}`)
    },
    serialize: (v: string | null) => String(v ?? "light"),
    options: VALID_THEMES.map(t => ({ value: t, label: t.charAt(0).toUpperCase() + t.slice(1) })),
  },
  {
    name: "ratio",
    label: "Ratio",
    default: "16:9",
    parse: (raw: string) => {
      const r = raw.trim()
      if (VALID_RATIOS.includes(r)) return r
      throw new Error(`Invalid ratio "${raw}". Use: ${VALID_RATIOS.join(", ")}`)
    },
    serialize: (v: string | null) => String(v ?? "16:9"),
    options: VALID_RATIOS.map(r => ({ value: r, label: r })),
  },
  {
    name: "data",
    label: "Slides",
    default: "",
    parse: (raw: string) => raw,
    serialize: (v: string | null) => String(v ?? ""),
    multiline: true,
    wide: true,
    helpText: "Separate slides with --- on its own line",
  },
]

// ── Styles ──

const slidesStyles = `
/* --- Placeholder card --- */
.gowiki-slides {
  margin: 0.5em 0;
  border: 1px solid #ddd;
  border-radius: 6px;
  padding: 1.2em;
  text-align: center;
  background: #f8f9fa;
  cursor: default;
}
#app.gowiki-editing .gowiki-slides {
  border: 1px dashed #ccc;
}
#app.gowiki-editing .gowiki-slides.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
.gowiki-slides-icon {
  font-size: 1.5em;
  margin-bottom: 0.2em;
}
.gowiki-slides-title {
  font-weight: 600;
  margin: 0.3em 0;
}
.gowiki-slides-info {
  color: #666;
  font-size: 0.85em;
  margin-bottom: 0.4em;
}
.gowiki-slides-present-btn {
  display: inline-block;
  margin-top: 0.4em;
  padding: 0.4em 1.4em;
  border: none;
  border-radius: 4px;
  background: #4e79a7;
  color: #fff;
  font-size: 0.9em;
  cursor: pointer;
}
.gowiki-slides-present-btn:hover {
  background: #3d6a96;
}

/* --- Fullscreen overlay --- */
.gowiki-slides-overlay {
  position: fixed;
  inset: 0;
  z-index: 10000;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}
.gowiki-slides-overlay.theme-light {
  background: #ffffff;
  color: #1a1a1a;
}
.gowiki-slides-overlay.theme-dark {
  background: #1a1a1a;
  color: #f0f0f0;
}

/* --- Viewport and slide --- */
.gowiki-slides-viewport {
  position: relative;
  overflow: hidden;
}
.gowiki-slides-slide {
  padding: 2em 3em;
  font-size: 1.5em;
  line-height: 1.5;
  height: 100%;
  box-sizing: border-box;
  overflow: hidden;
}
.gowiki-slides-slide h1 { font-size: 2em; margin: 0.3em 0; }
.gowiki-slides-slide h2 { font-size: 1.5em; margin: 0.3em 0; }
.gowiki-slides-slide h3 { font-size: 1.2em; margin: 0.3em 0; }
.gowiki-slides-slide ul, .gowiki-slides-slide ol {
  margin: 0.5em 0;
  padding-left: 1.2em;
}
.gowiki-slides-slide li { margin: 0.3em 0; }
.gowiki-slides-slide img { max-width: 80%; max-height: 60vh; }
.gowiki-slides-slide pre {
  background: #f4f4f4;
  padding: 0.8em;
  border-radius: 4px;
  font-size: 0.7em;
  overflow-x: auto;
}
.gowiki-slides-overlay.theme-dark .gowiki-slides-slide pre {
  background: #2d2d2d;
}
.gowiki-slides-slide code {
  background: #f0f0f0;
  padding: 0.1em 0.3em;
  border-radius: 3px;
  font-size: 0.9em;
}
.gowiki-slides-overlay.theme-dark .gowiki-slides-slide code {
  background: #333;
}
.gowiki-slides-slide pre code {
  background: none;
  padding: 0;
}
.gowiki-slides-slide table {
  border-collapse: collapse;
  margin: 0.5em auto;
}
.gowiki-slides-slide th, .gowiki-slides-slide td {
  border: 1px solid #ccc;
  padding: 0.3em 0.6em;
}
.gowiki-slides-overlay.theme-dark .gowiki-slides-slide th,
.gowiki-slides-overlay.theme-dark .gowiki-slides-slide td {
  border-color: #555;
}
.gowiki-slides-slide blockquote {
  border-left: 4px solid #ccc;
  margin: 0.5em 0;
  padding: 0.3em 1em;
  color: #555;
}
.gowiki-slides-overlay.theme-dark .gowiki-slides-slide blockquote {
  border-left-color: #555;
  color: #aaa;
}

/* --- Progress and counter --- */
.gowiki-slides-progress {
  position: absolute;
  bottom: 0;
  left: 0;
  height: 3px;
  background: #4e79a7;
  transition: width 0.15s ease;
}
.gowiki-slides-counter {
  position: absolute;
  bottom: 0.5em;
  right: 1em;
  font-size: 0.75em;
  opacity: 0.6;
  transition: opacity 0.3s;
}
.gowiki-slides-overlay.controls-hidden .gowiki-slides-counter {
  opacity: 0;
}
.gowiki-slides-overlay.controls-hidden .gowiki-slides-progress {
  opacity: 0;
}
`

// ── Plugin ──

export const slidePlugin: WikiPlugin = {
  register(reg: Registry) {
    // ── Schema ──
    reg.registerSchema({
      nodes: {
        slides: {
          group: "block",
          atom: true,
          attrs: {
            title: { default: "" },
            theme: { default: "light" },
            ratio: { default: "16:9" },
            data:  { default: "" },
          },
          toDOM(node: any) {
            return ["div", {
              class: "gowiki-slides",
              "data-slides-title": node.attrs.title,
              "data-slides-theme": node.attrs.theme,
              "data-slides-ratio": node.attrs.ratio,
              "data-slides-data": node.attrs.data,
            }, `Slides: ${node.attrs.title || "Untitled"}${countSlides(node.attrs.data) > 0 ? ` (${countSlides(node.attrs.data)} slides)` : ""}`]
          },
          parseDOM: [{
            tag: "div.gowiki-slides",
            getAttrs(dom: HTMLElement) {
              return {
                title: dom.getAttribute("data-slides-title") || "",
                theme: dom.getAttribute("data-slides-theme") || "light",
                ratio: dom.getAttribute("data-slides-ratio") || "16:9",
                data: dom.getAttribute("data-slides-data") || "",
              }
            },
          }],
        },
      },
    })

    // ── Properties ──
    reg.registerNodeProperties("slides", slidesProperties)

    // ── markdown-it block rule ──
    reg.registerMarkdownItPlugin((md: any) => {
      md.block.ruler.before("fence", "slides_fence", (state: any, startLine: number, endLine: number, silent: boolean) => {
        const startPos = state.bMarks[startLine] + state.tShift[startLine]
        const maxPos = state.eMarks[startLine]
        const firstLine = state.src.slice(startPos, maxPos)

        if (!firstLine.match(/^`{3,}slides(?:\s|$)/)) return false
        if (silent) return true

        const backtickCount = firstLine.match(/^(`+)/)![1].length
        const infoStr = firstLine.slice(backtickCount).replace(/^slides\s*/, "").trim()

        // Find closing fence
        let nextLine = startLine + 1
        let found = false
        for (; nextLine < endLine; nextLine++) {
          const lineStart = state.bMarks[nextLine] + state.tShift[nextLine]
          const lineEnd = state.eMarks[nextLine]
          const line = state.src.slice(lineStart, lineEnd)
          if (line.match(new RegExp("^`{" + backtickCount + ",}\\s*$"))) {
            found = true
            break
          }
        }
        if (!found) return false

        // Extract body
        const bodyLines: string[] = []
        for (let l = startLine + 1; l < nextLine; l++) {
          bodyLines.push(state.src.slice(state.bMarks[l], state.eMarks[l]))
        }
        const body = bodyLines.join("\n")

        // Parse info string
        const attrs = parseSlidesInfo(infoStr)

        // Emit single token
        const token = state.push("slides", "div", 0)
        token.block = true
        token.map = [startLine, nextLine + 1]
        token.meta = { ...attrs, data: body }

        state.line = nextLine + 1
        return true
      })
    })

    // ── Markdown → PM ──
    reg.registerText("slides", {
      run(ctx, tok) {
        const meta = tok.meta ?? {}
        ctx.push(ctx.schema.nodes.slides.create({
          title: meta.title ?? "",
          theme: meta.theme ?? "light",
          ratio: meta.ratio ?? "16:9",
          data: meta.data ?? "",
        }))
      },
    })

    // ── PM → Markdown ──
    reg.registerPMNode("slides", {
      print(node) {
        let out = serializeSlidesHeader(node.attrs) + "\n"
        const data = node.attrs.data || ""
        if (data) out += data + "\n"
        out += "```\n\n"
        return out
      },
    })

    // ── NodeView ──
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.slides"),
        props: {
          nodeViews: {
            slides(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new SlidesNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // ── Toolbar command ──
    reg.registerCommand("slides", "insert", (state, dispatch) => {
      const slidesType = reg.schema.nodes.slides
      if (!slidesType) return false
      if (dispatch) {
        const sampleData = "# Title Slide\n\n---\n\n# Slide 2\n\n---\n\n# Thank You"
        const node = slidesType.create({ data: sampleData })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === slidesType && insertedAt === null) {
              insertedAt = pos
              return false
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch { /* leave default selection */ }
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    // ── Styles ──
    reg.registerStyle("slides", slidesStyles)
  },
}
