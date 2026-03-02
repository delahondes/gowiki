import { Plugin as GowikiPlugin, NodePropertySpec } from "../compiler/registry"
import {
  tableNodes,
  tableEditing,
  goToNextCell,
  isInTable,
  addColumnAfter,
  addColumnBefore,
  addRowAfter,
  addRowBefore,
  deleteColumn,
  deleteRow,
} from "prosemirror-tables"
import { Node, Fragment, Schema } from "prosemirror-model"
import { Plugin as PMPlugin } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"
import { keymap } from "prosemirror-keymap"
import markdownItMultiMdTable from "markdown-it-multimd-table"
import { formulaDecoPlugin } from "./table_formulas"

// ─── Named color presets ─────────────────────────────────

const NAMED_COLORS: Record<string, string> = {
  red: "#fee2e2",
  green: "#dcfce7",
  yellow: "#fef9c3",
  orange: "#ffedd5",
  grey: "#f3f4f6",
  blue: "#dbeafe",
  none: "",
}

function resolveColor(name: string): string {
  if (name.startsWith("#")) return name
  return NAMED_COLORS[name.toLowerCase()] ?? name
}

// ─── Color rules engine ─────────────────────────────────

export type ColorRule = {
  op: string
  operand?: string
  color: string
}

export function parseColorRules(spec: string): ColorRule[] {
  const rules: ColorRule[] = []
  for (const part of spec.split(",").map(s => s.trim()).filter(Boolean)) {
    const tokens = part.split(/\s+/)
    if (tokens.length < 2) continue

    const raw = tokens[0]
    const color = tokens[tokens.length - 1]

    if (raw === "else") {
      rules.push({ op: "else", color })
      continue
    }
    if (raw === "empty") {
      rules.push({ op: "empty", color })
      continue
    }
    if (raw === "!empty") {
      rules.push({ op: "!empty", color })
      continue
    }

    const opMatch = raw.match(/^(>=|<=|!=|>|<|=|~)(.+)$/)
    if (opMatch) {
      rules.push({ op: opMatch[1], operand: opMatch[2], color })
      continue
    }

    rules.push({ op: "=", operand: raw, color })
  }
  return rules
}

export function evaluateColorRules(rules: ColorRule[], text: string): string | null {
  const trimmed = text.trim()
  const num = Number(trimmed)
  const isNum = trimmed !== "" && !isNaN(num)

  for (const rule of rules) {
    let match = false
    switch (rule.op) {
      case ">":
        match = isNum && num > Number(rule.operand)
        break
      case ">=":
        match = isNum && num >= Number(rule.operand)
        break
      case "<":
        match = isNum && num < Number(rule.operand)
        break
      case "<=":
        match = isNum && num <= Number(rule.operand)
        break
      case "=":
        match = trimmed === rule.operand
        break
      case "!=":
        match = trimmed !== rule.operand
        break
      case "~":
        try {
          match = new RegExp(rule.operand!).test(trimmed)
        } catch {
          match = false
        }
        break
      case "empty":
        match = trimmed === ""
        break
      case "!empty":
        match = trimmed !== ""
        break
      case "else":
        match = true
        break
    }
    if (match) {
      const resolved = resolveColor(rule.color)
      return resolved || null
    }
  }
  return null
}

// ─── Cell text helpers ───────────────────────────────────

export function getCellText(cell: Node): string {
  let text = ""
  cell.content.forEach(block => {
    text += block.textContent
  })
  return text
}

// ─── Header variants ────────────────────────────────────

const HEADER_VALUES = ["1st_row", "2_rows", "1st_col", "2_cols", "both"] as const
type HeaderVariant = (typeof HEADER_VALUES)[number]

