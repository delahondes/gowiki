// core/list/wysiwym.ts
//
// ProseMirror / WYSIWYM integration for the `list` plugin.
//
// Responsibilities:
// - PM schema contribution (bullet_list + list_item)
// - docmodel ↔ ProseMirror conversion
// - registration side-effects only

import { NodeSpec, NodeType } from "prosemirror-model"
import { Transaction } from "prosemirror-state"

import {
  registerNodeSpec,
  registerNodeFromPM,
  registerNodeToPM,
} from "../../frontend/registry"

import { DocNode } from "../../frontend/docmodel"

const KindBulletList = "bullet_list"
const KindListItem = "list_item"

/* ------------------------------------------------------------------
 * ProseMirror schema
 * ------------------------------------------------------------------ */

const bulletListSpec: NodeSpec = {
  group: "block",
  content: "list_item+",
  parseDOM: [{ tag: "ul" }],
  toDOM() {
    return ["ul", 0]
  },
}

const listItemSpec: NodeSpec = {
  content: "paragraph block*",
  parseDOM: [{ tag: "li" }],
  toDOM() {
    return ["li", 0]
  },
}

/* ------------------------------------------------------------------
 * Conversion: docmodel → ProseMirror
 * ------------------------------------------------------------------ */

function toPMBulletList(nodeType: NodeType, tr: Transaction) {
  const { $from, $to } = tr.selection
  const range = $from.blockRange($to)
  if (!range) return
  tr.wrap(range, [{ type: nodeType }])
}

function toPMListItem(nodeType: NodeType, tr: Transaction) {
  const { $from, $to } = tr.selection
  const range = $from.blockRange($to)
  if (!range) return
  tr.wrap(range, [{ type: nodeType }])
}

/* ------------------------------------------------------------------
 * Conversion: ProseMirror → docmodel
 * ------------------------------------------------------------------ */

function fromPMBulletList(children: DocNode[]): DocNode {
  return {
    kind: KindBulletList,
    payload: null,
    children,
  }
}

function fromPMListItem(children: DocNode[]): DocNode {
  return {
    kind: KindListItem,
    payload: null,
    children,
  }
}

/* ------------------------------------------------------------------
 * Registration (side effects)
 * ------------------------------------------------------------------ */

registerNodeSpec(KindBulletList, bulletListSpec)
registerNodeSpec(KindListItem, listItemSpec)

registerNodeToPM(KindBulletList, toPMBulletList)
registerNodeToPM(KindListItem, toPMListItem)

registerNodeFromPM(KindBulletList, fromPMBulletList)
registerNodeFromPM(KindListItem, fromPMListItem)