// Tables: pipe syntax, directives, cell attributes, formulas, merging,
// backticks-protection.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes, assertStrongMark, assertUnderlineMark, assertCodeMark } from "../helpers"

describe("basic tables", () => {
  it("2x2 pipe table", () => {
    const rt = roundTrip("| a | b |\n| --- | --- |\n| c | d |\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "table")).toBe(1)
    expect(countNodes(rt.doc, "table_row")).toBe(2)
    expect(countNodes(rt.doc, "table_header")).toBe(2)
    expect(countNodes(rt.doc, "table_cell")).toBe(2)
  })

  it("single-row header-only table (with data row)", () => {
    const rt = roundTrip("| only |\n| --- |\n| body |\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "table")).toBe(1)
  })

  it("wider table with 3 columns and 2 body rows", () => {
    const rt = roundTrip("| A | B | C |\n| --- | --- | --- |\n| 1 | 2 | 3 |\n| 4 | 5 | 6 |\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "table_row")).toBe(3)
  })

  it("cells with inline marks", () => {
    const rt = roundTrip("| head |\n| --- |\n| **bold** cell |\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "bold")
  })
})

describe("cell directives", () => {
  it("cell color {color=X}", () => {
    const rt = roundTrip("| head |\n| --- |\n| {color=yellow}shaded |\n")
    expect(rt.isStable).toBe(true)
    // The directive should be absorbed into cell attrs.
    let hasColor = false
    rt.doc.descendants((n) => {
      if (n.type.name === "table_cell" && n.attrs.cellColor === "yellow") hasColor = true
    })
    expect(hasColor).toBe(true)
  })

  it("cell text-color", () => {
    const rt = roundTrip("| head |\n| --- |\n| {text-color=red}alert |\n")
    expect(rt.isStable).toBe(true)
  })

  it("cell alignment", () => {
    const rt = roundTrip("| head |\n| --- |\n| {align=center}middle |\n")
    expect(rt.isStable).toBe(true)
  })

  it("cell vertical align", () => {
    const rt = roundTrip("| head |\n| --- |\n| {valign=bottom}sink |\n")
    expect(rt.isStable).toBe(true)
  })

  it("cell vertical text", () => {
    const rt = roundTrip("| head |\n| --- |\n| {vtext=vertical}rot |\n")
    expect(rt.isStable).toBe(true)
  })

  it("multiple cell directive keys combined", () => {
    const rt = roundTrip("| head |\n| --- |\n| {color=green align=right}text |\n")
    expect(rt.isStable).toBe(true)
  })
})

describe("table directive line", () => {
  it("{table headers=none}", () => {
    const rt = roundTrip("{table headers=none}\n| a | b |\n| --- | --- |\n| c | d |\n")
    expect(rt.isStable).toBe(true)
    let hv = ""
    rt.doc.descendants((n) => {
      if (n.type.name === "table") hv = n.attrs.headers
    })
    expect(hv).toBe("none")
  })

  it("{table caption=...}", () => {
    const rt = roundTrip('{table caption="My caption"}\n| a | b |\n| --- | --- |\n| c | d |\n')
    expect(rt.isStable).toBe(true)
  })
})

describe("formulas", () => {
  it("simple formula cell =2+3", () => {
    const rt = roundTrip("| a | b |\n| --- | --- |\n| 2 | =2+3 |\n")
    expect(rt.isStable).toBe(true)
    let hasFormula = false
    rt.doc.descendants((n) => {
      if (n.type.name === "table_cell" && n.attrs.formula) hasFormula = true
    })
    expect(hasFormula).toBe(true)
  })

  it("formula referencing a range =SUM(A1:A2)", () => {
    const rt = roundTrip("| n |\n| --- |\n| 1 |\n| 2 |\n| =SUM(A1:A2) |\n")
    expect(rt.isStable).toBe(true)
    let hasFormula = false
    rt.doc.descendants((n) => {
      if (n.type.name === "table_cell" && n.attrs.formula) hasFormula = true
    })
    expect(hasFormula).toBe(true)
  })

  it("formula with a *  (regression: formulas containing * were truncated)", () => {
    const rt = roundTrip("| a | b |\n| --- | --- |\n| 3 | =A1*2 |\n")
    expect(rt.isStable).toBe(true)
    let formula = ""
    rt.doc.descendants((n) => {
      if (n.type.name === "table_cell" && n.attrs.formula) formula = n.attrs.formula
    })
    expect(formula).toBe("A1*2")
  })
})

describe("backticks protect cells from directive/formula parsing", () => {
  it("backtick-wrapped `=formula` stays literal", () => {
    const rt = roundTrip("| a |\n| --- |\n| `=A1*2` |\n")
    expect(rt.isStable).toBe(true)
    // Must NOT have been turned into a formula attribute.
    let hasFormula = false
    rt.doc.descendants((n) => {
      if (n.type.name === "table_cell" && n.attrs.formula) hasFormula = true
    })
    expect(hasFormula).toBe(false)
    assertCodeMark(rt.doc, "=A1*2")
  })

  it("backtick-wrapped `{directive}` stays literal", () => {
    const rt = roundTrip("| a |\n| --- |\n| `{color=red}` |\n")
    expect(rt.isStable).toBe(true)
    // Cell color must not have been set from the code-span content.
    let hasColor = false
    rt.doc.descendants((n) => {
      if (n.type.name === "table_cell" && n.attrs.cellColor) hasColor = true
    })
    expect(hasColor).toBe(false)
    assertCodeMark(rt.doc, "{color=red}")
  })
})

describe("marks inside cells", () => {
  it("underline in header cell", () => {
    const rt = roundTrip("| _title_ |\n| --- |\n| body |\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "title")
  })

  it("bold in body cell", () => {
    const rt = roundTrip("| head |\n| --- |\n| **strong** cell |\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "strong")
  })
})
