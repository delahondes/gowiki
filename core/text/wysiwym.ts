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

/* ------------------------------------------------------------------
 * Conversion: docmodel → ProseMirror
 * ------------------------------------------------------------------ */

function toPMText(node: DocNode, schema: any) {
  // Create a ProseMirror text node with the payload string.
  return schema.text(node.payload || "")
}

/* ------------------------------------------------------------------
 * Conversion: ProseMirror → docmodel
 * ------------------------------------------------------------------ */

function fromPMText(node: PMNode): DocNode {
  return {
    kind: KindText,
    payload: node.text || "",
  }
}

/* ------------------------------------------------------------------
 * Registration (side effects)
 * ------------------------------------------------------------------ */

registerNodeToPM(KindText, toPMText)
registerNodeFromPM(KindText, fromPMText)