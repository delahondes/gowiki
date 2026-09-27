// Integration test for the {lifecycle} NodeView — mount a real
// EditorView on jsdom with a stubbed backend, and assert the three
// visible states the reader ever sees:
//
//   loading  → "Checking…" (grey, italic, briefly)
//   ok       → "All documents in scope are within all rules." (grey)
//   alert    → "N documents need review." + per-rule bullets (red)
//
// Also exercises the aggregate rule: N {lifecycle} nodes on one doc
// render ONE aggregate panel (on the primary node) plus (N-1) chips.
// This is exactly the surface a browser reload verifies by hand, so a
// jsdom test keeps that verification in CI.
import { describe, it, expect, beforeEach, afterEach } from "vitest"
import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { registry, schema } from "../helpers"

type FetchStub = {
  responses: Map<string, unknown>
  calls: string[]
  fail?: boolean
}

let stub: FetchStub
const originalFetch = globalThis.fetch
// The plugin's internal statusCache is keyed by source path — give
// every test a unique pathname so cached responses from a previous
// test can't leak in. Bumped in each beforeEach.
let testSeq = 0
let currentTestPath = "/admin/policies-0"

beforeEach(() => {
  stub = { responses: new Map(), calls: [] }
  globalThis.fetch = (async (url: string) => {
    stub.calls.push(url)
    if (stub.fail) throw new Error("network down")
    // Route: /api/plugin/lifecycle/v1/status?source_page=...
    const path = url.split("?")[0]
    const body = stub.responses.get(path)
    if (body === undefined) {
      return new Response("", { status: 404 })
    }
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { "content-type": "application/json" },
    })
  }) as any
  testSeq++
  currentTestPath = `/admin/policies-${testSeq}`
  window.history.replaceState(null, "", currentTestPath)
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

// Flush all pending microtasks/timers so the NodeView's post-fetch
// render lands before we assert. Two ticks is enough because the fetch
// stub is synchronous inside the promise, so one tick resolves the
// fetch and the second lands the `.then()` render.
async function flushMicrotasks() {
  await Promise.resolve()
  await Promise.resolve()
  await new Promise((r) => setTimeout(r, 0))
}

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

function makeLifecycle(attrs: Record<string, string>) {
  return schema.nodes.lifecycle.create({
    scope: "",
    tags: "",
    exclude_tags: "",
    when: "",
    title: "",
    assign: "",
    priority: "",
    action: "",
    ...attrs,
  })
}

