// core/paragraph/wysiwym.ts
//
// ProseMirror / WYSIWYM integration for the `paragraph` plugin.
// Fully self-contained, plugin-owned.

import { NodeSpec, Node as PMNode } from "prosemirror-model"

import {
  registerNodeSpec,
  registerNodeFromPM,
  registerNodeToPM,
} from "../../frontend/registry"

import { DocNode } from "../../frontend/docmodel"

const KindParagraph = "paragraph"

/* ------------------------------------------------------------------
 * ProseMirror schema
 * ------------------------------------------------------------------ */

const paragraphNodeSpec: NodeSpec = {
  content: "inline*",
  group: "block",
  parseDOM: [{ tag: "p" }],
  toDOM() {
    return ["p", 0]
  },
}

/* ------------------------------------------------------------------
 * Conversion: docmodel → ProseMirror
 * ------------------------------------------------------------------ */

function toPMParagraph(node: DocNode) {
  // Paragraph itself carries no payload;
  // children are handled by the kernel walker.
  return null
}

/* ------------------------------------------------------------------
 * Conversion: ProseMirror → docmodel
 * ------------------------------------------------------------------ */

function fromPMParagraph(_node: PMNode): DocNode {
  return {
    kind: KindParagraph,
    payload: null,
  }
}

/* ------------------------------------------------------------------
 * Registration (side effects)
 * ------------------------------------------------------------------ */

registerNodeSpec(KindParagraph, paragraphNodeSpec)
registerNodeToPM(KindParagraph, toPMParagraph)
registerNodeFromPM(KindParagraph, fromPMParagraph)