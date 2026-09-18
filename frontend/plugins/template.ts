import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

// ── {template} — the copy marker ──────────────────────────────────────
//
// Replaces the horizontal rule used today. Renders as that rule PLUS a
// Create document button. Visible in both view and edit modes: the whole
// point of a template is that anyone consulting it should be able to
// issue a document from it.

class TemplateMarkerNodeView {
  dom: HTMLElement
  private node: PMNode
  private status: {
    isValidated: boolean
    versionTag: string
    missingRoles: string[]
    hasReviewflow: boolean
    templateTitle: string
    titlePattern: string
  } | null = null

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-template-marker"
    this.dom.contentEditable = "false"
    this.render()
    this.loadStatus()
  }

  private render() {
    this.dom.innerHTML = ""

    const rule = document.createElement("hr")
    rule.className = "gowiki-template-rule"
    this.dom.appendChild(rule)

    const bar = document.createElement("div")
    bar.className = "gowiki-template-bar"

    const label = document.createElement("span")
    label.className = "gowiki-template-label"
    label.textContent = "Template — payload below this line"
    bar.appendChild(label)

    const btn = document.createElement("button")
    btn.type = "button"
    btn.className = "gowiki-template-create-btn"
    btn.textContent = "Create document"

    // If we already know the reviewflow is open, refuse upfront —
    // clicking would just get a 409 back. Named-role hints match the
    // spec's "loud refusal" requirement.
    if (this.status && this.status.hasReviewflow && !this.status.isValidated) {
      btn.disabled = true
      btn.title = `Template reviewflow not fully validated (missing: ${this.status.missingRoles.join(", ")})`
    }

    btn.addEventListener("click", (e) => {
      e.preventDefault()
      e.stopPropagation()
      this.openDialog()
    })
    bar.appendChild(btn)

    this.dom.appendChild(bar)
  }

  private currentPagePath(): string {
    // The wiki's current pathname doubles as the template path — this
    // NodeView only ever renders inside the template page itself.
    return window.location.pathname
  }

  private async loadStatus() {
    const path = this.currentPagePath()
    const cleanPath = path.replace(/^\/+/, "")

    // Pull the reviewflow status (loose — a template with no reviewflow
    // returns roles: {} which is fine).
    let isValidated = true
    let versionTag = ""
    let missingRoles: string[] = []
    let hasReviewflow = false
    try {
      const resp = await fetch(`/api/plugin/reviewflow/v1/status/${cleanPath}`)
      if (resp.ok) {
        const status = await resp.json()
        hasReviewflow = status.roles && Object.keys(status.roles).length > 0
        if (hasReviewflow) {
          isValidated = !!status.is_fully_validated
          versionTag = status.version_tag || ""
          missingRoles = Object.keys(status.missing_roles || {})
        }
      }
    } catch { /* leave defaults */ }

    // Read the page markdown so we can prefill the title pattern in the
    // dialog. Falling back to "" is fine — the user can type anything.
    let templateTitle = ""
    let titlePattern = ""
    try {
      const pageResp = await fetch(`/api/pages/${cleanPath}`)
      if (pageResp.ok) {
        const data = await pageResp.json()
        templateTitle = data.title || ""
        const md: string = data.markdown || ""
        titlePattern = extractTemplateTitlePattern(md)
      }
    } catch { /* leave defaults */ }

    this.status = { isValidated, versionTag, missingRoles, hasReviewflow, templateTitle, titlePattern }
    this.render()
  }

  private openDialog() {
    if (this.status && this.status.hasReviewflow && !this.status.isValidated) {
      alert(`Cannot create: the template's reviewflow is not fully validated. Missing role(s): ${this.status.missingRoles.join(", ")}.`)
      return
    }
    openCreateFromTemplateDialog({
      templatePath: this.currentPagePath(),
      templateTitle: this.status?.templateTitle || "",
      titlePattern: this.status?.titlePattern || "",
      versionTag: this.status?.versionTag || "",
    })
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    return true
  }

  stopEvent(event: Event): boolean {
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click") return false
    return true
  }

  ignoreMutation(): boolean {
    return true
  }
}

