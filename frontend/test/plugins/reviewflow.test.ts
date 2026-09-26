// {reviewflow ...} + {reviewflow-link ...} + {reviewflow-query ...}
//
// The reviewflow directive carries a version + arbitrary role
// assignments (author, reviewer, validator, and any other role name
// the wiki configures). Roles are stored as a JSON string on the
// node's `roles` attr; on serialize they emit in alphabetical order
// (a determinism-preserving choice worth pinning).
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

function firstReviewflow(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "reviewflow") {
      found = { ...n.attrs, _rolesParsed: JSON.parse(String(n.attrs.roles ?? "{}")) }
      return false
    }
    return true
  })
  return found
}

function firstReviewflowLink(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "reviewflow_link") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

function firstReviewflowQuery(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "reviewflow_query") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

describe("reviewflow: minimum shape", () => {
  it("bare {reviewflow} produces one node with empty attrs", () => {
    const rt = roundTrip("{reviewflow}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow")).toBe(1)
    const a = firstReviewflow(rt.doc)!
    expect(a.version).toBe("")
    expect(a._rolesParsed).toEqual({})
  })

  it("{reviewflow version=1.0}", () => {
    const rt = roundTrip("{reviewflow version=1.0}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflow(rt.doc)?.version).toBe("1.0")
  })
})

describe("reviewflow: role assignments", () => {
  it("author only", () => {
    const rt = roundTrip("{reviewflow author=alice}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflow(rt.doc)?._rolesParsed).toEqual({ author: "alice" })
  })

  it("author + reviewer + validator", () => {
    const rt = roundTrip("{reviewflow author=alice reviewer=bob validator=carol}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflow(rt.doc)?._rolesParsed).toEqual({
      author: "alice",
      reviewer: "bob",
      validator: "carol",
    })
  })

  it("arbitrary custom role names are accepted (collectExtra)", () => {
    const rt = roundTrip("{reviewflow author=alice qa=carol legal=dave}\n")
    expect(rt.isStable).toBe(true)
    const roles = firstReviewflow(rt.doc)?._rolesParsed as Record<string, string>
    expect(roles.qa).toBe("carol")
    expect(roles.legal).toBe("dave")
  })

  it("roles emit in alphabetical order on serialize (determinism)", () => {
    const rt = roundTrip("{reviewflow zebra=z alpha=a mango=m}\n")
    expect(rt.isStable).toBe(true)
    // The serialised order must be alphabetical regardless of input order.
    const rendered = rt.first
    const alphaIdx = rendered.indexOf("alpha=")
    const mangoIdx = rendered.indexOf("mango=")
    const zebraIdx = rendered.indexOf("zebra=")
    expect(alphaIdx).toBeGreaterThan(-1)
    expect(mangoIdx).toBeGreaterThan(alphaIdx)
    expect(zebraIdx).toBeGreaterThan(mangoIdx)
  })

  it("version always emits first when present", () => {
    const rt = roundTrip("{reviewflow author=alice version=2.0}\n")
    expect(rt.isStable).toBe(true)
    const rendered = rt.first
    expect(rendered.indexOf("version=")).toBeLessThan(rendered.indexOf("author="))
  })
})

describe("reviewflow-link (inline)", () => {
  it("{reviewflow-link version=1.0 page=/policy} in a paragraph", () => {
    const rt = roundTrip("See {reviewflow-link version=1.0 page=/policy} for details.\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_link")).toBe(1)
    const a = firstReviewflowLink(rt.doc)!
    expect(a.version).toBe("1.0")
    expect(a.page).toBe("/policy")
  })

  it("bare {reviewflow-link} still parses to a node", () => {
    const rt = roundTrip("Line {reviewflow-link}.\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_link")).toBe(1)
  })
})

describe("reviewflow-query", () => {
  it("bare {reviewflow-query} defaults to status=draft", () => {
    const rt = roundTrip("{reviewflow-query}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_query")).toBe(1)
    const a = firstReviewflowQuery(rt.doc)!
    expect(a.status).toBe("draft")
  })

  it("{reviewflow-query path=/foo} carries the path", () => {
    const rt = roundTrip("{reviewflow-query path=/foo}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflowQuery(rt.doc)?.path).toBe("/foo")
  })

  it("{reviewflow-query status=validated} (non-default)", () => {
    const rt = roundTrip("{reviewflow-query status=validated}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflowQuery(rt.doc)?.status).toBe("validated")
  })

  it("status=draft (default) is dropped on serialize", () => {
    const rt = roundTrip("{reviewflow-query status=draft}\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/status=draft/)
  })
})

describe("reviewflow: interaction with code fences", () => {
  it("{reviewflow} inside a fenced block stays literal", () => {
    const src = "```\n{reviewflow author=alice}\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow")).toBe(0)
  })

  it("{reviewflow-link} inside a backtick-protected cell stays literal", () => {
    const src = "| head |\n| --- |\n| `{reviewflow-link version=1 page=/foo}` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_link")).toBe(0)
  })
})
