import { Node, Schema } from "prosemirror-model"
import { Plugin as PMPlugin, Transaction } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"
import {
  getCellText,
  evaluateColorRules,
  parseColorRules,
  resolveColumnProps,
  type ColumnProps,
} from "./table"

// ─── Cell reference parsing ─────────────────────────────

type CellRef = { col: number; row: number }

function parseCellRef(ref: string): CellRef | null {
  const m = ref.match(/^([A-Z]+)(\d+)$/)
  if (!m) return null
  let col = 0
  for (const ch of m[1]) {
    col = col * 26 + (ch.charCodeAt(0) - 64)
  }
  return { col: col - 1, row: Number(m[2]) - 1 }
}

function parseRange(range: string): CellRef[] | null {
  const parts = range.split(":")
  if (parts.length !== 2) return null
  const start = parseCellRef(parts[0])
  const end = parseCellRef(parts[1])
  if (!start || !end) return null

  const refs: CellRef[] = []
  const minR = Math.min(start.row, end.row)
  const maxR = Math.max(start.row, end.row)
  const minC = Math.min(start.col, end.col)
  const maxC = Math.max(start.col, end.col)
  for (let r = minR; r <= maxR; r++) {
    for (let c = minC; c <= maxC; c++) {
      refs.push({ col: c, row: r })
    }
  }
  return refs
}

// ─── Tokenizer ──────────────────────────────────────────

type Token =
  | { type: "number"; value: number }
  | { type: "string"; value: string }
  | { type: "ref"; value: string }
  | { type: "range"; value: string }
  | { type: "func"; value: string }
  | { type: "op"; value: string }
  | { type: "paren"; value: "(" | ")" }
  | { type: "comma" }

function tokenize(expr: string): Token[] {
  const tokens: Token[] = []
  let i = 0

  while (i < expr.length) {
    const ch = expr[i]

    if (/\s/.test(ch)) {
      i++
      continue
    }

    // Number (including negative with unary minus)
    if (/\d/.test(ch) || (ch === "." && i + 1 < expr.length && /\d/.test(expr[i + 1]))) {
      let num = ""
      while (i < expr.length && (/\d/.test(expr[i]) || expr[i] === ".")) {
        num += expr[i++]
      }
      tokens.push({ type: "number", value: Number(num) })
      continue
    }

    // Cell ref, range, or function name
    if (/[A-Z]/i.test(ch)) {
      let word = ""
      while (i < expr.length && /[A-Za-z0-9]/.test(expr[i])) {
        word += expr[i++]
      }
      // Check for range (A1:B3)
      if (i < expr.length && expr[i] === ":") {
        let range = word + ":"
        i++
        while (i < expr.length && /[A-Za-z0-9]/.test(expr[i])) {
          range += expr[i++]
        }
        tokens.push({ type: "range", value: range.toUpperCase() })
        continue
      }
      // Check if it's a function (followed by paren)
      if (i < expr.length && expr[i] === "(") {
        tokens.push({ type: "func", value: word.toUpperCase() })
        continue
      }
      // Cell reference
      tokens.push({ type: "ref", value: word.toUpperCase() })
      continue
    }

    if (ch === "(") {
      tokens.push({ type: "paren", value: "(" })
      i++
      continue
    }
    if (ch === ")") {
      tokens.push({ type: "paren", value: ")" })
      i++
      continue
    }
    if (ch === ",") {
      tokens.push({ type: "comma" })
      i++
      continue
    }

    if ("+-*/".includes(ch)) {
      tokens.push({ type: "op", value: ch })
      i++
      continue
    }

    // Comparison operators
    if (ch === ">" || ch === "<" || ch === "!" || ch === "=") {
      let op = ch
      i++
      if (i < expr.length && expr[i] === "=") {
        op += "="
        i++
      }
      tokens.push({ type: "op", value: op })
      continue
    }

    // Unknown character — skip
    i++
  }

  return tokens
}

// ─── AST ─────────────────────────────────────────────────

type ASTNode =
  | { kind: "number"; value: number }
  | { kind: "ref"; ref: string }
  | { kind: "range"; range: string }
  | { kind: "relative"; direction: "ABOVE" | "LEFT" }
  | { kind: "binop"; op: string; left: ASTNode; right: ASTNode }
  | { kind: "unary"; op: string; operand: ASTNode }
  | { kind: "call"; func: string; args: ASTNode[] }

