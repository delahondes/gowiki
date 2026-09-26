// medialink plugin — a link whose href points to a media file (any
// extension other than `.md`) becomes a `medialink` inline atom node.
// Renders with a file-type icon in DOM. Serializes back to plain
// `[label](href)` syntax.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes } from "../helpers"

function firstMedialink(doc: any): Record<string, any> | null {
  let found: Record<string, any> | null = null
  doc.descendants((n: any) => {
    if (found === null && n.type.name === "medialink") found = { ...n.attrs }
  })
  return found
}

describe("medialink: detection from a link with a file extension", () => {
  it("[label](/media/report.pdf) becomes a medialink node", () => {
    const rt = roundTrip("[the report](/media/report.pdf)\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "medialink")).toBe(1)
    expect(firstMedialink(rt.doc)?.href).toBe("/media/report.pdf")
    expect(firstMedialink(rt.doc)?.label).toBe("the report")
  })

  it(".xlsx file link becomes a medialink", () => {
    const rt = roundTrip("[budget](/finance/budget.xlsx)\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "medialink")).toBe(1)
  })

  it(".png file link becomes a medialink (not an image node — no `!`)", () => {
    const rt = roundTrip("[preview](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "medialink")).toBe(1)
    expect(countNodes(rt.doc, "image")).toBe(0)
  })

  it(".md link stays a regular page link, NOT a medialink", () => {
    const rt = roundTrip("[docs](/docs/spec.md)\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "medialink")).toBe(0)
  })

  it("link without an extension stays a regular link", () => {
    const rt = roundTrip("[home](/index)\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "medialink")).toBe(0)
  })

  it("external http(s) link with extension is not a medialink", () => {
    const rt = roundTrip("[external](https://example.com/file.pdf)\n")
    expect(rt.isStable).toBe(true)
    // External links stay as regular links regardless of href extension.
    expect(countNodes(rt.doc, "medialink")).toBe(0)
  })
})

describe("medialink: version handling", () => {
  it("?v=N is extracted from href into the version attr", () => {
    const rt = roundTrip("[label](/media/report.pdf?v=3)\n")
    expect(rt.isStable).toBe(true)
    expect(firstMedialink(rt.doc)?.version).toBe("3")
    expect(firstMedialink(rt.doc)?.href).toBe("/media/report.pdf")
  })

  it("no version attr survives round-trip when source has none", () => {
    const rt = roundTrip("[l](/media/f.pdf)\n")
    expect(rt.isStable).toBe(true)
    expect(firstMedialink(rt.doc)?.version).toBeNull()
  })
})

describe("medialink: label escaping (the `\\[` regex fix)", () => {
  it("label containing square brackets is escaped on serialize", () => {
    // The fix was: `[[\]]` → `[[\]]` in the escape regex. Without it, the
    // bracket in a label like `[note]` was silently stripped or corrupted
    // by the escape pass.
    const rt = roundTrip("[a [note] here](/media/f.pdf)\n")
    expect(rt.isStable).toBe(true)
    expect(firstMedialink(rt.doc)?.label).toBe("a [note] here")
  })

  it("backslash-escaped brackets in the label round-trip", () => {
    // Raw `[` inside a link label breaks markdown-it's label span, so
    // the invariant to pin is: an escaped `\[` survives parse and
    // serialize unchanged.
    const rt = roundTrip("[open \\[ bracket](/media/f.pdf)\n")
    expect(rt.isStable).toBe(true)
    // The concrete label after normalisation may or may not carry the
    // escape; the round-trip stability is the load-bearing invariant.
    expect(firstMedialink(rt.doc)?.href).toBe("/media/f.pdf")
  })
})

describe("medialink: title attribute", () => {
  it('link with a "title" preserves it', () => {
    const rt = roundTrip('[label](/media/f.pdf "hover")\n')
    expect(rt.isStable).toBe(true)
    expect(firstMedialink(rt.doc)?.title).toBe("hover")
  })
})

describe("medialink: invariants", () => {
  it("medialink inside a code fence stays literal (no node created)", () => {
    const rt = roundTrip("```\n[l](/media/f.pdf)\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "medialink")).toBe(0)
  })

  it("medialink inside a backtick-wrapped table cell stays literal", () => {
    const src = "| head |\n| --- |\n| `[l](/media/f.pdf)` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "medialink")).toBe(0)
  })

  it("multiple medialinks in one paragraph each get their own node", () => {
    const rt = roundTrip("[a](/media/a.pdf) and [b](/media/b.xlsx)\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "medialink")).toBe(2)
  })
})