// ── {template-title} ─────────────────────────────────────────────────
//
// A muted label above the heading it prefixes, showing that the heading
// on the template page is a pattern the user completes at creation time.

class TemplateTitleNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-template-title"
    this.dom.contentEditable = "false"
    this.render()
  }

  private render() {
    this.dom.innerHTML = ""
    const label = document.createElement("span")
    label.className = "gowiki-template-title-label"
    label.textContent = "Template title pattern — user completes at creation"
    this.dom.appendChild(label)
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    return true
  }

  stopEvent(): boolean { return true }
  ignoreMutation(): boolean { return true }
}

// ── {template-stamp} ─────────────────────────────────────────────────
//
// In a template context: a muted note. In a NON-template context (a page
// that has no {template} directive): a loud error, per spec §5.

class TemplateStampNodeView {
  dom: HTMLElement
  private node: PMNode
  private isTemplateContext: boolean | null = null

  constructor(node: PMNode, view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-template-stamp"
    this.dom.contentEditable = "false"
    this.isTemplateContext = docHasTemplateMarker(view.state.doc)
    this.render()
  }

  private render() {
    this.dom.innerHTML = ""
    if (this.isTemplateContext) {
      const note = document.createElement("span")
      note.className = "gowiki-template-stamp-note"
      note.textContent = "Created from template: stamped on document creation"
      this.dom.appendChild(note)
    } else {
      const err = document.createElement("span")
      err.className = "gowiki-template-stamp-error"
      err.textContent = "⚠ Unresolved {template-stamp} — this page has no {template} marker. Recreate the document from its template via the Create document button."
      this.dom.appendChild(err)
    }
  }

  update(node: PMNode, view?: EditorView): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    if (view) {
      const ctx = docHasTemplateMarker(view.state.doc)
      if (ctx !== this.isTemplateContext) {
        this.isTemplateContext = ctx
        this.render()
      }
    }
    return true
  }

  stopEvent(): boolean { return true }
  ignoreMutation(): boolean { return true }
}

// ── {template-reviewflow …} ──────────────────────────────────────────
//
// Renders like a muted reviewflow: values are placeholders and will be
// resolved into {reviewflow …} at document creation.

class TemplateReviewflowNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-template-reviewflow"
    this.dom.contentEditable = "false"
    this.render()
  }

  private render() {
    this.dom.innerHTML = ""

    const label = document.createElement("span")
    label.className = "gowiki-template-reviewflow-label"
    label.textContent = "Template reviewflow — resolved at document creation"
    this.dom.appendChild(label)

    const roles: Record<string, string> = {}
    try {
      roles = JSON.parse(this.node.attrs.roles || "{}")
    } catch { /* empty */ }
    const version = this.node.attrs.version || "1.0 (default)"

    const summary = document.createElement("div")
    summary.className = "gowiki-template-reviewflow-summary"
    const parts: string[] = [`version=${version}`]
    for (const key of Object.keys(roles).sort()) {
      parts.push(`${key}=${roles[key]}`)
    }
    if (parts.length === 1) {
      parts.push("(actors inherited from template's own reviewflow)")
    }
    summary.textContent = parts.join(" · ")
    this.dom.appendChild(summary)
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    if (
      node.attrs.version !== this.node.attrs.version ||
      node.attrs.roles !== this.node.attrs.roles
    ) {
      this.node = node
      this.render()
    } else {
      this.node = node
    }
    return true
  }

  stopEvent(): boolean { return true }
  ignoreMutation(): boolean { return true }
}

// ── Helpers ──────────────────────────────────────────────────────────

function docHasTemplateMarker(doc: PMNode): boolean {
  let found = false
  doc.descendants((n) => {
    if (found) return false
    if (n.type.name === "template_marker") {
      found = true
      return false
    }
    return true
  })
  return found
}