function applyHeaderVariant(tableNode: Node, schema: Schema): Node {
  const headers: HeaderVariant = tableNode.attrs.headers ?? "1st_row"

  const rows: Node[][] = []
  tableNode.content.forEach(row => {
    const cells: Node[] = []
    row.content.forEach(cell => cells.push(cell))
    rows.push(cells)
  })

  const numRows = rows.length
  if (numRows === 0) return tableNode

  function shouldBeHeader(r: number, c: number): boolean {
    switch (headers) {
      case "1st_row":
        return r === 0
      case "2_rows":
        return r <= 1
      case "1st_col":
        return c === 0
      case "2_cols":
        return c <= 1
      case "both":
        return r === 0 || c === 0
    }
  }

  const newRows: Node[] = []
  for (let r = 0; r < numRows; r++) {
    const newCells: Node[] = []
    for (let c = 0; c < rows[r].length; c++) {
      const cell = rows[r][c]
      const wantHeader = shouldBeHeader(r, c)
      const isHeader = cell.type === schema.nodes.table_header

      if (wantHeader === isHeader) {
        newCells.push(cell)
      } else {
        const targetType = wantHeader
          ? schema.nodes.table_header
          : schema.nodes.table_cell
        newCells.push(
          targetType.create(cell.attrs, cell.content, cell.marks)
        )
      }
    }
    newRows.push(schema.nodes.table_row.create(null, newCells))
  }

  return tableNode.type.create(tableNode.attrs, newRows)
}

// ─── Cell merging ────────────────────────────────────────

function resolveMerges(tableNode: Node, schema: Schema): Node {
  const rows: Node[][] = []
  tableNode.content.forEach(row => {
    const cells: Node[] = []
    row.content.forEach(cell => cells.push(cell))
    rows.push(cells)
  })

  const numRows = rows.length
  if (numRows === 0) return tableNode
  const numCols = rows[0].length

  // Quick check: any merges?
  let hasMerges = false
  for (const row of rows) {
    for (const cell of row) {
      const text = getCellText(cell).trim()
      if (text === "<<" || text === "^^") {
        hasMerges = true
        break
      }
    }
    if (hasMerges) break
  }
  if (!hasMerges) return tableNode

  // Owner grid: tracks which real cell owns each position
  const owner: { r: number; c: number }[][] = rows.map((row, r) =>
    row.map((_, c) => ({ r, c }))
  )
  const spans: { colspan: number; rowspan: number }[][] = rows.map(row =>
    row.map(() => ({ colspan: 1, rowspan: 1 }))
  )
  const removed = new Set<string>()

  // First pass: << (colspan)
  for (let r = 0; r < numRows; r++) {
    for (let c = 0; c < numCols; c++) {
      if (getCellText(rows[r][c]).trim() === "<<" && c > 0) {
        const o = owner[r][c - 1]
        spans[o.r][o.c].colspan++
        owner[r][c] = o
        removed.add(`${r},${c}`)
      }
    }
  }

  // Second pass: ^^ (rowspan)
  for (let r = 0; r < numRows; r++) {
    const extended = new Set<string>()
    for (let c = 0; c < numCols; c++) {
      if (removed.has(`${r},${c}`)) continue
      if (getCellText(rows[r][c]).trim() === "^^" && r > 0) {
        const o = owner[r - 1][c]
        const oKey = `${o.r},${o.c}`
        if (!extended.has(oKey)) {
          spans[o.r][o.c].rowspan++
          extended.add(oKey)
        }
        owner[r][c] = o
        removed.add(`${r},${c}`)
      }
    }
  }

  // Rebuild table
  const newRows: Node[] = []
  for (let r = 0; r < numRows; r++) {
    const cells: Node[] = []
    for (let c = 0; c < numCols; c++) {
      if (removed.has(`${r},${c}`)) continue
      const cell = rows[r][c]
      const { colspan, rowspan } = spans[r][c]
      if (colspan > 1 || rowspan > 1) {
        cells.push(
          cell.type.create(
            { ...cell.attrs, colspan, rowspan },
            cell.content,
            cell.marks
          )
        )
      } else {
        cells.push(cell)
      }
    }
    newRows.push(schema.nodes.table_row.create(null, cells))
  }

  return tableNode.type.create(tableNode.attrs, newRows)
}

