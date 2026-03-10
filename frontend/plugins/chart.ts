import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import type { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin, NodePropertySpec, Registry } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

const VALID_TYPES = ["pie", "doughnut", "bar", "hbar", "line", "radar", "polar"]

const DEFAULT_PALETTE = [
  "#4e79a7", "#f28e2b", "#e15759", "#76b7b2",
  "#59a14f", "#edc948", "#b07aa1", "#ff9da7",
  "#9c755f", "#bab0ac", "#d37295", "#a0cbe8",
]

const DEFAULT_WIDTH = 400
const DEFAULT_HEIGHT = 250

// ── Info string parsing ──

interface ChartAttrs {
  type: string
  width: number
  height: number
  title: string
  legend: string
  values: string
  align: string
  colors: string
}

function parseChartInfo(info: string): ChartAttrs {
  const attrs: ChartAttrs = {
    type: "pie",
    width: DEFAULT_WIDTH,
    height: DEFAULT_HEIGHT,
    title: "",
    legend: "true",
    values: "false",
    align: "",
    colors: "",
  }

  // Extract quoted title first (may contain spaces)
  const titleMatch = info.match(/"([^"]*)"/)
  if (titleMatch) {
    attrs.title = titleMatch[1]
    info = info.slice(0, titleMatch.index) + info.slice(titleMatch.index! + titleMatch[0].length)
  }

  const tokens = info.trim().split(/\s+/).filter(Boolean)
  const colorList: string[] = []

  for (const tok of tokens) {
    const lower = tok.toLowerCase()
    if (VALID_TYPES.includes(lower)) {
      attrs.type = lower
    } else if (/^\d+x\d+$/.test(tok)) {
      const [w, h] = tok.split("x").map(Number)
      if (w > 0 && h > 0) { attrs.width = w; attrs.height = h }
    } else if (lower === "nolegend") {
      attrs.legend = "false"
    } else if (lower === "legend") {
      attrs.legend = "true"
    } else if (lower === "values") {
      attrs.values = "true"
    } else if (lower === "left" || lower === "center" || lower === "right") {
      attrs.align = lower === "center" ? "" : lower
    } else if (/^#[0-9a-fA-F]{3,8}$/.test(tok)) {
      colorList.push(tok)
    }
  }

  if (colorList.length > 0) attrs.colors = colorList.join(",")
  return attrs
}

// ── Serialization helpers ──

function serializeChartHeader(attrs: Record<string, any>): string {
  const parts: string[] = []

  // Type always emitted
  parts.push(attrs.type || "pie")

  // Canonical order: size, title, legend/nolegend, values, align, colors
  const w = attrs.width ?? DEFAULT_WIDTH
  const h = attrs.height ?? DEFAULT_HEIGHT
  if (w !== DEFAULT_WIDTH || h !== DEFAULT_HEIGHT) parts.push(`${w}x${h}`)

  if (attrs.title) parts.push(`"${attrs.title}"`)

  if (attrs.legend === "false") parts.push("nolegend")

  if (attrs.values === "true") parts.push("values")

  if (attrs.align && attrs.align !== "center") parts.push(attrs.align)

  if (attrs.colors) {
    const colorList = String(attrs.colors).split(",").map((c: string) => c.trim()).filter(Boolean)
    for (const c of colorList) parts.push(c)
  }

  return "```chart " + parts.join(" ")
}

// ── Data parsing ──

function parseChartData(raw: string): { labels: string[]; values: number[] } {
  const labels: string[] = []
  const values: number[] = []
  for (const line of raw.split("\n")) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith("#")) continue
    const m = trimmed.match(/^(.+?)\s*=\s*(-?\d+(?:\.\d+)?)$/)
    if (m) {
      labels.push(m[1].trim())
      values.push(parseFloat(m[2]))
    }
  }
  return { labels, values }
}

// ── Property definitions ──

