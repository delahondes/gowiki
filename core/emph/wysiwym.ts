// core/emph/wysiwym.ts
//
// ProseMirror / WYSIWYM integration for the `emph` plugin.
//
// This file is intentionally self-contained:
// - schema contribution
// - docmodel ↔ ProseMirror conversion
// - registration side-effects
//
// No editor logic lives outside the plugin except the registry.

import { MarkSpec, MarkType, Schema, Node as PMNode } from "prosemirror-model"

import {
  registerMarkSpec,
  registerMarkFromPM,
  registerMarkToPM,
} from "../../frontend/registry"  // kernel-side TS registry

import { DocNode } from "../../frontend/docmodel" // TS mirror of docmodel

const KindEmph = "emph"
console.log("Defining WYSIWYM for kind:", KindEmph)

/* ------------------------------------------------------------------
 * ProseMirror schema
 * ------------------------------------------------------------------ */

const emphMarkSpec: MarkSpec = {
  parseDOM: [
    { tag: "em" },
    { tag: "i" },
    { style: "font-style=italic" },
  ],
  toDOM() {
    return ["em", 0]
  },
}

/* ------------------------------------------------------------------
 * Conversion: docmodel → ProseMirror
 * ------------------------------------------------------------------ */

// Mark builders never receive children and are applied by buildInline via mark context, not here.
function toPMEmph(
  schema: Schema,
  _node: DocNode
): MarkType | null {
  return schema.marks.emph
}

/* ------------------------------------------------------------------
 * Conversion: ProseMirror → docmodel
 * ------------------------------------------------------------------ */

// When encountering a PM mark of this type, produce a docmodel EMPH
function fromPMEmph(): DocNode {
  return {
    Kind: KindEmph,
    Payload: null,
  }
}

/* ------------------------------------------------------------------
 * Registration (side effects)
 * ------------------------------------------------------------------ */

registerMarkSpec(KindEmph, emphMarkSpec)

registerMarkToPM(KindEmph, toPMEmph)

registerMarkFromPM(KindEmph, fromPMEmph)