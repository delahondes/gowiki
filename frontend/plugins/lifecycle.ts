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

// Rendering follows specs/directive-general-rendering.md: a discreet
// grey box (never yellow — yellow is reserved for the property
// panel). The box is reader-facing; the raw attributes are behind
// the panel, not on the page.
const styles = `
.gowiki-lifecycle {
  background: var(--gw-color-surface, #f3f4f6);
  border: 1px solid var(--gw-color-border, #d1d5db);
  border-left: 3px solid var(--gw-color-border-strong, #9ca3af);
  border-radius: 3px;
  padding: 8px 12px;
  margin: 0.6em 0;
  font-family: system-ui, -apple-system, sans-serif;
  font-size: 13px;
  line-height: 1.4;
  color: var(--gw-color-text, #1f2937);
}
.gowiki-lifecycle-header {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--gw-color-muted, #6b7280);
  margin-bottom: 2px;
}
.gowiki-lifecycle-status {
  color: var(--gw-color-muted, #6b7280);
  font-style: italic;
}
.gowiki-lifecycle--needs-setup .gowiki-lifecycle-status {
  color: var(--gw-color-danger, #b91c1c);
  font-style: normal;
}
`

// describeCondition turns a raw `when=` value (e.g. "stale:30m") into
// a reader-side phrase ("no attestation for 30 months"). Falls back
// to the raw value when parsing fails — better a visible oddity than
// a confidently-wrong description.
function describeCondition(raw: string): string {
  const parts = raw.split(":")
  if (parts.length !== 2 || parts[0] !== "stale") return raw || "the condition is met"
  const dur = parts[1]
  const unit = dur.slice(-1).toLowerCase()
  const n = parseInt(dur.slice(0, -1), 10)
  if (!Number.isFinite(n)) return `no attestation for ${dur}`
  const unitName = unit === "d" ? "day" : unit === "m" ? "month" : unit === "y" ? "year" : unit
  const plural = n === 1 ? "" : "s"
  return `no attestation for ${n} ${unitName}${plural}`
}

// describeScope turns the scope selectors into a compact reader phrase
// suitable for the status line ("pages under /qms/", "pages tagged
// sop or rec", etc.). Combines path + tag selectors with "and".
function describeScope(attrs: Record<string, any>): string {
  const parts: string[] = []
  const scope = String(attrs.scope || "").trim()
  if (scope) {
    // Best-effort: strip a leading `^` and trailing `.*` / `$` so the
    // path reads naturally in the status line. If the regex is more
    // complex we fall back to showing it verbatim.
    const stripped = scope.replace(/^\^/, "").replace(/(\.\*|\.\+)?\$?$/, "")
    parts.push(`pages under ${stripped}`)
  }
  const tags = String(attrs.tags || "")
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean)
  if (tags.length > 0) {
    parts.push(`pages tagged ${tags.join(" or ")}`)
  }
  const exclude = String(attrs.exclude_tags || "")
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean)
  if (exclude.length > 0) {
    parts.push(`excluding ${exclude.join(" and ")}`)
  }
  return parts.length > 0 ? parts.join(", ") : "every page in the wiki"
}

// NodeView: a discreet grey card per the general directive-rendering
// spec. Renders a short reader-facing status line — NOT the raw
// attributes, which live behind the property panel. When the rule is
// missing its required fields (title/assign), the status shifts to
// the "needs attention" variant so the author notices there's work to
// do without having to open the panel.
class LifecycleNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.contentEditable = "false"
    this.render()
  }

  private render() {
    this.dom.innerHTML = ""
    const needsSetup = !this.node.attrs.title || !this.node.attrs.assign
    this.dom.className = "gowiki-lifecycle" + (needsSetup ? " gowiki-lifecycle--needs-setup" : "")

    const header = document.createElement("div")
    header.className = "gowiki-lifecycle-header"
    header.textContent = "Lifecycle"
    this.dom.appendChild(header)

    const status = document.createElement("div")
    status.className = "gowiki-lifecycle-status"
    status.textContent = this.summarise()
    this.dom.appendChild(status)
  }

  // summarise returns the single reader-facing status line. It's a
  // passive description of the rule's INTENT — not a live count of
  // fires (which would need a backend fetch). We opt for grey/passive
  // most of the time per the spec's "prefer grey over red" guidance.
  private summarise(): string {
    const when = String(this.node.attrs.when || "").trim()
    if (!this.node.attrs.title || !this.node.attrs.assign) {
      return "Rule not yet configured — set title and assign in the property panel."
    }
    // Turn "stale:30m" into "no attestation for 30 months" reader-side.
    const cond = describeCondition(when)
    const scopePhrase = describeScope(this.node.attrs)
    return `Watching ${scopePhrase} — a review todo is created if ${cond}.`
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
