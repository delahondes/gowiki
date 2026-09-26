// Smoke test — proves the harness wires up correctly. Real coverage lives
// in test/corpus/*.test.ts.
import { describe, it, expect } from "vitest"
import { roundTrip, assertUnderlineMark, assertStrongMark } from "./helpers"

describe("harness smoke", () => {
  it("round-trips a plain paragraph", () => {
    const rt = roundTrip("Hello world.\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first.trim()).toBe("Hello world.")
  })

  it("round-trips a paragraph with underline and bold", () => {
    const rt = roundTrip("This has _underline_ and **bold**.\n")
    expect(rt.isStable).toBe(true)
    assertUnderlineMark(rt.doc, "underline")
    assertStrongMark(rt.doc, "bold")
  })
})
