// Tests for applyPinnedValue — the helper that locks a pinned field
// value on the {database-newrow} form. Two regression concerns
// motivate this file:
//
//   1. When the async foreign-key loader populates the select AFTER
//      pinning has run, the synthetic option we prepended (value
//      only, no label) must be relabelled with the real option's
//      text and the duplicate must be hidden. Otherwise the reader
//      sees the raw id ("1") beside the resolved label ("Gowiki").
//
//   2. A pinned select must expose the pinned value via `.value` for
//      the JSON submitter to pick up. The DOM `disabled` attribute
//      excludes an input from native form submits — we don't use
//      those, but avoiding disabled removes the whole class of bug
//      the pattern invites.
import { describe, it, expect } from "vitest"
import { applyPinnedValue } from "../../plugins/database"

async function nextTick() {
  await Promise.resolve()
  await new Promise((r) => setTimeout(r, 0))
}

describe("applyPinnedValue — select", () => {
  it("prepends a synthetic option when the target value isn't loaded yet", () => {
    const sel = document.createElement("select")
    // Only the placeholder is present at pin time.
    const placeholder = document.createElement("option")
    placeholder.value = ""
    placeholder.textContent = "-- Select --"
    sel.appendChild(placeholder)

    applyPinnedValue(sel, "1")

    expect(sel.value).toBe("1")
    // A synthetic option is prepended.
    expect(sel.options[0].value).toBe("1")
    expect(sel.options[0].dataset.pinnedSynthetic).toBe("1")
    // Pinned = read-only via CSS/ARIA, NOT the disabled attribute,
    // so `.value` still exposes the pinned value to JS submitters.
    expect(sel.disabled).toBe(false)
    expect(sel.getAttribute("aria-disabled")).toBe("true")
  })

  it("relabels the synthetic option and hides the duplicate when the real option arrives", async () => {
    const sel = document.createElement("select")
    sel.appendChild(Object.assign(document.createElement("option"), { value: "", textContent: "-- Select --" }))
    applyPinnedValue(sel, "1")
    // The synthetic label is the bare value until we know better.
    expect(sel.options[0].textContent).toBe("1")

    // Simulate the async foreign-key loader appending the real option.
    const real = document.createElement("option")
    real.value = "1"
    real.textContent = "Gowiki"
    sel.appendChild(real)

    await nextTick()

    // The synthetic (still at index 0) now carries the real label.
    expect(sel.options[0].textContent).toBe("Gowiki")
    // And the duplicate is hidden so the (locked) dropdown doesn't
    // show two entries with the same value.
    expect(real.hidden).toBe(true)
  })

  it("does NOT prepend a synthetic when the target value is already loaded", () => {
    const sel = document.createElement("select")
    sel.appendChild(Object.assign(document.createElement("option"), { value: "", textContent: "-- Select --" }))
    sel.appendChild(Object.assign(document.createElement("option"), { value: "1", textContent: "Gowiki" }))

    applyPinnedValue(sel, "1")

    // The real option is selected; no synthetic was added.
    expect(sel.value).toBe("1")
    expect(sel.options[0].dataset.pinnedSynthetic).toBeUndefined()
    // The Gowiki option now shows selected.
    expect(sel.options[1].selected).toBe(true)
  })

  it("snaps back to the pinned value if a change event tries to move it", () => {
    const sel = document.createElement("select")
    sel.appendChild(Object.assign(document.createElement("option"), { value: "", textContent: "-- Select --" }))
    sel.appendChild(Object.assign(document.createElement("option"), { value: "1", textContent: "Gowiki" }))
    sel.appendChild(Object.assign(document.createElement("option"), { value: "2", textContent: "Other" }))

    applyPinnedValue(sel, "1")
    expect(sel.value).toBe("1")

    // Simulate JS mutation followed by a change event.
    sel.value = "2"
    sel.dispatchEvent(new Event("change"))
    expect(sel.value).toBe("1")
  })
})

describe("applyPinnedValue — input", () => {
  it("locks a text input read-only with the pinned value", () => {
    const inp = document.createElement("input")
    inp.type = "text"
    applyPinnedValue(inp, "hello")
    expect(inp.value).toBe("hello")
    expect(inp.readOnly).toBe(true)
  })

  it("snaps back on change events for a text input", () => {
    const inp = document.createElement("input")
    inp.type = "text"
    applyPinnedValue(inp, "hello")
    inp.value = "changed"
    inp.dispatchEvent(new Event("change"))
    expect(inp.value).toBe("hello")
  })
})
