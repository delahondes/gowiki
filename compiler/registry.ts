import { Schema, Node as PMNode, Mark } from "prosemirror-model"
import type { CompileContext } from "./kernel"
import type { PrintContext } from "./pm_to_markdown"

/* -----------------------------
 * Markdown → PM handler interfaces
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
 * PM → Markdown printer interfaces
 * ----------------------------- */

export interface NodePrinter {
  print(
    node: PMNode,
    ctx: PrintContext,
    recurse: (node: PMNode) => string
  ): string
}

export interface MarkPrinter {
  open: string
  close: string
}

/* -----------------------------
 * Unified Registry
 * ----------------------------- */

export class Registry {
  /* Markdown → PM */
  private mdNodes = new Map<string, NodeHandler>()
  private mdMarks = new Map<string, MarkHandler>()
  private mdText = new Map<string, TextHandler>()

  /* PM → Markdown */
  private pmNodes = new Map<string, NodePrinter>()
  private pmMarks = new Map<string, MarkPrinter>()

  constructor(public readonly schema: Schema) {}

  /* ---- registration (Markdown → PM) ---- */

  registerNode(type: string, handler: NodeHandler) {
    this.assertFree(this.mdNodes, type, "MD node")
    this.mdNodes.set(type, handler)
  }

  registerMark(type: string, handler: MarkHandler) {
    this.assertFree(this.mdMarks, type, "MD mark")
    this.mdMarks.set(type, handler)
  }

  registerText(type: string, handler: TextHandler) {
    this.assertFree(this.mdText, type, "MD text")
    this.mdText.set(type, handler)
  }

  /* ---- lookup (Markdown → PM) ---- */

  getNode(type: string): NodeHandler | undefined {
    return this.mdNodes.get(type)
  }

  getMark(type: string): MarkHandler | undefined {
    return this.mdMarks.get(type)
  }

  getText(type: string): TextHandler | undefined {
    return this.mdText.get(type)
  }

  /* ---- registration (PM → Markdown) ---- */

  registerPMNode(name: string, printer: NodePrinter) {
    this.assertFree(this.pmNodes, name, "PM node")
    this.pmNodes.set(name, printer)
  }

  registerPMMark(name: string, printer: MarkPrinter) {
    this.assertFree(this.pmMarks, name, "PM mark")
    this.pmMarks.set(name, printer)
  }

  /* ---- lookup (PM → Markdown) ---- */

  getPMNode(name: string): NodePrinter | undefined {
    return this.pmNodes.get(name)
  }

  getPMMark(name: string): MarkPrinter | undefined {
    return this.pmMarks.get(name)
  }

  /* ---- helpers ---- */

  private assertFree(
    map: Map<string, unknown>,
    key: string,
    kind: string
  ) {
    if (map.has(key)) {
      throw new Error(
        `Duplicate ${kind} registration for "${key}"`
      )
    }
  }
}

export type Plugin = {
  register(reg: Registry): void
}