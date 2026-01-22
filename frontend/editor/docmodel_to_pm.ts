// frontend/editor/docmodel_to_pm.ts
//
// Minimal docmodel → ProseMirror conversion.
// This is editor-kernel code, NOT plugin code.

import { Schema, Node as PMNode, Mark } from "prosemirror-model"
import { EditorState, TextSelection } from "prosemirror-state"

import { DocNode,childrenOf } from "../docmodel"
import { getNodeToPM, getMarkSpec, getMarkToPM } from "../registry"
import { getNodeSpec  } from "../docmodel"
import { FLOW_INLINE, FLOW_BLOCK } from "../nodespec.gen"

/* ------------------------------------------------------------------
 * Entry point
 * ------------------------------------------------------------------ */

export function docModelToEditorState(
  schema: Schema,
  doc: DocNode,
  plugins: any[]
): EditorState {
  const pmDoc = buildPMDocument(schema, doc)


  if (!pmDoc) {
    throw new Error("PM document is undefined")
  }

  try {
    pmDoc.check()
  } catch (e) {
    console.error("Invalid PM document", pmDoc.toJSON())
    throw e
  }

  const selection = TextSelection.atStart(pmDoc);
  console.log("PM DOC JSON:", pmDoc.toJSON())
  return EditorState.create({ doc: pmDoc, schema, plugins, selection })
}

/* ------------------------------------------------------------------
 * Document builder
 * ------------------------------------------------------------------ */

// Block handling is intentionally minimal and paragraph-only for now.
function buildPMDocument(schema: Schema, doc: DocNode): PMNode {
  const blocks: PMNode[] = []
  console.log("Building PM document from docmodel:", doc)

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

  console.log("Starting building block for node:", node.Kind)
  // fragment is transparent
  if (node.Kind === "fragment") {
    for (const child of childrenOf(node)) {
      buildBlock(schema, child, out)
    }
    return
  }

  // 1. build children first
  const pmChildren: PMNode[] = []
  const spec = getNodeSpec(node.Kind)
  console.log("Node spec for", node.Kind, "is", spec)

  if (spec?.childrenFlow === FLOW_INLINE) {
    for (const child of childrenOf(node)) {
      buildInline(schema, child, pmChildren)
    }
  } else if (spec?.childrenFlow === FLOW_BLOCK) {
    for (const child of childrenOf(node)) {
      buildBlock(schema, child, pmChildren)
    }
  } else {
    throw new Error(
      `Cannot build block node "${node.Kind}": ` +
      `unknown or unsupported childrenFlow "${spec?.childrenFlow}"`
    )
  }
  if (pmChildren.length === 0) {
    throw new Error(
      `Block node "${node.Kind}" produced no PM children`
    )
  }

  // 2. delegate node construction to plugin
  console.log("Building block node:", node.Kind)
  const toPM = getNodeToPM(node.Kind)
  if (!toPM) {
    throw new Error(`No PM block mapping for docmodel kind "${node.Kind}"`)
  }

  const pmNode = toPM(schema, node, pmChildren)
  if (!pmNode) {
    throw new Error(`PM builder for "${node.Kind}" returned nothing`)
  }

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
  console.log("Starting building inline for node:", node.Kind)
  const startLen = out.length

  // TEXT: terminal emission
  if (node.Kind === "text") {
    const text = schema.text(node.Payload)
    out.push(
      activeMarks.length > 0 ? text.mark(activeMarks) : text
    )
    console.log("Built PM text node:", text)
    return
  }

  const markSpec = getMarkSpec(node.Kind)
  const toPMMark = getMarkToPM(node.Kind)
  console.log("Mark spec for", node.Kind, "is", markSpec)
  console.log("toPMMark for", node.Kind, "is", toPMMark)

  // Mark consistency check
  if ((markSpec && !toPMMark) || (!markSpec && toPMMark)) {
    throw new Error(
      `Inconsistent inline registration for kind "${node.Kind}": ` +
      `markSpec=${!!markSpec}, toPMMark=${!!toPMMark}`
    )
  }

  // MARK node: enrich context only
  if (markSpec && toPMMark) {
    const mark = toPMMark(schema, node)
    if (!mark) {
      throw new Error(`PM mark builder for "${node.Kind}" returned nothing`)
    }

    const nextMarks = [...activeMarks, mark]
    var outLenBefore = out.length
    for (const child of childrenOf(node)) {
      buildInline(schema, child, out, nextMarks)
      if (out.length === outLenBefore) {
        throw new Error(
          `Marked inline node "${node.Kind}" produced no PM output`
        )
      } else {
        outLenBefore = out.length
      }
    }
    return
  }

  const toPMNode = getNodeToPM(node.Kind)
  if (!toPMNode) {
    throw new Error(`No PM inline mapping for docmodel kind "${node.Kind}"`)
  }

  const pmChildren: PMNode[] = []
  for (const child of childrenOf(node)) {
    buildInline(schema, child, pmChildren, activeMarks)
  }

  const pmNode = toPMNode(schema, node, pmChildren)
  if (!pmNode) {
    throw new Error(`PM builder for "${node.Kind}" returned nothing`)
  } else {
    console.log(`Built PM node for inline "${node.Kind}" :`, pmNode)
  }

  out.push(pmNode)

  // Fail early if this inline produced nothing
  if (out.length === startLen) {
    throw new Error(
      `Inline node "${node.Kind}" produced no PM output`
    )
  }
}