// ─── Parser ──────────────────────────────────────────────

class Parser {
  private pos = 0
  constructor(private tokens: Token[]) {}

  parse(): ASTNode {
    const result = this.parseExpr()
    return result
  }

  private peek(): Token | undefined {
    return this.tokens[this.pos]
  }

  private consume(): Token {
    return this.tokens[this.pos++]
  }

  private parseExpr(): ASTNode {
    return this.parseComparison()
  }

  private parseComparison(): ASTNode {
    let left = this.parseAddSub()
    while (
      this.peek()?.type === "op" &&
      ["=", "!=", ">", "<", ">=", "<="].includes(this.peek()!.value as string)
    ) {
      const op = this.consume().value as string
      const right = this.parseAddSub()
      left = { kind: "binop", op, left, right }
    }
    return left
  }

  private parseAddSub(): ASTNode {
    let left = this.parseMulDiv()
    while (
      this.peek()?.type === "op" &&
      (this.peek()!.value === "+" || this.peek()!.value === "-")
    ) {
      const op = this.consume().value as string
      const right = this.parseMulDiv()
      left = { kind: "binop", op, left, right }
    }
    return left
  }

  private parseMulDiv(): ASTNode {
    let left = this.parseUnary()
    while (
      this.peek()?.type === "op" &&
      (this.peek()!.value === "*" || this.peek()!.value === "/")
    ) {
      const op = this.consume().value as string
      const right = this.parseUnary()
      left = { kind: "binop", op, left, right }
    }
    return left
  }

  private parseUnary(): ASTNode {
    if (
      this.peek()?.type === "op" &&
      this.peek()!.value === "-"
    ) {
      this.consume()
      const operand = this.parsePrimary()
      return { kind: "unary", op: "-", operand }
    }
    return this.parsePrimary()
  }

  private parsePrimary(): ASTNode {
    const tok = this.peek()
    if (!tok) throw new Error("Unexpected end of expression")

    if (tok.type === "number") {
      this.consume()
      return { kind: "number", value: tok.value }
    }

    if (tok.type === "ref") {
      this.consume()
      if (tok.value === "ABOVE" || tok.value === "LEFT") {
        return { kind: "relative", direction: tok.value }
      }
      return { kind: "ref", ref: tok.value }
    }

    if (tok.type === "range") {
      this.consume()
      return { kind: "range", range: tok.value }
    }

    if (tok.type === "func") {
      const funcName = tok.value
      this.consume()
      // Consume '('
      if (this.peek()?.type !== "paren" || this.peek()?.value !== "(") {
        throw new Error(`Expected ( after function ${funcName}`)
      }
      this.consume()

      const args: ASTNode[] = []
      if (this.peek()?.type !== "paren" || this.peek()?.value !== ")") {
        args.push(this.parseExpr())
        while (this.peek()?.type === "comma") {
          this.consume()
          args.push(this.parseExpr())
        }
      }

      if (this.peek()?.type !== "paren" || this.peek()?.value !== ")") {
        throw new Error(`Expected ) after function ${funcName} arguments`)
      }
      this.consume()

      return { kind: "call", func: funcName, args }
    }

    if (tok.type === "paren" && tok.value === "(") {
      this.consume()
      const expr = this.parseExpr()
      if (this.peek()?.type !== "paren" || this.peek()?.value !== ")") {
        throw new Error("Expected )")
      }
      this.consume()
      return expr
    }

    throw new Error(`Unexpected token: ${JSON.stringify(tok)}`)
  }
}

export function parseFormula(expr: string): ASTNode {
  const tokens = tokenize(expr)
  const parser = new Parser(tokens)
  return parser.parse()
}

// ─── Dependency extraction ──────────────────────────────

function extractRefs(ast: ASTNode): CellRef[] {
  const refs: CellRef[] = []

  function walk(node: ASTNode) {
    switch (node.kind) {
      case "ref": {
        const ref = parseCellRef(node.ref)
        if (ref) refs.push(ref)
        break
      }
      case "range": {
        const rangeRefs = parseRange(node.range)
        if (rangeRefs) refs.push(...rangeRefs)
        break
      }
      case "binop":
        walk(node.left)
        walk(node.right)
        break
      case "unary":
        walk(node.operand)
        break
      case "call":
        for (const arg of node.args) walk(arg)
        break
    }
  }

  walk(ast)
  return refs
}

