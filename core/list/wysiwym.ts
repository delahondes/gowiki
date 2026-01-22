// core/list/wysiwym.ts
//
// ProseMirror / WYSIWYM integration for the `list` plugin.
//
// Responsibilities:
// - PM schema contribution (bullet_list + list_item)
// - docmodel ↔ ProseMirror conversion
// - registration side-effects only

import { NodeSpec } from "prosemirror-model"

import {
  registerNodeSpec,
  registerNodeFromPM,
  registerNodeToPM,
} from "../../frontend/registry"

import { DocNode } from "../../frontend/docmodel"

const KindBulletList = "bullet_list"
const KindListItem = "list_item"

console.log("Defining WYSIWYM for kind:", KindListItem)
console.log("Defining WYSIWYM for kind:", KindBulletList)

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

function toPMBulletList(
  schema: any,
  node: DocNode,
  children: any[]
) {
  return schema.nodes.bullet_list.create(null, children)
}

function toPMListItem(
  schema: any,
  node: DocNode,
  children: any[]
) {
  return schema.nodes.list_item.create(null, children)
}

/* ------------------------------------------------------------------
 * Conversion: ProseMirror → docmodel
 * ------------------------------------------------------------------ */

function fromPMBulletList(children: DocNode[]): DocNode {
  return {
    Kind: KindBulletList,
    Payload: null,
    Children: children,
  }
}

function fromPMListItem(children: DocNode[]): DocNode {
  return {
    Kind: KindListItem,
    Payload: null,
    Children: children,
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