// Client-side twin of the Go markdown.ExtractTemplateTitlePattern helper.
// Reads the pattern-text after the first {template-title} directive.
export function extractTemplateTitlePattern(markdown: string): string {
  const lines = markdown.split("\n")
  for (let i = 0; i < lines.length; i++) {
    if (!/^\s*\{template-title(?:\s[^{}]*)?\}\s*$/.test(lines[i])) continue
    let j = i + 1
    while (j < lines.length && lines[j].trim() === "") j++
    const m = j < lines.length ? lines[j].match(/^#{1,6}\s+(.+?)\s*$/) : null
    return m ? m[1] : ""
  }
  return ""
}

// ── Create document dialog ───────────────────────────────────────────

interface CreateDialogOpts {
  templatePath: string
  templateTitle: string
  titlePattern: string
  versionTag: string
}

function openCreateFromTemplateDialog(opts: CreateDialogOpts) {
  const overlay = document.createElement("div")
  overlay.className = "gowiki-link-modal-overlay"

  const dialog = document.createElement("div")
  dialog.className = "gowiki-link-modal gowiki-template-dialog"

  const title = document.createElement("div")
  title.className = "gowiki-link-modal-title"
  title.textContent = "Create document from template"

  const src = document.createElement("div")
  src.className = "gowiki-template-dialog-src"
  const versionPart = opts.versionTag ? `, version ${opts.versionTag}` : ""
  src.textContent = `Template: ${opts.templateTitle || opts.templatePath}${versionPart}`

  // Destination.
  const pathLabel = document.createElement("label")
  pathLabel.className = "gowiki-link-modal-label"
  pathLabel.textContent = "Destination path"
  const pathInput = document.createElement("input")
  pathInput.type = "text"
  pathInput.className = "gowiki-link-modal-input"
  pathInput.placeholder = "/namespace/document-name"

  // Title.
  const titleLabel = document.createElement("label")
  titleLabel.className = "gowiki-link-modal-label"
  titleLabel.textContent = "Document title"
  const titleInput = document.createElement("input")
  titleInput.type = "text"
  titleInput.className = "gowiki-link-modal-input"
  titleInput.value = opts.titlePattern
  titleInput.placeholder = "e.g. VVP/SOFT02 : Verification and Validation Plan"

  // Reviewflow overrides (collapsed).
  const rfDetails = document.createElement("details")
  rfDetails.className = "gowiki-template-dialog-rf"
  const rfSummary = document.createElement("summary")
  rfSummary.textContent = "Reviewflow overrides (optional)"
  rfDetails.appendChild(rfSummary)

  const rfBox = document.createElement("div")
  rfBox.className = "gowiki-template-dialog-rf-box"
  function rfInput(name: string, placeholder: string): HTMLInputElement {
    const wrap = document.createElement("label")
    wrap.className = "gowiki-template-dialog-rf-row"
    const lbl = document.createElement("span")
    lbl.textContent = name
    const inp = document.createElement("input")
    inp.type = "text"
    inp.className = "gowiki-link-modal-input"
    inp.placeholder = placeholder
    wrap.appendChild(lbl)
    wrap.appendChild(inp)
    rfBox.appendChild(wrap)
    return inp
  }
  const versionInput = rfInput("version", "1.0")
  const authorInput = rfInput("author", "leave blank to inherit")
  const reviewerInput = rfInput("reviewer", "leave blank to inherit")
  const validationInput = rfInput("validation", "leave blank to inherit")
  rfDetails.appendChild(rfBox)

  // Warning + summary.
  const warning = document.createElement("div")
  warning.className = "gowiki-link-modal-warning"

  const summaryLabel = document.createElement("label")
  summaryLabel.className = "gowiki-link-modal-label"
  summaryLabel.textContent = "Summary (audit)"
  const summaryInput = document.createElement("input")
  summaryInput.type = "text"
  summaryInput.className = "gowiki-link-modal-input"
  summaryInput.value = `Created from ${opts.templateTitle || opts.templatePath}`

  // Actions.
  const buttons = document.createElement("div")
  buttons.className = "gowiki-link-modal-actions"
  const cancelBtn = document.createElement("button")
  cancelBtn.type = "button"
  cancelBtn.className = "gowiki-link-modal-btn"
  cancelBtn.textContent = "Cancel"
  const okBtn = document.createElement("button")
  okBtn.type = "button"
  okBtn.className = "gowiki-link-modal-btn"
  okBtn.textContent = "Create"

  function close() { overlay.remove() }

  async function submit() {
    warning.textContent = ""
    let path = pathInput.value.trim()
    const title = titleInput.value.trim()
    const summary = summaryInput.value.trim()
    if (!path) {
      warning.textContent = "Destination path is required."
      pathInput.focus()
      return
    }
    if (!path.startsWith("/")) path = "/" + path
    if (!title) {
      warning.textContent = "Title is required."
      titleInput.focus()
      return
    }
    okBtn.disabled = true
    okBtn.textContent = "Creating…"

    const reviewflow: Record<string, string> = {}
    if (versionInput.value.trim()) reviewflow.version = versionInput.value.trim()
    if (authorInput.value.trim()) reviewflow.author = authorInput.value.trim()
    if (reviewerInput.value.trim()) reviewflow.reviewer = reviewerInput.value.trim()
    if (validationInput.value.trim()) reviewflow.validation = validationInput.value.trim()

    try {
      const resp = await fetch("/api/pages/from-template", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          template_path: opts.templatePath,
          path,
          title,
          reviewflow: Object.keys(reviewflow).length > 0 ? reviewflow : undefined,
          summary,
        }),
      })
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}))
        warning.textContent = body.error || `Failed (HTTP ${resp.status})`
        okBtn.disabled = false
        okBtn.textContent = "Create"
        return
      }
      const result = await resp.json()
      close()
      // Navigate to the freshly created page.
      window.location.href = result.path
    } catch (err) {
      warning.textContent = "Network error: " + String(err)
      okBtn.disabled = false
      okBtn.textContent = "Create"
    }
  }

  cancelBtn.addEventListener("click", close)
  okBtn.addEventListener("click", submit)
  overlay.addEventListener("click", (e) => { if (e.target === overlay) close() })
  ;[pathInput, titleInput, summaryInput].forEach(inp => {
    inp.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { e.preventDefault(); submit() }
      if (e.key === "Escape") { e.preventDefault(); close() }
    })
  })

  buttons.appendChild(cancelBtn)
  buttons.appendChild(okBtn)
  dialog.appendChild(title)
  dialog.appendChild(src)
  dialog.appendChild(pathLabel)
  dialog.appendChild(pathInput)
  dialog.appendChild(titleLabel)
  dialog.appendChild(titleInput)
  dialog.appendChild(rfDetails)
  dialog.appendChild(summaryLabel)
  dialog.appendChild(summaryInput)
  dialog.appendChild(warning)
  dialog.appendChild(buttons)
  overlay.appendChild(dialog)
  document.body.appendChild(overlay)
  pathInput.focus()
}