// ─── Relative range expansion ────────────────────────────

function indexToColLetter(index: number): string {
  let letter = ""
  let n = index + 1
  while (n > 0) {
    n--
    letter = String.fromCharCode(65 + (n % 26)) + letter
    n = Math.floor(n / 26)
  }
  return letter
}

function expandRelatives(
  ast: ASTNode,
  cellRow: number,
  cellCol: number,
  dataRowStart: number,
  dataColStart: number
): ASTNode {
  switch (ast.kind) {
    case "relative": {
      if (ast.direction === "ABOVE") {
        if (cellRow <= dataRowStart) {
          return { kind: "range", range: "" }
        }
        const colLetter = indexToColLetter(cellCol)
        const startRow = dataRowStart + 1 // 1-based
        const endRow = cellRow // 1-based (row before current)
        return { kind: "range", range: `${colLetter}${startRow}:${colLetter}${endRow}` }
      } else {
        // LEFT
        if (cellCol <= dataColStart) {
          return { kind: "range", range: "" }
        }
        const startCol = indexToColLetter(dataColStart)
        const endCol = indexToColLetter(cellCol - 1)
        const row = cellRow + 1 // 1-based
        return { kind: "range", range: `${startCol}${row}:${endCol}${row}` }
      }
    }
    case "binop":
      return {
        kind: "binop",
        op: ast.op,
        left: expandRelatives(ast.left, cellRow, cellCol, dataRowStart, dataColStart),
        right: expandRelatives(ast.right, cellRow, cellCol, dataRowStart, dataColStart),
      }
    case "unary":
      return {
        kind: "unary",
        op: ast.op,
        operand: expandRelatives(ast.operand, cellRow, cellCol, dataRowStart, dataColStart),
      }
    case "call":
      return {
        kind: "call",
        func: ast.func,
        args: ast.args.map(a => expandRelatives(a, cellRow, cellCol, dataRowStart, dataColStart)),
      }
    default:
      return ast
  }
}

// ─── Evaluator ──────────────────────────────────────────

type CellValueGetter = (col: number, row: number) => number | string

function evaluateAST(
  ast: ASTNode,
  getValue: CellValueGetter
): number | string {
  switch (ast.kind) {
    case "number":
      return ast.value

    case "ref": {
      const ref = parseCellRef(ast.ref)
      if (!ref) return "#ERR"
      const val = getValue(ref.col, ref.row)
      if (typeof val === "string") {
        if (val.startsWith("#")) return val // propagate errors
        const n = Number(val)
        return val === "" ? 0 : isNaN(n) ? val : n
      }
      return val
    }

    case "range":
      return "#ERR" // ranges only valid as function arguments

    case "relative":
      return "#ERR" // should be expanded before evaluation

    case "unary": {
      const operand = evaluateAST(ast.operand, getValue)
      if (typeof operand === "string") return operand.startsWith("#") ? operand : "#ERR"
      return -operand
    }

    case "binop": {
      const left = evaluateAST(ast.left, getValue)
      const right = evaluateAST(ast.right, getValue)

      // Propagate errors
      if (typeof left === "string" && left.startsWith("#")) return left
      if (typeof right === "string" && right.startsWith("#")) return right

      const lNum = typeof left === "number" ? left : Number(left)
      const rNum = typeof right === "number" ? right : Number(right)

      switch (ast.op) {
        case "+":
          return isNaN(lNum) || isNaN(rNum) ? "#ERR" : lNum + rNum
        case "-":
          return isNaN(lNum) || isNaN(rNum) ? "#ERR" : lNum - rNum
        case "*":
          return isNaN(lNum) || isNaN(rNum) ? "#ERR" : lNum * rNum
        case "/":
          if (isNaN(lNum) || isNaN(rNum)) return "#ERR"
          if (rNum === 0) return "#DIV/0"
          return lNum / rNum
        case ">":
          return (lNum > rNum ? 1 : 0)
        case ">=":
          return (lNum >= rNum ? 1 : 0)
        case "<":
          return (lNum < rNum ? 1 : 0)
        case "<=":
          return (lNum <= rNum ? 1 : 0)
        case "=":
          return (left === right ? 1 : 0)
        case "!=":
          return (left !== right ? 1 : 0)
        default:
          return "#ERR"
      }
    }

    case "call":
      return evaluateFunc(ast.func, ast.args, getValue)
  }
}

