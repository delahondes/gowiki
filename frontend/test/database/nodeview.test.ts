// NodeView smoke tests — mount an EditorView with each database node
// type and prove the NodeView's DOM lands in the document. This is not a
// full interaction test; it verifies the plugin wiring survives a fresh
// mount (schema+plugins register cleanly, NodeView constructor runs, DOM
// gets emitted). Regressions in this area would otherwise only be
// visible in the browser.
//
// jsdom's `fetch` is either absent or fails; the NodeView's DB-schema
// fetch is wrapped in try/catch, so a failing fetch degrades to the
// static header (label + "No table specified" / "Row: <table>") which
// is exactly what we assert.
import { describe, it, expect, beforeEach, afterEach } from "vitest"
import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { registry, schema } from "../helpers"

// Silence unavailable /api/* fetches — the NodeView is defensive, but a
// noisy console makes real failures harder to spot.
const originalFetch = globalThis.fetch
beforeEach(() => {
  globalThis.fetch = (async () => {
    throw new Error("no network in tests")
  }) as any
})
afterEach(() => {
  globalThis.fetch = originalFetch
})

function mount(doc: any): { view: EditorView; container: HTMLElement } {
  const container = document.createElement("div")
  document.body.appendChild(container)
  const state = EditorState.create({
    schema,
    doc,
    plugins: registry.getEditorPlugins(),
  })
  const view = new EditorView(container, { state })
  return { view, container }
}

describe("database_row NodeView", () => {
  it("mounts and renders the label for a bound row", () => {
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_row.create({ table: "customers", _fields: { name: "Alice" } }),
    ])
    const { view, container } = mount(doc)
    try {
      const nodeEl = container.querySelector(".gowiki-database-row")
      expect(nodeEl).not.toBeNull()
      const label = nodeEl!.querySelector(".gowiki-database-row-label")
      expect(label?.textContent).toContain("customers")
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("bare placeholder (no table) renders the 'No table specified' hint", () => {
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_row.create({ table: "", _fields: {} }),
    ])
    const { view, container } = mount(doc)
    try {
      const err = container.querySelector(".gowiki-database-row .gowiki-database-error")
      expect(err?.textContent).toBe("No table specified")
    } finally {
      view.destroy()
      container.remove()
    }
  })
})

describe("database_query NodeView", () => {
  it("mounts a NodeView for a query with a table attr", () => {
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_query.create({ table: "orders" }),
    ])
    const { view, container } = mount(doc)
    try {
      const nodeEl = container.querySelector(".gowiki-database-query")
      expect(nodeEl).not.toBeNull()
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("renders the 'No table specified' hint when table is empty", () => {
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_query.create({ table: "" }),
    ])
    const { view, container } = mount(doc)
    try {
      const err = container.querySelector(".gowiki-database-query .gowiki-database-error")
      expect(err?.textContent).toBe("No table specified")
    } finally {
      view.destroy()
      container.remove()
    }
  })
})

describe("database_newrow NodeView", () => {
  it("mounts a NodeView", () => {
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_newrow.create({ table: "orders" }),
    ])
    const { view, container } = mount(doc)
    try {
      // Class prefix is consistent with sibling views.
      const el = container.querySelector('[class*="gowiki-database"]')
      expect(el).not.toBeNull()
    } finally {
      view.destroy()
      container.remove()
    }
  })
})
