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

describe("{template-todo …} directive", () => {
  // {template-todo} declares a distribution/acknowledgement task the
  // template carries. It stays inert on the template itself and resolves
  // to a real {todo} on the created document (backend resolver). The
  // frontend concern here is bijective round-trip: the args authors set
  // (title, assign, action, due, priority, …) survive parse → serialize
  // untouched, so the visual editor and the raw source agree.
  it("bare {template-todo} round-trips", () => {
    // A bare {template-todo} still round-trips because the serialiser
    // always emits title="" (required by the todo directive contract
    // that {template-todo} mirrors).
    const rt = roundTrip('{template}\n\n{template-todo title=""}\n\nBody\n')
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_todo")).toBe(1)
  })

  it("preserves title=", () => {
    const rt = roundTrip('{template}\n\n{template-todo title="Read the SOP"}\n\nBody\n')
    expect(rt.isStable).toBe(true)
    const node = firstNode(rt.doc, "template_todo")
    expect(node?.attrs.title).toBe("Read the SOP")
  })

  it("preserves assign= and action=", () => {
    const rt = roundTrip('{template}\n\n{template-todo title="Please read" assign="@ops" action=read}\n\nBody\n')
    expect(rt.isStable).toBe(true)
    const node = firstNode(rt.doc, "template_todo")
    expect(node?.attrs.assign).toBe("@ops")
    expect(node?.attrs.action).toBe("read")
  })

  it("mixed args (due, priority, tags, description) all survive", () => {
    const src =
      "{template}\n\n" +
      '{template-todo title="Full review" assign="@quality-team" due=2026-12-31 priority=high tags="sop09,q4" description="Sign only after reading the SOP end-to-end"}\n\n' +
      "Body\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const node = firstNode(rt.doc, "template_todo")
    expect(node?.attrs.title).toBe("Full review")
    expect(node?.attrs.assign).toBe("@quality-team")
    expect(node?.attrs.due).toBe("2026-12-31")
    expect(node?.attrs.priority).toBe("high")
    expect(node?.attrs.tags).toBe("sop09,q4")
    expect(node?.attrs.description).toBe("Sign only after reading the SOP end-to-end")
  })

  it("several {template-todo} lines coexist (multi-step distribution)", () => {
    // A template can declare N distribution steps (read → acknowledge →
    // validate). Each becomes a separate {todo} on the created document,
    // in the same relative order the author placed them.
    const src =
      "{template}\n\n" +
      '{template-todo title="Read" action=read}\n\n' +
      '{template-todo title="Acknowledge" action=acknowledge}\n\n' +
      '{template-todo title="Validate" action=validate}\n\n' +
      "Body\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_todo")).toBe(3)
  })

  it("default resolution/priority values drop on serialise", () => {
    // Consistency with the todo node: only non-default keys are emitted.
    const rt = roundTrip('{template}\n\n{template-todo title="X" resolution=any priority=normal}\n\nBody\n')
    expect(rt.isStable).toBe(true)
    const line = rt.first.split("\n").find((l) => l.startsWith("{template-todo"))
    expect(line).toBeDefined()
    expect(line).not.toMatch(/resolution=/)
    expect(line).not.toMatch(/priority=/)
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

  it("{template target=…} preserves the pin through round-trip", () => {
    // A target= pattern on {template} carries {{slug}} / {{title}}
    // tokens that MUST NOT be resolved at parse time (they resolve
    // when a document is created from the template). Round-trip
    // preserves the pattern verbatim.
    const src = "{template target=/qms/campaigns/{{slug}}}\n\nBody\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "template_marker")).toBe(1)
    let marker: any = null
    rt.doc.descendants((n) => {
      if (!marker && n.type.name === "template_marker") {
        marker = n
        return false
      }
    })
    expect(marker.attrs.target).toBe("/qms/campaigns/{{slug}}")
    expect(rt.first).toMatch(/target=\/qms\/campaigns\/\{\{slug\}\}/)
  })

  it("bare {template} still serializes without target=", () => {
    const rt = roundTrip("{template}\n\nBody\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/target=/)
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
