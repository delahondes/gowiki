// {lifecycle ...} — one directive on an admin document that spawns
// per-target todos (for `stale` rules) or surfaces a reviewflow
// overdue count (for `reviewflow_overdue`, alert-only). Multiple
// lifecycle directives on the same document render as ONE aggregate
// panel: the reader wants a single "is this document's lifecycle in
// order?" summary, not a card stack.
import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

// currentSourcePath — canonical path of the document whose editor
// we're mounted in. Matches the pattern used by reviewflow/todo/tag
// (window.location.pathname).
function currentSourcePath(): string {
  return window.location.pathname || "/"
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
    helpText: 'Format: "stale:<duration>" or "reviewflow_overdue" (alert-only, no todo).',
  },
  {
    name: "title",
    label: "Todo title",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: "Supports {{path}}, {{stale_days}}, {{last_attested}}. Optional for reviewflow_overdue (alert-only).",
  },
  {
    name: "assign",
    label: "Assign to",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: "User or @group. The alert / todo is addressed to this recipient.",
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

const ATTR_ORDER = ["scope", "tags", "exclude_tags", "when", "title", "assign", "priority", "action"]

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
}
.gowiki-lifecycle-headrow {
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
.gowiki-lifecycle-metasummary {
  flex: 0 0 auto;
  font-size: 11px;
  color: var(--gw-color-muted, #6b7280);
}
.gowiki-lifecycle-rules {
  margin: 6px 0 0 0;
  padding: 0 0 0 4px;
  list-style: none;
}
.gowiki-lifecycle-rules li {
  padding: 2px 0;
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.gowiki-lifecycle-rule-kind {
  flex: 0 0 auto;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--gw-color-muted, #6b7280);
  min-width: 130px;
}
.gowiki-lifecycle-rule-body {
  flex: 1 1 auto;
  color: var(--gw-color-text, #1f2937);
}
.gowiki-lifecycle-rule-body--ok {
  color: var(--gw-color-muted, #6b7280);
  font-style: italic;
}
.gowiki-lifecycle-rule-body--alert {
  color: #b91c1c;
  font-weight: 500;
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
.gowiki-lifecycle-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 8px;
  margin: 0.3em 0;
  background: var(--gw-color-surface, #f3f4f6);
  border: 1px solid var(--gw-color-border, #d1d5db);
  border-radius: 12px;
  font-family: system-ui, -apple-system, sans-serif;
  font-size: 11px;
  color: var(--gw-color-muted, #6b7280);
  cursor: pointer;
  user-select: none;
}
.gowiki-lifecycle-chip::before {
  content: "▪";
  color: var(--gw-color-border-strong, #9ca3af);
}
.gowiki-lifecycle-secondary-hidden {
  display: none;
}
.gowiki-lifecycle-drilldown-wrap {
  list-style: none;
  padding: 0 0 0 138px;
  margin: 0;
}
.gowiki-lifecycle-drilldown {
  margin: 0;
}
.gowiki-lifecycle-drilldown > summary {
  cursor: pointer;
  color: var(--gw-color-muted, #6b7280);
  font-size: 11px;
  padding: 2px 0;
  user-select: none;
}
.gowiki-lifecycle-drilldown > summary:hover {
  color: var(--gw-color-text, #1f2937);
}
.gowiki-lifecycle-drilldown-list {
  list-style: none;
  padding: 4px 0 4px 12px;
  margin: 0;
  max-height: 240px;
  overflow-y: auto;
  border-left: 2px solid var(--gw-color-border, #d1d5db);
}
.gowiki-lifecycle-drilldown-list li {
  padding: 1px 0;
  font-size: 12px;
}
.gowiki-lifecycle-drilldown-list a {
  color: var(--gw-color-text, #1f2937);
  text-decoration: none;
}
.gowiki-lifecycle-drilldown-list a:hover {
  text-decoration: underline;
}
`

type FiresState =
  | { kind: "loading" }
  | { kind: "unknown" }
  | { kind: "ok"; count: 0 }
  | { kind: "alert"; count: number; sample: string[] }

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
  kind: string
  alert_only?: boolean
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

function fetchStatus(sourcePath: string): Promise<StatusResponse | null> {
  const existing = statusCache.get(sourcePath)
  if (existing) return existing
  const p = fetch(`/api/plugin/lifecycle/v1/status?source_page=${encodeURIComponent(sourcePath)}`)
    .then((r) => (r.ok ? (r.json() as Promise<StatusResponse>) : null))
    .catch(() => null)
  statusCache.set(sourcePath, p)
  setTimeout(() => statusCache.delete(sourcePath), 30_000)
  return p
}

// summariseWhen turns raw when= text into reader-friendly hover text.
// Handles both `stale:<dur>` and bare kinds like `reviewflow_overdue`.
function summariseWhen(raw: string): string {
  if (raw === "reviewflow_overdue") return "reviewflow overdue on any role"
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

// kindLabel turns the parsed condition kind into the short label the
// bullet list uses when a rule fires. Reader-facing wording — no
// technical suffix.
function kindLabel(kind: string): string {
  switch (kind) {
    case "stale":
      return "staleness"
    case "reviewflow_overdue":
      return "reviewflow overdue"
    default:
      return kind
  }
}

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
  const title = String(attrs.title || "").trim()
  if (title) bits.push(`title: ${title}`)
  return bits.join("\n")
}

// collectLifecycleNodes walks the doc and returns every lifecycle node
// with its position, in doc order. The first entry is the "primary"
// — the one that renders the aggregate panel; the rest render as
// chips.
function collectLifecycleNodes(view: EditorView): Array<{ pos: number; node: PMNode }> {
  const out: Array<{ pos: number; node: PMNode }> = []
  const doc = view.state.doc
  doc.descendants((n, pos) => {
    if (n.type.name === "lifecycle") {
      out.push({ pos, node: n })
    }
  })
  return out
}

// matchEntry pairs one PM lifecycle node with its status entry from
// the backend response, keyed on (title, assign). Alert-only rules
// omit those so they match on (when, scope, tags) instead. Returns
// null when no match is found — the panel then renders that rule in
// the "unknown" state.
function matchEntry(entries: RuleStatusEntry[], node: PMNode): RuleStatusEntry | null {
  const attrs = node.attrs
  const isAlertOnly = String(attrs.when || "").trim() === "reviewflow_overdue"
  if (isAlertOnly) {
    for (const e of entries) {
      if (e.rule.when === "reviewflow_overdue" && e.rule.scope === String(attrs.scope || "").trim()) {
        return e
      }
    }
    return null
  }
  const wantTitle = String(attrs.title || "").trim()
  const wantAssign = String(attrs.assign || "").trim()
  if (!wantTitle || !wantAssign) return null
  for (const e of entries) {
    if (e.rule.title.trim() === wantTitle && e.rule.assign.trim() === wantAssign) {
      return e
    }
  }
  return null
}

function needsSetup(node: PMNode): boolean {
  const isAlertOnly = String(node.attrs.when || "").trim() === "reviewflow_overdue"
  if (isAlertOnly) return !String(node.attrs.scope || "").trim() && !String(node.attrs.tags || "").trim()
  return !node.attrs.title || !node.attrs.assign
}

// LifecycleNodeView — one PM NodeView per {lifecycle} node. The
// primary (first in doc order) renders the aggregate panel across
// ALL lifecycle nodes on the doc; secondary nodes render a compact
// chip so authors can still click into them for property editing.
class LifecycleNodeView {
  dom: HTMLElement
  private node: PMNode
  private view: EditorView
  private getPos: () => number | undefined
  private fires: FiresState = { kind: "loading" }
  private status: StatusResponse | null = null

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.getPos = getPos
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
      this.status = status
      if (!status) {
        this.fires = { kind: "unknown" }
      } else {
        const entry = matchEntry(status.rules, this.node)
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

  private isPrimary(): boolean {
    const all = collectLifecycleNodes(this.view)
    if (all.length === 0) return true
    const myPos = this.getPos()
    return myPos === all[0].pos
  }

  private render() {
    this.dom.innerHTML = ""
    if (this.isPrimary()) {
      this.renderPanel()
      return
    }
    // Secondary node. In VIEW mode the reader already sees every rule
    // aggregated on the primary — a chip here would just say "yes,
    // another rule exists" which is noise. In EDIT mode the chip is
    // the only way for the author to click into this specific rule's
    // property panel, so it stays.
    if (this.view.editable) {
      this.renderChip()
    } else {
      // Present but invisible — atom NodeViews still need a DOM node
      // so PM's positional bookkeeping works, but 0×0 keeps the
      // reader's view uncluttered.
      this.dom.className = "gowiki-lifecycle-secondary-hidden"
    }
  }

  // renderChip — the SECONDARY rendering in EDIT mode. Compact
  // one-line marker so the author knows the rule exists here and can
  // click to edit it, without duplicating the aggregate panel.
  private renderChip() {
    this.dom.className = "gowiki-lifecycle-chip-wrapper"
    const chip = document.createElement("span")
    chip.className = "gowiki-lifecycle-chip"
    const title = String(this.node.attrs.title || "").trim()
    const label = title || summariseWhen(String(this.node.attrs.when || "").trim() || "lifecycle rule")
    chip.textContent = `Lifecycle · ${label}`
    chip.title = buildDetailsTooltip(this.node.attrs)
    chip.addEventListener("click", () => {
      const pos = this.getPos()
      if (pos === undefined) return
      try {
        const tr = this.view.state.tr.setSelection(NodeSelection.create(this.view.state.doc, pos))
        this.view.dispatch(enablePropertiesPanel(tr))
        this.view.focus()
      } catch {
        // Position may have shifted; ignore.
      }
    })
    this.dom.appendChild(chip)
  }

  // renderPanel — the PRIMARY rendering. Aggregates every lifecycle
  // node on the doc into one card and reports the worst-case state.
  private renderPanel() {
    const all = collectLifecycleNodes(this.view)
    // Build per-rule states with matched status entries.
    const perRule = all.map(({ node }) => {
      const setup = needsSetup(node)
      const entry = this.status ? matchEntry(this.status.rules, node) : null
      let state: FiresState
      if (setup) {
        state = { kind: "unknown" }
      } else if (!this.status) {
        state = { kind: "loading" }
      } else if (!entry) {
        state = { kind: "unknown" }
      } else if (entry.fires_count === 0) {
        state = { kind: "ok", count: 0 }
      } else {
        state = { kind: "alert", count: entry.fires_count, sample: entry.sample_pages || [] }
      }
      return { node, setup, state, entry }
    })

    const anyAlert = perRule.some((r) => r.state.kind === "alert")
    const anySetup = perRule.some((r) => r.setup)

    let className = "gowiki-lifecycle"
    if (anySetup || anyAlert) className += " gowiki-lifecycle--alert"
    this.dom.className = className

    const headrow = document.createElement("div")
    headrow.className = "gowiki-lifecycle-headrow"
    this.dom.appendChild(headrow)

    const label = document.createElement("span")
    label.className = "gowiki-lifecycle-label"
    label.textContent = "Lifecycle"
    headrow.appendChild(label)

    const status = document.createElement("span")
    status.className = "gowiki-lifecycle-status"
    status.textContent = this.summariseAggregate(perRule, anyAlert, anySetup)
    headrow.appendChild(status)

    const metaSummary = document.createElement("span")
    metaSummary.className = "gowiki-lifecycle-metasummary"
    metaSummary.textContent = perRule.length === 1 ? "1 rule" : `${perRule.length} rules`
    headrow.appendChild(metaSummary)

    // Per-rule bullets only when at least one rule needs attention.
    // Green case stays as ONE calm line — the panel gets specific only
    // when specificity matters.
    if (anyAlert || anySetup) {
      const ul = document.createElement("ul")
      ul.className = "gowiki-lifecycle-rules"
      this.dom.appendChild(ul)
      for (const r of perRule) {
        const li = document.createElement("li")
        li.className = "gowiki-lifecycle-rule"
        const kindSpan = document.createElement("span")
        kindSpan.className = "gowiki-lifecycle-rule-kind"
        kindSpan.textContent = kindLabel(String(r.node.attrs.when || "").split(":")[0] || "?")
        li.appendChild(kindSpan)

        const body = document.createElement("span")
        body.className = "gowiki-lifecycle-rule-body"
        if (r.setup) {
          body.classList.add("gowiki-lifecycle-rule-body--alert")
          body.textContent = "rule not yet configured"
        } else {
          switch (r.state.kind) {
            case "loading":
              body.classList.add("gowiki-lifecycle-rule-body--ok")
              body.textContent = "checking…"
              break
            case "unknown":
              body.classList.add("gowiki-lifecycle-rule-body--ok")
              body.textContent = "status unavailable"
              break
            case "ok":
              body.classList.add("gowiki-lifecycle-rule-body--ok")
              body.textContent = "all in order"
              break
            case "alert": {
              body.classList.add("gowiki-lifecycle-rule-body--alert")
              const n = r.state.count
              body.textContent = n === 1 ? "1 document" : `${n} documents`
              break
            }
          }
        }
        li.appendChild(body)

        const details = document.createElement("span")
        details.className = "gowiki-lifecycle-details"
        details.textContent = "details"
        details.title = buildDetailsTooltip(r.node.attrs)
        li.appendChild(details)
        ul.appendChild(li)

        // Drill-down: when the rule fires on N documents, add a
        // collapsible list of the flagged paths as clickable links so
        // the reader can go straight to them. Uses <details>/<summary>
        // — native disclosure, no extra JS, keyboard-accessible.
        if (r.state.kind === "alert" && r.state.sample && r.state.sample.length > 0) {
          const drill = document.createElement("details")
          drill.className = "gowiki-lifecycle-drilldown"
          const summary = document.createElement("summary")
          const shown = r.state.sample.length
          const total = r.state.count
          summary.textContent =
            shown < total ? `show ${shown} of ${total} documents` : `show ${total === 1 ? "document" : "documents"}`
          drill.appendChild(summary)
          const listUl = document.createElement("ul")
          listUl.className = "gowiki-lifecycle-drilldown-list"
          for (const p of r.state.sample) {
            const listLi = document.createElement("li")
            const a = document.createElement("a")
            a.href = p
            a.textContent = p
            listLi.appendChild(a)
            listUl.appendChild(listLi)
          }
          drill.appendChild(listUl)
          const drillWrap = document.createElement("li")
          drillWrap.className = "gowiki-lifecycle-drilldown-wrap"
          drillWrap.appendChild(drill)
          ul.appendChild(drillWrap)
        }
      }
    }
  }

  // summariseAggregate returns the one-line header status for the
  // aggregate panel. Calm when everything is fine; specific about the
  // total when at least one rule fires.
  private summariseAggregate(
    perRule: Array<{ setup: boolean; state: FiresState }>,
    anyAlert: boolean,
    anySetup: boolean
  ): string {
    if (anySetup) {
      const n = perRule.filter((r) => r.setup).length
      return n === 1 ? "1 rule not yet configured." : `${n} rules not yet configured.`
    }
    if (anyAlert) {
      let total = 0
      for (const r of perRule) {
        if (r.state.kind === "alert") total += r.state.count
      }
      return total === 1 ? "1 document needs review." : `${total} documents need review.`
    }
    const anyLoading = perRule.some((r) => r.state.kind === "loading")
    if (anyLoading) return "Checking…"
    return "All documents in scope are within all rules."
  }

  update(node: PMNode) {
    if (node.type !== this.node.type) return false
    const prevKey = `${this.node.attrs.title}|${this.node.attrs.assign}|${this.node.attrs.when}|${this.node.attrs.scope}`
    this.node = node
    const nextKey = `${node.attrs.title}|${node.attrs.assign}|${node.attrs.when}|${node.attrs.scope}`
    this.render()
    if (prevKey !== nextKey) {
      this.fires = { kind: "loading" }
      this.status = null
      statusCache.delete(currentSourcePath())
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

    // When the doc changes (rules added / removed / reordered), the
    // primary lifecycle node may switch. Force a re-render of every
    // lifecycle NodeView by dispatching a no-op transaction — actually
    // PM's NodeView.update fires naturally for each affected node when
    // its position changes, and we recompute isPrimary() on every
    // render(), so this plugin body is minimal.
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
