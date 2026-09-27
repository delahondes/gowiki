// Regression test for the comment-position drift bug (rc.2 → rc.3):
//
// A comment's AnchorRange has a range-level TextQuote (the exact
// selected text + prefix/suffix). Point-level TextQuotes are usually
// absent. When a structural change inserts a block before the anchor
// (a new heading in the same section, a new paragraph), nodeIndex
// shifts and the structural resolution drifts to the wrong node.
// The old resolveRangeInPm accepted "both endpoints exact" without
// validating the resolved slice against the range's exact snippet, so
// the range-level fuzzy search (which WOULD find the right occurrence)
// never fired and the highlight landed several paragraphs off.
//
// The fix: verify the resolved slice's plain text against
// anchor.textQuote.exact when both endpoints resolve; on mismatch,
// fall through to fuzzyFindRangeInPm. This test pins the whole path.
import { describe, it, expect } from "vitest"
import { schema } from "./helpers"
import { rangeFromPm, resolveRangeInPm } from "../compiler/anchor"

function paragraphOf(text: string) {
  return schema.nodes.paragraph.create(null, [schema.text(text)])
}

function heading(level: number, text: string) {
  return schema.nodes.heading.create({ level }, [schema.text(text)])
}

describe("comment anchor — drift regression", () => {
  it("finds the right occurrence when a block is inserted before the anchor", () => {
    // Author writes a doc, selects "M4 process owner" in the FIFTH
    // content node, creates a comment. rangeFromPm records the address
    // (nodeIndex=4) plus a range TextQuote whose exact = "M4 process owner".
    const original = schema.nodes.doc.create(null, [
      heading(2, "8.6. Following Up The Actions"),
      paragraphOf("The change control responsible..."),
      paragraphOf("The evaluation of the efficiency of the actions..."),
      paragraphOf("Once the efficiency of the actions and the compliance with the new regulatory..."),
      heading(2, "8.7. Updating The List Of Applicable Standards And Regulation"),
      paragraphOf("Once the monitoring information is closed, the M4 process owner verifies..."),
    ])
    // Find the PM range of "M4 process owner" in the original doc.
    const originalPlain = "Once the monitoring information is closed, the M4 process owner verifies..."
    const targetStart = originalPlain.indexOf("M4 process owner")
    // Walk to the fifth paragraph's start position.
    let paragraphPos = 0
    let matches = 0
    original.descendants((n, pos) => {
      if (matches < 6 && (n.type.name === "paragraph" || n.type.name === "heading")) {
        matches++
        if (matches === 6) {
          paragraphPos = pos + 1
          return false
        }
      }
    })
    const from = paragraphPos + targetStart
    const to = from + "M4 process owner".length
    const anchor = rangeFromPm(original, from, to, { withTextQuote: true })
    expect(anchor.textQuote?.exact).toBe("M4 process owner")

    // Now insert an extra heading between 8.6 and the paragraph list
    // — simulates the structural drift that shifted nodeIndex. The
    // exact text "M4 process owner" is still uniquely present, just at
    // a different position + a different content-node index.
    const drifted = schema.nodes.doc.create(null, [
      heading(2, "8.6. Following Up The Actions"),
      heading(3, "8.6.1. A newly-added subsection heading"),
      paragraphOf("The change control responsible..."),
      paragraphOf("The evaluation of the efficiency of the actions..."),
      paragraphOf("Once the efficiency of the actions and the compliance with the new regulatory..."),
      heading(2, "8.7. Updating The List Of Applicable Standards And Regulation"),
      paragraphOf("Once the monitoring information is closed, the M4 process owner verifies..."),
    ])
    const resolved = resolveRangeInPm(drifted, anchor)
    // The fix: resolver must find "M4 process owner" in the drifted
    // doc, not the wrong position that raw structural drift would
    // produce.
    const slice = drifted.textBetween(resolved.from, resolved.to, "\n")
    expect(slice).toBe("M4 process owner")
  })

  it("fast path stays fast when the doc has NOT drifted (structural still exact)", () => {
    const doc = schema.nodes.doc.create(null, [
      paragraphOf("Alpha bravo charlie delta echo foxtrot"),
      paragraphOf("Golf hotel india juliett kilo lima mike"),
    ])
    // Pick a range in the second paragraph.
    let secondPos = 0
    let seen = 0
    doc.descendants((n, pos) => {
      if (n.type.name === "paragraph") {
        seen++
        if (seen === 2) {
          secondPos = pos + 1
          return false
        }
      }
    })
    const target = "india juliett"
    const idxInPara = "Golf hotel india juliett kilo lima mike".indexOf(target)
    const from = secondPos + idxInPara
    const to = from + target.length
    const anchor = rangeFromPm(doc, from, to, { withTextQuote: true })
    const resolved = resolveRangeInPm(doc, anchor)
    expect(resolved.confidence).toBe("exact")
    expect(doc.textBetween(resolved.from, resolved.to, "\n")).toBe(target)
  })

  it("picks the right occurrence when the exact text appears multiple times", () => {
    // The exact text "review" appears in two paragraphs. Comment was
    // on the SECOND one — prefix/suffix scoring in findBestQuoteMatch
    // should prefer that occurrence when the structural address
    // drifts to point at the first.
    const doc = schema.nodes.doc.create(null, [
      paragraphOf("Please review the first document."),
      paragraphOf("Then review the second document (the important one)."),
    ])
    // Anchor on the SECOND "review".
    let paraPos: number[] = []
    doc.descendants((n, pos) => {
      if (n.type.name === "paragraph") paraPos.push(pos + 1)
    })
    const secondPara = "Then review the second document (the important one)."
    const from = paraPos[1] + secondPara.indexOf("review")
    const to = from + "review".length
    const anchor = rangeFromPm(doc, from, to, { withTextQuote: true })
    expect(anchor.textQuote?.exact).toBe("review")

    // Insert a new paragraph BEFORE the second — nodeIndex for the
    // real target shifts by one.
    const drifted = schema.nodes.doc.create(null, [
      paragraphOf("Please review the first document."),
      paragraphOf("Inserted paragraph that shifts everything after it."),
      paragraphOf("Then review the second document (the important one)."),
    ])
    const resolved = resolveRangeInPm(drifted, anchor)
    // Must land on the SECOND-para occurrence (now paragraph 3),
    // not the first-para occurrence, thanks to prefix/suffix scoring.
    const before = drifted.textBetween(Math.max(0, resolved.from - 25), resolved.from, "\n")
    expect(before).toContain("Then ")
  })
})
