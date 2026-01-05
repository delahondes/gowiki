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