// {lifecycle ...} — one directive on an admin page that spawns per-target
// todos when a page in scope satisfies a condition (today: stale). The
// backend does the fanout; the frontend just needs to recognise the
// directive so the editor doesn't render an "Unknown directive" error
// and to give the author a friendly read-only block with a property
// panel to edit the rule's attributes.
import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

// Every attr the backend parser accepts, mirrored one-to-one. `parse`/
// `serialize` are the property-panel adapters (from panel text field
// back to attr, and vice versa). `default` values are what a freshly
// inserted rule shows; keeping `title` and `assign` empty forces the
// author into the panel before saving, since the backend refuses a
// rule without them.
const lifecycleProperties = [
  {
    name: "scope",
    label: "Scope regex",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: 'Path regex like "^/qms/.*". Optional if tags= is set.',
  },
  {
    name: "tags",
    label: "Tags (OR)",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: 'Comma-separated. Page must carry AT LEAST ONE (e.g. "sop,rec,tpl"). Optional if scope= is set.',
  },
  {
    name: "exclude_tags",
    label: "Exclude tags",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: 'Comma-separated. Page must carry NONE of these (e.g. "archived,draft").',
  },
  {
    name: "when",
    label: "Condition",
    default: "stale:30m",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: 'Format: "stale:<duration>". Units d/m/y. E.g. "stale:30m" for 30 months.',
  },
  {
    name: "title",
    label: "Todo title",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: "Supports {{path}}, {{stale_days}}, {{last_attested}}.",
  },
  {
    name: "assign",
    label: "Assign to",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: "User or @group.",
  },
  {
    name: "priority",
    label: "Priority",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: "low / normal / high (default: normal)",
  },
  {
    name: "action",
    label: "Todo action",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: 'WikiAction on the target page, e.g. "edit:." to edit the page itself.',
  },
]

// Order the serializer writes attrs in — matches the property panel
// order so authors can visually diff the source. `when` sits between
// selectors and template because that's how a reader parses it: "on
// pages matching X, when Y, do Z."
const ATTR_ORDER = ["scope", "tags", "exclude_tags", "when", "title", "assign", "priority", "action"]

const styles = `
.gowiki-lifecycle {
  background: #fff8e1;
  border: 1px solid #f0d060;
  border-left: 4px solid #c19a2a;
  border-radius: 4px;
  padding: 10px 14px;
  margin: 0.6em 0;
  font-family: system-ui, -apple-system, sans-serif;
  font-size: 13px;
  line-height: 1.4;
  color: #4a3d10;
}
.gowiki-lifecycle-header {
  font-weight: 600;
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #7a5f10;
  margin-bottom: 6px;
}
.gowiki-lifecycle-body { display: grid; grid-template-columns: max-content 1fr; gap: 2px 12px; }
.gowiki-lifecycle-key { color: #7a5f10; }
.gowiki-lifecycle-val { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; color: #2a2308; }
.gowiki-lifecycle-empty { color: #b09000; font-style: italic; }
`

// NodeView: yellow read-only card that shows the rule at a glance.
// Editing goes through the property panel (opens on click when the
// editor is in edit mode).
class LifecycleNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-lifecycle"
    this.dom.contentEditable = "false"
    this.render()
  }

  private render() {
    this.dom.innerHTML = ""
    const header = document.createElement("div")
    header.className = "gowiki-lifecycle-header"
    header.textContent = "Lifecycle rule"
    this.dom.appendChild(header)

    const body = document.createElement("div")
    body.className = "gowiki-lifecycle-body"

    const rows: [string, string][] = [
      ["scope", this.node.attrs.scope || ""],
      ["tags (OR)", this.node.attrs.tags || ""],
      ["exclude tags", this.node.attrs.exclude_tags || ""],
      ["when", this.node.attrs.when || ""],
      ["→ title", this.node.attrs.title || ""],
      ["→ assign", this.node.attrs.assign || ""],
      ["→ priority", this.node.attrs.priority || ""],
      ["→ action", this.node.attrs.action || ""],
    ]
    for (const [key, val] of rows) {
      if (!val) continue // omit empty rows — keeps the block terse
      const k = document.createElement("div")
      k.className = "gowiki-lifecycle-key"
      k.textContent = key
      const v = document.createElement("div")
      v.className = "gowiki-lifecycle-val"
      v.textContent = val
      body.appendChild(k)
      body.appendChild(v)
    }
    // Special case: brand-new rule (no title yet) — nudge the author.
    if (!this.node.attrs.title || !this.node.attrs.assign) {
      const nudge = document.createElement("div")
      nudge.className = "gowiki-lifecycle-empty"
      nudge.textContent = "Click to open the property panel and set title + assign."
      body.appendChild(nudge)
    }
    this.dom.appendChild(body)
  }

  update(node: PMNode) {
    if (node.type !== this.node.type) return false
    this.node = node
    this.render()
    return true
  }
}

