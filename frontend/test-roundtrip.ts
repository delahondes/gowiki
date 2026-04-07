#!/usr/bin/env npx tsx
/**
 * Round-trip test: markdown → PM → markdown → PM → markdown
 * Verifies that serialize→parse→serialize is stable.
 *
 * Usage:
 *   npx tsx test-roundtrip.ts                    # run all built-in cases
 *   npx tsx test-roundtrip.ts "==*hello*=="      # test a specific string
 *   npx tsx test-roundtrip.ts --file /tmp/test.md  # test from file
 */
import { markdownToPM } from "./compiler/markdown_to_pm.ts"
import { pmToMarkdown } from "./compiler/pm_to_markdown.ts"
import { buildRegistry } from "./compiler/build_registry.ts"
import { schema as basicSchema } from "prosemirror-schema-basic"

const registry = buildRegistry(basicSchema)
const schema = registry.buildSchema()
registry.bindSchema(schema)

function roundTrip(md: string): { pass1: string; pass2: string; stable: boolean } {
  const doc1 = markdownToPM(md, registry)
  const pass1 = pmToMarkdown(doc1, registry)
  const doc2 = markdownToPM(pass1, registry)
  const pass2 = pmToMarkdown(doc2, registry)
  return { pass1, pass2, stable: pass1 === pass2 }
}

function test(label: string, md: string) {
  const { pass1, pass2, stable } = roundTrip(md)
  const changed = pass1 !== md
  const icon = stable ? "\x1b[32m✓\x1b[0m" : "\x1b[31m✗\x1b[0m"
  console.log(`${icon} ${label}`)
  if (changed) {
    console.log(`  source → pass1 changed:`)
    diffLines(md, pass1)
  }
  if (!stable) {
    console.log(`  pass1 → pass2 differs:`)
    diffLines(pass1, pass2)
  }
}

function diffLines(a: string, b: string) {
  const la = a.split("\n"), lb = b.split("\n")
  const max = Math.max(la.length, lb.length)
  for (let i = 0; i < max; i++) {
    if (la[i] !== lb[i]) {
      console.log(`    line ${i + 1}:`)
      if (la[i] !== undefined) console.log(`      - ${JSON.stringify(la[i])}`)
      if (lb[i] !== undefined) console.log(`      + ${JSON.stringify(lb[i])}`)
    }
  }
}

// --- CLI mode ---
const args = process.argv.slice(2)
if (args[0] === "--file") {
  const fs = await import("fs")
  const content = fs.readFileSync(args[1], "utf-8")
  test(args[1], content)
  process.exit(0)
} else if (args.length > 0 && args[0] !== "--builtin") {
  test("cli input", args.join(" "))
  process.exit(0)
}

// --- Built-in test cases ---
console.log("\n=== Highlight round-trip tests ===\n")

test("basic highlight", "==hello==")
test("highlight with bold", "==**bold**==")
test("highlight with italic", "==*italic*==")
test("highlight with mixed", "==[*italic in brackets*]==")
test("highlight with escaped braces", "==\\{\\}*houhou*==")
test("highlight color green", "=={#ccffcc}green text==")
test("highlight color with bold", "=={#ccffcc}**bold green**==")
test("highlight color with mixed marks", "=={#ccffcc}before *italic* after==")

console.log("\n=== Table cell highlight tests ===\n")

test("highlight in table cell",
  "{table headers=1c}\n| Col1 | Col2 |\n| --- | --- |\n| ==[*placeholder*]== | text |")

test("highlight with escaped star in table",
  "{table headers=1c}\n| Col1 | Col2 |\n| --- | --- |\n| ==\\*literal\\*== | text |")

console.log("\n=== Basic mark tests ===\n")

test("bold", "**bold**")
test("italic", "*italic*")
test("bold + italic", "***both***")
test("link", "[text](https://example.com)")
test("auto link", "[](https://example.com)")
test("inline code", "`code`")
test("strikethrough", "~~strike~~")
test("underline", "_underline_")

console.log("\n=== Paragraph tests ===\n")

test("two paragraphs", "First paragraph\n\nSecond paragraph")
test("heading + paragraph", "# Heading\n\nParagraph")
test("ordered list with highlight",
  "1. un\n2. =={#ccffcc}\\{\\}**deux**==\n3. trois")

console.log("")
