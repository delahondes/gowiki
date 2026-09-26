// {include page=/path} — the current frontend schema uses `path` attr;
// `section=` scoping is a backend concern (the include NodeView fetches
// the resolved content over HTTP), so this file focuses on directive
// round-trip + semantic attrs + code-fence protection.
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

function firstIncludePath(doc: PMNode): string | null {
  let path: string | null = null
  doc.descendants((n) => {
    if (path === null && n.type.name === "include") {
      path = n.attrs.path ?? null
      return false
    }
    return true
  })
  return path
}

describe("include: minimum shape", () => {
  it("{include path=/docs/intro} produces one include node", () => {
    const rt = roundTrip("{include path=/docs/intro}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "include")).toBe(1)
    expect(firstIncludePath(rt.doc)).toBe("/docs/intro")
  })

  it("{include path=/} handles root path", () => {
    const rt = roundTrip("{include path=/}\n")
    expect(rt.isStable).toBe(true)
    expect(firstIncludePath(rt.doc)).toBe("/")
  })

  it("{include path=/docs/} handles namespace-index path", () => {
    const rt = roundTrip("{include path=/docs/}\n")
    expect(rt.isStable).toBe(true)
    expect(firstIncludePath(rt.doc)).toBe("/docs/")
  })
})

describe("include: multiple in one document", () => {
  it("two includes stay as two distinct nodes", () => {
    const src = "{include path=/a}\n\n{include path=/b}\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "include")).toBe(2)
    const paths: string[] = []
    rt.doc.descendants((n) => {
      if (n.type.name === "include") paths.push(n.attrs.path)
    })
    expect(paths).toEqual(["/a", "/b"])
  })
})

describe("include: interaction with code fences", () => {
  it("{include} inside a fenced block stays literal", () => {
    const src = "```\n{include path=/docs/intro}\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "include")).toBe(0)
  })

  it("{include} inside a backtick-protected table cell stays literal", () => {
    const src = "| head |\n| --- |\n| `{include path=/docs/intro}` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "include")).toBe(0)
  })
})

describe("include: unicode and special characters in path", () => {
  it("path with unicode segment", () => {
    const rt = roundTrip("{include path=/régulatoire/notes}\n")
    expect(rt.isStable).toBe(true)
    expect(firstIncludePath(rt.doc)).toBe("/régulatoire/notes")
  })

  it("path with hyphens and dots", () => {
    const rt = roundTrip("{include path=/docs/foo-bar.baz}\n")
    expect(rt.isStable).toBe(true)
    expect(firstIncludePath(rt.doc)).toBe("/docs/foo-bar.baz")
  })
})

describe("include: mixed with surrounding blocks", () => {
  it("include between paragraphs preserves both", () => {
    const src = "Above.\n\n{include path=/foo}\n\nBelow.\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "include")).toBe(1)
    expect(countNodes(rt.doc, "paragraph")).toBeGreaterThanOrEqual(2)
  })

  it("include inside a list item does NOT parse as a directive (block-level only)", () => {
    // The directive parser fires only at block-line boundaries. Inside a
    // list item body it stays literal text, so the round-trip serializer
    // must escape the `{` so the next parse sees the same content.
    const src = "- item\n- {include path=/foo}\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
  })
})