const chartProperties: NodePropertySpec[] = [
  {
    name: "type",
    label: "Type",
    default: "pie",
    parse: (raw: string) => {
      const t = raw.trim().toLowerCase()
      if (VALID_TYPES.includes(t)) return t
      throw new Error(`Invalid chart type "${raw}". Use: ${VALID_TYPES.join(", ")}`)
    },
    serialize: (v: string | null) => String(v ?? "pie"),
    options: VALID_TYPES.map(t => ({ value: t, label: t.charAt(0).toUpperCase() + t.slice(1) })),
  },
  {
    name: "width",
    label: "Width",
    default: String(DEFAULT_WIDTH),
    parse: (raw: string) => {
      const n = parseInt(raw, 10)
      if (isNaN(n) || n <= 0) throw new Error("Width must be a positive number")
      return String(n)
    },
    serialize: (v: string | null) => String(v ?? DEFAULT_WIDTH),
  },
  {
    name: "height",
    label: "Height",
    default: String(DEFAULT_HEIGHT),
    parse: (raw: string) => {
      const n = parseInt(raw, 10)
      if (isNaN(n) || n <= 0) throw new Error("Height must be a positive number")
      return String(n)
    },
    serialize: (v: string | null) => String(v ?? DEFAULT_HEIGHT),
  },
  {
    name: "title",
    label: "Title",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
  },
  {
    name: "legend",
    label: "Legend",
    default: "true",
    parse: (raw: string) => raw.trim().toLowerCase() === "false" ? "false" : "true",
    serialize: (v: string | null) => String(v ?? "true"),
    options: [{ value: "true", label: "Show" }, { value: "false", label: "Hide" }],
  },
  {
    name: "values",
    label: "Values",
    default: "false",
    parse: (raw: string) => raw.trim().toLowerCase() === "true" ? "true" : "false",
    serialize: (v: string | null) => String(v ?? "false"),
    options: [{ value: "false", label: "Hide" }, { value: "true", label: "Show" }],
  },
  {
    name: "align",
    label: "Align",
    default: "",
    parse: (raw: string) => {
      const v = raw.trim().toLowerCase()
      if (v === "center" || v === "") return ""
      if (v === "left" || v === "right") return v
      throw new Error(`Invalid alignment "${raw}". Use: left, center, right`)
    },
    serialize: (v: string | null) => String(v ?? ""),
    options: [
      { value: "", label: "Center" },
      { value: "left", label: "Left" },
      { value: "right", label: "Right" },
    ],
  },
  {
    name: "colors",
    label: "Colors",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: "Comma-separated hex colors (#rgb or #rrggbb)",
    wide: true,
  },
  {
    name: "data",
    label: "Data",
    default: "",
    parse: (raw: string) => raw,
    serialize: (v: string | null) => String(v ?? ""),
    multiline: true,
    wide: true,
    helpText: "One entry per line: Label = Value",
  },
]

// ── NodeView ──

let chartJSPromise: Promise<any> | null = null

function loadChartJS(): Promise<any> {
  if (!chartJSPromise) {
    chartJSPromise = import("chart.js/auto")
  }
  return chartJSPromise
}

class ChartNodeView {
  dom: HTMLElement
  private node: PMNode
  private canvas: HTMLCanvasElement
  private chartInstance: any = null

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-chart"
    this.dom.contentEditable = "false"
    this.applyAlign(node.attrs.align)

    this.canvas = document.createElement("canvas")
    this.canvas.width = node.attrs.width
    this.canvas.height = node.attrs.height
    this.dom.appendChild(this.canvas)

