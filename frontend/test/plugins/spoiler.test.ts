// spoiler plugin — ```spoiler <title>\n...\n``` fenced folded content.
// Renders as <details>/<summary> in DOM.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes } from "../helpers"

describe("spoiler: basic fence", () => {
  it("```spoiler Title\\ncontent\\n``` round-trips", () => {
    const src = "```spoiler My title\nhidden content\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "spoiler")).toBe(1)
  })

  it("spoiler stores the title verbatim", () => {
    const rt = roundTrip("```spoiler Warning ahead\ninside\n```\n")
    expect(rt.isStable).toBe(true)
    let title: string | null = null
    rt.doc.descendants((n) => {
      if (n.type.name === "spoiler") title = String(n.attrs.title ?? "")
    })
    expect(title).toBe("Warning ahead")
  })

  it("spoiler with no title (just ```spoiler) still parses", () => {
    const rt = roundTrip("```spoiler\ncontent\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "spoiler")).toBe(1)
  })

  it("multi-paragraph spoiler content round-trips", () => {
    const src = "```spoiler Details\n" + "first paragraph\n\n" + "second paragraph\n" + "```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "spoiler")).toBe(1)
  })

  it("spoiler containing a nested code fence with more backticks", () => {
    const src = "````spoiler Wrapper\n" + "```\n" + "inner code\n" + "```\n" + "````\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "spoiler")).toBe(1)
    expect(countNodes(rt.doc, "code_block")).toBe(1)
  })

  it("spoiler with inline formatting inside preserves marks", () => {
    const src = "```spoiler t\ntext with **bold** in it\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    let sawStrong = false
    rt.doc.descendants((n) => {
      if (n.isText) {
        for (const m of n.marks) if (m.type.name === "strong") sawStrong = true
      }
    })
    expect(sawStrong).toBe(true)
  })

  it("unterminated spoiler fence — parser leaves it as normal fenced code", () => {
    // If the closing ``` is missing, markdown-it should NOT emit a spoiler
    // node. Test that no spoiler is created; behaviour on unterminated
    // fences is left to the code-block fallback.
    const rt = roundTrip("```spoiler open\nnever closes\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "spoiler")).toBe(0)
  })

  it("spoiler title with unicode characters round-trips", () => {
    const rt = roundTrip("```spoiler Détail 🔎\ncontent\n```\n")
    expect(rt.isStable).toBe(true)
    let title: string | null = null
    rt.doc.descendants((n) => {
      if (n.type.name === "spoiler") title = String(n.attrs.title ?? "")
    })
    expect(title).toBe("Détail 🔎")
  })

  it("empty spoiler body round-trips", () => {
    const rt = roundTrip("```spoiler t\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "spoiler")).toBe(1)
  })
})
