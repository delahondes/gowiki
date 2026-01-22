// core/text/wysiwym.ts
//
// ProseMirror / WYSIWYM integration for the `text` plugin.
// Fully self-contained, plugin-owned.

import { Node as PMNode } from "prosemirror-model"

import {
  registerNodeFromPM,
  registerNodeToPM,
} from "../../frontend/registry"

import { DocNode } from "../../frontend/docmodel"

const KindText = "text"
console.log("Defining WYSIWYM for kind:", KindText)

/* ------------------------------------------------------------------
 * Conversion: docmodel → ProseMirror
 * ------------------------------------------------------------------ */

// not used
function toPMText(
  schema: any,
  node: DocNode,
  children: PMNode[]
): PMNode {
  // Create a ProseMirror text node with the payload string.
  return schema.text(node.Payload || "")
}

/* ------------------------------------------------------------------
 * Conversion: ProseMirror → docmodel
 * ------------------------------------------------------------------ */

function fromPMText(node: PMNode): DocNode {
  return {
    Kind: KindText,
    Payload: node.text || "",
  }
}

/* ------------------------------------------------------------------
 * Registration (side effects)
 * ------------------------------------------------------------------ */

registerNodeToPM(KindText, toPMText)
registerNodeFromPM(KindText, fromPMText)