// ── Styles ───────────────────────────────────────────────────────────

const templateStyles = `
.gowiki-template-marker {
  margin: 0.75em 0;
}

.gowiki-template-rule {
  border: none;
  border-top: 2px dashed var(--gw-color-border);
  margin: 0 0 6px 0;
}

.gowiki-template-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 4px 8px;
  background: var(--gw-color-surface-alt, #f5f8ff);
  border: 1px solid var(--gw-color-border);
  border-radius: 4px;
  font-size: 12px;
}

.gowiki-template-label {
  color: var(--gw-color-muted);
  font-style: italic;
}

.gowiki-template-create-btn {
  padding: 4px 12px;
  border: 1px solid var(--gw-color-accent, #4e79a7);
  background: var(--gw-color-accent, #4e79a7);
  color: white;
  border-radius: 3px;
  cursor: pointer;
  font-size: 12px;
  font-weight: 600;
}

.gowiki-template-create-btn:hover:not(:disabled) {
  filter: brightness(1.1);
}

.gowiki-template-create-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.gowiki-template-title {
  margin: 4px 0 -2px 0;
  padding: 1px 6px;
  font-size: 11px;
  color: var(--gw-color-muted);
  font-style: italic;
  border-left: 3px solid var(--gw-color-accent, #4e79a7);
  background: var(--gw-color-surface-alt, #f5f8ff);
}

.gowiki-template-stamp {
  margin: 4px 0;
  padding: 2px 8px;
  font-size: 12px;
  border-radius: 3px;
}

.gowiki-template-stamp-note {
  color: var(--gw-color-muted);
  font-style: italic;
}

.gowiki-template-stamp-error {
  color: var(--gw-color-error, #b71c1c);
  background: var(--gw-color-error-bg, #fce4ec);
  padding: 6px 10px;
  border-radius: 4px;
  border: 1px solid var(--gw-color-error, #ef9a9a);
  font-family: monospace;
  font-size: 12px;
  display: block;
}

.gowiki-template-reviewflow {
  margin: 4px 0;
  padding: 4px 8px;
  border-left: 3px solid var(--gw-color-accent, #4e79a7);
  background: var(--gw-color-surface-alt, #f5f8ff);
  font-size: 12px;
}

.gowiki-template-reviewflow-label {
  color: var(--gw-color-muted);
  font-style: italic;
  font-size: 11px;
}

.gowiki-template-reviewflow-summary {
  font-family: monospace;
  color: var(--gw-color-text);
  margin-top: 2px;
}

/* Selection outline shared with other block NodeViews. */
#app.gowiki-editing .gowiki-template-marker.ProseMirror-selectednode,
#app.gowiki-editing .gowiki-template-title.ProseMirror-selectednode,
#app.gowiki-editing .gowiki-template-stamp.ProseMirror-selectednode,
#app.gowiki-editing .gowiki-template-reviewflow.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}

.gowiki-template-dialog {
  min-width: 480px;
}

.gowiki-template-dialog-src {
  color: var(--gw-color-muted);
  font-size: 12px;
  margin: 4px 0 8px 0;
}

.gowiki-template-dialog-rf {
  margin: 8px 0;
}

.gowiki-template-dialog-rf > summary {
  cursor: pointer;
  color: var(--gw-color-muted);
  font-size: 12px;
  padding: 4px 0;
}

.gowiki-template-dialog-rf-box {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 4px 0 4px 12px;
}

.gowiki-template-dialog-rf-row {
  display: grid;
  grid-template-columns: 90px 1fr;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}
`