    this.renderChart()
  }

  private applyAlign(align: string) {
    this.dom.style.width = "fit-content"
    if (align === "left") {
      this.dom.style.marginLeft = "0"
      this.dom.style.marginRight = "auto"
    } else if (align === "right") {
      this.dom.style.marginLeft = "auto"
      this.dom.style.marginRight = "0"
    } else {
      this.dom.style.marginLeft = "auto"
      this.dom.style.marginRight = "auto"
    }
  }

  private async renderChart() {
    const { Chart } = await loadChartJS()

    if (this.chartInstance) {
      this.chartInstance.destroy()
      this.chartInstance = null
    }

    const { labels, values } = parseChartData(this.node.attrs.data || "")
    if (labels.length === 0) {
      this.canvas.width = 200
      this.canvas.height = 40
      const ctx2d = this.canvas.getContext("2d")
      if (ctx2d) {
        ctx2d.fillStyle = "#999"
        ctx2d.font = "13px sans-serif"
        ctx2d.fillText("No chart data", 10, 25)
      }
      return
    }

    const attrs = this.node.attrs
    const chartType = attrs.type === "hbar" ? "bar" : (attrs.type === "polar" ? "polarArea" : attrs.type)

    let palette: string[]
    if (attrs.colors) {
      palette = String(attrs.colors).split(",").map((c: string) => c.trim()).filter(Boolean)
    } else {
      palette = [...DEFAULT_PALETTE]
    }
    const bgColors = labels.map((_: string, i: number) => palette[i % palette.length])

    const plugins: any[] = []
    if (attrs.values === "true") {
      plugins.push({
        id: "gowiki-values",
        afterDatasetsDraw(chart: any) {
          const ctx2d = chart.ctx
          ctx2d.save()
          ctx2d.font = "bold 11px sans-serif"
          ctx2d.textAlign = "center"
          ctx2d.textBaseline = "middle"
          ctx2d.fillStyle = "#333"
          const meta = chart.getDatasetMeta(0)
          for (let i = 0; i < meta.data.length; i++) {
            const el = meta.data[i]
            const val = chart.data.datasets[0].data[i]
            const { x, y } = el.tooltipPosition()
            ctx2d.fillText(String(val), x, y)
          }
          ctx2d.restore()
        },
      })
    }

    this.chartInstance = new Chart(this.canvas, {
      type: chartType,
      data: {
        labels,
        datasets: [{
          data: values,
          backgroundColor: bgColors,
          borderColor: bgColors,
          borderWidth: 1,
        }],
      },
      options: {
        responsive: false,
        maintainAspectRatio: false,
        indexAxis: attrs.type === "hbar" ? "y" as const : "x" as const,
        plugins: {
          title: {
            display: !!attrs.title,
            text: attrs.title || "",
          },
          legend: {
            display: attrs.legend !== "false",
          },
        },
      },
      plugins,
    })
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.canvas.width = node.attrs.width
    this.canvas.height = node.attrs.height
    this.applyAlign(node.attrs.align)
    this.renderChart()
    return true
  }

  stopEvent(event: Event): boolean {
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click") return false
    return true
  }

  ignoreMutation(): boolean {
    return true
  }

  destroy() {
    if (this.chartInstance) {
      this.chartInstance.destroy()
      this.chartInstance = null
    }
  }
}

// ── Styles ──

const chartStyles = `
.gowiki-chart {
  margin: 0.5em 0;
}
.gowiki-chart canvas {
  max-width: 100%;
}
#app.gowiki-editing .gowiki-chart {
  border: 1px dashed #ccc;
  border-radius: 4px;
  padding: 4px;
}
#app.gowiki-editing .gowiki-chart.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
`

// ── Plugin ──

