// highlight mark — ==text== and ==={color=…}text== syntax.
// Rendered as <mark data-highlight="…">.
import { describe, it, expect } from "vitest"
import { roundTrip, assertHighlightMark, countNodes } from "../helpers"

describe("highlight: basic ==text== syntax", () => {
  it("==word== round-trips with highlight mark", () => {
    const rt = roundTrip("this is ==highlighted== text\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "highlighted")
  })

  it("==multi word== round-trips", () => {
    const rt = roundTrip("some ==multi word== span\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "multi word")
  })

  it("adjacent highlights are separate marks", () => {
    const rt = roundTrip("==one== ==two==\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "one")
    assertHighlightMark(rt.doc, "two")
  })

  it("highlight at the very start of a paragraph works", () => {
    const rt = roundTrip("==first== word\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "first")
  })

  it("highlight at the very end works", () => {
    const rt = roundTrip("last ==highlighted==\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "highlighted")
  })
})

describe("highlight: {color=…} attribute", () => {
  it("=={color=yellow}text== preserves the color attr", () => {
    const rt = roundTrip("=={color=yellow}text==\n")
    expect(rt.isStable).toBe(true)
    let color: string | null = null
    rt.doc.descendants((n) => {
      if (!n.isText) return
      for (const m of n.marks) {
        if (m.type.name === "highlight") color = String(m.attrs.color ?? "")
      }
    })
    expect(color).toBe("yellow")
  })

  it("=={color=#ff0000}text== accepts hex colors", () => {
    const rt = roundTrip("=={color=#ff0000}red==\n")
    expect(rt.isStable).toBe(true)
    let color: string | null = null
    rt.doc.descendants((n) => {
      if (!n.isText) return
      for (const m of n.marks) {
        if (m.type.name === "highlight") color = String(m.attrs.color ?? "")
      }
    })
    expect(color).toBe("#ff0000")
  })

  it("highlights of different colors coexist", () => {
    const rt = roundTrip("=={color=yellow}A== =={color=pink}B==\n")
    expect(rt.isStable).toBe(true)
    const colorsByText = new Map<string, string>()
    rt.doc.descendants((n) => {
      if (!n.isText) return
      for (const m of n.marks) {
        if (m.type.name === "highlight" && n.text) {
          colorsByText.set(n.text, String(m.attrs.color ?? ""))
        }
      }
    })
    expect(colorsByText.get("A")).toBe("yellow")
    expect(colorsByText.get("B")).toBe("pink")
  })
})

describe("highlight: invariants", () => {
  it("== inside a code fence stays literal (no highlight mark)", () => {
    const rt = roundTrip("```\n==not a highlight==\n```\n")
    expect(rt.isStable).toBe(true)
    let sawHighlight = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "highlight") sawHighlight = true
      }
    })
    expect(sawHighlight).toBe(false)
  })

  it("== inside inline code stays literal", () => {
    const rt = roundTrip("`==not a highlight==`\n")
    expect(rt.isStable).toBe(true)
    let sawHighlight = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "highlight") sawHighlight = true
      }
    })
    expect(sawHighlight).toBe(false)
  })

  it("== inside a backtick-wrapped table cell stays literal", () => {
    const src = "| head |\n| --- |\n| `==literal==` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    let sawHighlight = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "highlight") sawHighlight = true
      }
    })
    expect(sawHighlight).toBe(false)
  })

  it("highlight combined with other marks (nested marks)", () => {
    // Highlight around bold — both must round-trip.
    const rt = roundTrip("==**bold-inside-highlight**==\n")
    expect(rt.isStable).toBe(true)
    let sawBoth = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        const names = n.marks.map((m) => m.type.name)
        if (names.includes("highlight") && names.includes("strong")) sawBoth = true
      }
    })
    expect(sawBoth).toBe(true)
  })

  it("no false-positive from == inside a URL query string", () => {
    const rt = roundTrip("Link: [](https://example.com/?x=1&y==2)\n")
    expect(rt.isStable).toBe(true)
    let sawHighlight = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "highlight") sawHighlight = true
      }
    })
    expect(sawHighlight).toBe(false)
  })

  it("highlight in a table cell (unprotected) opens the mark", () => {
    const src = "| head |\n| --- |\n| ==highlighted cell== |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "highlighted cell")
  })

  it("highlight count matches source occurrences", () => {
    const rt = roundTrip("Some ==a==, ==b==, and ==c== in one line.\n")
    expect(rt.isStable).toBe(true)
    let count = 0
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "highlight") count++
      }
    })
    expect(count).toBe(3)
  })
})
