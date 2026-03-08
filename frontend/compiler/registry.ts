import type { Command, Plugin as PMPlugin } from "prosemirror-state"
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
  open: string | ((mark: Mark) => string)
  close: string | ((mark: Mark) => string)
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

export type SchemaNodeExtender = (spec: any) => any

export type EditorPluginFactory = (schema: Schema) => PMPlugin

export type StyleContribution = {
  id: string
  css: string
}

export type MarkdownItPlugin = (md: any) => void

export type NodePropertyOption = { value: string; label: string }

export type NodePropertySpec = {
  name: string
  label: string
  default?: string | null
  parse?: (raw: string) => string | null
  serialize?: (value: string | null) => string
  options?: NodePropertyOption[] | ((attrs: Record<string, any>) => NodePropertyOption[])
  visible?: (attrs: Record<string, any>) => boolean
  multiline?: boolean
  wide?: boolean
  helpText?: string
  backspaceEmpty?: string | null
}

export type DirectiveSpec = {
  appliesTo: string[]
  nodeType: string
  properties: NodePropertySpec[]
  parseUnknownAttr?: (key: string, value: string) => [attrName: string, parsed: any] | null
}

export type SelfContainedDirectiveSpec = {
  tokenType: string
  nodeType: string
  properties: NodePropertySpec[]
  /** When true, unknown key=value pairs are passed through as raw strings. */
  collectExtra?: boolean
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

  /* Editor plugins */
  private editorPlugins: EditorPluginFactory[] = []

  /* Styles */
  private styles = new Map<string, string>()

  /* Markdown-it plugins */
  private mdPlugins: MarkdownItPlugin[] = []

  /* Directives + node properties */
  private directives = new Map<string, DirectiveSpec>()
  private selfContainedDirectives = new Map<string, SelfContainedDirectiveSpec>()
  private nodeProperties = new Map<string, NodePropertySpec[]>()

  /* Schema contributions */
  private schemaNodes: SchemaNodesSpec = {}
  private schemaMarks: SchemaMarksSpec = {}
  private schemaNodeExtenders = new Map<string, SchemaNodeExtender[]>()

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

  extendSchemaNode(name: string, extender: SchemaNodeExtender) {
    const extenders = this.schemaNodeExtenders.get(name) ?? []
    extenders.push(extender)
    this.schemaNodeExtenders.set(name, extenders)
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

  /* ---- editor plugins ---- */

  registerEditorPlugin(factory: EditorPluginFactory) {
    this.editorPlugins.push(factory)
  }

  getEditorPlugins(): PMPlugin[] {
    return this.editorPlugins.map(factory => factory(this.schema))
  }

  /* ---- styles ---- */

  registerStyle(id: string, css: string) {
    this.assertFree(this.styles as Map<string, unknown>, id, "style")
    this.styles.set(id, css)
  }

  getStyles(): StyleContribution[] {
    return Array.from(this.styles.entries()).map(([id, css]) => ({
      id,
      css,
    }))
  }

  /* ---- Markdown-it plugins ---- */

  registerMarkdownItPlugin(plugin: MarkdownItPlugin) {
    this.mdPlugins.push(plugin)
  }

  getMarkdownItPlugins(): MarkdownItPlugin[] {
    return [...this.mdPlugins]
  }

  /* ---- directives + properties ---- */

  registerDirective(name: string, spec: DirectiveSpec) {
    this.assertFree(this.directives as Map<string, unknown>, name, "directive")
    this.directives.set(name, spec)
    this.registerNodeProperties(spec.nodeType, spec.properties)
  }

  getDirective(name: string): DirectiveSpec | undefined {
    return this.directives.get(name)
  }

  registerSelfContainedDirective(name: string, spec: SelfContainedDirectiveSpec) {
    if (this.directives.has(name)) {
      throw new Error(`Directive name "${name}" is already registered as a prefix directive`)
    }
    this.assertFree(this.selfContainedDirectives as Map<string, unknown>, name, "self-contained directive")
    this.selfContainedDirectives.set(name, spec)
    this.registerNodeProperties(spec.nodeType, spec.properties)
  }

  getSelfContainedDirective(name: string): SelfContainedDirectiveSpec | undefined {
    return this.selfContainedDirectives.get(name)
  }

  getNodeProperties(nodeType: string): NodePropertySpec[] {
    return this.nodeProperties.get(nodeType) ?? []
  }

  registerNodeProperties(
    nodeType: string,
    properties: NodePropertySpec[]
  ) {
    const existing = this.nodeProperties.get(nodeType) ?? []
    for (const prop of properties) {
      if (existing.some(p => p.name === prop.name)) {
        throw new Error(
          `Duplicate property "${prop.name}" for node "${nodeType}"`
        )
      }
      existing.push(prop)
    }
    this.nodeProperties.set(nodeType, existing)
  }

  buildSchema(): Schema {
    const nodes = { ...this.schemaNodes }
    for (const [name, extenders] of this.schemaNodeExtenders.entries()) {
      const base = nodes[name]
      if (!base) {
        throw new Error(`Cannot extend missing schema node "${name}"`)
      }
      let current = base
      for (const extend of extenders) {
        current = extend(current)
      }
      nodes[name] = current
    }

    return new Schema({
      nodes,
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
