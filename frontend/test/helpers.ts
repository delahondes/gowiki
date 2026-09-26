// Shared harness for the dialect round-trip regression corpus.
//
// The dialect invariant we test: `serialize(parse(x))` may normalise `x`
// once (whitespace, ordering of directive keys, etc.), but from that
// first serialisation onward the pipeline is idempotent:
//
//     let first  = serialize(parse(x))
//     let stable = serialize(parse(first))
//     assert first === stable
//
// Bugs surface in two forms: text that changes across the second pass
// (round-trip drift), or a mark that never opened in the first place
// (silent mark loss — the drift is zero but the semantics are wrong).
// The corpus asserts both.
import { schema as basicSchema } from "prosemirror-schema-basic"
import type { Node as PMNode, Mark } from "prosemirror-model"
import { markdownToPM } from "../compiler/markdown_to_pm"
import { pmToMarkdown } from "../compiler/pm_to_markdown"
import { buildRegistry } from "../compiler/build_registry"

// Build a single registry + schema for the whole suite. Building these is
// expensive and produces the same output on every call.
export const registry = buildRegistry(basicSchema)
export const schema = registry.buildSchema()
registry.bindSchema(schema)

export interface RoundTripResult {
  doc: PMNode
  first: string
  stable: string
  isStable: boolean
}

export function roundTrip(source: string): RoundTripResult {
  const doc = markdownToPM(source, registry)
  const first = pmToMarkdown(doc, registry)
  const doc2 = markdownToPM(first, registry)
  const stable = pmToMarkdown(doc2, registry)
  return { doc, first, stable, isStable: first === stable }
}

// Walk every text node in the doc; call visitor with (text, marks).
export function forEachTextNode(
  doc: PMNode,
  visit: (text: string, marks: readonly Mark[], node: PMNode) => void
): void {
  doc.descendants((n) => {
    if (n.isText) visit(n.text ?? "", n.marks, n)
  })
}

// Find the first text node whose text is exactly `target` and return its
// mark type names. Returns null when no such node exists.
export function marksOnText(doc: PMNode, target: string): string[] | null {
  let found: string[] | null = null
  forEachTextNode(doc, (text, marks) => {
    if (found === null && text === target) {
      found = marks.map((m) => m.type.name)
    }
  })
  return found
}

// Convenience assertion for the common shape: `sourceHas` a marked
// fragment of `expectedText` — assert that after parsing, some text node
// exactly `expectedText` carries `markName`.
export function assertMarkOnText(doc: PMNode, markName: string, expectedText: string): void {
  const marks = marksOnText(doc, expectedText)
  if (marks === null) {
    throw new Error(
      `Expected a text node "${expectedText}" carrying mark "${markName}", ` +
        `but no text node with that exact content was found.`
    )
  }
  if (!marks.includes(markName)) {
    throw new Error(`Text node "${expectedText}" is missing mark "${markName}" — ` + `has: [${marks.join(", ")}]`)
  }
}

// Count nodes of a given type name (e.g. "hard_break", "table",
// "code_block") anywhere in the doc.
export function countNodes(doc: PMNode, typeName: string): number {
  let n = 0
  doc.descendants((node) => {
    if (node.type.name === typeName) n++
  })
  return n
}

// Convenience per-mark helpers so per-case assertions read naturally.
export function assertUnderlineMark(doc: PMNode, text: string): void {
  assertMarkOnText(doc, "underline", text)
}
export function assertStrongMark(doc: PMNode, text: string): void {
  assertMarkOnText(doc, "strong", text)
}
export function assertEmMark(doc: PMNode, text: string): void {
  assertMarkOnText(doc, "em", text)
}
export function assertHighlightMark(doc: PMNode, text: string): void {
  assertMarkOnText(doc, "highlight", text)
}
export function assertCodeMark(doc: PMNode, text: string): void {
  assertMarkOnText(doc, "code", text)
}
export function assertStrikeMark(doc: PMNode, text: string): void {
  assertMarkOnText(doc, "strikethrough", text)
}
export function assertSubMark(doc: PMNode, text: string): void {
  assertMarkOnText(doc, "subscript", text)
}
export function assertSuperMark(doc: PMNode, text: string): void {
  assertMarkOnText(doc, "superscript", text)
}