export const lifecyclePlugin: WikiPlugin = {
  register(reg) {
    // Schema node — atomic block (no editable content; the property
    // panel is the only way to change attrs).
    reg.registerSchema({
      nodes: {
        lifecycle: {
          group: "block",
          atom: true,
          attrs: {
            scope: { default: "" },
            tags: { default: "" },
            exclude_tags: { default: "" },
            when: { default: "" },
            title: { default: "" },
            assign: { default: "" },
            priority: { default: "" },
            action: { default: "" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-lifecycle",
                "data-lifecycle-scope": node.attrs.scope || "",
                "data-lifecycle-when": node.attrs.when || "",
              },
              `Lifecycle rule (${node.attrs.when || "no condition"})`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-lifecycle",
              getAttrs(dom: HTMLElement) {
                return {
                  scope: dom.getAttribute("data-lifecycle-scope") || "",
                  when: dom.getAttribute("data-lifecycle-when") || "",
                }
              },
            },
          ],
        },
      },
    })

    // Self-contained directive: {lifecycle scope=... when=... title=... assign=...}
    reg.registerSelfContainedDirective("lifecycle", {
      tokenType: "lifecycle",
      nodeType: "lifecycle",
      properties: lifecycleProperties,
    })

    // Markdown → PM: build the node from the parsed directive attrs.
    reg.registerText("lifecycle", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.lifecycle.create({
            scope: attrs.scope ?? "",
            tags: attrs.tags ?? "",
            exclude_tags: attrs.exclude_tags ?? "",
            when: attrs.when ?? "",
            title: attrs.title ?? "",
            assign: attrs.assign ?? "",
            priority: attrs.priority ?? "",
            // Backend accepts either `action` or `do`; property panel
            // uses `action` so we normalise here.
            action: attrs.action ?? attrs.do ?? "",
          })
        )
      },
    })

    // PM → Markdown: write attrs in a stable order, omit empties so
    // the source stays terse and doesn't collect noise.
    reg.registerPMNode("lifecycle", {
      print(node) {
        const parts: string[] = []
        for (const key of ATTR_ORDER) {
          const v = String(node.attrs[key] ?? "").trim()
          if (!v) continue
          // Quote values that contain a space or a `"` so the backend
          // parser reads them as one attribute. Matches the reviewflow
          // convention.
          const needsQuote = /[\s"]/.test(v)
          const val = needsQuote ? `"${v.replace(/"/g, '\\"')}"` : v
          parts.push(`${key}=${val}`)
        }
        if (parts.length === 0) return "{lifecycle}\n\n"
        return `{lifecycle ${parts.join(" ")}}\n\n`
      },
    })

    // Editor plugin: mount the NodeView.
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.lifecycle"),
        props: {
          nodeViews: {
            lifecycle(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new LifecycleNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Command: insert an empty rule and open the property panel so the
    // author can fill it in without touching the raw markdown.
    reg.registerCommand("lifecycle", "insert", (state, dispatch) => {
      const lcType = reg.schema.nodes.lifecycle
      if (!lcType) return false
      if (dispatch) {
        // Seed with a sensible default `when=stale:30m` so a
        // freshly-inserted rule is a working shape minus title/assign,
        // matching the property panel's `default:` for `when`.
        const node = lcType.create({
          scope: "",
          tags: "",
          exclude_tags: "",
          when: "stale:30m",
          title: "",
          assign: "",
          priority: "",
          action: "",
        })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(Math.max(0, approxPos - 5), Math.min(tr.doc.content.size, approxPos + 5), (n, pos) => {
          if (n.type === lcType && insertedAt === null) {
            insertedAt = pos
            return false
          }
        })
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch {
            // Leave default selection on failure.
          }
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    reg.registerStyle("lifecycle", styles)
  },
}
