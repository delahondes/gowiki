// Behaviour-side tests for the {database-newrow} NodeView — the
// counterpart to newrow_syntax.test.ts (which covers the parse ↔
// serialize round trip). These prove that the rendered FORM reflects
// what the directive declared:
//   • fields= whitelist actually filters + reorders the inputs
//   • foo=~bar actually pre-fills an input WITHOUT locking it
//   • a pinned / defaulted field is forced-visible even when the
//     fields= whitelist omits it
//
// The NodeView fetches the table schema from /api/database/.../schema;
// jsdom has no network, so each test installs a fetch mock that
// returns a hand-crafted schema and then awaits the form to land.

import { describe, it, expect, beforeEach, afterEach } from "vitest"
import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { registry, schema } from "../helpers"

type Field = { name: string; label?: string; type: string; default_value?: string }

const originalFetch = globalThis.fetch
let currentSchema: { fields: Field[] } | null = null

beforeEach(() => {
  currentSchema = null
  globalThis.fetch = (async (url: string | URL) => {
    const u = String(url)
    if (u.includes("/schema") && currentSchema) {
      return {
        ok: true,
        json: async () => currentSchema,
      } as any
    }
    throw new Error("unexpected fetch in test: " + u)
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

// Wait until the form shows up (fetch resolves + render runs). The
// NodeView dispatches `gowiki:node-rendered` on the document once the
// form is in place; we also poll briefly as a belt-and-braces.
async function waitForForm(container: HTMLElement, timeoutMs = 200): Promise<void> {
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    if (container.querySelector(".gowiki-database-form")) return
    await new Promise((r) => setTimeout(r, 5))
  }
  throw new Error("form did not render within " + timeoutMs + "ms")
}

function fieldLabels(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll(".gowiki-database-form-field label")).map((l) => l.textContent || "")
}

function inputFor(container: HTMLElement, label: string): HTMLInputElement | HTMLSelectElement | null {
  const rows = Array.from(container.querySelectorAll(".gowiki-database-form-field"))
  for (const row of rows) {
    const lbl = row.querySelector("label")
    if (lbl?.textContent === label) {
      const el = row.querySelector("input, select") as HTMLInputElement | HTMLSelectElement | null
      return el
    }
  }
  return null
}

describe("database_newrow NodeView — fields= whitelist", () => {
  it("shows only the whitelisted columns, in the given order", async () => {
    currentSchema = {
      fields: [
        { name: "title", type: "text" },
        { name: "severity", type: "text" },
        { name: "reporter", type: "text" },
        { name: "assignee", type: "text" },
      ],
    }
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_newrow.create({
        table: "tickets",
        fields: "severity,title",
        _pinned: "{}",
        _defaults: "{}",
      }),
    ])
    const { view, container } = mount(doc)
    try {
      await waitForForm(container)
      // Order matches the whitelist, NOT the schema's declaration
      // order. assignee + reporter are hidden.
      expect(fieldLabels(container)).toEqual(["severity", "title"])
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("empty whitelist shows every (non-archived) column (today's behaviour)", async () => {
    currentSchema = {
      fields: [
        { name: "title", type: "text" },
        { name: "severity", type: "text" },
      ],
    }
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_newrow.create({
        table: "tickets",
        fields: "",
        _pinned: "{}",
        _defaults: "{}",
      }),
    ])
    const { view, container } = mount(doc)
    try {
      await waitForForm(container)
      expect(fieldLabels(container)).toEqual(["title", "severity"])
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("pinned field is forced-visible even when the whitelist omits it", async () => {
    currentSchema = {
      fields: [
        { name: "title", type: "text" },
        { name: "severity", type: "text" },
        { name: "software", type: "text" },
      ],
    }
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_newrow.create({
        table: "tickets",
        fields: "title", // deliberately omits "software"
        _pinned: JSON.stringify({ software: "gowiki" }),
        _defaults: "{}",
      }),
    ])
    const { view, container } = mount(doc)
    try {
      await waitForForm(container)
      const labels = fieldLabels(container)
      // Whitelist entries come first; forced-visible pinned appended.
      expect(labels).toEqual(["title", "software"])
      // And the pinned input carries the locked value.
      const software = inputFor(container, "software") as HTMLInputElement
      expect(software.value).toBe("gowiki")
      expect(software.classList.contains("gowiki-database-pinned")).toBe(true)
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("defaulted field is forced-visible even when the whitelist omits it", async () => {
    currentSchema = {
      fields: [
        { name: "title", type: "text" },
        { name: "reporter", type: "text" },
      ],
    }
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_newrow.create({
        table: "tickets",
        fields: "title",
        _pinned: "{}",
        _defaults: JSON.stringify({ reporter: "alice" }),
      }),
    ])
    const { view, container } = mount(doc)
    try {
      await waitForForm(container)
      expect(fieldLabels(container)).toEqual(["title", "reporter"])
    } finally {
      view.destroy()
      container.remove()
    }
  })
})

describe("database_newrow NodeView — foo=~bar default modifier", () => {
  it("pre-fills the input with the default value", async () => {
    currentSchema = { fields: [{ name: "reporter", type: "text" }] }
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_newrow.create({
        table: "tickets",
        _pinned: "{}",
        _defaults: JSON.stringify({ reporter: "alice" }),
      }),
    ])
    const { view, container } = mount(doc)
    try {
      await waitForForm(container)
      const reporter = inputFor(container, "reporter") as HTMLInputElement
      expect(reporter.value).toBe("alice")
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("does NOT lock the input — the user is free to edit", async () => {
    currentSchema = { fields: [{ name: "reporter", type: "text" }] }
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_newrow.create({
        table: "tickets",
        _pinned: "{}",
        _defaults: JSON.stringify({ reporter: "alice" }),
      }),
    ])
    const { view, container } = mount(doc)
    try {
      await waitForForm(container)
      const reporter = inputFor(container, "reporter") as HTMLInputElement
      // Default is NOT the locked class pinned fields get.
      expect(reporter.classList.contains("gowiki-database-pinned")).toBe(false)
      // And the DOM lets the user actually overwrite the value.
      reporter.value = "bob"
      expect(reporter.value).toBe("bob")
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("pinned wins over a conflicting default on the same key", async () => {
    currentSchema = { fields: [{ name: "owner", type: "text" }] }
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.database_newrow.create({
        table: "tickets",
        _pinned: JSON.stringify({ owner: "alice" }),
        _defaults: JSON.stringify({ owner: "bob" }), // ignored
      }),
    ])
    const { view, container } = mount(doc)
    try {
      await waitForForm(container)
      const owner = inputFor(container, "owner") as HTMLInputElement
      expect(owner.value).toBe("alice")
      expect(owner.classList.contains("gowiki-database-pinned")).toBe(true)
    } finally {
      view.destroy()
      container.remove()
    }
  })
})
