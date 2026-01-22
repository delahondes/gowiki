// frontend/docmodel.ts
//
// TypeScript mirror of the Go docmodel.
// This file is intentionally minimal and structural only.
// It exists to type WYSI

// docmodel.ts (or docmodel/nodespec.ts)
import { NODE_SPECS, type NodeSpec } from "./nodespec.gen"

const nodeSpecMap = new Map<string, NodeSpec>(
  NODE_SPECS.map(s => [s.kind, s])
)

export function getNodeSpec(kind: string): NodeSpec | undefined {
  return nodeSpecMap.get(kind)
}

export type DocNode = {
  Kind: string
  Payload: any
  Children?: DocNode[]
}

export function childrenOf(node: DocNode): readonly DocNode[] {
  return node.Children ?? []
}

/* ------------------------------------------------------------------
 * Kernel constructors
 * ------------------------------------------------------------------ */

// Document is kernel-level (not a plugin)
export function newDocument(children: DocNode[]): DocNode {
  return {
    Kind: "document",
    Payload: null,
    Children: children,
  }
}

// Fragment is kernel-level glue (paragraphs, inline containers, etc.)
export function newFragment(children: DocNode[]): DocNode {
  return {
    Kind: "fragment",
    Payload: null,
    Children:children,
  }
}

// TEMPORARY: text constructor
// This mirrors the core text plugin and exists only to unblock
// pm_to_docmodel wiring. It should later be replaced by importing
// the text plugin’s factory.
export function newText(text: string): DocNode {
  return {
    Kind: "text",
    Payload: text,
    Children: [],
  }
}