// ── Properties ───────────────────────────────────────────────────────

const templateMarkerProperties = [] as any[]
const templateTitleProperties = [] as any[]
const templateStampProperties = [] as any[]
const templateReviewflowProperties = [
  {
    name: "version",
    label: "Version",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (v: string | null) => String(v ?? ""),
    helpText: "Optional. Defaults to 1.0 at creation.",
  },
]

// ── Plugin registration ──────────────────────────────────────────────

export const templatePlugin: WikiPlugin = {
  register(reg) {
    // Schema
    reg.registerSchema({
      nodes: {
        template_marker: {
          group: "block",
          atom: true,
          attrs: {},
          toDOM() {
            return ["div", { class: "gowiki-template-marker" }, "Template — payload below"]
          },
          parseDOM: [{ tag: "div.gowiki-template-marker" }],
        },
        template_title: {
          group: "block",
          atom: true,
          attrs: {},
          toDOM() {
            return ["div", { class: "gowiki-template-title" }, "Template title pattern"]
          },
          parseDOM: [{ tag: "div.gowiki-template-title" }],
        },
        template_stamp: {
          group: "block",
          atom: true,
          attrs: {},
          toDOM() {
            return ["div", { class: "gowiki-template-stamp" }, "Template stamp"]
          },
          parseDOM: [{ tag: "div.gowiki-template-stamp" }],
        },
        template_reviewflow: {
          group: "block",
          atom: true,
          attrs: {
            version: { default: "" },
            roles: { default: "{}" },
          },
          toDOM(node: PMNode) {
            return ["div", { class: "gowiki-template-reviewflow", "data-version": node.attrs.version || "" }, "Template reviewflow"]
          },
          parseDOM: [
            {
              tag: "div.gowiki-template-reviewflow",
              getAttrs(dom: HTMLElement) {
                return { version: dom.getAttribute("data-version") || "" }
              },
            },
          ],
        },
      },
    })

    // Self-contained directives.
    reg.registerSelfContainedDirective("template", {
      tokenType: "template_marker",
      nodeType: "template_marker",
      properties: templateMarkerProperties,
    })
    reg.registerSelfContainedDirective("template-title", {
      tokenType: "template_title",
      nodeType: "template_title",
      properties: templateTitleProperties,
    })
    reg.registerSelfContainedDirective("template-stamp", {
      tokenType: "template_stamp",
      nodeType: "template_stamp",
      properties: templateStampProperties,
    })
    reg.registerSelfContainedDirective("template-reviewflow", {
      tokenType: "template_reviewflow",
      nodeType: "template_reviewflow",
      properties: templateReviewflowProperties,
      collectExtra: true,
    })

    // Markdown → PM.
    reg.registerText("template_marker", {
      run(ctx) {
        ctx.push(ctx.schema.nodes.template_marker.create({}))
      },
    })
    reg.registerText("template_title", {
      run(ctx) {
        ctx.push(ctx.schema.nodes.template_title.create({}))
      },
    })
    reg.registerText("template_stamp", {
      run(ctx) {
        ctx.push(ctx.schema.nodes.template_stamp.create({}))
      },
    })
    reg.registerText("template_reviewflow", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        const version = attrs.version ?? ""
        const roles: Record<string, string> = {}
        for (const [k, v] of Object.entries(attrs)) {
          if (k !== "version" && k !== "_args") roles[k] = String(v)
        }
        ctx.push(
          ctx.schema.nodes.template_reviewflow.create({
            version,
            roles: JSON.stringify(roles),
          })
        )
      },
    })

    // PM → Markdown.
    reg.registerPMNode("template_marker", {
      print() { return `{template}\n\n` },
    })
    reg.registerPMNode("template_title", {
      print() { return `{template-title}\n` },
    })
    reg.registerPMNode("template_stamp", {
      print() { return `{template-stamp}\n\n` },
    })
    reg.registerPMNode("template_reviewflow", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.version) parts.push(`version=${node.attrs.version}`)
        let roles: Record<string, string> = {}
        try { roles = JSON.parse(node.attrs.roles || "{}") } catch { /* empty */ }
        for (const key of Object.keys(roles).sort()) {
          parts.push(`${key}=${roles[key]}`)
        }
        return parts.length ? `{template-reviewflow ${parts.join(" ")}}\n\n` : `{template-reviewflow}\n\n`
      },
    })

    // Editor plugin: NodeViews.
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.template"),
        props: {
          nodeViews: {
            template_marker(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TemplateMarkerNodeView(node, view, getPos)
            },
            template_title(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TemplateTitleNodeView(node, view, getPos)
            },
            template_stamp(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TemplateStampNodeView(node, view, getPos)
            },
            template_reviewflow(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TemplateReviewflowNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Insert commands.
    reg.registerCommand("template", "insert", (state, dispatch) => {
      const type = reg.schema.nodes.template_marker
      if (!type) return false
      if (dispatch) {
        const node = type.create({})
        const tr = state.tr.replaceSelectionWith(node)
        dispatch(tr.scrollIntoView())
      }
      return true
    })
    reg.registerCommand("template-stamp", "insert", (state, dispatch) => {
      const type = reg.schema.nodes.template_stamp
      if (!type) return false
      if (dispatch) {
        const node = type.create({})
        const tr = state.tr.replaceSelectionWith(node)
        dispatch(tr.scrollIntoView())
      }
      return true
    })
    reg.registerCommand("template-title", "insert", (state, dispatch) => {
      const type = reg.schema.nodes.template_title
      if (!type) return false
      if (dispatch) {
        const node = type.create({})
        const tr = state.tr.replaceSelectionWith(node)
        dispatch(tr.scrollIntoView())
      }
      return true
    })
    reg.registerCommand("template-reviewflow", "insert", (state, dispatch) => {
      const type = reg.schema.nodes.template_reviewflow
      if (!type) return false
      if (dispatch) {
        const node = type.create({ version: "", roles: "{}" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === type) {
              try {
                tr = tr.setSelection(NodeSelection.create(tr.doc, pos))
                tr = enablePropertiesPanel(tr)
              } catch { /* ignore */ }
              return false
            }
          }
        )
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    reg.registerStyle("template", templateStyles)
  },
}