export const chartPlugin: WikiPlugin = {
  register(reg: Registry) {
    // ── Schema ──
    reg.registerSchema({
      nodes: {
        chart: {
          group: "block",
          atom: true,
          attrs: {
            type:   { default: "pie" },
            width:  { default: DEFAULT_WIDTH },
            height: { default: DEFAULT_HEIGHT },
            title:  { default: "" },
            legend: { default: "true" },
            values: { default: "false" },
            align:  { default: "" },
            colors: { default: "" },
            data:   { default: "" },
          },
          toDOM(node: any) {
            return ["div", {
              class: "gowiki-chart",
              "data-chart-type": node.attrs.type,
              "data-chart-width": String(node.attrs.width),
              "data-chart-height": String(node.attrs.height),
              "data-chart-title": node.attrs.title,
              "data-chart-legend": node.attrs.legend,
              "data-chart-values": node.attrs.values,
              "data-chart-align": node.attrs.align,
              "data-chart-colors": node.attrs.colors,
              "data-chart-data": node.attrs.data,
            }, `Chart: ${node.attrs.type}${node.attrs.title ? " — " + node.attrs.title : ""}`]
          },
          parseDOM: [{
            tag: "div.gowiki-chart",
            getAttrs(dom: HTMLElement) {
              return {
                type: dom.getAttribute("data-chart-type") || "pie",
                width: parseInt(dom.getAttribute("data-chart-width") || String(DEFAULT_WIDTH), 10),
                height: parseInt(dom.getAttribute("data-chart-height") || String(DEFAULT_HEIGHT), 10),
                title: dom.getAttribute("data-chart-title") || "",
                legend: dom.getAttribute("data-chart-legend") || "true",
                values: dom.getAttribute("data-chart-values") || "false",
                align: dom.getAttribute("data-chart-align") || "",
                colors: dom.getAttribute("data-chart-colors") || "",
                data: dom.getAttribute("data-chart-data") || "",
              }
            },
          }],
        },
      },
    })

    // ── Properties ──
    reg.registerNodeProperties("chart", chartProperties)

    // ── markdown-it block rule ──
    reg.registerMarkdownItPlugin((md: any) => {
      md.block.ruler.before("fence", "chart_fence", (state: any, startLine: number, endLine: number, silent: boolean) => {
        const startPos = state.bMarks[startLine] + state.tShift[startLine]
        const maxPos = state.eMarks[startLine]
        const firstLine = state.src.slice(startPos, maxPos)

        if (!firstLine.match(/^`{3,}chart(?:\s|$)/)) return false
        if (silent) return true

        const backtickCount = firstLine.match(/^(`+)/)![1].length
        const infoStr = firstLine.slice(backtickCount).replace(/^chart\s*/, "").trim()

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
        const attrs = parseChartInfo(infoStr)

        // Emit single token
        const token = state.push("chart", "div", 0)
        token.block = true
        token.map = [startLine, nextLine + 1]
        token.meta = { ...attrs, data: body }

        state.line = nextLine + 1
        return true
      })
    })

    // ── Markdown → PM ──
    reg.registerText("chart", {
      run(ctx, tok) {
        const meta = tok.meta ?? {}
        ctx.push(ctx.schema.nodes.chart.create({
          type: meta.type ?? "pie",
          width: meta.width ?? DEFAULT_WIDTH,
          height: meta.height ?? DEFAULT_HEIGHT,
          title: meta.title ?? "",
          legend: meta.legend ?? "true",
          values: meta.values ?? "false",
          align: meta.align ?? "",
          colors: meta.colors ?? "",
          data: meta.data ?? "",
        }))
      },
    })

    // ── PM → Markdown ──
    reg.registerPMNode("chart", {
      print(node) {
        let out = serializeChartHeader(node.attrs) + "\n"
        const data = node.attrs.data || ""
        if (data) out += data + "\n"
        out += "```\n\n"
        return out
      },
    })

    // ── NodeView ──
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.chart"),
        props: {
          nodeViews: {
            chart(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new ChartNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // ── Toolbar command ──
    reg.registerCommand("chart", "insert", (state, dispatch) => {
      const chartType = reg.schema.nodes.chart
      if (!chartType) return false
      if (dispatch) {
        const sampleData = "Item 1 = 30\nItem 2 = 50\nItem 3 = 20"
        const node = chartType.create({ type: "pie", data: sampleData })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === chartType && insertedAt === null) {
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
    reg.registerStyle("chart", chartStyles)
  },
}