function resolveRangeValues(
  args: ASTNode[],
  getValue: CellValueGetter
): (number | string)[] {
  const values: (number | string)[] = []
  for (const arg of args) {
    if (arg.kind === "range") {
      const refs = parseRange(arg.range)
      if (refs) {
        for (const ref of refs) {
          values.push(getValue(ref.col, ref.row))
        }
      }
    } else {
      values.push(evaluateAST(arg, getValue))
    }
  }
  return values
}

function toNumbers(values: (number | string)[]): number[] | string {
  const nums: number[] = []
  for (const v of values) {
    if (typeof v === "string") {
      if (v.startsWith("#")) return v // propagate error
      if (v === "") continue // skip empty
      const n = Number(v)
      if (isNaN(n)) return "#ERR"
      nums.push(n)
    } else {
      nums.push(v)
    }
  }
  return nums
}

function evaluateFunc(
  name: string,
  args: ASTNode[],
  getValue: CellValueGetter
): number | string {
  switch (name) {
    case "SUM": {
      const values = resolveRangeValues(args, getValue)
      const nums = toNumbers(values)
      if (typeof nums === "string") return nums
      return nums.reduce((a, b) => a + b, 0)
    }

    case "AVG": {
      const values = resolveRangeValues(args, getValue)
      const nums = toNumbers(values)
      if (typeof nums === "string") return nums
      if (nums.length === 0) return "#DIV/0"
      return nums.reduce((a, b) => a + b, 0) / nums.length
    }

    case "MIN": {
      const values = resolveRangeValues(args, getValue)
      const nums = toNumbers(values)
      if (typeof nums === "string") return nums
      if (nums.length === 0) return "#ERR"
      return Math.min(...nums)
    }

    case "MAX": {
      const values = resolveRangeValues(args, getValue)
      const nums = toNumbers(values)
      if (typeof nums === "string") return nums
      if (nums.length === 0) return "#ERR"
      return Math.max(...nums)
    }

    case "COUNT": {
      const values = resolveRangeValues(args, getValue)
      return values.filter(v => {
        if (typeof v === "string") return v !== ""
        return true
      }).length
    }

    case "IF": {
      if (args.length < 2 || args.length > 3) return "#ERR"
      const cond = evaluateAST(args[0], getValue)
      if (typeof cond === "string" && cond.startsWith("#")) return cond
      const truthy =
        typeof cond === "number" ? cond !== 0 : cond !== "" && cond !== "0"
      if (truthy) {
        return evaluateAST(args[1], getValue)
      }
      return args.length >= 3 ? evaluateAST(args[2], getValue) : 0
    }

    case "ROUND": {
      if (args.length !== 2) return "#ERR"
      const n = evaluateAST(args[0], getValue)
      const d = evaluateAST(args[1], getValue)
      if (typeof n === "string") return n.startsWith("#") ? n : "#ERR"
      if (typeof d === "string") return d.startsWith("#") ? d : "#ERR"
      const factor = Math.pow(10, d)
      return Math.round(n * factor) / factor
    }

    default:
      return "#ERR"
  }
}

// ─── Table formula evaluation ───────────────────────────

type CellInfo = {
  col: number
  row: number
  text: string
  formula: string | null
  pos: number
  nodeSize: number
}

function collectTableCells(
  tableNode: Node,
  tablePos: number,
  schema: Schema
): { cells: CellInfo[]; totalCols: number; totalRows: number } {
  const cells: CellInfo[] = []
  let totalRows = 0
  let totalCols = 0

  tableNode.content.forEach((row, rowOffset) => {
    const rowPos = tablePos + 1 + rowOffset
    let colIdx = 0

    row.content.forEach((cell, cellOffset) => {
      const cellPos = rowPos + 1 + cellOffset
      cells.push({
        col: colIdx,
        row: totalRows,
        text: getCellText(cell),
        formula: cell.attrs.formula ?? null,
        pos: cellPos,
        nodeSize: cell.nodeSize,
      })
      colIdx += cell.attrs.colspan ?? 1
    })

    totalCols = Math.max(totalCols, colIdx)
    totalRows++
  })

  return { cells, totalCols, totalRows }
}