// ─── Cell color + formula parsing ────────────────────────

const CELL_COLOR_RE = /^\{color=([^\s}]+)(?:\s+text-color=([^\s}]+))?\}\s*/

function applyCellFeatures(tableNode: Node, schema: Schema): Node {
  let changed = false

  const newRows: Node[] = []
  tableNode.content.forEach(row => {
    const newCells: Node[] = []
    row.content.forEach(cell => {
      const result = processCellFeatures(cell, schema)
      if (result !== cell) changed = true
      newCells.push(result)
    })
    newRows.push(schema.nodes.table_row.create(null, newCells))
  })

  return changed
    ? tableNode.type.create(tableNode.attrs, newRows)
    : tableNode
}

function processCellFeatures(cell: Node, schema: Schema): Node {
  if (cell.childCount === 0) return cell
  const firstBlock = cell.child(0)
  if (firstBlock.type !== schema.nodes.paragraph) return cell
  if (firstBlock.childCount === 0) return cell

  const firstChild = firstBlock.child(0)
  if (!firstChild.isText) return cell
  const text = firstChild.text ?? ""

  let newAttrs = { ...cell.attrs }
  let newText = text
  let changed = false

  // Cell color directive
  const colorMatch = newText.match(CELL_COLOR_RE)
  if (colorMatch) {
    newAttrs.cellColor = colorMatch[1]
    if (colorMatch[2]) newAttrs.cellTextColor = colorMatch[2]
    newText = newText.slice(colorMatch[0].length)
    changed = true
  }

  // Formula detection
  if (newText.trimStart().startsWith("=") && newText.trim().length > 1) {
    newAttrs.formula = newText.trim().slice(1)
    changed = true
  }

  if (!changed) return cell

  // Rebuild cell with modified text and attrs
  let newFirstBlock: Node
  if (newText === "") {
    if (firstBlock.childCount === 1) {
      newFirstBlock = schema.nodes.paragraph.create()
    } else {
      const children: Node[] = []
      for (let i = 1; i < firstBlock.childCount; i++) {
        children.push(firstBlock.child(i))
      }
      newFirstBlock = schema.nodes.paragraph.create(null, children)
    }
  } else if (newText !== text) {
    const children: Node[] = [schema.text(newText, firstChild.marks)]
    for (let i = 1; i < firstBlock.childCount; i++) {
      children.push(firstBlock.child(i))
    }
    newFirstBlock = schema.nodes.paragraph.create(null, children)
  } else {
    newFirstBlock = firstBlock
  }

  const cellChildren: Node[] = [newFirstBlock]
  for (let i = 1; i < cell.childCount; i++) {
    cellChildren.push(cell.child(i))
  }

  return cell.type.create(newAttrs, cellChildren, cell.marks)
}

// ─── Column attribute aggregation ────────────────────────

export type ColumnPropEntry = {
  align?: string
  width?: string
  color?: string
}
export type ColumnProps = Record<string, ColumnPropEntry>

function aggregateTableAttrs(raw: Record<string, any>): Record<string, any> {
  const clean: Record<string, any> = {}
  let columns: ColumnProps | null = null

  for (const [key, value] of Object.entries(raw)) {
    if (key.startsWith("_col.")) {
      const m = key.match(/^_col\.(\d+)\.(\w+)$/)
      if (m) {
        columns = columns ?? {}
        const colKey = m[1]
        const prop = m[2]
        columns[colKey] = columns[colKey] ?? {}
        ;(columns[colKey] as any)[prop] = value
      }
    } else {
      clean[key] = value
    }
  }

  if (columns) clean.columns = columns
  return clean
}

// ─── Width normalization ─────────────────────────────────

