import { Schema } from "prosemirror-model"
import { CompileContext } from "./kernel"

/* -----------------------------
 * Handler interfaces
 * ----------------------------- */

export interface NodeHandler {
  open?: (ctx: CompileContext, token: any) => void
  close?: (ctx: CompileContext, token: any) => void
}

export interface MarkHandler {
  open?: (ctx: CompileContext, token: any) => void
  close?: (ctx: CompileContext, token: any) => void
}

export interface TextHandler {
  run: (ctx: CompileContext, token: any) => void
}

/* -----------------------------
 * Registry
 * ----------------------------- */

export class Registry {
  private nodeHandlers = new Map<string, NodeHandler>()
  private markHandlers = new Map<string, MarkHandler>()
  private textHandlers = new Map<string, TextHandler>()

  constructor(public readonly schema: Schema) {}

  /* ---- registration ---- */

  registerNode(type: string, handler: NodeHandler) {
    this.assertFree(this.nodeHandlers, type, "node")
    this.nodeHandlers.set(type, handler)
  }

  registerMark(type: string, handler: MarkHandler) {
    this.assertFree(this.markHandlers, type, "mark")
    this.markHandlers.set(type, handler)
  }

  registerText(type: string, handler: TextHandler) {
    this.assertFree(this.textHandlers, type, "text")
    this.textHandlers.set(type, handler)
  }

  /* ---- lookup ---- */

  getNode(type: string): NodeHandler | undefined {
    return this.nodeHandlers.get(type)
  }

  getMark(type: string): MarkHandler | undefined {
    return this.markHandlers.get(type)
  }

  getText(type: string): TextHandler | undefined {
    return this.textHandlers.get(type)
  }

  /* ---- helpers ---- */

  private assertFree(
    map: Map<string, unknown>,
    type: string,
    kind: string
  ) {
    if (map.has(type)) {
      throw new Error(
        `Duplicate ${kind} handler registration for token "${type}"`
      )
    }
  }
}