// Inline marks + links.
import { describe, it, expect } from "vitest"
import {
  roundTrip,
  assertUnderlineMark,
  assertStrongMark,
  assertEmMark,
  assertHighlightMark,
  assertCodeMark,
  assertStrikeMark,
  assertSubMark,
  assertSuperMark,
  assertMarkOnText,
  countNodes,
  registry,
} from "../helpers"
import { markdownToPM } from "../../compiler/markdown_to_pm"

describe("inline marks", () => {
  it("italic *word*", () => {
    const rt = roundTrip("This is *italic* text.\n")
    expect(rt.isStable).toBe(true)
    assertEmMark(rt.doc, "italic")
  })

  it("bold **word**", () => {
    const rt = roundTrip("This is **bold** text.\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "bold")
  })

  it("underline _word_ (dialect divergence: NOT italic)", () => {
    const rt = roundTrip("This is _underlined_ text.\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "underlined")
  })

  it("strikethrough ~~word~~", () => {
    const rt = roundTrip("This is ~~struck~~ text.\n")
    expect(rt.isStable).toBe(true)
    assertStrikeMark(rt.doc, "struck")
  })

  it("subscript ~word~", () => {
    const rt = roundTrip("H~2~O molecule.\n")
    expect(rt.isStable).toBe(true)
    assertSubMark(rt.doc, "2")
  })

  it("superscript ^word^", () => {
    const rt = roundTrip("E = mc^2^ famously.\n")
    expect(rt.isStable).toBe(true)
    assertSuperMark(rt.doc, "2")
  })

  it("highlight ==word==", () => {
    const rt = roundTrip("This is ==highlighted== text.\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "highlighted")
  })

  it("highlight with color =={color=yellow}text==", () => {
    const rt = roundTrip("Warning: =={color=yellow}be careful== here.\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "be careful")
  })

  it("inline code `word`", () => {
    const rt = roundTrip("Use the `printf` function.\n")
    expect(rt.isStable).toBe(true)
    assertCodeMark(rt.doc, "printf")
  })

  it("code_expand @`text with {{VAR}}`", () => {
    const rt = roundTrip("Path: @`/data/{{USER}}/file`\n")
    expect(rt.isStable).toBe(true)
    // code_expand mark is distinct from code
    assertMarkOnText(rt.doc, "code_expand", "/data/{{USER}}/file")
  })
})

describe("nested marks", () => {
  it("bold containing italic **strong *inside* text**", () => {
    const rt = roundTrip("**strong *inside* text**\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "strong ")
    assertEmMark(rt.doc, "inside")
  })

  it("underline containing bold _under **strong** end_", () => {
    const rt = roundTrip("_under **strong** end_\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "under ")
    assertStrongMark(rt.doc, "strong")
  })

  it("highlight containing underline ==high _under_ end==", () => {
    const rt = roundTrip("==high _under_ end==\n")
    expect(rt.isStable).toBe(true)
    assertHighlightMark(rt.doc, "high ")
    assertUnderlineMark(rt.doc, "under")
  })

  it("code protects markdown inside `not **bold**`", () => {
    const rt = roundTrip("Literal: `not **bold** here`.\n")
    expect(rt.isStable).toBe(true)
    // No strong mark should have been created — the ** is literal inside code.
    assertCodeMark(rt.doc, "not **bold** here")
  })
})

describe("inline footnotes", () => {
  it("simple footnote ^[note]", () => {
    const rt = roundTrip("Some text^[with a note] here.\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "footnote")).toBe(1)
  })

  it("footnote with inline markdown ^[with *em* and `code`]", () => {
    const rt = roundTrip("Text^[with *em* and `code`].\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "footnote")).toBe(1)
  })
})

describe("links", () => {
  it("external link [text](url)", () => {
    const rt = roundTrip("See [Anthropic](https://anthropic.com) site.\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "link", "Anthropic")
  })

  it("internal link [text](/path)", () => {
    const rt = roundTrip("See [the docs](/docs/) page.\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "link", "the docs")
  })

  it("relative link [text](./sibling)", () => {
    const rt = roundTrip("See [sibling](./sibling) page.\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "link", "sibling")
  })

  it("mailto [text](mailto:user@example.com)", () => {
    const rt = roundTrip("Contact [me](mailto:user@example.com).\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "link", "me")
  })

  it("autolink https URL becomes a link on round-trip", () => {
    const rt = roundTrip("Visit https://example.com now.\n")
    expect(rt.isStable).toBe(true)
    // Auto-linking happens on SERIALIZE (plain URL text becomes `[](url)`),
    // so the link mark is visible on the doc built from the round-tripped
    // markdown, not the first parse of the raw source.
    const doc2 = markdownToPM(rt.first, registry)
    let found = false
    doc2.descendants(n => {
      if (n.isText && n.marks.some(m => m.type.name === "link")) found = true
    })
    expect(found).toBe(true)
  })

  it("bare email address autolinks on round-trip", () => {
    const rt = roundTrip("Email support@example.com anytime.\n")
    expect(rt.isStable).toBe(true)
    const doc2 = markdownToPM(rt.first, registry)
    let found = false
    doc2.descendants(n => {
      if (n.isText && n.marks.some(m => m.type.name === "link")) found = true
    })
    expect(found).toBe(true)
  })
})
