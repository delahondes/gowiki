// mermaid plugin — ```mermaid [size=… caption=…]\n<mermaid source>\n```
// fenced block. Registers a `mermaid_diagram` PM node whose attrs carry
// the raw diagram source + size + caption.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes } from "../helpers"

function firstMermaid(doc: any): Record<string, any> | null {
  let found: Record<string, any> | null = null
  doc.descendants((n: any) => {
    if (found === null && n.type.name === "mermaid_diagram") found = { ...n.attrs }
  })
  return found
}

describe("mermaid: basic fenced block", () => {
  it("minimal ```mermaid\\ngraph LR; A-->B\\n``` parses", () => {
    const src = "```mermaid\ngraph LR; A-->B\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "mermaid_diagram")).toBe(1)
    expect(firstMermaid(rt.doc)?.data).toBe("graph LR; A-->B")
  })

  it("multi-line diagram source is preserved verbatim", () => {
    const src = "```mermaid\n" + "sequenceDiagram\n" + "  Alice->>Bob: Hello\n" + "  Bob-->>Alice: Hi\n" + "```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const data: string = firstMermaid(rt.doc)?.data ?? ""
    expect(data).toContain("sequenceDiagram")
    expect(data).toContain("Alice->>Bob: Hello")
    expect(data).toContain("Bob-->>Alice: Hi")
  })

  it("empty mermaid body is degenerate but stable", () => {
    const src = "```mermaid\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "mermaid_diagram")).toBe(1)
  })
})

describe("mermaid: attributes on the info string", () => {
  it("size=500px round-trips", () => {
    const src = "```mermaid size=500px\ngraph TD; A-->B\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(firstMermaid(rt.doc)?.size).toBe("500px")
  })

  it("size in percent round-trips", () => {
    const src = "```mermaid size=60%\ngraph TD; A-->B\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(firstMermaid(rt.doc)?.size).toBe("60%")
  })

  it("caption round-trips (quoted values with spaces)", () => {
    const src = '```mermaid caption="System overview"\ngraph LR; A-->B\n```\n'
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(firstMermaid(rt.doc)?.caption).toBe("System overview")
  })

  it("size + caption together on one info string round-trip", () => {
    const src = '```mermaid size=400px caption="Flow"\ngraph TD; A-->B\n```\n'
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(firstMermaid(rt.doc)?.size).toBe("400px")
    expect(firstMermaid(rt.doc)?.caption).toBe("Flow")
  })
})

describe("mermaid: invariants", () => {
  it("mermaid fence NOT interpreted inside a spoiler fence with more backticks", () => {
    // Outer four-backtick spoiler protects the inner ```mermaid.
    const src = "````spoiler wrap\n" + "```mermaid\n" + "graph LR; A-->B\n" + "```\n" + "````\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    // The spoiler contains a mermaid diagram inside — the mermaid node
    // is created inside the spoiler's inner tokenization.
    expect(countNodes(rt.doc, "mermaid_diagram")).toBe(1)
    expect(countNodes(rt.doc, "spoiler")).toBe(1)
  })

  it("plain ``` (no mermaid) does not create a mermaid node", () => {
    const rt = roundTrip("```\ngraph LR; A-->B\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "mermaid_diagram")).toBe(0)
    expect(countNodes(rt.doc, "code_block")).toBe(1)
  })

  it("mermaid with unicode characters in caption round-trips", () => {
    const src = '```mermaid caption="Système"\ngraph LR; A-->B\n```\n'
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(firstMermaid(rt.doc)?.caption).toBe("Système")
  })
})
