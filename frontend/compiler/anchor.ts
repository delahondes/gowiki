// Anchor — shared position-anchoring for the editor.
//
// Vocabulary used by mode-switch, draft restore, comments, and co-edition presence.
//
// An AnchorPoint is a structural address into the document: (nodeIndex, plainOffset).
//   - nodeIndex counts content nodes (paragraph, heading, code_block, list_item, table_cell)
//     in document order. Containers (list, table, blockquote) are walked through but
//     don't count themselves.
//   - plainOffset is the character offset within that node, counting only characters that
//     appear as visible text (markdown delimiters, link URLs, atoms like footnotes are
//     skipped). For code_block, plainOffset is the raw offset (no syntax to strip).
//
// An AnchorRange is a pair of AnchorPoints (start, end).
//
// A TextQuote is an optional fuzzy-match fingerprint (prefix, suffix, exact text).
// It's used as a fallback when the structural address no longer resolves — primarily
// for persistent anchors like comments, where the document can change arbitrarily
// between the anchor's creation and its resolution.
//
// Resolution returns a Confidence: "exact" (structural address matched and quote agreed),
// "fuzzy" (text-quote fallback succeeded), or "lost" (no usable position).

import type { Node as PMNode } from "prosemirror-model"
import type { EditorView } from "prosemirror-view"

// ── Types ───────────────────────────────────────────────────────────────────

export type ContentNodeType = "paragraph" | "heading" | "code_block" | "list_item" | "table_cell"

export interface ContentNode {
  rawStart: number
  rawEnd: number
  type: ContentNodeType
}

export interface TextQuote {
  prefix: string   // ≤ QUOTE_CTX chars of plain text immediately before the anchor
  suffix: string   // ≤ QUOTE_CTX chars of plain text immediately after the anchor
  exact?: string   // for ranges: the selected text (truncated to QUOTE_EXACT_MAX)
}

export interface AnchorPoint {
  nodeIndex: number
  plainOffset: number
  textQuote?: TextQuote
}

export interface AnchorRange {
  start: AnchorPoint
  end: AnchorPoint
  textQuote?: TextQuote
}

export type Confidence = "exact" | "fuzzy" | "lost"

export interface ResolvedPoint {
  pos: number
  confidence: Confidence
}

export interface ResolvedRange {
  from: number
  to: number
  confidence: Confidence
}

const QUOTE_CTX = 32           // chars of prefix/suffix in a textQuote
const QUOTE_EXACT_MAX = 200    // max length of stored exact text

// PM content types that directly contain inline text and represent one structural slot.
const PM_CONTENT_TYPES: ReadonlySet<string> = new Set([
  "paragraph", "heading", "code_block",
])

// Containers that we descend into when walking content nodes.
const PM_CONTAINER_TYPES: ReadonlySet<string> = new Set([
  "doc", "table", "table_row", "table_cell", "table_header",
  "bullet_list", "ordered_list", "list_item", "blockquote", "spoiler",
])

// ── Markdown scanning ───────────────────────────────────────────────────────

