// {slides ...} — the deck-marker directive. The plugin registers a
// self-contained "slides" directive with attrs: title, theme (default
// "light"), ratio (default "16:9"), background, font. Defaults are
// dropped on serialize.
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

function firstSlidesAttrs(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "slides") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

describe("slides: minimum shape", () => {
  it("{slides} bare produces one slides node with defaults", () => {
    const rt = roundTrip("{slides}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "slides")).toBe(1)
    const a = firstSlidesAttrs(rt.doc)!
    expect(a.theme).toBe("light")
    expect(a.ratio).toBe("16:9")
    expect(a.title).toBe("")
  })

  it("bare {slides} does NOT re-emit default attrs", () => {
    const rt = roundTrip("{slides}\n")
    expect(rt.first).toMatch(/^\{slides\}/)
    expect(rt.first).not.toMatch(/theme=light/)
    expect(rt.first).not.toMatch(/ratio=16:9/)
  })
})

describe("slides: individual attrs", () => {
  it("title=", () => {
    const rt = roundTrip('{slides title="Kickoff"}\n')
    expect(rt.isStable).toBe(true)
    expect(firstSlidesAttrs(rt.doc)?.title).toBe("Kickoff")
  })

  it("theme=dark (non-default)", () => {
    const rt = roundTrip("{slides theme=dark}\n")
    expect(rt.isStable).toBe(true)
    expect(firstSlidesAttrs(rt.doc)?.theme).toBe("dark")
  })

  it("theme=light (default) is dropped on serialize", () => {
    const rt = roundTrip("{slides theme=light}\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/theme=light/)
  })

  it("ratio=4:3 (non-default)", () => {
    const rt = roundTrip("{slides ratio=4:3}\n")
    expect(rt.isStable).toBe(true)
    expect(firstSlidesAttrs(rt.doc)?.ratio).toBe("4:3")
  })

  it("ratio=16:9 (default) is dropped on serialize", () => {
    const rt = roundTrip("{slides ratio=16:9}\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/ratio=16:9/)
  })

  it("background attribute", () => {
    const rt = roundTrip("{slides background=#eeeeee}\n")
    expect(rt.isStable).toBe(true)
    expect(firstSlidesAttrs(rt.doc)?.background).toBe("#eeeeee")
  })

  it("font attribute (single token, no spaces)", () => {
    // NOTE: the slides serializer at plugins/slide.ts writes the font
    // value UNQUOTED, so a font like `"Inter, sans-serif"` does not
    // round-trip (the space + comma get re-parsed as multiple attrs).
    // This test pins the working single-token case; the multi-token
    // case is a real serializer bug worth quoting fixes.
    const rt = roundTrip("{slides font=Inter}\n")
    expect(rt.isStable).toBe(true)
    expect(firstSlidesAttrs(rt.doc)?.font).toBe("Inter")
  })
})

describe("slides: multiple attrs combined", () => {
  it("title + theme + ratio round-trip", () => {
    const rt = roundTrip('{slides title="Kickoff" theme=dark ratio=4:3}\n')
    expect(rt.isStable).toBe(true)
    const a = firstSlidesAttrs(rt.doc)!
    expect(a.title).toBe("Kickoff")
    expect(a.theme).toBe("dark")
    expect(a.ratio).toBe("4:3")
  })
})

describe("slides: interaction with code fences", () => {
  it("{slides} inside a fenced block stays literal", () => {
    const src = "```\n{slides theme=dark}\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "slides")).toBe(0)
  })

  it("{slides} inside a backtick-protected table cell stays literal", () => {
    const src = "| head |\n| --- |\n| `{slides theme=dark}` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "slides")).toBe(0)
  })
})
