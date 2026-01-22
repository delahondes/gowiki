// core/paragraph/wysiwym.ts
//
// ProseMirror / WYSIWYM integration for the `paragraph` plugin.
// Fully self-contained, plugin-owned.

import { NodeSpec, Node as PMNode, Schema } from "prosemirror-model"

import {
  registerNodeSpec,
  registerNodeFromPM,
  registerNodeToPM,
} from "../registry"

import { DocNode } from "../docmodel"

const KindParagraph = "paragraph"
const KindPseudoParagraph = "pseudo_paragraph"

/* ------------------------------------------------------------------
 * ProseMirror schema
 * ------------------------------------------------------------------ */

const paragraphNodeSpec: NodeSpec = {
  attrs: { pseudo: { default: false } },
  content: "inline*",
  group: "block",
  parseDOM: [{ tag: "p", getAttrs: () => ({ pseudo: false }) }],
  toDOM() {
    return ["p", 0]
  },
}

/* ------------------------------------------------------------------
 * Conversion: docmodel → ProseMirror
 * ------------------------------------------------------------------ */

function toPMParagraph(
  schema: Schema,
  node: DocNode,
  children: PMNode[]
): PMNode {
  return schema.node(
    "paragraph",
    { pseudo: node.Kind === "pseudo_paragraph" },
    children
  )
}

/* ------------------------------------------------------------------
 * Conversion: ProseMirror → docmodel
 * ------------------------------------------------------------------ */

function fromPMParagraph(_node: PMNode): DocNode {
  if (_node.attrs?.pseudo === true) {
    return {
      Kind: KindPseudoParagraph,
      Payload: null,
    }
  }
  return {
    Kind: KindParagraph,
    Payload: null,
  }
}

/* ------------------------------------------------------------------
 * Registration (side effects)
 * ------------------------------------------------------------------ */
console.log("Defining WYSIWYM for kinds:", KindParagraph, "and", KindPseudoParagraph)
registerNodeSpec(KindParagraph, paragraphNodeSpec)
registerNodeToPM(KindParagraph, toPMParagraph)
registerNodeFromPM(KindParagraph, fromPMParagraph)

registerNodeSpec(KindPseudoParagraph, paragraphNodeSpec)
registerNodeToPM(KindPseudoParagraph, toPMParagraph)
registerNodeFromPM(KindPseudoParagraph, fromPMParagraph)