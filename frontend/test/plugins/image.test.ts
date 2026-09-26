// image plugin — `![alt](src)` plus the `{image ...}` directive for
// size/version/align/wrap/caption/label/bg attributes. Sized images
// serialize as `{image ...}![alt](src)` inline in a paragraph.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes } from "../helpers"

function firstImage(doc: any): Record<string, any> | null {
  let found: Record<string, any> | null = null
  doc.descendants((n: any) => {
    if (found === null && n.type.name === "image") found = { ...n.attrs }
  })
  return found
}

describe("image: bare ![alt](src) syntax", () => {
  it("plain image round-trips", () => {
    const rt = roundTrip("![alt text](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "image")).toBe(1)
    expect(firstImage(rt.doc)?.src).toBe("/media/pic.png")
    expect(firstImage(rt.doc)?.alt).toBe("alt text")
  })

  it("image with empty alt is legal", () => {
    const rt = roundTrip("![](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.alt).toBe("")
  })

  it("image with title attr preserves the title", () => {
    const rt = roundTrip('![alt](/media/pic.png "hover title")\n')
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.title).toBe("hover title")
  })

  it("URL-embedded ?v=N moves into the version attr", () => {
    const rt = roundTrip("![alt](/media/pic.png?v=3)\n")
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.version).toBe("3")
  })
})

describe("image: {image ...} directive", () => {
  it("size attr round-trips", () => {
    const rt = roundTrip("{image size=500px}\n![alt](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.size).toBe("500px")
  })

  it("size in percent round-trips", () => {
    const rt = roundTrip("{image size=40%}\n![alt](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.size).toBe("40%")
  })

  it("align + wrap round-trip on the same directive", () => {
    const rt = roundTrip("{image align=center wrap=left}\n![alt](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.align).toBe("center")
    expect(firstImage(rt.doc)?.wrap).toBe("left")
  })

  it("caption + label round-trip", () => {
    const rt = roundTrip('{image caption="Figure 1" label=fig-1}\n![alt](/media/pic.png)\n')
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.caption).toBe("Figure 1")
    expect(firstImage(rt.doc)?.label).toBe("fig-1")
  })

  it("directive version takes precedence over URL-embedded ?v=", () => {
    const rt = roundTrip("{image version=5}\n![alt](/media/pic.png?v=3)\n")
    expect(rt.isStable).toBe(true)
    // Whichever wins, the doc must round-trip stably.
    expect(firstImage(rt.doc)?.version).toBe("5")
  })

  it("bg attr accepts one of the enum values (auto|light|invert|none)", () => {
    const rt = roundTrip("{image bg=invert}\n![alt](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.bg).toBe("invert")
  })
})

describe("image: inline vs standalone forms", () => {
  it("inline sized image in a paragraph: `text {image size=…}![alt](src) text`", () => {
    // When an image with directive attrs is mid-paragraph, the serializer
    // uses inline directive syntax.
    const src = "before {image size=200px}![alt](/media/pic.png) after\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(firstImage(rt.doc)?.size).toBe("200px")
  })

  it("plain inline image (no size) stays inline without directive", () => {
    const src = "before ![alt](/media/pic.png) after\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "image")).toBe(1)
  })
})

describe("image: invariants", () => {
  it("image markdown inside a code fence stays literal", () => {
    const rt = roundTrip("```\n![alt](/media/pic.png)\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "image")).toBe(0)
  })

  it("image markdown inside a backtick-wrapped table cell stays literal", () => {
    const src = "| head |\n| --- |\n| `![alt](/media/pic.png)` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "image")).toBe(0)
  })

  it("image with special characters in alt text — escapes correctly", () => {
    const rt = roundTrip("![a b [brackets]](/media/pic.png)\n")
    expect(rt.isStable).toBe(true)
  })
})
