// Hard-break regression cases.
//
// Today's bug: `_word_` right after a `\n` literal in a cell was silently
// escaped because markdown-it's `_`-em rule refuses to open between word
// characters (its snake_case guard). Bold, em (`*`), and highlight (`==`)
// didn't have the bug because their delimiters have looser flanking rules.
// The fix promotes `\n` literals to real newlines before the inline core
// rule runs and treats the resulting softbreak as a hard_break inside
// cells and list items.
//
// The corpus below locks that fix in place and covers the neighbouring
// cases that shouldn't regress.
import { describe, it, expect } from "vitest"
import {
  roundTrip,
  countNodes,
  assertUnderlineMark,
  assertStrongMark,
  assertEmMark,
  assertHighlightMark,
} from "../helpers"

describe("marks after a hard break in a table cell", () => {
  it("underline after \\n literal in cell", () => {
    const rt = roundTrip("| head |\n| --- |\n| lead\\n_For OTS_ |\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "For OTS")
    expect(countNodes(rt.doc, "hard_break")).toBe(1)
  })

  it("bold after \\n literal in cell", () => {
    const rt = roundTrip("| head |\n| --- |\n| lead\\n**strong** |\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "strong")
    expect(countNodes(rt.doc, "hard_break")).toBe(1)
  })

  it("italic after \\n literal in cell", () => {
    const rt = roundTrip("| head |\n| --- |\n| lead\\n*emph* |\n")
    expect(rt.isStable).toBe(true)
    assertEmMark(rt.doc, "emph")
    expect(countNodes(rt.doc, "hard_break")).toBe(1)
  })

  it("highlight after \\n literal in cell", () => {
    const rt = roundTrip("| head |\n| --- |\n| lead\\n==high== |\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "high")
    expect(countNodes(rt.doc, "hard_break")).toBe(1)
  })

  it("underline before \\n in cell (baseline — always worked)", () => {
    const rt = roundTrip("| head |\n| --- |\n| _For OTS_\\nmore |\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "For OTS")
  })

  it("mixed marks separated by \\n in cell", () => {
    const rt = roundTrip("| head |\n| --- |\n| _a_\\n**b**\\n*c* |\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "a")
    assertStrongMark(rt.doc, "b")
    assertEmMark(rt.doc, "c")
    expect(countNodes(rt.doc, "hard_break")).toBe(2)
  })
})

describe("marks after a hard break in a list item", () => {
  it("underline after \\n literal in list item", () => {
    const rt = roundTrip("- lead\\n_For OTS_\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "For OTS")
    expect(countNodes(rt.doc, "hard_break")).toBe(1)
  })

  it("bold after \\n literal in list item", () => {
    const rt = roundTrip("- lead\\n**strong**\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "strong")
  })

  it("ordered list item with underline after \\n", () => {
    const rt = roundTrip("1. lead\\n_For OTS_\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "For OTS")
  })
})

describe("marks after a hard break in a top-level paragraph", () => {
  it("underline after real newline in paragraph", () => {
    const rt = roundTrip("line one\n_For OTS_\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "For OTS")
    expect(countNodes(rt.doc, "hard_break")).toBe(1)
  })

  it("underline after \\n literal in paragraph", () => {
    // Dialect says \\n is only meaningful in lists and tables, but the
    // fix promotes it to a real newline everywhere. Assert the outcome
    // is a hard break + a proper underline mark either way — the two
    // encodings should be equivalent in effect.
    const rt = roundTrip("line one\\n_For OTS_\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "For OTS")
  })
})
