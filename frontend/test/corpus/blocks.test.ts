// Block-level structures: headings, paragraphs, lists, code blocks,
// blockquotes, spoilers.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes, assertStrongMark, assertEmMark } from "../helpers"

describe("headings", () => {
  for (let level = 1; level <= 6; level++) {
    it(`h${level} #-prefix heading`, () => {
      const hashes = "#".repeat(level)
      const rt = roundTrip(`${hashes} Heading level ${level}\n`)
      expect(rt.isStable).toBe(true)
      expect(countNodes(rt.doc, "heading")).toBe(1)
    })
  }

  it("numbered heading `## 1. Section`", () => {
    const rt = roundTrip("## 1. Numbered section\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "heading")).toBe(1)
  })

  it("heading with inline marks", () => {
    const rt = roundTrip("## Section with **bold** and *em*\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "bold")
    assertEmMark(rt.doc, "em")
  })
})

describe("paragraphs and breaks", () => {
  it("plain paragraph", () => {
    const rt = roundTrip("Just a plain paragraph.\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "paragraph")).toBe(1)
  })

  it("single newline in top-level paragraph = hard break", () => {
    // Per the dialect: a bare newline inside a top-level paragraph is a
    // hard break, not a soft wrap.
    const rt = roundTrip("first line\nsecond line\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "hard_break")).toBe(1)
  })

  it("two paragraphs separated by a blank line", () => {
    const rt = roundTrip("first para\n\nsecond para\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "paragraph")).toBe(2)
    expect(countNodes(rt.doc, "hard_break")).toBe(0)
  })
})

describe("lists", () => {
  it("unordered list with `-` (only allowed marker)", () => {
    const rt = roundTrip("- one\n- two\n- three\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "bullet_list")).toBe(1)
    expect(countNodes(rt.doc, "list_item")).toBe(3)
  })

  it("ordered list with `1.`", () => {
    const rt = roundTrip("1. first\n2. second\n3. third\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "ordered_list")).toBe(1)
    expect(countNodes(rt.doc, "list_item")).toBe(3)
  })

  it("nested unordered list", () => {
    const rt = roundTrip("- outer\n  - inner a\n  - inner b\n- other\n")
    expect(rt.isStable).toBe(true)
    // Two bullet_list nodes: outer and one nested inside the first item.
    expect(countNodes(rt.doc, "bullet_list")).toBe(2)
    expect(countNodes(rt.doc, "list_item")).toBe(4)
  })

  it("ordered nested inside unordered", () => {
    const rt = roundTrip("- one\n  1. sub a\n  2. sub b\n- two\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "bullet_list")).toBe(1)
    expect(countNodes(rt.doc, "ordered_list")).toBe(1)
  })

  it("list item with inline marks", () => {
    const rt = roundTrip("- item with **bold** inside\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "bold")
  })
})

describe("code blocks", () => {
  it("fenced code block with language specifier", () => {
    const rt = roundTrip("```go\nfunc main() {}\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "code_block")).toBe(1)
  })

  it("fenced code block preserves internal newlines", () => {
    const rt = roundTrip("```python\ndef f():\n    return 1\n```\n")
    expect(rt.isStable).toBe(true)
    // Text content should contain the internal newline.
    let text = ""
    rt.doc.descendants(n => {
      if (n.type.name === "code_block") text = n.textContent
    })
    expect(text).toContain("\n")
    expect(text).toContain("def f():")
  })

  it("code block with no language specifier", () => {
    const rt = roundTrip("```\nplain code\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "code_block")).toBe(1)
  })

  it("code block containing markdown-looking content (unparsed)", () => {
    const rt = roundTrip("```md\n**not** _really_ marked\n```\n")
    expect(rt.isStable).toBe(true)
    // No strong/underline marks should have been created — everything is
    // literal text inside the code block.
    let foundMark = false
    rt.doc.descendants(n => {
      if (n.isText && n.marks.some(m => m.type.name === "strong" || m.type.name === "underline")) {
        foundMark = true
      }
    })
    expect(foundMark).toBe(false)
  })
})

describe("blockquotes and spoilers", () => {
  it("plain blockquote", () => {
    const rt = roundTrip("> a quoted line\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "blockquote")).toBe(1)
  })

  it("blockquote with inline marks", () => {
    const rt = roundTrip("> A quote with **bold** inside.\n")
    expect(rt.isStable).toBe(true)
    assertStrongMark(rt.doc, "bold")
  })

  it("multi-line blockquote", () => {
    const rt = roundTrip("> line one\n> line two\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "blockquote")).toBe(1)
  })
})