function evaluateTableFormulas(
  cells: CellInfo[],
  dataRowStart: number,
  dataColStart: number
): Map<string, number | string> {
  // Build cell map
  const cellMap = new Map<string, CellInfo>()
  for (const cell of cells) {
    cellMap.set(`${cell.col},${cell.row}`, cell)
  }

  // Find formula cells and parse their ASTs
  const formulaCells: { cell: CellInfo; ast: ASTNode }[] = []
  for (const cell of cells) {
    if (!cell.formula) continue
    try {
      let ast = parseFormula(cell.formula)
      // Expand ABOVE/LEFT relative references to concrete ranges
      ast = expandRelatives(ast, cell.row, cell.col, dataRowStart, dataColStart)
      formulaCells.push({ cell, ast })
    } catch {
      // Parse error — will show #ERR
    }
  }

  // Build dependency graph and detect cycles
  const results = new Map<string, number | string>()
  const evaluating = new Set<string>()
  const evaluated = new Set<string>()

  function getCellValue(col: number, row: number): number | string {
    const key = `${col},${row}`
    if (results.has(key)) return results.get(key)!

    const cell = cellMap.get(key)
    if (!cell) return 0

    if (!cell.formula) {
      // Non-formula cell: return text as number or string
      const text = cell.text.trim()
      if (text === "") return 0
      const n = Number(text)
      return isNaN(n) ? text : n
    }

    // Circular reference detection
    if (evaluating.has(key)) return "#CIRC"
    if (evaluated.has(key)) return results.get(key) ?? "#ERR"

    evaluating.add(key)

    const fc = formulaCells.find(f => f.cell === cell)
    if (!fc) {
      evaluating.delete(key)
      evaluated.add(key)
      results.set(key, "#ERR")
      return "#ERR"
    }

    let result: number | string
    try {
      result = evaluateAST(fc.ast, getCellValue)
    } catch {
      result = "#ERR"
    }

    evaluating.delete(key)
    evaluated.add(key)
    results.set(key, result)
    return result
  }

  // Evaluate all formula cells
  for (const { cell } of formulaCells) {
    const key = `${cell.col},${cell.row}`
    if (!evaluated.has(key)) {
      getCellValue(cell.col, cell.row)
    }
  }

  return results
}

// ─── Result formatting ──────────────────────────────────

function formatResult(result: number | string): string {
  if (typeof result === "number") {
    if (Number.isInteger(result)) return String(result)
    return result
      .toFixed(4)
      .replace(/0+$/, "")
      .replace(/\.$/, "")
  }
  return String(result)
}

function isErrorResult(result: number | string): boolean {
  return typeof result === "string" && result.startsWith("#")
}

// ─── Header boundary helpers ─────────────────────────────

function getDataBoundaries(headers: string): { dataRowStart: number; dataColStart: number } {
  // Parse NrMc header syntax: "1r" → 1 row, "2r1c" → 2 rows + 1 col, etc.
  const m = headers.match(/^(\d+)r(?:(\d+)c)?$/)
  if (m) {
    return { dataRowStart: parseInt(m[1]), dataColStart: m[2] ? parseInt(m[2]) : 0 }
  }
  const m2 = headers.match(/^(\d+)c$/)
  if (m2) {
    return { dataRowStart: 0, dataColStart: parseInt(m2[1]) }
  }
  if (headers === "none") {
    return { dataRowStart: 0, dataColStart: 0 }
  }
  // Legacy fallback
  switch (headers) {
    case "1st_row": return { dataRowStart: 1, dataColStart: 0 }
    case "2_rows": return { dataRowStart: 2, dataColStart: 0 }
    case "1st_col": return { dataRowStart: 0, dataColStart: 1 }
    case "2_cols": return { dataRowStart: 0, dataColStart: 2 }
    case "both": return { dataRowStart: 1, dataColStart: 1 }
    default: return { dataRowStart: 1, dataColStart: 0 }
  }
}

// ─── Formula sync logic ──────────────────────────────────
//
// Manages formula_display inline atom nodes inside formula cells.
// - Cells with formula attr: paragraph contains exactly one formula_display atom
// - Cells without formula attr: no formula_display atoms

