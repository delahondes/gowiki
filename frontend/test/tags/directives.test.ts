// {tag ...} and {tag-query ...} round-trip corpus.
//
// Both are self-contained directives: `tag` inline-style (produces one
// `tag` node whose `values` attr is the positional _args); `tag-query`
// block-style with typed attrs (tag, exclude, path, render, groupby).
//
// The serializer for {tag} always emits `{tag <values>}\n\n`; the
// parser accepts `{tag foo bar}` or `{tag values=foo,bar}`. We pin the
// current normalisation so any future serializer change surfaces here.
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

function firstTagQuery(doc: PMNode): Record<string, any> | null {
  let found: Record<string, any> | null = null
  doc.descendants(n => {
    if (found === null && n.type.name === "tag_query") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

function firstTagNode(doc: PMNode): Record<string, any> | null {
  let found: Record<string, any> | null = null
  doc.descendants(n => {
    if (found === null && n.type.name === "tag") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

// ─── {tag} ──────────────────────────────────────────────

describe("{tag}", () => {
  it("positional args become the values attr", () => {
    const rt = roundTrip("{tag foo bar baz}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "tag")).toBe(1)
    expect(firstTagNode(rt.doc)?.values).toBe("foo bar baz")
  })

  it("single value", () => {
    const rt = roundTrip("{tag alpha}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTagNode(rt.doc)?.values).toBe("alpha")
  })

  it("values=comma-list form (parser accepts values= explicitly)", () => {
    // The parser reads `values=` OR positional args. The serializer
    // always writes the positional form — so first-pass round-trip
    // normalises, and the second pass is idempotent.
    const rt = roundTrip("{tag values=alpha,beta,gamma}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTagNode(rt.doc)?.values).toBe("alpha,beta,gamma")
  })

  it("hyphens and dots in tag names", () => {
    const rt = roundTrip("{tag api-v2 v1.2.3}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTagNode(rt.doc)?.values).toBe("api-v2 v1.2.3")
  })

  it("unicode tag names (accented + CJK)", () => {
    const rt = roundTrip("{tag français 日本語}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTagNode(rt.doc)?.values).toBe("français 日本語")
  })

  it("empty {tag} — no values", () => {
    const rt = roundTrip("{tag}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTagNode(rt.doc)?.values ?? "").toBe("")
  })

  it("{tag} inside a code fence stays literal (no node)", () => {
    const rt = roundTrip("```\n{tag foo}\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "tag")).toBe(0)
  })
})

// ─── {tag-query} ────────────────────────────────────────

describe("{tag-query} — every attr the plugin surfaces", () => {
  it("bare tag=X", () => {
    const rt = roundTrip("{tag-query tag=docs}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "tag_query")).toBe(1)
    const q = firstTagQuery(rt.doc)!
    expect(q.tag).toBe("docs")
    expect(q.exclude).toBe("")
    expect(q.path).toBe("")
    // render defaults to "table" and is omitted on write when default,
    // so the parsed attr may be "" or "table" depending on the pass.
    // Assert on the SEMANTIC state, not the string of the attr.
    expect(q.render === "" || q.render === "table").toBe(true)
    expect(q.groupby).toBe("")
  })

  it("all attrs at once, non-default values", () => {
    const rt = roundTrip(
      "{tag-query tag=docs exclude=draft,archived path=/manual render=list groupby=folder}\n"
    )
    expect(rt.isStable).toBe(true)
    const q = firstTagQuery(rt.doc)!
    expect(q.tag).toBe("docs")
    expect(q.exclude).toBe("draft,archived")
    expect(q.path).toBe("/manual")
    expect(q.render).toBe("list")
    expect(q.groupby).toBe("folder")
  })

  it("exclude with a single tag", () => {
    const rt = roundTrip("{tag-query tag=t exclude=draft}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTagQuery(rt.doc)?.exclude).toBe("draft")
  })

  it("render=table is the default and is NOT re-emitted", () => {
    // Explicit render=table should collapse to omitted on the first
    // round-trip, but the second pass has to be stable.
    const rt = roundTrip("{tag-query tag=t render=table}\n")
    expect(rt.isStable).toBe(true)
    // The current serializer omits render when it equals "table":
    // assert on the semantic + text.
    expect(rt.first).not.toContain("render=table")
  })

  it("render=list survives round-trip", () => {
    const rt = roundTrip("{tag-query tag=t render=list}\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first).toContain("render=list")
    expect(firstTagQuery(rt.doc)?.render).toBe("list")
  })

  it("groupby=folder", () => {
    const rt = roundTrip("{tag-query tag=t groupby=folder}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTagQuery(rt.doc)?.groupby).toBe("folder")
  })

  it("groupby with spaces gets quoted (per serializer's /\\s/ check)", () => {
    // Author writes a groupby value containing whitespace (currently
    // unused semantically, but the serializer defensively quotes it).
    // We can't easily construct this via a plain source line, so we
    // simulate by round-tripping the un-quoted form and asserting the
    // parser handles a quoted variant.
    const rt = roundTrip(`{tag-query tag=t groupby="by folder"}\n`)
    expect(rt.isStable).toBe(true)
    expect(firstTagQuery(rt.doc)?.groupby).toBe("by folder")
    // Post-first-pass, the serializer re-quotes because the value
    // contains a space.
    expect(rt.first).toContain(`groupby="by folder"`)
  })

  it("path with a trailing slash preserved", () => {
    const rt = roundTrip("{tag-query tag=t path=/docs/}\n")
    expect(rt.isStable).toBe(true)
    expect(firstTagQuery(rt.doc)?.path).toBe("/docs/")
  })

  it("unknown attr is dropped silently (permissive)", () => {
    // The parser accepts unknown keys into attrs but the schema only
    // carries the known ones — so on serialize, the unknown is lost.
    // We pin the stability of the LOSS: the second pass equals the
    // first (both without the unknown).
    const rt = roundTrip("{tag-query tag=t future_flag=xyz}\n")
    expect(rt.isStable).toBe(true)
    expect(rt.stable).not.toContain("future_flag")
    expect(firstTagQuery(rt.doc)?.tag).toBe("t")
  })

  it("{tag-query} inside a code fence stays literal", () => {
    const rt = roundTrip("```\n{tag-query tag=t}\n```\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "tag_query")).toBe(0)
  })

  it("empty tag= is accepted (matches parser permissiveness)", () => {
    const rt = roundTrip("{tag-query tag=}\n")
    expect(rt.isStable).toBe(true)
    // Parser accepts empty; runtime handler would return an error at
    // render time. Not our concern here.
  })
})