// Enumerate content nodes from raw markdown. Returns one entry per node in document order.
// Ported verbatim from the prior implementation in main.js — must remain behavior-identical
// for the mode-switch refactor to be a no-op.
export function scanMarkdownContentNodes(markdown: string): ContentNode[] {
  const nodes: ContentNode[] = []
  const lines = markdown.split("\n")
  let i = 0
  let inCodeFence = false
  let fenceMarker = ""

  // Pre-compute line-start byte offsets in one pass; the original used reduce() per
  // iteration which is O(n²) on large docs. Same semantics, O(n).
  const lineStarts: number[] = new Array(lines.length)
  {
    let acc = 0
    for (let k = 0; k < lines.length; k++) {
      lineStarts[k] = acc
      acc += lines[k].length + 1
    }
  }

  while (i < lines.length) {
    const line = lines[i]
    const lineStart = lineStarts[i]

    // Code fence toggle
    const fenceMatch = line.match(/^(`{3,}|~{3,})/)
    if (fenceMatch) {
      if (!inCodeFence) {
        inCodeFence = true
        fenceMarker = fenceMatch[1][0]
        const startLine = i + 1
        i++
        while (i < lines.length) {
          const cl = lines[i]
          if (cl.startsWith(fenceMarker.repeat(fenceMatch[1].length))) break
          i++
        }
        if (startLine < i) {
          const codeStart = lineStarts[startLine]
          const codeEnd = lineStarts[i] - 1
          nodes.push({ rawStart: codeStart, rawEnd: codeEnd, type: "code_block" })
        }
        inCodeFence = false
        i++
        continue
      }
    }

    // Blank line — skip
    if (line.trim() === "") { i++; continue }

    // Directive on its own line: {name ...}
    if (/^\{[\p{L}][\p{L}0-9_-]*(\s[^}]*)?\}\s*$/u.test(line)) { i++; continue }

    // Table separator row: | --- | --- |
    if (/^\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/.test(line)) { i++; continue }

    // Horizontal rule
    if (/^([-*_])\1{2,}\s*$/.test(line)) { i++; continue }

    // Table row: | cell | cell |
    if (line.includes("|") && /^\|/.test(line.trim())) {
      const stripped = line.replace(/^\|/, "").replace(/\|\s*$/, "")
      const cellTexts: string[] = []
      let cur = "", depth = 0, ci = 0
      for (ci = 0; ci < stripped.length; ci++) {
        const ch = stripped[ci]
        if (ch === "\\" && ci + 1 < stripped.length) { cur += ch + stripped[ci + 1]; ci++; continue }
        if (ch === "`") depth = depth ? 0 : 1
        if (ch === "|" && !depth) {
          cellTexts.push(cur)
          cur = ""
          continue
        }
        cur += ch
      }
      cellTexts.push(cur)

      let scanPos = lineStart
      for (const cellRaw of cellTexts) {
        const trimmed = cellRaw.trim()
        if (trimmed === "") {
          scanPos = markdown.indexOf("|", scanPos) + 1
          nodes.push({ rawStart: scanPos, rawEnd: scanPos, type: "table_cell" })
          continue
        }
        const cellStart = markdown.indexOf(trimmed, scanPos)
        if (cellStart >= 0) {
          nodes.push({ rawStart: cellStart, rawEnd: cellStart + trimmed.length, type: "table_cell" })
          scanPos = cellStart + trimmed.length
        }
      }
      i++
      continue
    }

    // Heading: ## text
    const headingMatch = line.match(/^(#{1,6})\s+(.*)$/)
    if (headingMatch) {
      const prefix = headingMatch[1].length + 1
      const contentStart = lineStart + prefix
      nodes.push({ rawStart: contentStart, rawEnd: lineStart + line.length, type: "heading" })
      i++
      continue
    }

    // List item
    const listMatch = line.match(/^(\s*(?:[-*]|\d+\.)\s+)(.*)$/)
    if (listMatch) {
      const prefix = listMatch[1].length
      const contentStart = lineStart + prefix
      let endLine = i
      while (endLine + 1 < lines.length) {
        const next = lines[endLine + 1]
        if (next.trim() === "") break
        if (/^\s*(?:[-*]|\d+\.)\s/.test(next)) break
        if (/^#{1,6}\s/.test(next)) break
        if (/^\|/.test(next.trim())) break
        endLine++
      }
      const endPos = lineStarts[endLine] + lines[endLine].length
      nodes.push({ rawStart: contentStart, rawEnd: endPos, type: "list_item" })
      i = endLine + 1
      continue
    }

    // Blockquote
    if (line.startsWith("> ") || line === ">") {
      const prefix = line.startsWith("> ") ? 2 : 1
      const contentStart = lineStart + prefix
      nodes.push({ rawStart: contentStart, rawEnd: lineStart + line.length, type: "paragraph" })
      i++
      continue
    }

    // Paragraph
    {
      const paraStart = lineStart
      let endLine = i
      while (endLine + 1 < lines.length) {
        const next = lines[endLine + 1]
        if (next.trim() === "") break
        if (/^#{1,6}\s/.test(next)) break
        if (/^\s*(?:[-*]|\d+\.)\s/.test(next)) break
        if (/^\|/.test(next.trim())) break
        if (/^(`{3,}|~{3,})/.test(next)) break
        if (/^\{[\p{L}][\p{L}0-9_-]*(\s[^}]*)?\}\s*$/u.test(next)) break
        if (/^([-*_])\1{2,}\s*$/.test(next)) break
        if (next.startsWith("> ")) break
        endLine++
      }
      const endPos = lineStarts[endLine] + lines[endLine].length
      nodes.push({ rawStart: paraStart, rawEnd: endPos, type: "paragraph" })
      i = endLine + 1
    }
  }
  return nodes
}

// ── Raw ↔ plain conversions ─────────────────────────────────────────────────

// Plain-text offset corresponding to a raw offset within one content node's raw text.
// Strips inline markdown syntax and counts only characters that appear in PM as visible text.
// Ported from main.js.
export function rawToPlainOffset(rawText: string, rawOffset: number): number {
  let plain = 0
  let i = 0
  const len = rawText.length
  const target = Math.min(rawOffset, len)

  while (i < target) {
    if (rawText[i] === "\\" && i + 1 < len) { i += 2; plain++; continue }
    if (rawText[i] === "^" && i + 1 < len && rawText[i + 1] === "[") {
      let depth = 1, j = i + 2
      while (j < len && depth > 0) {
        if (rawText[j] === "\\") { j += 2; continue }
        if (rawText[j] === "[") depth++
        if (rawText[j] === "]") depth--
        j++
      }
      if (depth === 0) {
        if (target <= j) return plain
        i = j; continue
      }
    }
    if (rawText[i] === "{" && i + 1 < len && rawText[i + 1] === "#") {
      const j = rawText.indexOf("}", i + 2)
      if (j >= 0) {
        if (target <= j + 1) return plain
        i = j + 1; continue
      }
    }
    if (rawText[i] === "{" && rawText.startsWith("ref ", i + 1)) {
      const j = rawText.indexOf("}", i + 2)
      if (j >= 0) {
        if (target <= j + 1) return plain
        i = j + 1; continue
      }
    }
    if (rawText[i] === "{" && i + 1 < len && rawText[i + 1] === "{") {
      const j = rawText.indexOf("}}", i + 2)
      if (j >= 0) {
        if (target <= j + 2) return plain
        i = j + 2; continue
      }
    }
    if (rawText[i] === "=" && i + 1 < len && rawText[i + 1] === "=") {
      i += 2
      if (i < len && rawText[i] === "{") {
        const j = rawText.indexOf("}", i)
        if (j >= 0) i = j + 1
      }
      continue
    }
    if (rawText[i] === "[") {
      let depth = 1, j = i + 1
      while (j < len && depth > 0) {
        if (rawText[j] === "\\") { j += 2; continue }
        if (rawText[j] === "[") depth++
        if (rawText[j] === "]") depth--
        j++
      }
      if (depth === 0 && j < len && rawText[j] === "(") {
        let pd = 1, k = j + 1
        while (k < len && pd > 0) {
          if (rawText[k] === "\\") { k += 2; continue }
          if (rawText[k] === "(") pd++
          if (rawText[k] === ")") pd--
          k++
        }
        if (pd === 0) {
          const textStart = i + 1, textEnd = j - 1
          if (target <= textStart) { i = textStart; continue }
          if (target <= textEnd) { i++; continue }
          if (target < k) return plain + rawToPlainOffset(rawText.slice(textStart, textEnd), textEnd - textStart)
          plain += rawToPlainOffset(rawText.slice(textStart, textEnd), textEnd - textStart)
          i = k
          continue
        }
      }
    }
    if (rawText[i] === "*" && i + 1 < len && rawText[i + 1] === "*") { i += 2; continue }
    if (rawText[i] === "*") { i++; continue }
    if (rawText[i] === "~" && i + 1 < len && rawText[i + 1] === "~") { i += 2; continue }
    if (rawText[i] === "~") { i++; continue }
    if (rawText[i] === "^") { i++; continue }
    if (rawText[i] === "_") { i++; continue }
    if (rawText[i] === "`") { i++; continue }
    if (rawText[i] === "@" && i + 1 < len && rawText[i + 1] === "`") { i++; continue }
    i++; plain++
  }
  return plain
}

// Inverse of rawToPlainOffset: given a plain offset, find the raw character position.
// Ported from main.js.
export function plainToRawOffset(rawText: string, plainTarget: number): number {
  let plain = 0
  let i = 0
  const len = rawText.length

  while (i < len && plain < plainTarget) {
    if (rawText[i] === "\\" && i + 1 < len) { i += 2; plain++; continue }
    if (rawText[i] === "^" && i + 1 < len && rawText[i + 1] === "[") {
      let depth = 1, j = i + 2
      while (j < len && depth > 0) {
        if (rawText[j] === "\\") { j += 2; continue }
        if (rawText[j] === "[") depth++
        if (rawText[j] === "]") depth--
        j++
      }
      if (depth === 0) { i = j; continue }
    }
    if (rawText[i] === "{" && i + 1 < len && rawText[i + 1] === "#") {
      const j = rawText.indexOf("}", i + 2)
      if (j >= 0) { i = j + 1; continue }
    }
    if (rawText[i] === "{" && rawText.startsWith("ref ", i + 1)) {
      const j = rawText.indexOf("}", i + 2)
      if (j >= 0) { i = j + 1; continue }
    }
    if (rawText[i] === "{" && i + 1 < len && rawText[i + 1] === "{") {
      const j = rawText.indexOf("}}", i + 2)
      if (j >= 0) { i = j + 2; continue }
    }
    if (rawText[i] === "=" && i + 1 < len && rawText[i + 1] === "=") {
      i += 2
      if (i < len && rawText[i] === "{") {
        const j = rawText.indexOf("}", i)
        if (j >= 0) i = j + 1
      }
      continue
    }
    if (rawText[i] === "[") {
      let depth = 1, j = i + 1
      while (j < len && depth > 0) {
        if (rawText[j] === "\\") { j += 2; continue }
        if (rawText[j] === "[") depth++
        if (rawText[j] === "]") depth--
        j++
      }
      if (depth === 0 && j < len && rawText[j] === "(") {
        let pd = 1, k = j + 1
        while (k < len && pd > 0) {
          if (rawText[k] === "\\") { k += 2; continue }
          if (rawText[k] === "(") pd++
          if (rawText[k] === ")") pd--
          k++
        }
        if (pd === 0) { i++; continue }
      }
    }
    if (rawText[i] === "*" && i + 1 < len && rawText[i + 1] === "*") { i += 2; continue }
    if (rawText[i] === "*") { i++; continue }
    if (rawText[i] === "~" && i + 1 < len && rawText[i + 1] === "~") { i += 2; continue }
    if (rawText[i] === "~") { i++; continue }
    if (rawText[i] === "^") { i++; continue }
    if (rawText[i] === "_") { i++; continue }
    if (rawText[i] === "`") { i++; continue }
    if (rawText[i] === "@" && i + 1 < len && rawText[i + 1] === "`") { i++; continue }
    i++; plain++
  }
  return i
}

// Plain text of one content node — used to build text quotes and for fuzzy search.
function rawNodePlainText(rawText: string): string {
  // Allocate the full plain string by scanning once.
  let out = ""
  let i = 0
  const len = rawText.length
  while (i < len) {
    if (rawText[i] === "\\" && i + 1 < len) { out += rawText[i + 1]; i += 2; continue }
    if (rawText[i] === "^" && i + 1 < len && rawText[i + 1] === "[") {
      let depth = 1, j = i + 2
      while (j < len && depth > 0) {
        if (rawText[j] === "\\") { j += 2; continue }
        if (rawText[j] === "[") depth++
        if (rawText[j] === "]") depth--
        j++
      }
      if (depth === 0) { i = j; continue }
    }
    if (rawText[i] === "{" && i + 1 < len && rawText[i + 1] === "#") {
      const j = rawText.indexOf("}", i + 2); if (j >= 0) { i = j + 1; continue }
    }
    if (rawText[i] === "{" && rawText.startsWith("ref ", i + 1)) {
      const j = rawText.indexOf("}", i + 2); if (j >= 0) { i = j + 1; continue }
    }
    if (rawText[i] === "{" && i + 1 < len && rawText[i + 1] === "{") {
      const j = rawText.indexOf("}}", i + 2); if (j >= 0) { i = j + 2; continue }
    }
    if (rawText[i] === "=" && i + 1 < len && rawText[i + 1] === "=") {
      i += 2
      if (i < len && rawText[i] === "{") { const j = rawText.indexOf("}", i); if (j >= 0) i = j + 1 }
      continue
    }
    if (rawText[i] === "[") {
      let depth = 1, j = i + 1
      while (j < len && depth > 0) {
        if (rawText[j] === "\\") { j += 2; continue }
        if (rawText[j] === "[") depth++
        if (rawText[j] === "]") depth--
        j++
      }
      if (depth === 0 && j < len && rawText[j] === "(") {
        let pd = 1, k = j + 1
        while (k < len && pd > 0) {
          if (rawText[k] === "\\") { k += 2; continue }
          if (rawText[k] === "(") pd++
          if (rawText[k] === ")") pd--
          k++
        }
        if (pd === 0) { out += rawNodePlainText(rawText.slice(i + 1, j - 1)); i = k; continue }
      }
    }
    if (rawText[i] === "*" && i + 1 < len && rawText[i + 1] === "*") { i += 2; continue }
    if (rawText[i] === "*") { i++; continue }
    if (rawText[i] === "~" && i + 1 < len && rawText[i + 1] === "~") { i += 2; continue }
    if (rawText[i] === "~") { i++; continue }
    if (rawText[i] === "^") { i++; continue }
    if (rawText[i] === "_") { i++; continue }
    if (rawText[i] === "`") { i++; continue }
    if (rawText[i] === "@" && i + 1 < len && rawText[i + 1] === "`") { i++; continue }
    out += rawText[i]; i++
  }
  return out
}

// Plain text of one PM content node — mirrors the structure used by rawNodePlainText.
function pmNodePlainText(node: PMNode): string {
  if (node.type.name === "code_block") return node.textContent
  let out = ""
  node.forEach((child) => {
    if (child.isText) out += child.text || ""
    else if (child.type.name === "hard_break") out += "\n"
    // Atom inline nodes (footnote, flow_marker, etc.) contribute 0 plain chars.
  })
  return out
}

// ── Compute anchors ─────────────────────────────────────────────────────────

// Structural address from a PM cursor position. Optional textQuote captures the
// plain-text neighborhood for fuzzy fallback.
export function pointFromPm(doc: PMNode, pmPos: number, opts: { withTextQuote?: boolean } = {}): AnchorPoint {
  let idx = 0
  let result: AnchorPoint | null = null
  let foundNode: PMNode | null = null

  doc.descendants((node, pos) => {
    if (result) return false
    if (PM_CONTENT_TYPES.has(node.type.name)) {
      const nodeEnd = pos + node.nodeSize
      if (pmPos >= pos && pmPos <= nodeEnd) {
        let charCount = 0
        if (node.type.name === "code_block") {
          charCount = Math.max(0, pmPos - pos - 1)
        } else {
          let captured = false
          node.forEach((child, offset) => {
            if (captured) return
            const childPos = pos + 1 + offset
            const childEnd = childPos + child.nodeSize
            if (pmPos <= childPos) { captured = true; return }
            if (child.isText) {
              if (pmPos < childEnd) { charCount += pmPos - childPos; captured = true; return }
              charCount += (child.text || "").length
            } else if (child.type.name === "hard_break") {
              charCount += 1
            }
          })
        }
        result = { nodeIndex: idx, plainOffset: charCount }
        foundNode = node
        return false
      }
      idx++
      return false
    }
    return PM_CONTAINER_TYPES.has(node.type.name)
  })

  if (!result) result = { nodeIndex: Math.max(0, idx - 1), plainOffset: 0 }
  if (opts.withTextQuote && foundNode) {
    result.textQuote = buildTextQuoteFromNode(foundNode, result.plainOffset, result.plainOffset)
  }
  return result
}

// Structural address from a raw markdown cursor position.
export function pointFromRaw(markdown: string, rawPos: number, opts: { withTextQuote?: boolean } = {}): AnchorPoint {
  const nodes = scanMarkdownContentNodes(markdown)
  for (let idx = 0; idx < nodes.length; idx++) {
    const n = nodes[idx]
    if (rawPos >= n.rawStart && rawPos <= n.rawEnd) {
      const rawText = markdown.slice(n.rawStart, n.rawEnd)
      const rawOffsetInNode = rawPos - n.rawStart
      const plainOffset = n.type === "code_block"
        ? rawOffsetInNode
        : rawToPlainOffset(rawText, rawOffsetInNode)
      const anchor: AnchorPoint = { nodeIndex: idx, plainOffset }
      if (opts.withTextQuote) {
        const plain = n.type === "code_block" ? rawText : rawNodePlainText(rawText)
        anchor.textQuote = buildTextQuoteFromPlain(plain, plainOffset, plainOffset)
      }
      return anchor
    }
  }
  for (let idx = 0; idx < nodes.length; idx++) {
    if (nodes[idx].rawStart > rawPos) return { nodeIndex: idx, plainOffset: 0 }
  }
  return nodes.length > 0
    ? { nodeIndex: nodes.length - 1, plainOffset: 0 }
    : { nodeIndex: 0, plainOffset: 0 }
}

// Range anchor from a PM selection. textQuote captures the doc-wide plain-text
// neighborhood plus the selected text — this is the fallback used by comments.
export function rangeFromPm(doc: PMNode, from: number, to: number, opts: { withTextQuote?: boolean } = {}): AnchorRange {
  const start = pointFromPm(doc, from)
  const end = pointFromPm(doc, to)
  const range: AnchorRange = { start, end }
  if (opts.withTextQuote) {
    range.textQuote = buildTextQuoteFromDocRange(doc, from, to)
  }
  return range
}

// ── Resolve anchors ─────────────────────────────────────────────────────────

// Resolve a structural address to a PM position.
function resolveAddressInPm(doc: PMNode, nodeIndex: number, plainOffset: number): { pos: number; node: PMNode | null } {
  let idx = 0
  let result = 1
  let resultNode: PMNode | null = null
  let done = false
  doc.descendants((node, pos) => {
    if (done) return false
    if (PM_CONTENT_TYPES.has(node.type.name)) {
      if (idx === nodeIndex) {
        if (node.type.name === "code_block") {
          result = Math.min(pos + 1 + plainOffset, pos + node.nodeSize - 1)
        } else {
          let charCount = 0
          let found = false
          node.forEach((child, offset) => {
            if (found) return
            const childPos = pos + 1 + offset
            if (child.isText) {
              const textLen = (child.text || "").length
              if (charCount + textLen >= plainOffset) {
                result = childPos + (plainOffset - charCount)
                found = true
                return
              }
              charCount += textLen
            } else if (child.type.name === "hard_break") {
              if (charCount + 1 >= plainOffset) { result = childPos; found = true; return }
              charCount += 1
            }
          })
          if (!found) result = pos + node.nodeSize - 1
        }
        resultNode = node
        done = true
        return false
      }
      idx++
      return false
    }
    return PM_CONTAINER_TYPES.has(node.type.name)
  })
  return { pos: result, node: resultNode }
}

export function resolvePointInPm(doc: PMNode, anchor: AnchorPoint): ResolvedPoint {
  const totalNodes = countPmContentNodes(doc)
  if (anchor.nodeIndex < totalNodes) {
    const { pos, node } = resolveAddressInPm(doc, anchor.nodeIndex, anchor.plainOffset)
    if (node && (!anchor.textQuote || textQuoteMatchesAtPoint(pmNodePlainText(node), anchor.plainOffset, anchor.textQuote))) {
      return { pos, confidence: "exact" }
    }
  }
  // Fall back to text-quote search over the whole doc.
  if (anchor.textQuote) {
    const fuzzy = fuzzyFindPointInPm(doc, anchor.textQuote)
    if (fuzzy) return { pos: fuzzy, confidence: "fuzzy" }
  }
  // Last resort: best-effort resolution at whatever the address yields.
  const { pos } = resolveAddressInPm(doc, anchor.nodeIndex, anchor.plainOffset)
  return { pos, confidence: "lost" }
}

export function resolvePointInRaw(markdown: string, anchor: AnchorPoint): ResolvedPoint {
  const nodes = scanMarkdownContentNodes(markdown)
  if (anchor.nodeIndex < nodes.length) {
    const n = nodes[anchor.nodeIndex]
    const rawText = markdown.slice(n.rawStart, n.rawEnd)
    const plain = n.type === "code_block" ? rawText : rawNodePlainText(rawText)
    if (!anchor.textQuote || textQuoteMatchesAtPoint(plain, anchor.plainOffset, anchor.textQuote)) {
      const pos = n.type === "code_block"
        ? Math.min(n.rawStart + anchor.plainOffset, n.rawEnd)
        : n.rawStart + plainToRawOffset(rawText, anchor.plainOffset)
      return { pos, confidence: "exact" }
    }
  }
  if (anchor.textQuote) {
    const fuzzy = fuzzyFindPointInRaw(markdown, anchor.textQuote, nodes)
    if (fuzzy !== null) return { pos: fuzzy, confidence: "fuzzy" }
  }
  if (anchor.nodeIndex >= nodes.length) {
    return { pos: nodes.length > 0 ? nodes[nodes.length - 1].rawEnd : 0, confidence: "lost" }
  }
  const n = nodes[anchor.nodeIndex]
  const rawText = markdown.slice(n.rawStart, n.rawEnd)
  const pos = n.type === "code_block"
    ? Math.min(n.rawStart + anchor.plainOffset, n.rawEnd)
    : n.rawStart + plainToRawOffset(rawText, anchor.plainOffset)
  return { pos, confidence: "lost" }
}

export function resolveRangeInPm(doc: PMNode, anchor: AnchorRange): ResolvedRange {
  const start = resolvePointInPm(doc, anchor.start)
  const end = resolvePointInPm(doc, anchor.end)
  // If both endpoints resolved exactly, we're done.
  if (start.confidence === "exact" && end.confidence === "exact") {
    return { from: Math.min(start.pos, end.pos), to: Math.max(start.pos, end.pos), confidence: "exact" }
  }
  // Try the range-level fuzzy search using the textQuote (with exact selected text).
  if (anchor.textQuote?.exact) {
    const fuzzy = fuzzyFindRangeInPm(doc, anchor.textQuote)
    if (fuzzy) return { from: fuzzy.from, to: fuzzy.to, confidence: "fuzzy" }
  }
  // Fall back to whatever endpoints we got.
  const confidence: Confidence = (start.confidence === "lost" || end.confidence === "lost") ? "lost" : "fuzzy"
  return { from: Math.min(start.pos, end.pos), to: Math.max(start.pos, end.pos), confidence }
}

// ── Text-quote helpers ──────────────────────────────────────────────────────

function buildTextQuoteFromNode(node: PMNode, startOffset: number, endOffset: number): TextQuote {
  const plain = pmNodePlainText(node)
  return buildTextQuoteFromPlain(plain, startOffset, endOffset)
}

function buildTextQuoteFromPlain(plain: string, startOffset: number, endOffset: number): TextQuote {
  const s = Math.max(0, Math.min(startOffset, plain.length))
  const e = Math.max(s, Math.min(endOffset, plain.length))
  const prefix = plain.slice(Math.max(0, s - QUOTE_CTX), s)
  const suffix = plain.slice(e, Math.min(plain.length, e + QUOTE_CTX))
  const quote: TextQuote = { prefix, suffix }
  if (e > s) {
    const exact = plain.slice(s, e)
    quote.exact = exact.length > QUOTE_EXACT_MAX ? exact.slice(0, QUOTE_EXACT_MAX) : exact
  }
  return quote
}

function buildTextQuoteFromDocRange(doc: PMNode, from: number, to: number): TextQuote {
  // Build a doc-wide plain-text projection with PM-position landmarks, then locate from/to.
  const { plain, marks } = buildDocPlainProjection(doc)
  const startPlain = pmPosToPlain(marks, from, plain.length)
  const endPlain = pmPosToPlain(marks, to, plain.length)
  return buildTextQuoteFromPlain(plain, startPlain, endPlain)
}

// Does the textQuote agree with what's actually at `offset` in `plain`?
function textQuoteMatchesAtPoint(plain: string, offset: number, quote: TextQuote): boolean {
  const o = Math.max(0, Math.min(offset, plain.length))
  // For a point anchor with no exact, prefix.endsWith(plain.slice(...)) and suffix.startsWith(plain.slice(...)).
  // We accept the match if either context aligns (the doc near this point looks right).
  const before = plain.slice(Math.max(0, o - quote.prefix.length), o)
  const after = plain.slice(o, Math.min(plain.length, o + quote.suffix.length))
  if (quote.exact) {
    // For ranges this is called per-endpoint, so don't insist on exact here.
    if (before === quote.prefix && after.startsWith(quote.exact)) return true
    if (after === quote.suffix && before.endsWith(quote.exact)) return true
  }
  return before.endsWith(quote.prefix) || after.startsWith(quote.suffix)
}

// ── Fuzzy fallback search ───────────────────────────────────────────────────

interface PlainProjection {
  plain: string
  // Per content-node landmarks: where each node's plain text starts in `plain`, and what PM position it occupies.
  marks: Array<{ plainStart: number; plainEnd: number; pmStart: number; pmEnd: number; isCodeBlock: boolean }>
}

function buildDocPlainProjection(doc: PMNode): PlainProjection {
  const marks: PlainProjection["marks"] = []
  const parts: string[] = []
  let plainLen = 0
  doc.descendants((node, pos) => {
    if (PM_CONTENT_TYPES.has(node.type.name)) {
      const text = pmNodePlainText(node)
      const plainStart = plainLen
      parts.push(text)
      plainLen += text.length
      // Separator between blocks so a phrase doesn't accidentally span two paragraphs.
      parts.push("\n")
      plainLen += 1
      marks.push({
        plainStart,
        plainEnd: plainStart + text.length,
        pmStart: pos + 1,
        pmEnd: pos + node.nodeSize - 1,
        isCodeBlock: node.type.name === "code_block",
      })
      return false
    }
    return PM_CONTAINER_TYPES.has(node.type.name)
  })
  return { plain: parts.join(""), marks }
}

function pmPosToPlain(marks: PlainProjection["marks"], pmPos: number, plainLen: number): number {
  for (const m of marks) {
    if (pmPos < m.pmStart) return m.plainStart
    if (pmPos <= m.pmEnd) {
      // Linear interpolation by character — accurate for text-only nodes, approximate for nodes with atoms.
      const span = m.pmEnd - m.pmStart
      const offsetInPm = pmPos - m.pmStart
      const plainSpan = m.plainEnd - m.plainStart
      if (span === 0) return m.plainStart
      return m.plainStart + Math.min(plainSpan, Math.round(offsetInPm * plainSpan / span))
    }
  }
  return plainLen
}

function plainToPmPos(marks: PlainProjection["marks"], plainPos: number): number | null {
  for (const m of marks) {
    if (plainPos >= m.plainStart && plainPos <= m.plainEnd) {
      const plainSpan = m.plainEnd - m.plainStart
      const pmSpan = m.pmEnd - m.pmStart
      if (plainSpan === 0) return m.pmStart
      const offsetInPlain = plainPos - m.plainStart
      return m.pmStart + Math.min(pmSpan, Math.round(offsetInPlain * pmSpan / plainSpan))
    }
  }
  return null
}

function fuzzyFindPointInPm(doc: PMNode, quote: TextQuote): number | null {
  const proj = buildDocPlainProjection(doc)
  const plainPos = fuzzyFindPointInPlain(proj.plain, quote)
  if (plainPos === null) return null
  return plainToPmPos(proj.marks, plainPos)
}

function fuzzyFindRangeInPm(doc: PMNode, quote: TextQuote): { from: number; to: number } | null {
  if (!quote.exact) return null
  const proj = buildDocPlainProjection(doc)
  const hit = findBestQuoteMatch(proj.plain, quote)
  if (!hit) return null
  const from = plainToPmPos(proj.marks, hit.start)
  const to = plainToPmPos(proj.marks, hit.end)
  if (from === null || to === null) return null
  return { from, to }
}

function fuzzyFindPointInRaw(markdown: string, quote: TextQuote, nodes: ContentNode[]): number | null {
  // Build a markdown-wide plain projection from the scanned nodes.
  const parts: string[] = []
  const marks: Array<{ plainStart: number; plainEnd: number; rawStart: number; rawEnd: number; isCodeBlock: boolean }> = []
  let plainLen = 0
  for (const n of nodes) {
    const rawText = markdown.slice(n.rawStart, n.rawEnd)
    const text = n.type === "code_block" ? rawText : rawNodePlainText(rawText)
    const plainStart = plainLen
    parts.push(text); plainLen += text.length
    parts.push("\n"); plainLen += 1
    marks.push({
      plainStart, plainEnd: plainStart + text.length,
      rawStart: n.rawStart, rawEnd: n.rawEnd,
      isCodeBlock: n.type === "code_block",
    })
  }
  const plain = parts.join("")
  const plainPos = fuzzyFindPointInPlain(plain, quote)
  if (plainPos === null) return null
  // Map plain pos back to raw markdown char offset.
  for (const m of marks) {
    if (plainPos >= m.plainStart && plainPos <= m.plainEnd) {
      const rawText = markdown.slice(m.rawStart, m.rawEnd)
      const offsetInPlain = plainPos - m.plainStart
      if (m.isCodeBlock) return m.rawStart + Math.min(offsetInPlain, rawText.length)
      return m.rawStart + plainToRawOffset(rawText, offsetInPlain)
    }
  }
  return null
}

function fuzzyFindPointInPlain(plain: string, quote: TextQuote): number | null {
  // Try anchoring on prefix+suffix concatenation first (most specific).
  const combined = quote.prefix + (quote.exact || "") + quote.suffix
  if (combined.length >= 6) {
    const positions = allIndexesOf(plain, combined)
    if (positions.length > 0) return positions[0] + quote.prefix.length + (quote.exact?.length || 0)
  }
  // Try prefix alone (anchor right after it).
  if (quote.prefix.length >= 4) {
    const positions = allIndexesOf(plain, quote.prefix)
    if (positions.length === 1) return positions[0] + quote.prefix.length
  }
  // Try suffix alone (anchor right before it).
  if (quote.suffix.length >= 4) {
    const positions = allIndexesOf(plain, quote.suffix)
    if (positions.length === 1) return positions[0]
  }
  return null
}

function findBestQuoteMatch(plain: string, quote: TextQuote): { start: number; end: number } | null {
  if (!quote.exact) return null
  const exact = quote.exact
  const positions = allIndexesOf(plain, exact)
  if (positions.length === 0) {
    // Whitespace-tolerant fallback.
    const normalized = plain.replace(/\s+/g, " ")
    const idx = normalized.indexOf(exact.replace(/\s+/g, " "))
    if (idx < 0) return null
    return { start: idx, end: idx + exact.length }
  }
  if (positions.length === 1) return { start: positions[0], end: positions[0] + exact.length }
  // Multiple matches — score by context.
  let bestStart = positions[0], bestScore = -1
  for (const p of positions) {
    let score = 0
    if (quote.prefix) {
      const ctx = plain.slice(Math.max(0, p - quote.prefix.length), p)
      if (ctx.endsWith(quote.prefix)) score += 2
      else if (ctx.includes(quote.prefix)) score += 1
    }
    if (quote.suffix) {
      const ctx = plain.slice(p + exact.length, p + exact.length + quote.suffix.length)
      if (ctx.startsWith(quote.suffix)) score += 2
      else if (ctx.includes(quote.suffix)) score += 1
    }
    if (score > bestScore) { bestScore = score; bestStart = p }
  }
  return { start: bestStart, end: bestStart + exact.length }
}

function allIndexesOf(haystack: string, needle: string): number[] {
  if (!needle) return []
  const out: number[] = []
  let from = 0
  while (true) {
    const idx = haystack.indexOf(needle, from)
    if (idx < 0) break
    out.push(idx)
    from = idx + 1
  }
  return out
}

function countPmContentNodes(doc: PMNode): number {
  let count = 0
  doc.descendants((node) => {
    if (PM_CONTENT_TYPES.has(node.type.name)) { count++; return false }
    return PM_CONTAINER_TYPES.has(node.type.name)
  })
  return count
}

// ── DOM bridge ──────────────────────────────────────────────────────────────

// Convert a window.Selection (typically from a user click-drag in the rendered view)
// into PM doc positions. Returns null if the selection is empty or outside the view.
export function domSelectionToPmRange(view: EditorView, selection: Selection | null): { from: number; to: number } | null {
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null
  const range = selection.getRangeAt(0)
  if (!view.dom.contains(range.startContainer) || !view.dom.contains(range.endContainer)) return null
  try {
    const from = view.posAtDOM(range.startContainer, range.startOffset, -1)
    const to = view.posAtDOM(range.endContainer, range.endOffset, 1)
    if (from < 0 || to < 0) return null
    return { from: Math.min(from, to), to: Math.max(from, to) }
  } catch {
    return null
  }
}

// ── Back-compat exports (drop after consumers migrate) ──────────────────────
//
// The shapes below match the names used by main.js so the move to the module is
// a pure import rename for that file.

export function computeContentAddress(markdown: string, cursorPos: number): { nodeIndex: number; plainOffset: number } {
  const a = pointFromRaw(markdown, cursorPos)
  return { nodeIndex: a.nodeIndex, plainOffset: a.plainOffset }
}

export function computePmContentAddress(doc: PMNode, pmPos: number): { nodeIndex: number; plainOffset: number } {
  const a = pointFromPm(doc, pmPos)
  return { nodeIndex: a.nodeIndex, plainOffset: a.plainOffset }
}

export function resolveRawContentAddress(markdown: string, nodeIndex: number, plainOffset: number): number {
  return resolvePointInRaw(markdown, { nodeIndex, plainOffset }).pos
}

export function resolveContentAddress(doc: PMNode, nodeIndex: number, plainOffset: number): number {
  return resolvePointInPm(doc, { nodeIndex, plainOffset }).pos
}