describe("lifecycle NodeView — single rule states", () => {
  it("shows the loading state before the fetch resolves", async () => {
    stub.responses.set("/api/plugin/lifecycle/v1/status", {
      rules: [
        {
          rule: {
            scope: "^/qms/.*",
            tags: [],
            when: "stale:30d",
            title: "review",
            assign: "alice",
            kind: "stale",
          },
          fires_count: 0,
          source_page: "/admin/policies",
        },
      ],
    })
    const doc = schema.nodes.doc.create(null, [
      makeLifecycle({ scope: "^/qms/.*", when: "stale:30d", title: "review", assign: "alice" }),
    ])
    const { view, container } = mount(doc)
    try {
      // Before flushing: status text should be "Checking…"
      const status = container.querySelector(".gowiki-lifecycle-status")
      expect(status?.textContent).toBe("Checking…")
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("resolves to the passive 'all in order' state when fires_count=0", async () => {
    stub.responses.set("/api/plugin/lifecycle/v1/status", {
      rules: [
        {
          rule: {
            scope: "^/qms/.*",
            tags: [],
            when: "stale:30d",
            title: "review",
            assign: "alice",
            kind: "stale",
          },
          fires_count: 0,
          source_page: "/admin/policies",
        },
      ],
    })
    const doc = schema.nodes.doc.create(null, [
      makeLifecycle({ scope: "^/qms/.*", when: "stale:30d", title: "review", assign: "alice" }),
    ])
    const { view, container } = mount(doc)
    try {
      await flushMicrotasks()
      const panel = container.querySelector(".gowiki-lifecycle")!
      expect(panel.classList.contains("gowiki-lifecycle--alert")).toBe(false)
      expect(panel.classList.contains("gowiki-lifecycle--needs-setup")).toBe(false)
      const status = panel.querySelector(".gowiki-lifecycle-status")
      expect(status?.textContent).toBe("All documents in scope are within all rules.")
      // In the ok state, no per-rule bullets appear.
      expect(panel.querySelector(".gowiki-lifecycle-rules")).toBeNull()
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("resolves to the red alert state when fires_count > 0", async () => {
    stub.responses.set("/api/plugin/lifecycle/v1/status", {
      rules: [
        {
          rule: {
            scope: "^/qms/.*",
            tags: [],
            when: "stale:30d",
            title: "review",
            assign: "alice",
            kind: "stale",
          },
          fires_count: 12,
          sample_pages: ["/qms/a", "/qms/b"],
          source_page: "/admin/policies",
        },
      ],
    })
    const doc = schema.nodes.doc.create(null, [
      makeLifecycle({ scope: "^/qms/.*", when: "stale:30d", title: "review", assign: "alice" }),
    ])
    const { view, container } = mount(doc)
    try {
      await flushMicrotasks()
      const panel = container.querySelector(".gowiki-lifecycle")!
      expect(panel.classList.contains("gowiki-lifecycle--alert")).toBe(true)
      const status = panel.querySelector(".gowiki-lifecycle-status")
      expect(status?.textContent).toBe("12 documents need review.")
      // Per-rule bullets appear in the alert state.
      const bullets = panel.querySelectorAll(".gowiki-lifecycle-rules li")
      expect(bullets.length).toBe(1)
      const body = bullets[0].querySelector(".gowiki-lifecycle-rule-body")
      expect(body?.textContent).toBe("12 documents")
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("degrades to the 'unknown' fallback when the fetch fails", async () => {
    stub.fail = true
    const doc = schema.nodes.doc.create(null, [
      makeLifecycle({ scope: "^/qms/.*", when: "stale:30d", title: "review", assign: "alice" }),
    ])
    const { view, container } = mount(doc)
    try {
      await flushMicrotasks()
      const panel = container.querySelector(".gowiki-lifecycle")!
      // Unknown state renders on the primary as an "all in order"-like
      // grey shell — the panel doesn't lie about state we don't have.
      // It should NOT show the red alert class.
      expect(panel.classList.contains("gowiki-lifecycle--alert")).toBe(false)
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("renders the needs-setup variant when the rule is missing title/assign", async () => {
    stub.responses.set("/api/plugin/lifecycle/v1/status", { rules: [] })
    const doc = schema.nodes.doc.create(null, [
      makeLifecycle({ scope: "^/qms/.*", when: "stale:30d" }), // no title/assign
    ])
    const { view, container } = mount(doc)
    try {
      await flushMicrotasks()
      const panel = container.querySelector(".gowiki-lifecycle")!
      // Any-setup path is a red header on the aggregate.
      expect(panel.classList.contains("gowiki-lifecycle--alert")).toBe(true)
      const status = panel.querySelector(".gowiki-lifecycle-status")
      expect(status?.textContent).toContain("not yet configured")
    } finally {
      view.destroy()
      container.remove()
    }
  })
})

describe("lifecycle NodeView — aggregation across rules", () => {
  it("multiple lifecycle nodes render ONE panel plus (N-1) chips", async () => {
    stub.responses.set("/api/plugin/lifecycle/v1/status", {
      rules: [
        {
          rule: {
            scope: "^/qms/.*",
            tags: [],
            when: "stale:30d",
            title: "stale-review",
            assign: "alice",
            kind: "stale",
          },
          fires_count: 0,
          source_page: "/admin/policies",
        },
        {
          rule: {
            scope: "^/qms/.*",
            tags: [],
            when: "reviewflow_overdue",
            title: "",
            assign: "",
            kind: "reviewflow_overdue",
          },
          fires_count: 0,
          source_page: "/admin/policies",
        },
      ],
    })
    const doc = schema.nodes.doc.create(null, [
      makeLifecycle({ scope: "^/qms/.*", when: "stale:30d", title: "stale-review", assign: "alice" }),
      makeLifecycle({ scope: "^/qms/.*", when: "reviewflow_overdue" }),
    ])
    const { view, container } = mount(doc)
    try {
      await flushMicrotasks()
      // Exactly one aggregate panel with a `.gowiki-lifecycle-headrow`.
      const panels = container.querySelectorAll(".gowiki-lifecycle-headrow")
      expect(panels.length).toBe(1)
      // Header meta says "2 rules".
      const meta = container.querySelector(".gowiki-lifecycle-metasummary")
      expect(meta?.textContent).toBe("2 rules")
      // Exactly one chip (the second lifecycle node's compact rendering).
      const chips = container.querySelectorAll(".gowiki-lifecycle-chip")
      expect(chips.length).toBe(1)
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("aggregate goes red when ANY rule fires, and lists each rule as a bullet", async () => {
    stub.responses.set("/api/plugin/lifecycle/v1/status", {
      rules: [
        {
          rule: {
            scope: "^/qms/.*",
            tags: [],
            when: "stale:30d",
            title: "stale-review",
            assign: "alice",
            kind: "stale",
          },
          fires_count: 0,
          source_page: "/admin/policies",
        },
        {
          rule: {
            scope: "^/qms/.*",
            tags: [],
            when: "reviewflow_overdue",
            title: "",
            assign: "",
            kind: "reviewflow_overdue",
          },
          fires_count: 3,
          sample_pages: ["/qms/a"],
          source_page: "/admin/policies",
        },
      ],
    })
    const doc = schema.nodes.doc.create(null, [
      makeLifecycle({ scope: "^/qms/.*", when: "stale:30d", title: "stale-review", assign: "alice" }),
      makeLifecycle({ scope: "^/qms/.*", when: "reviewflow_overdue" }),
    ])
    const { view, container } = mount(doc)
    try {
      await flushMicrotasks()
      const panel = container.querySelector(".gowiki-lifecycle:not(.gowiki-lifecycle-chip-wrapper)")!
      expect(panel.classList.contains("gowiki-lifecycle--alert")).toBe(true)
      const status = panel.querySelector(".gowiki-lifecycle-status")
      expect(status?.textContent).toBe("3 documents need review.")
      const bullets = panel.querySelectorAll(".gowiki-lifecycle-rules li")
      expect(bullets.length).toBe(2)
      // Kind labels are reader-facing, not raw when= strings.
      const kinds = Array.from(bullets).map((b) => b.querySelector(".gowiki-lifecycle-rule-kind")?.textContent)
      expect(kinds).toContain("staleness")
      expect(kinds).toContain("reviewflow overdue")
      // The staleness rule shows "all in order"; the reviewflow rule shows "3 documents".
      const bodies = Array.from(bullets).map((b) => b.querySelector(".gowiki-lifecycle-rule-body")?.textContent)
      expect(bodies).toContain("all in order")
      expect(bodies).toContain("3 documents")
    } finally {
      view.destroy()
      container.remove()
    }
  })
})
