import { Node, Schema } from "prosemirror-model"
import { Plugin as PMPlugin } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"
import {
  getCellText,
  evaluateColorRules,
  parseColorRules,
  type ColumnProps,
  type ColorRule,
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
  cells: CellInfo[]
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
      const ast = parseFormula(cell.formula)
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

// ─── Decoration plugin ──────────────────────────────────

export function formulaDecoPlugin(schema: Schema): PMPlugin {
  return new PMPlugin({
    props: {
      decorations(state) {
        const decos: Decoration[] = []

        state.doc.descendants((node, pos) => {
          if (node.type !== schema.nodes.table) return true

          const { cells } = collectTableCells(node, pos, schema)
          const hasFormulas = cells.some(c => c.formula)
          if (!hasFormulas) return false

          const results = evaluateTableFormulas(cells)

          // Column color rules for applying to computed results
          const columns: ColumnProps | null = node.attrs.columns
          const colorRules: Record<string, ColorRule[]> = {}
          if (columns) {
            for (const [colKey, props] of Object.entries(columns)) {
              if (props.color) {
                colorRules[colKey] = parseColorRules(props.color)
              }
            }
          }

          for (const cell of cells) {
            if (!cell.formula) continue
            const key = `${cell.col},${cell.row}`
            const result = results.get(key)
            if (result === undefined) continue

            const resultStr = formatResult(result)
            const isError = isErrorResult(result)

            // Widget showing the computed result, placed before the
            // (hidden) paragraph inside the cell
            const widget = Decoration.widget(
              cell.pos + 1,
              () => {
                const span = document.createElement("span")
                span.className = isError
                  ? "formula-display formula-display-error"
                  : "formula-display"
                span.textContent = resultStr
                span.contentEditable = "false"
                return span
              },
              { side: -1, key: `formula-${cell.pos}` }
            )
            decos.push(widget)

            // Apply column color to formula cell based on computed result
            const colKey = String(cell.col + 1)
            if (colorRules[colKey] && !isError) {
              const bg = evaluateColorRules(
                colorRules[colKey],
                resultStr
              )
              if (bg) {
                decos.push(
                  Decoration.node(
                    cell.pos,
                    cell.pos + cell.nodeSize,
                    { style: `background: ${bg}; ` }
                  )
                )
              }
            }
          }

          return false
        })

        return DecorationSet.create(state.doc, decos)
      },
    },
  })
}