function normalizeTableWidth(raw: string): string | null {
  const value = String(raw ?? "").trim().toLowerCase()
  if (!value) return null

  const pct = value.match(/^(\d+)%$/)
  if (pct) {
    const n = Number(pct[1])
    if (n > 0) return `${n}%`
    throw new Error("Table width percent must be > 0")
  }

  const px = value.match(/^(\d+)px$/)
  if (px) {
    const n = Number(px[1])
    if (n > 0) return `${n}px`
    throw new Error("Table width in px must be > 0")
  }

  throw new Error(`Invalid table width "${raw}". Expected 80% or 800px.`)
}

// ─── Table properties ────────────────────────────────────

const tableProperties: NodePropertySpec[] = [
  {
    name: "width",
    label: "Width",
    default: null,
    parse: (raw: string) => normalizeTableWidth(raw),
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "headers",
    label: "Headers",
    default: "1st_row",
    parse: (raw: string) => {
      if ((HEADER_VALUES as readonly string[]).includes(raw)) return raw
      throw new Error(`Invalid headers value: ${raw}`)
    },
    options: [
      { value: "1st_row", label: "1st row" },
      { value: "2_rows", label: "2 rows" },
      { value: "1st_col", label: "1st column" },
      { value: "2_cols", label: "2 columns" },
      { value: "both", label: "Both" },
    ],
  },
]

// ─── DOM helpers ─────────────────────────────────────────

function addStyleToDOM(spec: any, style: string): any {
  if (!style || !Array.isArray(spec)) return spec
  const [tag, maybeAttrs, ...rest] = spec
  const hasAttrs =
    maybeAttrs && typeof maybeAttrs === "object" && !Array.isArray(maybeAttrs)
  const attrs = hasAttrs ? maybeAttrs : {}
  const existing = attrs.style ? String(attrs.style) : ""
  const newStyle = existing
    ? `${existing}${existing.trim().endsWith(";") ? " " : "; "}${style}`
    : style
  const newAttrs = { ...attrs, style: newStyle }
  const children = hasAttrs ? rest : [maybeAttrs, ...rest]
  return [tag, newAttrs, ...children]
}

// ─── Serialization grid ──────────────────────────────────

type GridCell =
  | { type: "cell"; node: Node }
  | { type: "<<" }
  | { type: "^^" }

function buildSerializationGrid(tableNode: Node): {
  grid: GridCell[][]
  totalCols: number
} {
  const rows: Node[][] = []
  tableNode.content.forEach(row => {
    const cells: Node[] = []
    row.content.forEach(cell => cells.push(cell))
    rows.push(cells)
  })

  if (rows.length === 0) return { grid: [], totalCols: 0 }

  // Determine total columns (max across all rows, accounting for spans)
  let totalCols = 0
  for (const row of rows) {
    let rowCols = 0
    for (const cell of row) {
      rowCols += cell.attrs.colspan ?? 1
    }
    totalCols = Math.max(totalCols, rowCols)
  }

  const grid: (GridCell | null)[][] = Array.from(
    { length: rows.length },
    () => new Array(totalCols).fill(null)
  )

  for (let r = 0; r < rows.length; r++) {
    let c = 0
    for (const cell of rows[r]) {
      while (c < totalCols && grid[r][c] !== null) c++
      if (c >= totalCols) break

      const colspan = cell.attrs.colspan ?? 1
      const rowspan = cell.attrs.rowspan ?? 1

      grid[r][c] = { type: "cell", node: cell }

      for (let dc = 1; dc < colspan; dc++) {
        if (c + dc < totalCols) grid[r][c + dc] = { type: "<<" }
      }

      for (let dr = 1; dr < rowspan; dr++) {
        if (r + dr < rows.length) {
          for (let dc = 0; dc < colspan; dc++) {
            if (c + dc < totalCols) grid[r + dr][c + dc] = { type: "^^" }
          }
        }
      }

      c += colspan
    }
  }

  const finalGrid: GridCell[][] = grid.map(row =>
    row.map(cell => cell ?? { type: "cell", node: null as any })
  )

  return { grid: finalGrid, totalCols }
}

