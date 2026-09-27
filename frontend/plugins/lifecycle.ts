// {lifecycle ...} — one directive on an admin document that spawns
// per-target todos when another document in scope satisfies a
// condition (today: stale). The backend does the fanout; the frontend
// renders a discreet reader-facing card and fetches live status so
// the reader sees "in order" vs "N documents need review" rather than
// a technical description of the rule's mechanics.
import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

// currentSourcePath — the canonical path of the document whose editor
// we're currently mounted in. Matches how other plugins (reviewflow,
// todo, tag) derive their page path: straight off window.location.
function currentSourcePath(): string {
  const p = window.location.pathname || "/"
  return p
}

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
    helpText: 'Comma-separated. Document must carry AT LEAST ONE (e.g. "sop,rec,tpl"). Optional if scope= is set.',
  },
  {
    name: "exclude_tags",
    label: "Exclude tags",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: 'Comma-separated. Document must carry NONE of these (e.g. "archived,draft").',
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
    helpText: 'WikiAction on the target document, e.g. "edit:." to edit it.',
  },
]

// Serializer attr order — matches the property panel visual order so the
// source stays a mirror of what the author sees while editing.
const ATTR_ORDER = ["scope", "tags", "exclude_tags", "when", "title", "assign", "priority", "action"]

// Rendering follows specs/directive-general-rendering.md: a discreet
// grey card by default, red only when something actually needs
// attention. The status line is human — a sentence a reader can
// understand — not a translation of the raw attributes. Technical
// detail is available on hover.
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
  line-height: 1.5;
  color: var(--gw-color-text, #1f2937);
  display: flex;
  align-items: baseline;
  gap: 12px;
}
.gowiki-lifecycle-label {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--gw-color-muted, #6b7280);
  flex: 0 0 auto;
}
.gowiki-lifecycle-status {
  flex: 1 1 auto;
  color: var(--gw-color-muted, #6b7280);
  font-style: italic;
}
.gowiki-lifecycle-details {
  flex: 0 0 auto;
  font-size: 11px;
  color: var(--gw-color-muted, #6b7280);
  border-bottom: 1px dotted currentColor;
  cursor: help;
  font-style: normal;
}
.gowiki-lifecycle--alert {
  background: #fef2f2;
  border-color: #fecaca;
  border-left-color: #dc2626;
}
.gowiki-lifecycle--alert .gowiki-lifecycle-status {
  color: #b91c1c;
  font-style: normal;
  font-weight: 500;
}
.gowiki-lifecycle--needs-setup {
  background: #fef2f2;
  border-color: #fecaca;
  border-left-color: #dc2626;
}
.gowiki-lifecycle--needs-setup .gowiki-lifecycle-status {
  color: #b91c1c;
  font-style: normal;
}
`

type FiresState =
  | { kind: "loading" }
  | { kind: "unknown" }
  | { kind: "ok"; count: 0 }
  | { kind: "alert"; count: number; sample: string[] }

// Cache per source-document path so multiple lifecycle NodeViews on the
// same page share one fetch. Keyed by source document; value is the
// promise (loading) or resolved payload.
const statusCache = new Map<string, Promise<StatusResponse | null>>()

interface RuleView {
  scope: string
  tags: string[]
  exclude_tags?: string[]
  when: string
  title: string
  assign: string
  priority?: string
  action?: string
}
interface RuleStatusEntry {
  rule: RuleView
  fires_count: number
  sample_pages?: string[]
  source_page: string
}
interface StatusResponse {
  rules: RuleStatusEntry[]
}

// fetchStatus loads (or reuses the cached fetch of) the lifecycle
// status for the source document. Returns null on any failure so the
// caller falls back to a passive "status unavailable" render.
function fetchStatus(sourcePath: string): Promise<StatusResponse | null> {
  const existing = statusCache.get(sourcePath)
  if (existing) return existing
  const p = fetch(`/api/plugin/lifecycle/v1/status?source_page=${encodeURIComponent(sourcePath)}`)
    .then((r) => (r.ok ? (r.json() as Promise<StatusResponse>) : null))
    .catch(() => null)
  statusCache.set(sourcePath, p)
  // Auto-expire after 30 s so a re-render after an edit picks up the
  // latest scan. The cache is a "same NodeView-mount burst" dedup, not
  // a long-lived cache.
  setTimeout(() => statusCache.delete(sourcePath), 30_000)
  return p
}

// matchRule finds the status entry that corresponds to this NodeView's
// attrs. Matches on (title, assign) — the natural human-readable
// identifier for a rule on a given source document. Falls back to
// tolerating extra whitespace so a serialise/parse mismatch doesn't
// hide the status.
function matchRule(status: StatusResponse, attrs: Record<string, unknown>): RuleStatusEntry | null {
  const wantTitle = String(attrs.title || "").trim()
  const wantAssign = String(attrs.assign || "").trim()
  if (!wantTitle || !wantAssign) return null
  for (const s of status.rules) {
    if (s.rule.title.trim() === wantTitle && s.rule.assign.trim() === wantAssign) {
      return s
    }
  }
  return null
}

// summariseWhen turns a raw "stale:30m" back into "no attestation for
// 30 months" for the hover tooltip. Kept minimal because the reader
// never sees this unless they hover the details marker.
function summariseWhen(raw: string): string {
  const parts = raw.split(":")
  if (parts.length !== 2 || parts[0] !== "stale") return raw
  const dur = parts[1]
  const unit = dur.slice(-1).toLowerCase()
  const n = parseInt(dur.slice(0, -1), 10)
  if (!Number.isFinite(n)) return `no attestation for ${dur}`
  const unitName = unit === "d" ? "day" : unit === "m" ? "month" : unit === "y" ? "year" : unit
  const plural = n === 1 ? "" : "s"
  return `no attestation for ${n} ${unitName}${plural}`
}

// buildDetailsTooltip is the technical hover text on the small
// `[details]` marker. Never in the main status line — reserved for the
// author or admin who wants to double-check what the rule actually
// covers.
function buildDetailsTooltip(attrs: Record<string, unknown>): string {
  const bits: string[] = []
  const scope = String(attrs.scope || "").trim()
  if (scope) bits.push(`scope: ${scope}`)
  const tags = String(attrs.tags || "").trim()
  if (tags) bits.push(`tags: ${tags}`)
  const excludeTags = String(attrs.exclude_tags || "").trim()
  if (excludeTags) bits.push(`exclude_tags: ${excludeTags}`)
  const when = String(attrs.when || "").trim()
  if (when) bits.push(`condition: ${summariseWhen(when)}`)
  const assign = String(attrs.assign || "").trim()
  if (assign) bits.push(`assign: ${assign}`)
  return bits.join("\n")
}

// LifecycleNodeView — discreet grey card with a human status line and
// a small `[details]` marker for the technical hover. Fetches live
// backend status on mount; renders passive by default and shifts to a
// loud red state only when the rule actually fires on documents or is
// missing required setup.
class LifecycleNodeView {
  dom: HTMLElement
  private node: PMNode
  private fires: FiresState = { kind: "loading" }

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.contentEditable = "false"
    this.render()
    this.loadStatus()
  }

  private loadStatus() {
    const sourcePath = currentSourcePath()
    if (!sourcePath) {
      this.fires = { kind: "unknown" }
      this.render()
      return
    }
    fetchStatus(sourcePath).then((status) => {
      if (!status) {
        this.fires = { kind: "unknown" }
      } else {
        const entry = matchRule(status, this.node.attrs)
        if (!entry) {
          this.fires = { kind: "unknown" }
        } else if (entry.fires_count === 0) {
          this.fires = { kind: "ok", count: 0 }
        } else {
          this.fires = { kind: "alert", count: entry.fires_count, sample: entry.sample_pages || [] }
        }
      }
      this.render()
    })
  }

  private render() {
    this.dom.innerHTML = ""
    const needsSetup = !this.node.attrs.title || !this.node.attrs.assign
    const isAlert = !needsSetup && this.fires.kind === "alert"

    let className = "gowiki-lifecycle"
    if (needsSetup) className += " gowiki-lifecycle--needs-setup"
    else if (isAlert) className += " gowiki-lifecycle--alert"
    this.dom.className = className

    const label = document.createElement("span")
    label.className = "gowiki-lifecycle-label"
    label.textContent = "Lifecycle"
    this.dom.appendChild(label)

    const status = document.createElement("span")
    status.className = "gowiki-lifecycle-status"
    status.textContent = this.summarise()
    this.dom.appendChild(status)

    if (!needsSetup) {
      const details = document.createElement("span")
      details.className = "gowiki-lifecycle-details"
      details.textContent = "details"
      details.title = buildDetailsTooltip(this.node.attrs)
      this.dom.appendChild(details)
    }
  }

  // summarise returns the single reader-facing status line. Human
  // sentence, no directive attributes echoed literally. The mechanics
  // sit behind the `[details]` hover — the reader only wants to know
  // whether the rule is doing its job.
  private summarise(): string {
    if (!this.node.attrs.title || !this.node.attrs.assign) {
      return "Rule not yet configured — set title and assign in the property panel."
    }
    switch (this.fires.kind) {
      case "loading":
        return "Checking…"
      case "unknown":
        return "Rule active."
      case "ok":
        return "All documents in scope are within the rule."
      case "alert": {
        const n = this.fires.count
        return n === 1 ? "1 document needs review." : `${n} documents need review.`
      }
    }
  }

  update(node: PMNode) {
    if (node.type !== this.node.type) return false
    const prevKey = `${this.node.attrs.title}|${this.node.attrs.assign}`
    this.node = node
    const nextKey = `${node.attrs.title}|${node.attrs.assign}`
    this.render()
    // If the identifying attrs changed (title or assign edited), the
    // cached status entry no longer matches — re-fetch so the new
    // identity resolves against the backend's current state.
    if (prevKey !== nextKey) {
      this.fires = { kind: "loading" }
      this.render()
      this.loadStatus()
    }
    return true
  }
}

export const lifecyclePlugin: WikiPlugin = {
  register(reg) {
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

    reg.registerSelfContainedDirective("lifecycle", {
      tokenType: "lifecycle",
      nodeType: "lifecycle",
      properties: lifecycleProperties,
    })

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
            action: attrs.action ?? attrs.do ?? "",
          })
        )
      },
    })

    reg.registerPMNode("lifecycle", {
      print(node) {
        const parts: string[] = []
        for (const key of ATTR_ORDER) {
          const v = String(node.attrs[key] ?? "").trim()
          if (!v) continue
          const needsQuote = /[\s"]/.test(v)
          const val = needsQuote ? `"${v.replace(/"/g, '\\"')}"` : v
          parts.push(`${key}=${val}`)
        }
        if (parts.length === 0) return "{lifecycle}\n\n"
        return `{lifecycle ${parts.join(" ")}}\n\n`
      },
    })

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

    reg.registerCommand("lifecycle", "insert", (state, dispatch) => {
      const lcType = reg.schema.nodes.lifecycle
      if (!lcType) return false
      if (dispatch) {
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
