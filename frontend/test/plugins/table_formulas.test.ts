// table_formulas.ts exports `parseFormula` — a pure AST parser for cell
// formulas. Formula cells themselves live on table cells as a `formula`
// attr and round-trip through table.ts (covered in the tables corpus);
// this file pins the parser and its interaction invariants.
import { describe, it, expect } from "vitest"
import { parseFormula, shiftFormula } from "../../plugins/table_formulas"
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

// Excel-style paste shift. shiftFormula operates on the formula STRING —
// the copy/paste path in table.ts records the source cell's visual row/col
// on copy, then hands the delta (dstRow-srcRow, dstCol-srcCol) here on
// paste. Every ref that isn't pinned with $ moves by the delta; refs off
// the grid become #REF so a broken paste is visible.
describe("shiftFormula — Excel-style copy/paste shift", () => {
  it("no-op when delta is zero", () => {
    expect(shiftFormula("A1+B2", 0, 0)).toBe("A1+B2")
  })

  it("shifts a single ref one row down", () => {
    // Copy =A1 from row 1, paste into row 2 → =A2.
    expect(shiftFormula("A1", 1, 0)).toBe("A2")
  })

  it("shifts a single ref one column right", () => {
    // Copy =A1 from col A, paste into col B → =B1.
    expect(shiftFormula("A1", 0, 1)).toBe("B1")
  })

  it("shifts every ref in a product", () => {
    // The scenario the user described: multiply a cell by the one two to
    // its left, in row 3; paste to row 4, expect all refs shifted down.
    expect(shiftFormula("B3*A3", 1, 0)).toBe("B4*A4")
  })

  it("shifts range endpoints independently", () => {
    // =SUM(A1:C3) pasted 2 rows down + 1 col right becomes =SUM(B3:D5).
    expect(shiftFormula("SUM(A1:C3)", 2, 1)).toBe("SUM(B3:D5)")
  })

  it("$ pins the column: $A1 stays column A", () => {
    // The $ on the col disables that axis of the shift.
    expect(shiftFormula("$A1", 1, 5)).toBe("$A2")
  })

  it("$ pins the row: A$1 stays row 1", () => {
    expect(shiftFormula("A$1", 3, 4)).toBe("E$1")
  })

  it("$A$1 is fully pinned — nothing moves", () => {
    expect(shiftFormula("$A$1+B2", 4, 4)).toBe("$A$1+F6")
  })

  it("mixed refs shift independently — classic total column pattern", () => {
    // Sum from a fixed anchor to the current row: =SUM($A$1:A5)
    // Paste one row down → =SUM($A$1:A6).
    expect(shiftFormula("SUM($A$1:A5)", 1, 0)).toBe("SUM($A$1:A6)")
  })

  it("returns #REF when a ref would land off the grid (negative col)", () => {
    // A2 shifted left one col → column index -1, no such letter.
    expect(shiftFormula("A2+B2", 0, -1)).toContain("#REF")
  })

  it("returns #REF when a ref would land off the grid (row < 1)", () => {
    expect(shiftFormula("A2", -3, 0)).toBe("#REF")
  })

  it("leaves LEFT / ABOVE alone (they are position-relative already)", () => {
    // These keywords expand to a range centered on the CURRENT cell at
    // eval time, so they don't need a copy-time rewrite.
    expect(shiftFormula("SUM(LEFT)", 5, 5)).toBe("SUM(LEFT)")
    expect(shiftFormula("SUM(ABOVE)+B1", 2, 0)).toBe("SUM(ABOVE)+B3")
  })

  it("multi-letter columns shift correctly across the Z boundary", () => {
    // Z1 shifted +1 col → AA1.
    expect(shiftFormula("Z1", 0, 1)).toBe("AA1")
    // AA1 shifted -1 col → Z1.
    expect(shiftFormula("AA1", 0, -1)).toBe("Z1")
  })

  it("$A$1-style ref still parses and evaluates", () => {
    // The parser must accept $ prefixes without choking; shiftFormula
    // relies on the tokenizer/regex both recognizing them.
    const ast = parseFormula("$A$1+B2")
    expect(ast).toBeDefined()
    expect(ast.kind).toBe("binop")
  })
})