// ─── Serialize cell content ──────────────────────────────

function serializeCellContent(
  cell: Node,
  recurse: (node: Node) => string
): string {
  let prefix = ""
  if (cell.attrs.cellColor) {
    prefix = `{color=${cell.attrs.cellColor}`
    if (cell.attrs.cellTextColor)
      prefix += ` text-color=${cell.attrs.cellTextColor}`
    prefix += "} "
  }

  if (cell.attrs.formula) {
    return prefix + `=${cell.attrs.formula}`
  }

  let txt = ""
  cell.content.forEach(p => {
    txt += recurse(p).trim()
  })

  return prefix + txt
}

// ─── Styles ──────────────────────────────────────────────

const tableStyles = `
.ProseMirror table {
  border-collapse: collapse;
  margin: 0.3em 0;
}

.ProseMirror th,
.ProseMirror td {
  border: 1px solid #ccc;
  padding: 0.25em 0.5em;
  vertical-align: top;
}

.ProseMirror th {
  background: #f7f7f7;
  font-weight: 600;
  text-align: left;
}

.ProseMirror td > p,
.ProseMirror th > p {
  margin: 0;
}

.ProseMirror .selectedCell {
  background: #cce5ff;
}

.ProseMirror .formula-result {
  color: #666;
  font-style: italic;
}
`

// ─── Column decoration plugin ────────────────────────────

function columnDecoPlugin(schema: Schema): PMPlugin {
  return new PMPlugin({
    props: {
      decorations(state) {
        const decos: Decoration[] = []

        state.doc.descendants((node, pos) => {
          if (node.type !== schema.nodes.table) return true

          const columns: ColumnProps | null = node.attrs.columns
          if (!columns) return true

          // Pre-parse color rules
          const colorRules: Record<string, ColorRule[]> = {}
          for (const [colKey, props] of Object.entries(columns)) {
            if (props.color) {
              colorRules[colKey] = parseColorRules(props.color)
            }
          }

          // Walk rows and cells
          node.content.forEach((row, rowOffset) => {
            const rowPos = pos + 1 + rowOffset
            let colIdx = 0

            row.content.forEach((cell, cellOffset) => {
              const cellPos = rowPos + 1 + cellOffset
              const colKey = String(colIdx + 1)
              const props = columns[colKey]

              if (props) {
                let style = ""
                if (props.align) style += `text-align: ${props.align}; `
                if (props.width) style += `width: ${props.width}; `

                // Column color (only if cell doesn't have its own color)
                if (colorRules[colKey] && !cell.attrs.cellColor) {
                  const text = getCellText(cell)
                  const bg = evaluateColorRules(colorRules[colKey], text)
                  if (bg) style += `background: ${bg}; `
                }

                if (style) {
                  decos.push(
                    Decoration.node(cellPos, cellPos + cell.nodeSize, {
                      style,
                    })
                  )
                }
              }

              colIdx += cell.attrs.colspan ?? 1
            })
          })

          return false
        })

        return DecorationSet.create(state.doc, decos)
      },
    },
  })
}

// ─── Public API ──────────────────────────────────────────

export function makeTable(
  schema: any,
  rows: number,
  cols: number,
  withHeader = true
): Node {
  const { table, table_row, table_cell, table_header } = schema.nodes

  const rowNodes: Node[] = []

  for (let r = 0; r < rows; r++) {
    const cells: Node[] = []
    for (let c = 0; c < cols; c++) {
      const cellType = withHeader && r === 0 ? table_header : table_cell
      cells.push(cellType.createAndFill()!)
    }
    rowNodes.push(table_row.create(null, cells))
  }

  return table.create(null, rowNodes)
}

/**
 * Table plugin — enhanced with headers, column properties,
 * cell color, cell merging, and formula support.
 */
