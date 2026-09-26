// table_formulas.ts exports `parseFormula` — a pure AST parser for cell
// formulas. Formula cells themselves live on table cells as a `formula`
// attr and round-trip through table.ts (covered in the tables corpus);
// this file pins the parser and its interaction invariants.
import { describe, it, expect } from "vitest"
import { parseFormula } from "../../plugins/table_formulas"
import { roundTrip, countNodes } from "../helpers"

// A tiny AST-walking helper — parseFormula returns a discriminated
// union; we just want to know the top-level kind and (for binops) the
// operator to prove the parse worked.
function shape(ast: any): string {
  if (!ast || typeof ast !== "object") return String(ast)
  switch (ast.kind) {
    case "binop":
      return `binop(${ast.op}, ${shape(ast.left)}, ${shape(ast.right)})`
    case "unary":
      return `unary(${ast.op}, ${shape(ast.operand)})`
    case "call":
      return `call(${ast.func}, [${ast.args.map(shape).join(", ")}])`
    case "ref":
      return `ref(${ast.ref})`
    case "range":
      return `range(${ast.range})`
    case "relative":
      return `relative(${ast.direction})`
    case "number":
      return `num(${ast.value})`
    default:
      return `unknown(${ast.kind ?? "?"})`
  }
}

describe("parseFormula: literals + refs", () => {
  it("integer literal", () => {
    expect(shape(parseFormula("42"))).toBe("num(42)")
  })
  it("float literal", () => {
    expect(shape(parseFormula("3.14"))).toBe("num(3.14)")
  })
  it("single cell ref A1", () => {
    expect(shape(parseFormula("A1"))).toBe("ref(A1)")
  })
  it("range A1:B5", () => {
    expect(shape(parseFormula("A1:B5"))).toBe("range(A1:B5)")
  })
})

describe("parseFormula: binary operators", () => {
  it("A1+B1", () => {
    expect(shape(parseFormula("A1+B1"))).toBe("binop(+, ref(A1), ref(B1))")
  })
  it("A1-B1", () => {
    expect(shape(parseFormula("A1-B1"))).toBe("binop(-, ref(A1), ref(B1))")
  })
  // Regression: the formula-with-* case was a real bug fix in the parser.
  // Pinning here so a regression can never re-introduce the same failure.
  it("A1*B1 (regression: * was mis-tokenised as markdown emphasis)", () => {
    expect(shape(parseFormula("A1*B1"))).toBe("binop(*, ref(A1), ref(B1))")
  })
  it("A1/B1", () => {
    expect(shape(parseFormula("A1/B1"))).toBe("binop(/, ref(A1), ref(B1))")
  })
  it("respects operator precedence: A1+B1*C1", () => {
    expect(shape(parseFormula("A1+B1*C1"))).toBe("binop(+, ref(A1), binop(*, ref(B1), ref(C1)))")
  })
  it("respects parentheses: (A1+B1)*C1", () => {
    expect(shape(parseFormula("(A1+B1)*C1"))).toBe("binop(*, binop(+, ref(A1), ref(B1)), ref(C1))")
  })
})

describe("parseFormula: function calls", () => {
  it("SUM(A1:A5)", () => {
    expect(shape(parseFormula("SUM(A1:A5)"))).toBe("call(SUM, [range(A1:A5)])")
  })
  it("SUM(ABOVE) — relative range placeholder", () => {
    expect(shape(parseFormula("SUM(ABOVE)"))).toBe("call(SUM, [relative(ABOVE)])")
  })
  it("SUM(LEFT) — relative range placeholder", () => {
    expect(shape(parseFormula("SUM(LEFT)"))).toBe("call(SUM, [relative(LEFT)])")
  })
  it("AVG(A1, B1, C1) — multiple args", () => {
    expect(shape(parseFormula("AVG(A1, B1, C1)"))).toBe("call(AVG, [ref(A1), ref(B1), ref(C1)])")
  })
  it("nested calls: MAX(SUM(A1:A5), B1)", () => {
    expect(shape(parseFormula("MAX(SUM(A1:A5), B1)"))).toBe("call(MAX, [call(SUM, [range(A1:A5)]), ref(B1)])")
  })
})

describe("parseFormula: unary", () => {
  it("-A1", () => {
    expect(shape(parseFormula("-A1"))).toBe("unary(-, ref(A1))")
  })
  it("-(A1+B1)", () => {
    expect(shape(parseFormula("-(A1+B1)"))).toBe("unary(-, binop(+, ref(A1), ref(B1)))")
  })
})

describe("parseFormula: error surface", () => {
  it("throws on empty input", () => {
    expect(() => parseFormula("")).toThrow()
  })
  it("throws on unclosed parenthesis", () => {
    expect(() => parseFormula("(A1+B1")).toThrow()
  })
  it("throws on missing operand after operator", () => {
    // "A1+" has an operator with nothing on its right; the parser
    // reaches EOF looking for the RHS and raises.
    expect(() => parseFormula("A1+")).toThrow()
  })
})

describe("formula cells in tables (round-trip via table.ts)", () => {
  // Formula cells serialise as `=expr` inside their cell. Round-trip
  // stability is what matters here; the formula parser is separately
  // tested above.
  it("=A1+B1 in a body cell round-trips as a formula (not literal text)", () => {
    const src = "{table headers=none}\n| a | b | =A1+B1 |\n| --- | --- | --- |\n| 1 | 2 | 3 |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    // formula cells expose `formula` attr on the cell node
    let hasFormula = false
    rt.doc.descendants((n) => {
      if ((n.type.name === "table_cell" || n.type.name === "table_header") && n.attrs.formula) {
        hasFormula = true
      }
    })
    expect(hasFormula).toBe(true)
  })

  it("backtick-protected =formula stays literal in a cell", () => {
    // Wrapping in backticks opts the cell out of every in-cell parser,
    // formulas included — this pins the invariant from CLAUDE.md.
    const src = "| head |\n| --- |\n| `=A1+B1` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    let sawFormulaAttr = false
    rt.doc.descendants((n) => {
      if ((n.type.name === "table_cell" || n.type.name === "table_header") && n.attrs.formula) {
        sawFormulaAttr = true
      }
    })
    expect(sawFormulaAttr).toBe(false)
    // And the doc still contains a table.
    expect(countNodes(rt.doc, "table")).toBe(1)
  })
})
