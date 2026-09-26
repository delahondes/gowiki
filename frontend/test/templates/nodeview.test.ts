// NodeView smoke tests for the template plugin.
//
// The four template atoms each install a NodeView. We mount the editor,
// build a doc holding each atom, and assert the constructor ran and put
// its DOM in place. Not an interaction test — a regression here would
// otherwise only be visible in the browser.
//
// jsdom lacks a real /api layer; the marker NodeView fires a background
// fetch to `/api/reviewflow/…`. We stub globalThis.fetch to reject so the
// component degrades to its offline render (which is what we assert).
import { describe, it, expect, beforeEach, afterEach } from "vitest"
import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { registry, schema } from "../helpers"

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

describe("template_marker NodeView", () => {
  it("mounts and renders the 'Template — payload below…' label + Create-document button", () => {
    const doc = schema.nodes.doc.create(null, [schema.nodes.template_marker.create({})])
    const { view, container } = mount(doc)
    try {
      const el = container.querySelector(".gowiki-template-marker")
      expect(el).not.toBeNull()
      // The label and the Create-document button are the two visible bits.
      expect(el!.textContent).toMatch(/Template/)
      const btn = el!.querySelector("button.gowiki-template-create-btn")
      expect(btn).not.toBeNull()
      expect(btn!.textContent).toMatch(/Create document/i)
    } finally {
      view.destroy()
      container.remove()
    }
  })
})

describe("template_title NodeView", () => {
  it("mounts and renders a title-pattern hint", () => {
    const doc = schema.nodes.doc.create(null, [schema.nodes.template_title.create({})])
    const { view, container } = mount(doc)
    try {
      const el = container.querySelector(".gowiki-template-title")
      expect(el).not.toBeNull()
    } finally {
      view.destroy()
      container.remove()
    }
  })
})

describe("template_stamp NodeView", () => {
  it("in a page WITH {template}: renders the read-only stamp note", () => {
    const doc = schema.nodes.doc.create(null, [
      schema.nodes.template_marker.create({}),
      schema.nodes.template_stamp.create({}),
    ])
    const { view, container } = mount(doc)
    try {
      const stamp = container.querySelector(".gowiki-template-stamp")
      expect(stamp).not.toBeNull()
      // "Created from template: stamped on document creation" note.
      const note = stamp!.querySelector(".gowiki-template-stamp-note")
      expect(note).not.toBeNull()
      expect(note!.textContent).toMatch(/stamped on document creation/i)
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("without a {template} marker: renders the unresolved-stamp error", () => {
    // A {template-stamp} outside a template context is a real error —
    // stamps are meant to be filled in at creation time from the template
    // marker's presence.
    const doc = schema.nodes.doc.create(null, [schema.nodes.template_stamp.create({})])
    const { view, container } = mount(doc)
    try {
      const stamp = container.querySelector(".gowiki-template-stamp")
      expect(stamp).not.toBeNull()
      const err = stamp!.querySelector(".gowiki-template-stamp-error")
      expect(err).not.toBeNull()
      expect(err!.textContent).toMatch(/Unresolved/i)
    } finally {
      view.destroy()
      container.remove()
    }
  })
})

describe("template_reviewflow NodeView", () => {
  it("renders the reviewflow summary with the stored version + role map", () => {
    const roles = JSON.stringify({ author: "alice", reviewer: "bob" })
    const doc = schema.nodes.doc.create(null, [schema.nodes.template_reviewflow.create({ version: "1.0", roles })])
    const { view, container } = mount(doc)
    try {
      const el = container.querySelector(".gowiki-template-reviewflow")
      expect(el).not.toBeNull()
      // Summary line must reflect the attrs — we don't assert exact
      // formatting to stay resilient to cosmetic tweaks, just that both
      // pieces of information appear in the rendered text.
      const text = el!.textContent ?? ""
      expect(text).toContain("1.0")
      expect(text).toContain("alice")
      expect(text).toContain("bob")
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("mounts cleanly with empty attrs (no version, no roles)", () => {
    const doc = schema.nodes.doc.create(null, [schema.nodes.template_reviewflow.create({ version: "", roles: "{}" })])
    const { view, container } = mount(doc)
    try {
      const el = container.querySelector(".gowiki-template-reviewflow")
      expect(el).not.toBeNull()
    } finally {
      view.destroy()
      container.remove()
    }
  })
})
