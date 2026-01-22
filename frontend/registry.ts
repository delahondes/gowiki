// frontend/registry.ts
import { MarkSpec, NodeSpec, Schema } from "prosemirror-model"

export const markSpecs = new Map<string, MarkSpec>()
export const toPM = new Map<string, Function>()
export const fromPM = new Map<string, Function>()

export const nodeSpecs = new Map<string, NodeSpec>()
export const nodeToPM = new Map<string, Function>()
export const nodeFromPM = new Map<string, Function>()

export function registerMarkSpec(kind: string, spec: MarkSpec) {
  markSpecs.set(kind, spec)
}

export function registerMarkToPM(kind: string, fn: Function) {
  toPM.set(kind, fn)
}

export function registerMarkFromPM(kind: string, fn: Function) {
  fromPM.set(kind, fn)
}

export function registerNodeSpec(kind: string, spec: NodeSpec) {
  nodeSpecs.set(kind, spec)
}

export function registerNodeToPM(kind: string, fn: Function) {
  nodeToPM.set(kind, fn)
}

export function registerNodeFromPM(pmType: string, fn: Function) {
  nodeFromPM.set(pmType, fn)
}

export function getMarkSpec(kind: string): MarkSpec | undefined {
  return markSpecs.get(kind)
}

export function getMarkToPM(kind: string): Function | undefined {
  return toPM.get(kind)
}

export function getMarkFromPM(kind: string): Function | undefined {
  return fromPM.get(kind)
}

export function getNodeSpec(kind: string): NodeSpec | undefined {
  return nodeSpecs.get(kind)
}

export function getNodeToPM(kind: string): Function | undefined {
  return nodeToPM.get(kind)
}

export function getNodeFromPM(pmType: string): Function | undefined {
  return nodeFromPM.get(pmType)
}

// ------------------------------------------------------------------
// Schema construction (kernel-level)
// ------------------------------------------------------------------

/**
 * Build a ProseMirror Schema from all registered node and mark specs.
 * Core/kernel nodes (doc, text) are defined here.
 * Plugin nodes and marks are injected via the registries above.
 */
export function buildSchema(): Schema {
  // Kernel nodes
  const nodes: Record<string, NodeSpec> = {
    doc: {
      content: "block+",
    },
    text: {
      group: "inline",
    },
  }

  // Plugin-provided nodes
  for (const [kind, spec] of nodeSpecs.entries()) {
    nodes[kind] = spec
  }

  // Plugin-provided marks
  const marks: Record<string, MarkSpec> = {}
  for (const [kind, spec] of markSpecs.entries()) {
    marks[kind] = spec
  }

  return new Schema({ nodes, marks })
}