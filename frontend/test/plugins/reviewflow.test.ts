// {reviewflow ...} + {reviewflow-link ...} + {reviewflow-query ...}
//
// The reviewflow directive carries a version + arbitrary role
// assignments (author, reviewer, validator, and any other role name
// the wiki configures). Roles are stored as a JSON string on the
// node's `roles` attr; on serialize they emit in alphabetical order
// (a determinism-preserving choice worth pinning).
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"
import { rowMatchesUser, rowMatchesSignedBy } from "../../plugins/reviewflow"

function firstReviewflow(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "reviewflow") {
      found = { ...n.attrs, _rolesParsed: JSON.parse(String(n.attrs.roles ?? "{}")) }
      return false
    }
    return true
  })
  return found
}

function firstReviewflowLink(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "reviewflow_link") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

function firstReviewflowQuery(doc: PMNode): Record<string, unknown> | null {
  let found: Record<string, unknown> | null = null
  doc.descendants((n) => {
    if (found === null && n.type.name === "reviewflow_query") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

describe("reviewflow: minimum shape", () => {
  it("bare {reviewflow} produces one node with empty attrs", () => {
    const rt = roundTrip("{reviewflow}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow")).toBe(1)
    const a = firstReviewflow(rt.doc)!
    expect(a.version).toBe("")
    expect(a._rolesParsed).toEqual({})
  })

  it("{reviewflow version=1.0}", () => {
    const rt = roundTrip("{reviewflow version=1.0}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflow(rt.doc)?.version).toBe("1.0")
  })
})

describe("reviewflow: role assignments", () => {
  it("author only", () => {
    const rt = roundTrip("{reviewflow author=alice}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflow(rt.doc)?._rolesParsed).toEqual({ author: "alice" })
  })

  it("author + reviewer + validator", () => {
    const rt = roundTrip("{reviewflow author=alice reviewer=bob validator=carol}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflow(rt.doc)?._rolesParsed).toEqual({
      author: "alice",
      reviewer: "bob",
      validator: "carol",
    })
  })

  it("arbitrary custom role names are accepted (collectExtra)", () => {
    const rt = roundTrip("{reviewflow author=alice qa=carol legal=dave}\n")
    expect(rt.isStable).toBe(true)
    const roles = firstReviewflow(rt.doc)?._rolesParsed as Record<string, string>
    expect(roles.qa).toBe("carol")
    expect(roles.legal).toBe("dave")
  })

  it("roles emit in alphabetical order on serialize (determinism)", () => {
    const rt = roundTrip("{reviewflow zebra=z alpha=a mango=m}\n")
    expect(rt.isStable).toBe(true)
    // The serialised order must be alphabetical regardless of input order.
    const rendered = rt.first
    const alphaIdx = rendered.indexOf("alpha=")
    const mangoIdx = rendered.indexOf("mango=")
    const zebraIdx = rendered.indexOf("zebra=")
    expect(alphaIdx).toBeGreaterThan(-1)
    expect(mangoIdx).toBeGreaterThan(alphaIdx)
    expect(zebraIdx).toBeGreaterThan(mangoIdx)
  })

  it("version always emits first when present", () => {
    const rt = roundTrip("{reviewflow author=alice version=2.0}\n")
    expect(rt.isStable).toBe(true)
    const rendered = rt.first
    expect(rendered.indexOf("version=")).toBeLessThan(rendered.indexOf("author="))
  })
})

describe("reviewflow-link (inline)", () => {
  it("{reviewflow-link version=1.0 page=/policy} in a paragraph", () => {
    const rt = roundTrip("See {reviewflow-link version=1.0 page=/policy} for details.\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_link")).toBe(1)
    const a = firstReviewflowLink(rt.doc)!
    expect(a.version).toBe("1.0")
    expect(a.page).toBe("/policy")
  })

  it("bare {reviewflow-link} still parses to a node", () => {
    const rt = roundTrip("Line {reviewflow-link}.\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_link")).toBe(1)
  })
})

describe("reviewflow-query", () => {
  it("bare {reviewflow-query} defaults to status=draft", () => {
    const rt = roundTrip("{reviewflow-query}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_query")).toBe(1)
    const a = firstReviewflowQuery(rt.doc)!
    expect(a.status).toBe("draft")
  })

  it("{reviewflow-query path=/foo} carries the path", () => {
    const rt = roundTrip("{reviewflow-query path=/foo}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflowQuery(rt.doc)?.path).toBe("/foo")
  })

  it("{reviewflow-query status=validated} (non-default)", () => {
    const rt = roundTrip("{reviewflow-query status=validated}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflowQuery(rt.doc)?.status).toBe("validated")
  })

  it("status=draft (default) is dropped on serialize", () => {
    const rt = roundTrip("{reviewflow-query status=draft}\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/status=draft/)
  })

  // user + when filters, added rc.3. Bijectivity pinned: non-default
  // values round-trip on both attrs, default values drop, and the
  // parsed doc reflects the requested filter.
  it("{reviewflow-query user=@me when=next} round-trips both attrs", () => {
    const rt = roundTrip("{reviewflow-query user=@me when=next}\n")
    expect(rt.isStable).toBe(true)
    const a = firstReviewflowQuery(rt.doc)!
    expect(a.user).toBe("@me")
    expect(a.when).toBe("next")
  })

  it("{reviewflow-query user=etienne.formstecher} defaults when to 'any' and drops it", () => {
    const rt = roundTrip("{reviewflow-query user=etienne.formstecher}\n")
    expect(rt.isStable).toBe(true)
    const a = firstReviewflowQuery(rt.doc)!
    expect(a.user).toBe("etienne.formstecher")
    expect(a.when).toBe("any")
    expect(rt.first).not.toMatch(/when=/)
  })

  it("{reviewflow-query when=overdue} without user is legal source but ineffective (user filter empty)", () => {
    // The filter runs only when user is non-empty, but the attrs must
    // still parse and round-trip so an author can add user later
    // without losing their `when` choice.
    const rt = roundTrip("{reviewflow-query when=overdue}\n")
    expect(rt.isStable).toBe(true)
    const a = firstReviewflowQuery(rt.doc)!
    expect(a.when).toBe("overdue")
    expect(a.user).toBe("")
  })

  it("unknown when= value falls back to 'any'", () => {
    // Guards against a typo silently becoming a permanent bogus filter.
    const rt = roundTrip("{reviewflow-query user=alice when=whenever}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflowQuery(rt.doc)!.when).toBe("any")
  })

  // signed_by is INDEPENDENT of user/when — a dashboard like
  // "docs waiting on me where Etienne already signed" is one query:
  //   {reviewflow-query user=@me when=next signed_by=etienne.formstecher}
  it("{reviewflow-query signed_by=etienne.formstecher} round-trips", () => {
    const rt = roundTrip("{reviewflow-query signed_by=etienne.formstecher}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflowQuery(rt.doc)!.signed_by).toBe("etienne.formstecher")
  })

  it("{reviewflow-query signed_by=@me} round-trips (resolved at render time)", () => {
    // @me is kept VERBATIM in source; the client resolves it from the
    // viewer at render time. A fixed-login dashboard uses the full
    // username; a personal "my sign-offs" dashboard uses @me.
    const rt = roundTrip("{reviewflow-query signed_by=@me}\n")
    expect(rt.isStable).toBe(true)
    expect(firstReviewflowQuery(rt.doc)!.signed_by).toBe("@me")
  })

  it("empty signed_by drops on serialise", () => {
    const rt = roundTrip("{reviewflow-query}\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/signed_by=/)
  })

  it("signed_by combines with user+when (the 'waiting on me where X signed' case)", () => {
    const rt = roundTrip("{reviewflow-query user=@me when=next signed_by=etienne.formstecher}\n")
    expect(rt.isStable).toBe(true)
    const a = firstReviewflowQuery(rt.doc)!
    expect(a.user).toBe("@me")
    expect(a.when).toBe("next")
    expect(a.signed_by).toBe("etienne.formstecher")
  })
})

// The rowMatchesUser predicate is the runtime filter behind
// `{reviewflow-query user=X when=Y}`. It runs client-side over the
// fetched Status objects — pinning each `when` value individually so a
// future refactor of the filter table can't silently break a `next` or
// `overdue` dashboard without a test complaining.
describe("rowMatchesUser predicate", () => {
  // Helper to keep case bodies short. Only the fields the predicate
  // actually reads (roles, missing_roles, next_roles, overdue_roles)
  // are meaningful; the rest satisfy the type.
  function mkStatus(overrides: Partial<any> = {}): any {
    return {
      roles: {},
      missing_roles: {},
      version_tag: "1.0",
      current_page_version: 1,
      validated_page_version: 0,
      is_fully_validated: false,
      ...overrides,
    }
  }

  it("user with no role on the page never matches, regardless of when", () => {
    const st = mkStatus({ roles: { author: "someone.else" } })
    expect(rowMatchesUser(st, "alice", "any")).toBe(false)
    expect(rowMatchesUser(st, "alice", "next")).toBe(false)
    expect(rowMatchesUser(st, "alice", "missing")).toBe(false)
    expect(rowMatchesUser(st, "alice", "overdue")).toBe(false)
  })

  it("when=any matches on role assignment alone (even if user already confirmed)", () => {
    // Widest possible filter: role holder shows up on the "who has a
    // stake in this page" dashboard even after they've signed.
    const st = mkStatus({
      roles: { reviewer: "alice" },
      missing_roles: {}, // alice already confirmed
    })
    expect(rowMatchesUser(st, "alice", "any")).toBe(true)
  })

  it("when=missing narrows to roles that still need confirmation", () => {
    const stillMissing = mkStatus({
      roles: { reviewer: "alice" },
      missing_roles: { reviewer: "alice" },
    })
    expect(rowMatchesUser(stillMissing, "alice", "missing")).toBe(true)

    const alreadySigned = mkStatus({
      roles: { reviewer: "alice" },
      missing_roles: {},
    })
    expect(rowMatchesUser(alreadySigned, "alice", "missing")).toBe(false)
  })

  it("when=next matches only when the user's role is queue-actionable", () => {
    // Sequential: author must confirm before reviewer's slot opens.
    // NextRoles carries only "author" (queue head), so alice-as-reviewer
    // is NOT actionable yet even though her role is missing.
    const notYet = mkStatus({
      roles: { author: "bob", reviewer: "alice" },
      missing_roles: { author: "bob", reviewer: "alice" },
      next_roles: ["author"],
      parallel: false,
    })
    expect(rowMatchesUser(notYet, "alice", "next")).toBe(false)
    // But `when=missing` still matches — she has a missing role, just
    // not one she can act on yet.
    expect(rowMatchesUser(notYet, "alice", "missing")).toBe(true)

    // After author confirms, the queue advances; reviewer becomes next.
    const actionable = mkStatus({
      roles: { author: "bob", reviewer: "alice" },
      missing_roles: { reviewer: "alice" },
      next_roles: ["reviewer"],
      parallel: false,
    })
    expect(rowMatchesUser(actionable, "alice", "next")).toBe(true)
  })

  it("when=next in parallel mode treats every missing role as actionable", () => {
    // Parallel: all missing roles are in NextRoles — no queue.
    const st = mkStatus({
      roles: { author: "bob", reviewer: "alice", validator: "carol" },
      missing_roles: { reviewer: "alice", validator: "carol" },
      next_roles: ["reviewer", "validator"],
      parallel: true,
    })
    expect(rowMatchesUser(st, "alice", "next")).toBe(true)
    expect(rowMatchesUser(st, "carol", "next")).toBe(true)
    // bob already confirmed → no missing role for him → no match.
    expect(rowMatchesUser(st, "bob", "next")).toBe(false)
  })

  it("when=overdue requires BOTH missing AND overdue for the user's role", () => {
    // Overdue for reviewer, alice IS reviewer, and role is missing → match.
    const overdue = mkStatus({
      roles: { reviewer: "alice" },
      missing_roles: { reviewer: "alice" },
      overdue_roles: ["reviewer"],
    })
    expect(rowMatchesUser(overdue, "alice", "overdue")).toBe(true)

    // Overdue but for a different role — alice's own role isn't overdue.
    const overdueElsewhere = mkStatus({
      roles: { author: "bob", reviewer: "alice" },
      missing_roles: { author: "bob", reviewer: "alice" },
      overdue_roles: ["author"],
    })
    expect(rowMatchesUser(overdueElsewhere, "alice", "overdue")).toBe(false)

    // Missing but not overdue yet — deadline not reached.
    const missingButNotOverdue = mkStatus({
      roles: { reviewer: "alice" },
      missing_roles: { reviewer: "alice" },
      overdue_roles: [],
    })
    expect(rowMatchesUser(missingButNotOverdue, "alice", "overdue")).toBe(false)
  })

  it("user holding two roles on the same page — any matching role wins", () => {
    // Rare but real (a small team where one person is both author and
    // reviewer). Match on the union: any role the user holds counts.
    const st = mkStatus({
      roles: { author: "alice", reviewer: "alice" },
      missing_roles: { reviewer: "alice" }, // author already confirmed
      next_roles: ["reviewer"],
    })
    expect(rowMatchesUser(st, "alice", "any")).toBe(true)
    expect(rowMatchesUser(st, "alice", "missing")).toBe(true)
    expect(rowMatchesUser(st, "alice", "next")).toBe(true)
  })
})

// signed_by is the "has acted" mirror of when=missing's "has to act".
// Both are "user holds a role on the page" filtered further; the
// predicate below says the user's seat is NOT in missing_roles for
// the current version.
describe("rowMatchesSignedBy predicate", () => {
  function mkStatus(overrides: Partial<any> = {}): any {
    return {
      roles: {},
      missing_roles: {},
      version_tag: "1.0",
      current_page_version: 1,
      validated_page_version: 0,
      is_fully_validated: false,
      ...overrides,
    }
  }

  it("user with no role on the page never matches", () => {
    const st = mkStatus({
      roles: { reviewer: "bob" },
      missing_roles: {},
    })
    expect(rowMatchesSignedBy(st, "alice")).toBe(false)
  })

  it("matches when the user's role has been confirmed on the current version", () => {
    const st = mkStatus({
      roles: { reviewer: "alice" },
      missing_roles: {}, // confirmed
    })
    expect(rowMatchesSignedBy(st, "alice")).toBe(true)
  })

  it("does not match when the user's role is still missing", () => {
    // "signed_by" is the mirror of "when=missing": one is "has to act",
    // the other is "has acted". Both can't be true for the same row.
    const st = mkStatus({
      roles: { reviewer: "alice" },
      missing_roles: { reviewer: "alice" },
    })
    expect(rowMatchesSignedBy(st, "alice")).toBe(false)
  })

  it("fully-validated page matches every holder", () => {
    const st = mkStatus({
      roles: { author: "alice", reviewer: "bob", validator: "carol" },
      missing_roles: {},
      is_fully_validated: true,
    })
    expect(rowMatchesSignedBy(st, "alice")).toBe(true)
    expect(rowMatchesSignedBy(st, "bob")).toBe(true)
    expect(rowMatchesSignedBy(st, "carol")).toBe(true)
  })

  it("user holding two seats — matches if ANY seat has been confirmed", () => {
    // The rare case of one person holding multiple roles on the same
    // page. If they've confirmed one but not the other, the "has
    // acted" answer is still yes.
    const st = mkStatus({
      roles: { author: "alice", reviewer: "alice" },
      missing_roles: { reviewer: "alice" }, // author already confirmed, reviewer not yet
    })
    expect(rowMatchesSignedBy(st, "alice")).toBe(true)
  })

  it("partial validation on a 3-role page: non-signer still missing, signer counts", () => {
    const st = mkStatus({
      roles: { author: "alice", reviewer: "bob", validator: "carol" },
      missing_roles: { reviewer: "bob", validator: "carol" }, // only alice signed
    })
    expect(rowMatchesSignedBy(st, "alice")).toBe(true)
    expect(rowMatchesSignedBy(st, "bob")).toBe(false)
    expect(rowMatchesSignedBy(st, "carol")).toBe(false)
  })
})

describe("reviewflow: interaction with code fences", () => {
  it("{reviewflow} inside a fenced block stays literal", () => {
    const src = "```\n{reviewflow author=alice}\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow")).toBe(0)
  })

  it("{reviewflow-link} inside a backtick-protected cell stays literal", () => {
    const src = "| head |\n| --- |\n| `{reviewflow-link version=1 page=/foo}` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "reviewflow_link")).toBe(0)
  })
})
