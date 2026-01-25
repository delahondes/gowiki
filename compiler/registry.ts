import type { Command } from "prosemirror-state"
import { Schema, Node as PMNode, Mark } from "prosemirror-model"
import type { CompileContext } from "./kernel"
import type { PrintContext } from "./pm_to_markdown"
import { listen } from "node:quic"

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

export type CommandListener = (
  namespace: string,
  name: string,
  cmd: Command
) => void

export type SchemaNodesSpec = { [name: string]: any }
export type SchemaMarksSpec = { [name: string]: any }

export type SchemaContributor = {
  nodes?: SchemaNodesSpec
  marks?: SchemaMarksSpec
}

export class Registry {
  /* Markdown → PM */
  private mdNodes = new Map<string, NodeHandler>()
  private mdMarks = new Map<string, MarkHandler>()
  private mdText = new Map<string, TextHandler>()

  /* PM → Markdown */
  private pmNodes = new Map<string, NodePrinter>()
  private pmMarks = new Map<string, MarkPrinter>()

  /* Menu commands */
  private commands = new Map<string, Command>() // key is "namespace.name"
  private commandListeners: CommandListener[] = []

  /* Schema contributions */
  private schemaNodes: SchemaNodesSpec = {}
  private schemaMarks: SchemaMarksSpec = {}

  constructor(public readonly schema: Schema) {}
  registerSchema(contrib: SchemaContributor) {
    if (contrib.nodes) {
      for (const k of Object.keys(contrib.nodes)) {
        this.assertFree(
          new Map(Object.keys(this.schemaNodes).map(k => [k, true])),
          k,
          "schema node"
        )
        this.schemaNodes[k] = contrib.nodes[k]
      }
    }

    if (contrib.marks) {
      for (const k of Object.keys(contrib.marks)) {
        this.assertFree(
          new Map(Object.keys(this.schemaMarks).map(k => [k, true])),
          k,
          "schema mark"
        )
        this.schemaMarks[k] = contrib.marks[k]
      }
    }
  }

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

  /* ---- menu commands ---- */

  onCommand(listener: CommandListener) {
    this.commandListeners.push(listener)

    // Replay already-registered commands for late subscribers.
    for (const [fullName, cmd] of this.commands.entries()) {
      const dot = fullName.indexOf(".")
      if (dot < 0) continue
      const namespace = fullName.slice(0, dot)
      const name = fullName.slice(dot + 1)
      console.log("Replaying command for listener", fullName, listener)
      listener(namespace, name, cmd)
    }
  }

  registerCommand(
    namespace: string,
    name: string,
    cmd: Command
  ) {
    const fullName = `${namespace}.${name}`
    this.assertFree(this.commands, fullName, "command")
    this.commands.set(fullName, cmd)

    for (const l of this.commandListeners) {
      console.log("Registering command to listener", fullName, l)
      l(namespace, name, cmd)
    }
  }

  buildSchema(): Schema {
    return new Schema({
      nodes: this.schemaNodes,
      marks: this.schemaMarks,
    })
  }

  bindSchema(schema: Schema) {
    // Replace the temporary schema reference with the final one.
    ;(this as any).schema = schema
  }
}

export type Plugin = {
  register(reg: Registry): void
}