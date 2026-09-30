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
    const paraPos: number[] = []
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

  // Second regression, discovered on wiki.gmt.bio: the anchored text
  // `(standards or paragraphs of standards)` was DELETED from the doc
  // during a later edit. The old resolveRangeInPm still returned the
  // structural position with confidence "fuzzy" — so the comment
  // highlight landed on random text ("isk analysis and the criticality
  // of the") several paragraphs away. Deleted anchors must resolve as
  // "lost" so the caller marks the comment as orphaned.
  it("reports confidence 'lost' when the exact text is deleted from the doc", () => {
    const original = schema.nodes.doc.create(null, [
      paragraphOf(
        "The audit program also identifies the audit criteria (standards or paragraphs of standards), the auditors, and the frequency of audits."
      ),
    ])
    // Anchor the phrase inside the parens.
    let paraPos = 0
    original.descendants((n, pos) => {
      if (n.type.name === "paragraph") {
        paraPos = pos + 1
        return false
      }
    })
    const target = "(standards or paragraphs of standards)"
    const plain =
      "The audit program also identifies the audit criteria (standards or paragraphs of standards), the auditors, and the frequency of audits."
    const from = paraPos + plain.indexOf(target)
    const to = from + target.length
    const anchor = rangeFromPm(original, from, to, { withTextQuote: true })
    expect(anchor.textQuote?.exact).toBe(target)

    // Someone rewrites the paragraph and strips the parenthetical.
    const rewritten = schema.nodes.doc.create(null, [
      paragraphOf("The audit program also identifies the audit criteria, the auditors, and the frequency of audits."),
    ])
    const resolved = resolveRangeInPm(rewritten, anchor)
    expect(resolved.confidence).toBe("lost")
    // The whole point: a lost resolution makes resolveCommentToRange
    // return null in the caller, which flags the comment as orphaned.
    // We don't care about the exact from/to when confidence is "lost".
  })

  // Third regression, motivated by the same wiki.gmt.bio case: the
  // anchor text may still exist somewhere in the doc even though the
  // author moved the whole paragraph. Fuzzy MUST find it — the
  // structural drift is arbitrary. This keeps the fast path honest.
  it("finds a moved paragraph — structural drift + text still present", () => {
    const original = schema.nodes.doc.create(null, [
      paragraphOf("Intro paragraph."),
      paragraphOf("The audit program identifies the audit criteria, the auditors, and the frequency."),
      paragraphOf("Closing remarks."),
    ])
    let pos = 0
    let seen = 0
    original.descendants((n, p) => {
      if (n.type.name === "paragraph") {
        seen++
        if (seen === 2) {
          pos = p + 1
          return false
        }
      }
    })
    const target = "identifies the audit criteria"
    const paraText = "The audit program identifies the audit criteria, the auditors, and the frequency."
    const from = pos + paraText.indexOf(target)
    const anchor = rangeFromPm(original, from, from + target.length, { withTextQuote: true })

    // Author reorders: the target paragraph now sits FIRST, closing
    // remarks disappear, new paragraphs added at the end.
    const reordered = schema.nodes.doc.create(null, [
      paragraphOf(paraText),
      paragraphOf("Follow-up section."),
      paragraphOf("Additional notes and clarifications go here."),
    ])
    const resolved = resolveRangeInPm(reordered, anchor)
    expect(resolved.confidence).not.toBe("lost")
    const slice = reordered.textBetween(resolved.from, resolved.to, "\n")
    expect(slice).toBe(target)
  })

  // Fourth regression: an author rewords the paragraph, keeping the
  // exact selected text intact but changing everything AROUND it.
  // Prefix and suffix don't match anymore, but the exact text still
  // uniquely occurs — fuzzy must still find it.
  it("finds the target when only the exact text survives an edit (prefix/suffix rewritten)", () => {
    const original = schema.nodes.doc.create(null, [
      paragraphOf("Before context. UNIQUE_MARKER_PHRASE. After context."),
    ])
    let paraPos = 0
    original.descendants((n, pos) => {
      if (n.type.name === "paragraph") {
        paraPos = pos + 1
        return false
      }
    })
    const target = "UNIQUE_MARKER_PHRASE"
    const plain = "Before context. UNIQUE_MARKER_PHRASE. After context."
    const from = paraPos + plain.indexOf(target)
    const anchor = rangeFromPm(original, from, from + target.length, { withTextQuote: true })
    expect(anchor.textQuote?.prefix).toContain("context.")

    const rewritten = schema.nodes.doc.create(null, [
      paragraphOf("Completely different wording around: UNIQUE_MARKER_PHRASE. And a fresh trailing sentence."),
    ])
    const resolved = resolveRangeInPm(rewritten, anchor)
    expect(resolved.confidence).not.toBe("lost")
    const slice = rewritten.textBetween(resolved.from, resolved.to, "\n")
    expect(slice).toBe(target)
  })

  // Fifth regression: the exact text exists TWICE now (once in the
  // original spot, once in an inserted paragraph). Prefix/suffix
  // scoring must pick the original occurrence rather than the newer
  // one — that's what "anchored to a specific sentence" means.
  it("picks the semantically-right occurrence when duplicates appear after an edit", () => {
    const original = schema.nodes.doc.create(null, [
      paragraphOf("Section one: the process owner reviews the checklist."),
    ])
    let paraPos = 0
    original.descendants((n, pos) => {
      if (n.type.name === "paragraph") {
        paraPos = pos + 1
        return false
      }
    })
    const target = "process owner"
    const plain = "Section one: the process owner reviews the checklist."
    const from = paraPos + plain.indexOf(target)
    const anchor = rangeFromPm(original, from, from + target.length, { withTextQuote: true })
    expect(anchor.textQuote?.prefix).toContain("Section one:")

    // Author adds a SECOND paragraph that ALSO mentions "process owner"
    // but in a different context — no "Section one" prefix.
    const withDuplicate = schema.nodes.doc.create(null, [
      paragraphOf("Section one: the process owner reviews the checklist."),
      paragraphOf("An external process owner may act as backup."),
    ])
    const resolved = resolveRangeInPm(withDuplicate, anchor)
    expect(resolved.confidence).not.toBe("lost")
    // The BEFORE window from the resolved position must still mention
    // Section one — i.e. we picked the original occurrence, not the
    // new backup-context one.
    const before = withDuplicate.textBetween(Math.max(0, resolved.from - 40), resolved.from, "\n")
    expect(before).toContain("Section one")
  })
})
