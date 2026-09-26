// Small-nasty cases: escaping, unicode, entities, empty structures,
// images with sizes.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes, assertUnderlineMark } from "../helpers"

describe("escaping", () => {
  it("backslash-escaped underscore stays literal", () => {
    const rt = roundTrip("plain \\_underscored\\_ text\n")
    expect(rt.isStable).toBe(true)
    // The underscores are escaped — no underline mark should exist.
    let found = false
    rt.doc.descendants(n => {
      if (n.isText && n.marks.some(m => m.type.name === "underline")) found = true
    })
    expect(found).toBe(false)
  })

  it("literal curly braces via backslash", () => {
    const rt = roundTrip("Use \\{brace\\} literally.\n")
    expect(rt.isStable).toBe(true)
  })

  it("literal asterisk", () => {
    const rt = roundTrip("2 \\* 3 equals 6.\n")
    expect(rt.isStable).toBe(true)
  })
})

describe("unicode", () => {
  it("French characters in body text", () => {
    const rt = roundTrip("Élève naïve étude\n")
    expect(rt.isStable).toBe(true)
    let found = false
    rt.doc.descendants(n => {
      if (n.isText && n.text?.includes("Élève")) found = true
    })
    expect(found).toBe(true)
  })

  it("French characters inside underline", () => {
    const rt = roundTrip("Ceci est _à revoir_ absolument\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "à revoir")
  })

  it("French characters in a cell", () => {
    const rt = roundTrip("| clé |\n| --- |\n| _à revoir_ |\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "à revoir")
  })

  it("French unicode in a directive value", () => {
    // Directive key=value parsing must accept Unicode letters (\p{L}).
    const rt = roundTrip("{tag values=français,électronique}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "tag")).toBe(1)
  })

  it("emoji in body text (BMP + supplementary)", () => {
    const rt = roundTrip("Hi \u{1F44B} world ☀\n")
    expect(rt.isStable).toBe(true)
  })
})

describe("HTML entities NOT interpreted (dialect promises this, currently PARTIAL)", () => {
  // The dialect spec says HTML entities must not be interpreted (use UTF-8
  // directly). The current parser inherits markdown-it's default behaviour
  // and decodes them, so `&amp;` becomes `&`. `CLAUDE.md` marks this
  // "partial". Skipped tests are ready to un-skip the day the dialect is
  // brought into full compliance.
  it.skip("&amp; stays literal", () => {
    const rt = roundTrip("Fish &amp; chips\n")
    expect(rt.isStable).toBe(true)
    let text = ""
    rt.doc.descendants(n => {
      if (n.isText) text += n.text
    })
    expect(text).toContain("&amp;")
  })

  it.skip("&lt; stays literal", () => {
    const rt = roundTrip("Compare &lt; and &gt;\n")
    expect(rt.isStable).toBe(true)
    let text = ""
    rt.doc.descendants(n => {
      if (n.isText) text += n.text
    })
    expect(text).toContain("&lt;")
  })

  // The one thing we CAN assert today: round-trip stability holds even if
  // entities are decoded. So the "&amp;" input serialises to "&", parses
  // to "&", and stays that way. Not spec-correct, but at least stable.
  it("&amp; round-trip is stable (locking in current partial behaviour)", () => {
    const rt = roundTrip("Fish &amp; chips\n")
    expect(rt.isStable).toBe(true)
  })
})

describe("edge cases", () => {
  it("empty document", () => {
    const rt = roundTrip("")
    expect(rt.isStable).toBe(true)
  })

  it("only whitespace", () => {
    const rt = roundTrip("\n\n\n")
    expect(rt.isStable).toBe(true)
  })

  it("single-cell table (one column, one body row)", () => {
    const rt = roundTrip("| head |\n| --- |\n| body |\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "table")).toBe(1)
  })

  it("empty cell in a table", () => {
    const rt = roundTrip("| a | b |\n| --- | --- |\n|  | value |\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "table_cell")).toBe(2)
  })

  it("empty paragraph between two full ones", () => {
    const rt = roundTrip("first\n\n\n\nthird\n")
    expect(rt.isStable).toBe(true)
  })
})

describe("images", () => {
  it("inline image ![alt](path)", () => {
    const rt = roundTrip("Here: ![alt](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "image")).toBe(1)
  })

  it("image with size property", () => {
    const rt = roundTrip("{image size=400px}\n![alt](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    let size = ""
    rt.doc.descendants(n => {
      if (n.type.name === "image") size = n.attrs.size ?? ""
    })
    expect(size).toBe("400px")
  })

  it("image with caption + label", () => {
    const rt = roundTrip(
      "{image caption=\"A picture\" label=fig1}\n![alt](/media/pic.png)\n",
    )
    expect(rt.isStable).toBe(true)
  })
})
