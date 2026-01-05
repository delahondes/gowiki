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

import { MarkSpec, MarkType } from "prosemirror-model"
import { Transaction } from "prosemirror-state"

import {
  registerMarkSpec,
  registerMarkFromPM,
  registerMarkToPM,
} from "../../frontend/registry"  // kernel-side TS registry

import { DocNode } from "../../frontend/docmodel" // TS mirror of docmodel

const KindEmph = "emph"

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

// Given a docmodel node of kind EMPH, apply the PM mark
function toPMEmph(markType: MarkType, tr: Transaction, from: number, to: number) {
  tr.addMark(from, to, markType.create())
}

/* ------------------------------------------------------------------
 * Conversion: ProseMirror → docmodel
 * ------------------------------------------------------------------ */

// When encountering a PM mark of this type, produce a docmodel EMPH
function fromPMEmph(): DocNode {
  return {
    kind: KindEmph,
    payload: null,
  }
}

/* ------------------------------------------------------------------
 * Registration (side effects)
 * ------------------------------------------------------------------ */

registerMarkSpec(KindEmph, emphMarkSpec)

registerMarkToPM(KindEmph, toPMEmph)

registerMarkFromPM(KindEmph, fromPMEmph)