// Round-trip corpus for the four template directives:
//
//   {template}              — the copy marker
//   {template-title}        — pattern that becomes the created page's H1
//   {template-stamp}        — resolved at creation into the "Created from…" sentence
//   {template-reviewflow …} — resolved at creation into a {reviewflow …} directive
//
// Each parses to its own atom node (no editable content), so round-trip
// simply proves the serializer emits the canonical marker line back.
//
// The four directives are self-contained (registerSelfContainedDirective),
// so their serialised form is fixed and the parser doesn't need per-attr
// wrangling — except for template-reviewflow, which carries a version +
// role→user map.
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

function firstNode(doc: PMNode, name: string): PMNode | null {
  let found: PMNode | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === name) {
      found = n
      return false
    }
    return true
  })
  return found
}

describe("{template} marker", () => {
  it("parses to a template_marker atom and round-trips", () => {
    const rt = roundTrip("{template}\n\nBody\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_marker")).toBe(1)
  })

  it("{template} works when it's the only content", () => {
    const rt = roundTrip("{template}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_marker")).toBe(1)
  })

  it("{template} inside a code fence stays literal (no node produced)", () => {
    const rt = roundTrip("```\n{template}\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_marker")).toBe(0)
  })

  it("{template} in a table cell — backticks protect", () => {
    const rt = roundTrip("| h |\n| --- |\n| `{template}` |\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_marker")).toBe(0)
  })
})

describe("{template-title} directive", () => {
  it("parses to template_title atom and round-trips", () => {
    const rt = roundTrip("{template}\n\n{template-title}\n# Placeholder title\n\nBody\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_title")).toBe(1)
  })

  it("standalone {template-title} (no marker) still parses", () => {
    // The self-contained directive itself doesn't require a {template}
    // parent — the resolver's requirement is a runtime check, not a parse
    // constraint.
    const rt = roundTrip("{template-title}\n# T\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_title")).toBe(1)
  })
})

describe("{template-stamp} directive", () => {
  it("bare {template-stamp} round-trips", () => {
    const rt = roundTrip("{template}\n\n{template-stamp}\n\nBody\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_stamp")).toBe(1)
  })

  it("{template-stamp} without a {template} parent still parses (semantic error caught elsewhere)", () => {
    const rt = roundTrip("{template-stamp}\n\nBody\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_stamp")).toBe(1)
  })
})

describe("{template-reviewflow …} directive", () => {
  it("bare {template-reviewflow} round-trips", () => {
    const rt = roundTrip("{template}\n\n{template-reviewflow}\n\nBody\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_reviewflow")).toBe(1)
    const node = firstNode(rt.doc, "template_reviewflow")
    expect(node?.attrs.version).toBe("")
  })

  it("preserves version=", () => {
    const rt = roundTrip("{template}\n\n{template-reviewflow version=1.2}\n\nBody\n")
    expect(rt.isStable).toBe(true)
    const node = firstNode(rt.doc, "template_reviewflow")
    expect(node?.attrs.version).toBe("1.2")
  })

  it("preserves role→user pairs (author=, reviewer=)", () => {
    const rt = roundTrip("{template}\n\n{template-reviewflow author=alice reviewer=bob}\n\nBody\n")
    expect(rt.isStable).toBe(true)
    const node = firstNode(rt.doc, "template_reviewflow")
    const roles = JSON.parse(node?.attrs.roles as string)
    expect(roles.author).toBe("alice")
    expect(roles.reviewer).toBe("bob")
  })

  it("mixed version + roles + validation role", () => {
    const rt = roundTrip(
      "{template}\n\n{template-reviewflow version=2.0 author=alice reviewer=bob validation=cathy}\n\nBody\n"
    )
    expect(rt.isStable).toBe(true)
    const node = firstNode(rt.doc, "template_reviewflow")
    expect(node?.attrs.version).toBe("2.0")
    const roles = JSON.parse(node?.attrs.roles as string)
    expect(roles.author).toBe("alice")
    expect(roles.reviewer).toBe("bob")
    expect(roles.validation).toBe("cathy")
  })

  it("roles are serialised in a stable order (alphabetical)", () => {
    const rt = roundTrip("{template}\n\n{template-reviewflow validation=cathy author=alice reviewer=bob}\n\nBody\n")
    expect(rt.isStable).toBe(true)
    // The stable serialisation must sort role keys — otherwise round-trip
    // would drift on any input whose order differs from the printer's.
    const line = rt.first.split("\n").find((l) => l.startsWith("{template-reviewflow"))
    expect(line).toBeDefined()
    const authorIdx = line!.indexOf("author=")
    const reviewerIdx = line!.indexOf("reviewer=")
    const validationIdx = line!.indexOf("validation=")
    expect(authorIdx).toBeLessThan(reviewerIdx)
    expect(reviewerIdx).toBeLessThan(validationIdx)
  })
})

describe("full template payload — combined directives", () => {
  it("complete template with all four directives round-trips", () => {
    const src =
      "{template}\n\n" +
      "{template-title}\n# My Title\n\n" +
      "{template-stamp}\n\n" +
      "{template-reviewflow version=1.0 author=alice reviewer=bob}\n\n" +
      "Body content.\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_marker")).toBe(1)
    expect(countNodes(rt.doc, "template_title")).toBe(1)
    expect(countNodes(rt.doc, "template_stamp")).toBe(1)
    expect(countNodes(rt.doc, "template_reviewflow")).toBe(1)
  })

  it("variable placeholders {{VAR}} parse to template_var atoms and round-trip", () => {
    // Template variable substitution happens at document creation time on
    // the backend — the frontend represents each {{X}} as a template_var
    // node so the raw source and the visual mode stay in step. Round-trip
    // must preserve the two atoms with their names.
    const src = "{template}\n\n# Report for {{customer}}\n\nAmount: {{amount}}\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_var")).toBe(2)
    // Collect the variable names carried by the atoms and assert both survived.
    const names: string[] = []
    rt.doc.descendants((n) => {
      if (n.type.name === "template_var") names.push(String(n.attrs.name ?? ""))
    })
    expect(names).toContain("customer")
    expect(names).toContain("amount")
  })
})
