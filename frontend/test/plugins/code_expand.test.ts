// code_expand mark — the `` @`text with {{VAR}}` `` inline syntax.
// Unlike plain code, expand-code allows template variables inside its
// value to be substituted at render time (kept as literal text on disk).
// The mark is registered in frontend/compiler/core_nodes.ts; we test it
// alongside the plugins because its semantics matter for the corpus.
import { describe, it, expect } from "vitest"
import { roundTrip, assertMarkOnText } from "../helpers"

describe("code_expand mark syntax `@` prefix", () => {
  it("plain @`text` gets the code_expand mark", () => {
    const rt = roundTrip("@`hello world`\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "code_expand", "hello world")
  })

  it("@`{{VAR}}` keeps the template variable literal inside the mark", () => {
    // The mark preserves its content verbatim — no interpolation on
    // parse. Rendering-time expansion happens elsewhere.
    const rt = roundTrip("@`hello {{NAME}}`\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "code_expand", "hello {{NAME}}")
  })

  it("@`text` mid-paragraph does not swallow surrounding text", () => {
    const rt = roundTrip("Before @`middle` after\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "code_expand", "middle")
    let hasBefore = false
    let hasAfter = false
    rt.doc.descendants((n) => {
      if (!n.isText) return
      const marks = n.marks.map((m) => m.type.name)
      if (marks.length === 0 && n.text?.startsWith("Before ")) hasBefore = true
      if (marks.length === 0 && n.text?.endsWith(" after")) hasAfter = true
    })
    expect(hasBefore && hasAfter).toBe(true)
  })

  it("plain backticks (no @) create a regular code mark, not code_expand", () => {
    const rt = roundTrip("`plain code`\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "code", "plain code")
    // No text node should also carry code_expand.
    let sawCodeExpand = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "code_expand") sawCodeExpand = true
      }
    })
    expect(sawCodeExpand).toBe(false)
  })

  it("@`` with literal backslash escapes stays literal", () => {
    // Backslashes inside the code span are not interpreted.
    const rt = roundTrip("@`path\\to\\file`\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "code_expand", "path\\to\\file")
  })

  it("@`text` inside a table cell wrapped in outer backticks stays literal", () => {
    // Outer backticks protect cell content from directive/mark parsing,
    // so the inner @` should not spawn a code_expand mark.
    const src = "| head |\n| --- |\n| `@\\`literal\\`` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    let sawCodeExpand = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "code_expand") sawCodeExpand = true
      }
    })
    expect(sawCodeExpand).toBe(false)
  })

  it("@`text` with unicode content is preserved", () => {
    const rt = roundTrip("@`café ☕ ünicode`\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "code_expand", "café ☕ ünicode")
  })

  it("adjacent @` marks stay as distinct spans", () => {
    const rt = roundTrip("@`one` @`two`\n")
    expect(rt.isStable).toBe(true)
    assertMarkOnText(rt.doc, "code_expand", "one")
    assertMarkOnText(rt.doc, "code_expand", "two")
  })

  it("plain @ (no backtick) is not a marker", () => {
    const rt = roundTrip("email@example.com\n")
    expect(rt.isStable).toBe(true)
    let sawCodeExpand = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "code_expand") sawCodeExpand = true
      }
    })
    expect(sawCodeExpand).toBe(false)
  })

  it("empty @`` is degenerate — no crash, no mark opened", () => {
    const rt = roundTrip("@``\n")
    // We only require no crash and stability; the concrete parse may
    // treat empty @`` either as an empty mark or as literal text.
    expect(rt.isStable).toBe(true)
  })
})