export const tablePlugin: GowikiPlugin = {
  register(reg) {
    reg.registerMarkdownItPlugin(md => {
      md.use(markdownItMultiMdTable, {
        multiline: false,
        rowspan: false,
        headerless: false,
        multibody: false,
      })
    })

    const nodes = tableNodes({
      tableGroup: "block",
      cellContent: "block+",
      cellAttributes: {
        cellColor: {
          default: null,
          getFromDOM: (dom: HTMLElement) =>
            dom.getAttribute("data-cell-color") || null,
          setDOMAttr(value: any, attrs: any) {
            if (value) {
              attrs["data-cell-color"] = value
              const existing = attrs.style || ""
              attrs.style = existing + `background: ${resolveColor(value)}; `
            }
          },
        },
        cellTextColor: {
          default: null,
          getFromDOM: (dom: HTMLElement) =>
            dom.getAttribute("data-cell-text-color") || null,
          setDOMAttr(value: any, attrs: any) {
            if (value) {
              attrs["data-cell-text-color"] = value
              const existing = attrs.style || ""
              attrs.style = existing + `color: ${resolveColor(value)}; `
            }
          },
        },
        formula: {
          default: null,
          getFromDOM: (dom: HTMLElement) =>
            dom.getAttribute("data-formula") || null,
          setDOMAttr(value: any, attrs: any) {
            if (value) {
              attrs["data-formula"] = value
            }
          },
        },
      },
    })

    const baseTable = nodes.table
    nodes.table = {
      ...baseTable,
      attrs: {
        ...baseTable.attrs,
        width: { default: null },
        headers: { default: "1st_row" },
        columns: { default: null },
      },
      toDOM(node) {
        const domSpec = baseTable.toDOM
          ? baseTable.toDOM(node)
          : ["table", ["tbody", 0]]
        const width = node.attrs.width ?? null
        return width ? addStyleToDOM(domSpec, `width: ${width};`) : domSpec
      },
    }

    reg.registerSchema({ nodes })

    reg.registerDirective("table", {
      nodeType: "table",
      appliesTo: ["table_open"],
      properties: tableProperties,
      parseUnknownAttr(key, value) {
        const m = key.match(/^col(\d+)\.(align|width|color)$/)
        if (!m) return null
        return [`_col.${m[1]}.${m[2]}`, value]
      },
    })

    /* ----------------------------
     * Markdown -> PM
     * ---------------------------- */

    reg.registerNode("table_open", {
      open(ctx) {
        const rawAttrs = ctx.token?.meta?.directives?.table ?? null
        const attrs = rawAttrs ? aggregateTableAttrs(rawAttrs) : null
        ctx.open(reg.schema.nodes.table.create(attrs))
      },
    })

    reg.registerNode("table_close", {
      close(ctx) {
        ctx.close()
        let tableNode = ctx.popLast()!
        tableNode = resolveMerges(tableNode, reg.schema)
        tableNode = applyHeaderVariant(tableNode, reg.schema)
        tableNode = applyCellFeatures(tableNode, reg.schema)
        ctx.push(tableNode)
      },
    })

    reg.registerNode("thead_open", { open() {} })
    reg.registerNode("thead_close", { close() {} })
    reg.registerNode("tbody_open", { open() {} })
    reg.registerNode("tbody_close", { close() {} })

    reg.registerNode("tr_open", {
      open(ctx) {
        ctx.open(reg.schema.nodes.table_row.create())
      },
    })

    reg.registerNode("tr_close", {
      close(ctx) {
        ctx.close()
      },
    })

    reg.registerNode("th_open", {
      open(ctx) {
        ctx.open(reg.schema.nodes.table_header.create())
        ctx.open(reg.schema.nodes.paragraph.create())
      },
    })

    reg.registerNode("th_close", {
      close(ctx) {
        ctx.close()
        ctx.close()
      },
    })

    reg.registerNode("td_open", {
      open(ctx) {
        ctx.open(reg.schema.nodes.table_cell.create())
        ctx.open(reg.schema.nodes.paragraph.create())
      },
    })

    reg.registerNode("td_close", {
      close(ctx) {
        ctx.close()
        ctx.close()
      },
    })

    /* ----------------------------
     * PM -> Markdown
     * ---------------------------- */

    reg.registerPMNode("table", {
      print(node, ctx, recurse) {
        const { grid, totalCols } = buildSerializationGrid(node)
        if (grid.length === 0) return ""

        // Build directive line
        const directiveParts: string[] = []

        const headers = node.attrs.headers ?? "1st_row"
        if (headers !== "1st_row") {
          directiveParts.push(`headers=${headers}`)
        }

        const width = node.attrs.width ?? null
        if (width) {
          directiveParts.push(`width=${width}`)
        }

        const columns: ColumnProps | null = node.attrs.columns
        if (columns) {
          // Sort by column number for deterministic output
          const sortedKeys = Object.keys(columns).sort(
            (a, b) => Number(a) - Number(b)
          )
          for (const colKey of sortedKeys) {
            const props = columns[colKey]
            if (props.align)
              directiveParts.push(`col${colKey}.align=${props.align}`)
            if (props.width)
              directiveParts.push(`col${colKey}.width=${props.width}`)
            if (props.color)
              directiveParts.push(`col${colKey}.color="${props.color}"`)
          }
        }

        let out = ""
        if (directiveParts.length > 0) {
          out += `{table ${directiveParts.join(" ")}}\n`
        }

        // Serialize rows
        for (let r = 0; r < grid.length; r++) {
          const cells: string[] = []
          for (let c = 0; c < totalCols; c++) {
            const entry = grid[r][c]
            if (entry.type === "<<") {
              cells.push("<<")
            } else if (entry.type === "^^") {
              cells.push("^^")
            } else {
              cells.push(serializeCellContent(entry.node, recurse))
            }
          }
          out += "| " + cells.join(" | ") + " |\n"

          // Separator after first row
          if (r === 0) {
            out +=
              "| " + Array(totalCols).fill("---").join(" | ") + " |\n"
          }
        }

        return out + "\n"
      },
    })

    /* ----------------------------
     * Editor integration
     * ---------------------------- */

    const tabInTable = (state: any, dispatch: any, view: any) => {
      if (goToNextCell(1)(state, dispatch)) return true
      if (!isInTable(state)) return false
      if (dispatch) {
        addRowAfter(state, dispatch)
        if (view) {
          goToNextCell(1)(view.state, view.dispatch)
        }
      }
      return true
    }

    reg.registerEditorPlugin(() =>
      keymap({
        Tab: tabInTable,
        "Shift-Tab": goToNextCell(-1),
      })
    )

    reg.registerEditorPlugin(() => tableEditing())

    // Column property decorations
    reg.registerEditorPlugin(schema => columnDecoPlugin(schema))

    // Formula evaluation decorations
    reg.registerEditorPlugin(schema => formulaDecoPlugin(schema))

    /* ----------------------------
     * Styles
     * ---------------------------- */

    reg.registerStyle("table", tableStyles)

    /* ----------------------------
     * Commands
     * ---------------------------- */

    reg.registerCommand("table", "insert", (state, dispatch) => {
      const tableNode = makeTable(reg.schema, 3, 3, true)
      if (!tableNode) return false
      if (dispatch) {
        dispatch(state.tr.replaceSelectionWith(tableNode))
      }
      return true
    })

    reg.registerCommand("table", "row.addBefore", addRowBefore)
    reg.registerCommand("table", "row.addAfter", addRowAfter)
    reg.registerCommand("table", "column.addBefore", addColumnBefore)
    reg.registerCommand("table", "column.addAfter", addColumnAfter)
    reg.registerCommand("table", "row.delete", deleteRow)
    reg.registerCommand("table", "column.delete", deleteColumn)
  },
}
