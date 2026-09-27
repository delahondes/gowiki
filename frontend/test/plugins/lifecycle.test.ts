// {lifecycle ...} — one directive on an admin document declaring a
// rule that fans out to per-target-document todos when a condition is
// met. The frontend cares about round-trip stability (the directive
// survives parse → serialize losslessly) and about the NodeView's
// three visible states: loading, ok (grey/passive), alert (red/loud).
//
// The backend has its own suite for the rule fanout and HTTP surface
// (backend/internal/lifecycle/*_test.go). This file exercises the
// dialect surface + the JS side of the rendering box.
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

function firstLifecycle(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "lifecycle") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

describe("lifecycle: minimum shape", () => {
  it("bare {lifecycle} produces one node with empty attrs", () => {
    const rt = roundTrip("{lifecycle}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "lifecycle")).toBe(1)
    const a = firstLifecycle(rt.doc)!
    expect(a.scope).toBe("")
    expect(a.tags).toBe("")
    expect(a.when).toBe("")
    expect(a.title).toBe("")
    expect(a.assign).toBe("")
  })

  it("a fully-configured rule round-trips losslessly", () => {
    const rt = roundTrip(
      `{lifecycle scope="^/qms/.*" tags=sop,rec when=stale:30m title="review {{path}}" assign=alice}\n`
    )
    expect(rt.isStable).toBe(true)
    const a = firstLifecycle(rt.doc)!
    expect(a.scope).toBe("^/qms/.*")
    expect(a.tags).toBe("sop,rec")
    expect(a.when).toBe("stale:30m")
    expect(a.title).toBe("review {{path}}")
    expect(a.assign).toBe("alice")
  })
})

describe("lifecycle: serialization determinism", () => {
  it("attrs emit in the canonical order (scope, tags, exclude_tags, when, title, assign, priority, action)", () => {
    const rt = roundTrip(
      `{lifecycle action=edit priority=high assign=alice title=t when=stale:30d exclude_tags=archived tags=sop scope="^/x/.*"}\n`
    )
    expect(rt.isStable).toBe(true)
    const rendered = rt.first
    const positions = ["scope=", "tags=", "exclude_tags=", "when=", "title=", "assign=", "priority=", "action="].map(
      (key) => rendered.indexOf(key)
    )
    for (let i = 1; i < positions.length; i++) {
      expect(positions[i]).toBeGreaterThan(positions[i - 1])
    }
  })

  it("empty attrs are dropped on serialize (source stays terse)", () => {
    const rt = roundTrip(`{lifecycle scope="^/x/.*" when=stale:30d title=t assign=a}\n`)
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/tags=/)
    expect(rt.first).not.toMatch(/exclude_tags=/)
    expect(rt.first).not.toMatch(/priority=/)
    expect(rt.first).not.toMatch(/action=/)
  })

  it("values containing whitespace are quoted", () => {
    const rt = roundTrip(`{lifecycle scope="^/x/.*" when=stale:30d title="review this page" assign=a}\n`)
    expect(rt.isStable).toBe(true)
    expect(rt.first).toMatch(/title="review this page"/)
  })

  it("values with an embedded double quote are backslash-escaped", () => {
    const rt = roundTrip(`{lifecycle scope="^/x/.*" when=stale:30d title="say \\"hi\\"" assign=a}\n`)
    expect(rt.isStable).toBe(true)
    expect(rt.first).toContain(`\\"hi\\"`)
  })
})

describe("lifecycle: reviewflow_overdue kind", () => {
  it("round-trips with only a scope (no title/assign required)", () => {
    const rt = roundTrip(`{lifecycle scope="^/qms/.*" when=reviewflow_overdue}\n`)
    expect(rt.isStable).toBe(true)
    const a = firstLifecycle(rt.doc)!
    expect(a.when).toBe("reviewflow_overdue")
    expect(a.scope).toBe("^/qms/.*")
    expect(a.title).toBe("")
    expect(a.assign).toBe("")
  })

  it("preserves assign= when set (alert addressee)", () => {
    const rt = roundTrip(`{lifecycle scope="^/qms/.*" when=reviewflow_overdue assign=qms-lead}\n`)
    expect(rt.isStable).toBe(true)
    const a = firstLifecycle(rt.doc)!
    expect(a.when).toBe("reviewflow_overdue")
    expect(a.assign).toBe("qms-lead")
    // assign= must appear in the serialised source so it survives a save.
    expect(rt.first).toMatch(/assign=qms-lead/)
  })
})

describe("lifecycle: interaction with code fences", () => {
  it("{lifecycle ...} inside a fenced block stays literal", () => {
    const src = "```\n{lifecycle scope=/x when=stale:30d title=t assign=a}\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "lifecycle")).toBe(0)
  })

  it("{lifecycle ...} inside a backtick-protected cell stays literal", () => {
    const src = "| head |\n| --- |\n| `{lifecycle scope=/x when=stale:30d title=t assign=a}` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "lifecycle")).toBe(0)
  })
})
