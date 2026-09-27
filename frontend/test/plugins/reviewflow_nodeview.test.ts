// Integration test for the {reviewflow} NodeView — asserts the
// draft-with-published notice ("You're viewing a draft. The
// published version is here.") renders when:
//
//   - the current version is NOT fully validated, AND
//   - a previously-validated version exists (validated_page_version > 0)
//
// This is the SURFACE the involved reader sees when the backend's
// hide_drafts_from_uninvolved gate is on and swaps content for
// outsiders — inside the review chain the reader still gets the WIP
// but with a pointer to the authoritative version.
import { describe, it, expect, beforeEach, afterEach } from "vitest"
import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { registry, schema } from "../helpers"

const originalFetch = globalThis.fetch
let testSeq = 0
let currentTestPath = "/qms/proc/0"
let statusResponse: unknown = null

beforeEach(() => {
  testSeq++
  currentTestPath = `/qms/proc/${testSeq}`
  window.history.replaceState(null, "", currentTestPath)
  globalThis.fetch = (async (url: string) => {
    if (url.includes("/api/plugin/reviewflow/v1/status")) {
      return new Response(JSON.stringify(statusResponse ?? {}), {
        status: 200,
        headers: { "content-type": "application/json" },
      })
    }
    return new Response("", { status: 404 })
  }) as any
})

afterEach(() => {
  globalThis.fetch = originalFetch
  statusResponse = null
})

async function flushMicrotasks() {
  await Promise.resolve()
  await Promise.resolve()
  await new Promise((r) => setTimeout(r, 0))
}

function mount(): { view: EditorView; container: HTMLElement } {
  const container = document.createElement("div")
  document.body.appendChild(container)
  const doc = schema.nodes.doc.create(null, [
    schema.nodes.reviewflow.create({
      version: "1.0",
      roles: JSON.stringify({ author: "alice", reviewer: "bob" }),
    }),
  ])
  const state = EditorState.create({
    schema,
    doc,
    plugins: registry.getEditorPlugins(),
  })
  const view = new EditorView(container, { state })
  return { view, container }
}

describe("reviewflow NodeView — draft-with-published notice", () => {
  it("shows the notice with a ?v=N link when a validated version exists and current is draft", async () => {
    statusResponse = {
      roles: { author: "alice", reviewer: "bob" },
      version_tag: "1.1",
      current_page_version: 2,
      validated_page_version: 1,
      missing_roles: { reviewer: "bob" },
      is_fully_validated: false,
    }
    const { view, container } = mount()
    try {
      await flushMicrotasks()
      const notice = container.querySelector(".gowiki-rf-draft-notice")
      expect(notice).not.toBeNull()
      expect(notice!.textContent).toContain("You're viewing a draft")
      const link = notice!.querySelector("a")
      expect(link).not.toBeNull()
      expect((link as HTMLAnchorElement).getAttribute("href")).toBe(`${currentTestPath}?v=1`)
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("does NOT show the notice when the current version is fully validated", async () => {
    statusResponse = {
      roles: { author: "alice", reviewer: "bob" },
      version_tag: "1.0",
      current_page_version: 1,
      validated_page_version: 1,
      missing_roles: {},
      is_fully_validated: true,
    }
    const { view, container } = mount()
    try {
      await flushMicrotasks()
      expect(container.querySelector(".gowiki-rf-draft-notice")).toBeNull()
    } finally {
      view.destroy()
      container.remove()
    }
  })

  it("does NOT show the notice when there is no validated version yet (first draft)", async () => {
    statusResponse = {
      roles: { author: "alice", reviewer: "bob" },
      version_tag: "0.1",
      current_page_version: 1,
      validated_page_version: 0,
      missing_roles: { author: "alice", reviewer: "bob" },
      is_fully_validated: false,
    }
    const { view, container } = mount()
    try {
      await flushMicrotasks()
      // A first draft with no prior validated version has nothing to
      // link to — the notice is suppressed rather than showing a
      // broken link.
      expect(container.querySelector(".gowiki-rf-draft-notice")).toBeNull()
    } finally {
      view.destroy()
      container.remove()
    }
  })
})
