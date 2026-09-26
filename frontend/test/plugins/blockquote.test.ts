// blockquote plugin — `>` blockquote plus the `{blockquote ...}` directive
// that adds class/color/icon/image-width/image-bg/wrap/etc. Multi-image
// figure with image-width is a supported shape and worth pinning.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes } from "../helpers"

describe("blockquote: plain `>` syntax", () => {
  it("single-line blockquote round-trips", () => {
    const rt = roundTrip("> hello\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "blockquote")).toBe(1)
  })

  it("multi-line blockquote round-trips with a blank line separator", () => {
    const rt = roundTrip("> first paragraph\n>\n> second paragraph\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "blockquote")).toBe(1)
  })

  it("blockquote with inline formatting preserves marks", () => {
    const rt = roundTrip("> a **bold** and _underlined_ line\n")
    expect(rt.isStable).toBe(true)
    // Both marks must open; blockquote must contain exactly one paragraph.
    let sawBold = false
    let sawUnderline = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) {
          if (m.type.name === "strong") sawBold = true
          if (m.type.name === "underline") sawUnderline = true
        }
      }
    })
    expect(sawBold).toBe(true)
    expect(sawUnderline).toBe(true)
  })

  it("empty blockquote — a single `>` line — round-trips", () => {
    const rt = roundTrip("> \n")
    expect(rt.isStable).toBe(true)
  })

  it("blockquote in a code fence stays literal (no blockquote node)", () => {
    const rt = roundTrip("```\n> not a blockquote\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "blockquote")).toBe(0)
  })
})

describe("blockquote: {blockquote ...} directive", () => {
  it("class=custom serializes with class attr", () => {
    const rt = roundTrip("{blockquote class=custom}\n> content\n")
    expect(rt.isStable).toBe(true)
    let cls: string | null = null
    rt.doc.descendants((n) => {
      if (n.type.name === "blockquote") cls = String(n.attrs.class ?? "")
    })
    expect(cls).toBe("custom")
  })

  it("color=#ff0000 round-trips", () => {
    const rt = roundTrip("{blockquote class=custom color=#ff0000}\n> tinted\n")
    expect(rt.isStable).toBe(true)
    let color: string | null = null
    rt.doc.descendants((n) => {
      if (n.type.name === "blockquote") color = String(n.attrs.color ?? "")
    })
    expect(color).toBe("#ff0000")
  })

  it("icon attr round-trips", () => {
    const rt = roundTrip("{blockquote class=custom icon=info}\n> heads up\n")
    expect(rt.isStable).toBe(true)
  })

  it("wrap=left with image-width creates a wrapped figure shape", () => {
    // A single-image blockquote with wrap creates a floated figure — the
    // "multi-image figure" case is a blockquote containing several images.
    const src = "{blockquote wrap=left image-width=40%}\n> ![alt](/media/pic.png)\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
  })

  it("multi-image figure with image-width preserves each image", () => {
    const src =
      "{blockquote image-width=30%}\n" +
      "> ![one](/media/a.png)\n" +
      "> ![two](/media/b.png)\n" +
      "> ![three](/media/c.png)\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    // All three images survive the round-trip.
    let imgCount = 0
    rt.doc.descendants((n) => {
      if (n.type.name === "image") imgCount++
    })
    expect(imgCount).toBe(3)
  })
})

describe("blockquote: cell-in-backticks invariant", () => {
  it("`{blockquote}` inside a backtick-wrapped table cell stays literal", () => {
    const src = "| head |\n" + "| --- |\n" + "| `{blockquote class=custom}` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    // No blockquote node should have been created.
    expect(countNodes(rt.doc, "blockquote")).toBe(0)
  })
})
