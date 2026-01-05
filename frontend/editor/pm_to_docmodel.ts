// frontend/editor/pm_to_docmodel.ts
//
// ProseMirror → docmodel conversion
// Editor-kernel code, NOT plugin code.

import { Node as PMNode } from "prosemirror-model"

import {
  DocNode,
  newDocument,
} from "../docmodel"

import { getMarkFromPM, getNodeFromPM } from "../registry"

/* ------------------------------------------------------------------
 * Entry point
 * ------------------------------------------------------------------ */

export function pmToDocModel(pmDoc: PMNode): DocNode {
  const children: DocNode[] = []

  pmDoc.forEach(child => {
    children.push(pmNodeToDoc(child))
  })

  return newDocument(children)
}

/* ------------------------------------------------------------------
 * Node conversion
 * ------------------------------------------------------------------ */

function pmNodeToDoc(node: PMNode): DocNode {
  const fromPM = getNodeFromPM(node.type.name)
  if (!fromPM) {
    throw new Error(`No docmodel mapping for PM node "${node.type.name}"`)
  }
  return fromPM(node, pmInlineToDoc)
}

/* ------------------------------------------------------------------
 * Inline content
 * ------------------------------------------------------------------ */

function pmInlineToDoc(node: PMNode): DocNode[] {
  const out: DocNode[] = []

  node.forEach(child => {
    out.push(pmNodeToDoc(child))
  })

  return out
}