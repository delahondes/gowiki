// frontend/docmodel.ts
//
// TypeScript mirror of the Go docmodel.
// This file is intentionally minimal and structural only.
// It exists to type WYSIWYM plugins and editor integration.

export type DocNode = {
  kind: string
  payload: any
  children?: DocNode[]
}

export function childrenOf(node: DocNode): readonly DocNode[] {
  return node.children ?? []
}

/* ------------------------------------------------------------------
 * Kernel constructors
 * ------------------------------------------------------------------ */

// Document is kernel-level (not a plugin)
export function newDocument(children: DocNode[]): DocNode {
  return {
    kind: "document",
    payload: null,
    children,
  }
}

// Fragment is kernel-level glue (paragraphs, inline containers, etc.)
export function newFragment(children: DocNode[]): DocNode {
  return {
    kind: "fragment",
    payload: null,
    children,
  }
}

// TEMPORARY: text constructor
// This mirrors the core text plugin and exists only to unblock
// pm_to_docmodel wiring. It should later be replaced by importing
// the text plugin’s factory.
export function newText(text: string): DocNode {
  return {
    kind: "text",
    payload: text,
    children: [],
  }
}