function runFormulaSync(state: any, schema: Schema): Transaction | null {
  let tr: Transaction | null = null

  state.doc.descendants((tableNode: Node, tablePos: number) => {
    if (tableNode.type !== schema.nodes.table) return true

    const { cells } = collectTableCells(tableNode, tablePos, schema)
    const hasFormulas = cells.some(c => c.formula != null)

    const { dataRowStart, dataColStart } = hasFormulas
      ? getDataBoundaries(tableNode.attrs.headers ?? "1st_row")
      : { dataRowStart: 0, dataColStart: 0 }

    const results = hasFormulas
      ? evaluateTableFormulas(cells, dataRowStart, dataColStart)
      : new Map<string, number | string>()

    for (const cell of cells) {
      const cellNode = state.doc.nodeAt(cell.pos)
      if (!cellNode || cellNode.childCount === 0) continue

      const para = cellNode.child(0)
      const paraContentStart = cell.pos + 2 // cell open + para open
      const firstChild = para.childCount > 0 ? para.child(0) : null
      const hasDisplay = firstChild?.type === schema.nodes.formula_display

      if (cell.formula != null) {
        const key = `${cell.col},${cell.row}`
        const result = formatResult(results.get(key) ?? "#ERR")

        if (hasDisplay && para.childCount === 1 && firstChild!.attrs.result === result) {
          continue // already correct
        }

        if (!tr) tr = state.tr
        const displayAtom = schema.nodes.formula_display.create({ result })
        const mappedStart = tr.mapping.map(paraContentStart)
        const mappedEnd = tr.mapping.map(paraContentStart + para.content.size)
        tr.replaceWith(mappedStart, mappedEnd, displayAtom)
      } else {
        // No formula: remove formula_display if present
        if (hasDisplay) {
          if (!tr) tr = state.tr
          const mappedStart = tr.mapping.map(paraContentStart)
          const mappedEnd = tr.mapping.map(paraContentStart + para.content.size)
          tr.delete(mappedStart, mappedEnd)
        }
      }
    }

    return false
  })

  return tr
}

// ─── Formula sync plugin ─────────────────────────────────

export function formulaSyncPlugin(schema: Schema): PMPlugin {
  return new PMPlugin({
    view(editorView) {
      // Run initial sync on mount — appendTransaction doesn't fire
      // until the first user transaction, so formulas would be blank.
      requestAnimationFrame(() => {
        const tr = runFormulaSync(editorView.state, schema)
        if (tr) editorView.dispatch(tr)
      })
      return {}
    },
    appendTransaction(_transactions, _oldState, newState) {
      return runFormulaSync(newState, schema)
    },
  })
}

// ─── Formula color decoration plugin ────────────────────
//
// Applies column color rules to formula cells based on their
// computed results (read from formula_display atom attrs).

export function formulaColorPlugin(schema: Schema): PMPlugin {
  return new PMPlugin({
    props: {
      decorations(state) {
        const decos: Decoration[] = []

        state.doc.descendants((tableNode, tablePos) => {
          if (tableNode.type !== schema.nodes.table) return true

          const columns: ColumnProps | null = tableNode.attrs.columns
          if (!columns) return false

          tableNode.content.forEach((row, rowOffset) => {
            const rowPos = tablePos + 1 + rowOffset
            let colIdx = 0

            row.content.forEach((cell, cellOffset) => {
              const cellPos = rowPos + 1 + cellOffset

              if (cell.attrs.formula != null) {
                const para = cell.child(0)
                if (para.childCount > 0 && para.child(0).type === schema.nodes.formula_display) {
                  const result = String(para.child(0).attrs.result ?? "")
                  const isError = result.startsWith("#")

                  if (!isError && !cell.attrs.cellColor) {
                    const colProps = resolveColumnProps(columns, colIdx + 1)
                    if (colProps?.color) {
                      const rules = parseColorRules(colProps.color)
                      const bg = evaluateColorRules(rules, result)
                      if (bg) {
                        decos.push(
                          Decoration.node(cellPos, cellPos + cell.nodeSize, {
                            style: `background: ${bg}; `,
                          })
                        )
                      }
                    }
                  }
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
