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

describe("HTML entities are NOT interpreted (dialect invariant)", () => {
  // The dialect promises HTML entities stay literal — authors use UTF-8
  // directly, and a source that contains `&amp;` means the five characters
  // `&`, `a`, `m`, `p`, `;`. Enforced by disabling markdown-it's `entity`
  // inline rule.
  it("&amp; stays literal", () => {
    const rt = roundTrip("Fish &amp; chips\n")
    expect(rt.isStable).toBe(true)
    let text = ""
    rt.doc.descendants(n => {
      if (n.isText) text += n.text
    })
    expect(text).toContain("&amp;")
  })

  it("&lt; and &gt; stay literal", () => {
    const rt = roundTrip("Compare &lt; and &gt;\n")
    expect(rt.isStable).toBe(true)
    let text = ""
    rt.doc.descendants(n => {
      if (n.isText) text += n.text
    })
    expect(text).toContain("&lt;")
    expect(text).toContain("&gt;")
  })

  it("numeric entity &#123; stays literal", () => {
    const rt = roundTrip("The escape sequence &#123; is inert\n")
    expect(rt.isStable).toBe(true)
    let text = ""
    rt.doc.descendants(n => {
      if (n.isText) text += n.text
    })
    expect(text).toContain("&#123;")
  })

  it("hex entity &#x2603; stays literal (would have been ☃ if decoded)", () => {
    const rt = roundTrip("Snowman escape: &#x2603;\n")
    expect(rt.isStable).toBe(true)
    let text = ""
    rt.doc.descendants(n => {
      if (n.isText) text += n.text
    })
    expect(text).toContain("&#x2603;")
    expect(text).not.toContain("☃")
  })

  it("entity in a table cell stays literal", () => {
    const rt = roundTrip("| head |\n| --- |\n| Fish &amp; chips |\n")
    expect(rt.isStable).toBe(true)
    let text = ""
    rt.doc.descendants(n => {
      if (n.isText) text += n.text
    })
    expect(text).toContain("&amp;")
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
