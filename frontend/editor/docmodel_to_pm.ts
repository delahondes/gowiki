// frontend/editor/docmodel_to_pm.ts
//
// Minimal docmodel → ProseMirror conversion.
// This is editor-kernel code, NOT plugin code.

import { Schema, Node as PMNode, Mark } from "prosemirror-model"
import { EditorState } from "prosemirror-state"

import { DocNode,childrenOf } from "../docmodel"
import { getMarkSpec, getMarkToPM } from "../registry"

/* ------------------------------------------------------------------
 * Entry point
 * ------------------------------------------------------------------ */

export function docModelToEditorState(
  schema: Schema,
  doc: DocNode
): EditorState {
  const pmDoc = buildPMDocument(schema, doc)
  return EditorState.create({ doc: pmDoc })
}

/* ------------------------------------------------------------------
 * Document builder
 * ------------------------------------------------------------------ */

// Block handling is intentionally minimal and paragraph-only for now.
function buildPMDocument(schema: Schema, doc: DocNode): PMNode {
  const blocks: PMNode[] = []

  for (const child of childrenOf(doc)) {
    buildBlock(schema, child, blocks)
  }

  return schema.nodes.doc.create(null, blocks)
}

function buildBlock(
  schema: Schema,
  node: DocNode,
  out: PMNode[]
) {
  // Block nodes must be handled by plugins
  const toPM = (schema as any)._blockToPM?.get(node.kind)
  if (!toPM) {
    throw new Error(`No PM block mapping for docmodel kind "${node.kind}"`)
  }

  const pmNode = toPM(schema, node, buildInline)
  out.push(pmNode)
}

/* ------------------------------------------------------------------
 * Inline handling
 * ------------------------------------------------------------------ */

function buildInline(
  schema: Schema,
  node: DocNode,
  out: PMNode[],
  activeMarks: Mark[] = []
) {
  switch (node.kind) {
    case "text": {
      const text = schema.text(node.payload)
      if (activeMarks.length > 0) {
        out.push(text.mark(activeMarks))
      } else {
        out.push(text)
      }
      return
    }

    default: {
      // Try mark plugin
      const _markSpec = getMarkSpec(node.kind)
      const toPM = getMarkToPM(node.kind)

      if (!_markSpec || !toPM) {
        throw new Error(`No PM mapping for docmodel kind "${node.kind}"`)
      }

      const markType = schema.marks[node.kind]
      if (!markType) {
        throw new Error(`Schema missing mark "${node.kind}"`)
      }

      const newMarks = [...activeMarks, markType.create()]

      for (const child of childrenOf(node)) {
        buildInline(schema, child, out, newMarks)
      }
    }
  }
}