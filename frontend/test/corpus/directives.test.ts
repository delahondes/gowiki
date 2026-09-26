// Self-contained block directives {name key=value}.
//
// The full directive registry is discovered at runtime from the plugins.
// Here we cover the shipping-critical ones: include, tag, tag-query,
// database-query, version-link, reviewflow-link, template, changes,
// favorites, todo, todo-list.
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes } from "../helpers"

describe("include directive", () => {
  it("{include path=/absolute}", () => {
    const rt = roundTrip("{include path=/docs/shared}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "include")).toBe(1)
  })

  it("{include path=./sibling}", () => {
    const rt = roundTrip("{include path=./sibling}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "include")).toBe(1)
  })
})

describe("tag directives", () => {
  it("{tag values=foo,bar}", () => {
    const rt = roundTrip("{tag values=foo,bar}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "tag")).toBe(1)
  })

  it("{tag-query values=foo}", () => {
    const rt = roundTrip("{tag-query values=foo}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "tag_query")).toBe(1)
  })
})

describe("database query directives", () => {
  it("{database-query table=orders}", () => {
    const rt = roundTrip("{database-query table=orders}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "database_query")).toBe(1)
  })
})

describe("version linking", () => {
  it("{version-link version=3 page=/docs/spec}", () => {
    const rt = roundTrip("{version-link version=3 page=/docs/spec}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "version_link")).toBe(1)
  })

  it("{reviewflow-link version=1.0 page=/policies/qms}", () => {
    const rt = roundTrip("{reviewflow-link version=1.0 page=/policies/qms}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_link")).toBe(1)
  })
})

describe("changes and favorites", () => {
  it("{changes count=10}", () => {
    const rt = roundTrip("{changes count=10}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "changes")).toBe(1)
  })

  it("{favorites}", () => {
    const rt = roundTrip("{favorites}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "favorites")).toBe(1)
  })
})

describe("todos", () => {
  it("{todo title=... assign=...}", () => {
    const rt = roundTrip("{todo title=\"What to do\" assign=alice}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "todo")).toBe(1)
  })

  it("{todo-list}", () => {
    const rt = roundTrip("{todo-list}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "todo_list")).toBe(1)
  })
})

describe("template marker", () => {
  it("{template} bare marker (marks the current page as a template)", () => {
    // {template} is a boolean marker node, not a path reference. Path
    // resolution happens through {template-stamp} on the consuming page.
    const rt = roundTrip("{template}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_marker")).toBe(1)
  })
})

describe("reviewflow directive", () => {
  it("{reviewflow} block", () => {
    const rt = roundTrip("{reviewflow}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow")).toBe(1)
  })
})
