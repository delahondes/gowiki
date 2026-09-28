// initComments' default-collapse rule: when the doc has comments but
// every top-level thread is resolved, the sidebar starts collapsed
// (only a corner chip). One unresolved thread keeps the sidebar
// expanded. This is the reader-facing signal that "the archive is
// there without demanding your attention," and a regression here
// would silently push readers back into the noise-heavy layout.
import { describe, it, expect, beforeEach, afterEach } from "vitest"
import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { schema, registry } from "../helpers"
import { initComments, destroyComments } from "../../plugins/comment"

interface Row {
  id: string
  text: string
  resolved: boolean
  parent_id?: string
  anchor?: { selected: string; before: string; after: string }
  author?: string
  created_at?: string
  updated_at?: string
}

function serializeRows(rows: Row[]) {
  return {
    comments: rows.map((r) => ({
      id: r.id,
      text: r.text,
      resolved: r.resolved,
      parent_id: r.parent_id ?? "",
      author: r.author ?? "alice",
      created_at: r.created_at ?? "2026-01-01T00:00:00Z",
      updated_at: r.updated_at ?? "2026-01-01T00:00:00Z",
      anchor: r.anchor ?? { selected: r.text, before: "", after: "" },
    })),
  }
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
  await new Promise((r) => setTimeout(r, 0))
}

let container: HTMLElement | null = null
let view: EditorView | null = null
let originalFetch: typeof globalThis.fetch
let stubbedRows: Row[] = []

// jsdom has no ResizeObserver / IntersectionObserver. The comment
// plugin's reflow logic uses ResizeObserver; a no-op stub is enough
// to let the sidebar render.
class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (typeof (globalThis as any).ResizeObserver === "undefined") {
  ;(globalThis as any).ResizeObserver = NoopResizeObserver
}

beforeEach(() => {
  // The plugin looks up `#main` to mount its sidebar / collapsed chip.
  const main = document.createElement("div")
  main.id = "main"
  document.body.appendChild(main)
  container = main

  const doc = schema.nodes.doc.create(null, [schema.nodes.paragraph.create(null, [schema.text("body")])])
  const state = EditorState.create({ schema, doc, plugins: registry.getEditorPlugins() })
  view = new EditorView(main, { state })

  // fetchComments uses global fetch (no auth needed for the read
  // path). Stub it so initComments loads the rows this test cares
  // about — the URL is /api/plugin/comment/v1/<path>.
  originalFetch = globalThis.fetch
  globalThis.fetch = (async (url: string) => {
    if (typeof url === "string" && url.startsWith("/api/plugin/comment/v1/")) {
      return new Response(JSON.stringify(serializeRows(stubbedRows)), {
        status: 200,
        headers: { "content-type": "application/json" },
      })
    }
    return new Response("", { status: 404 })
  }) as any
})

afterEach(() => {
  destroyComments()
  if (view) view.destroy()
  view = null
  if (container) container.remove()
  container = null
  globalThis.fetch = originalFetch
  stubbedRows = []
})

async function bootWithRows(rows: Row[], pagePath: string) {
  stubbedRows = rows
  await initComments({
    pagePath,
    view: view!,
    authFetch: (async () => new Response("", { status: 200 })) as any,
    username: "alice",
    isAdmin: false,
  })
  await flush()
}

describe("comment sidebar default state", () => {
  it("collapses when every top-level thread is resolved", async () => {
    await bootWithRows(
      [
        { id: "1", text: "old discussion", resolved: true },
        { id: "2", text: "another old discussion", resolved: true },
      ],
      "/all-resolved"
    )
    // Sidebar should NOT be mounted (collapsed default).
    expect(container!.querySelector("#comment-sidebar")).toBeNull()
    // Corner chip IS mounted with a checkmark label — nothing to act on.
    const chip = container!.querySelector(".comment-collapsed-toggle")
    expect(chip).not.toBeNull()
    expect(chip!.textContent).toContain("✓")
  })

  it("stays expanded when at least one top-level thread is unresolved", async () => {
    await bootWithRows(
      [
        { id: "1", text: "live question", resolved: false },
        { id: "2", text: "settled", resolved: true },
      ],
      "/one-open"
    )
    expect(container!.querySelector("#comment-sidebar")).not.toBeNull()
    expect(container!.querySelector(".comment-collapsed-toggle")).toBeNull()
  })

  it("does not force a collapsed state when the doc has no comments", async () => {
    await bootWithRows([], "/no-comments")
    // No comments = no sidebar and no chip — the plugin is silent.
    expect(container!.querySelector("#comment-sidebar")).toBeNull()
    expect(container!.querySelector(".comment-collapsed-toggle")).toBeNull()
  })

  it("replies to a resolved top-level thread do not force expansion", async () => {
    // The doc has one root thread (resolved) and two replies to it.
    // Replies inherit the parent's resolution — they are not their
    // own threads. The count of unresolved TOP-LEVEL threads is 0,
    // so the sidebar starts collapsed.
    await bootWithRows(
      [
        { id: "root", text: "closed conversation", resolved: true },
        { id: "r1", text: "afterthought", resolved: false, parent_id: "root" },
        { id: "r2", text: "second afterthought", resolved: false, parent_id: "root" },
      ],
      "/replies-only"
    )
    expect(container!.querySelector("#comment-sidebar")).toBeNull()
    expect(container!.querySelector(".comment-collapsed-toggle")).not.toBeNull()
  })
})
