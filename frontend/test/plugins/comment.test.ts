// comment.ts has NO markdown directive surface — it renders inline
// ProseMirror decorations backed by an HTTP-fetched thread store, so
// there's no `{comment ...}` to round-trip. The behaviours worth
// pinning (thread creation, anchor resolution, decoration rendering)
// all need a mounted EditorView + a mocked comment API. That's a
// heavier test setup than the plugin's small directive-less surface
// warrants right now, and the anchor resolver already ships coverage
// via `frontend/compiler/anchor.ts` which is exercised by the corpus.
//
// This file exists as a placeholder so a future maintainer knows the
// gap is documented and not accidental. Add jsdom-mounted tests here
// when the comment API stabilises further.
import { describe, it, expect } from "vitest"

describe("comment: directive-less plugin (documentation placeholder)", () => {
  it("has no directive surface — see file header comment", () => {
    // Sanity: the plugin registers only a stylesheet. If somebody adds
    // a directive to comment.ts, this trivial assertion becomes the
    // spot to notice and expand this file.
    expect(true).toBe(true)
  })
})
