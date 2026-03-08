import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin, Registry } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

// --- Properties ---

const reviewflowProperties = [
  {
    name: "version",
    label: "Version tag",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "roles",
    label: "Roles",
    default: "{}",
    multiline: true,
    parse: (raw: string) => {
      // Parse "rolename=username\n..." into JSON string
      const roles: Record<string, string> = {}
      for (const line of raw.split("\n")) {
        const trimmed = line.trim()
        if (!trimmed) continue
        const eq = trimmed.indexOf("=")
        if (eq > 0) {
          roles[trimmed.slice(0, eq).trim()] = trimmed.slice(eq + 1).trim()
        }
      }
      return JSON.stringify(roles)
    },
    serialize: (value: string | null) => {
      // Convert JSON string back to "rolename=username\n..." lines
      try {
        const roles = JSON.parse(value || "{}")
        return Object.entries(roles)
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([k, v]) => `${k}=${v}`)
          .join("\n")
      } catch {
        return value || ""
      }
    },
    helpText: "One role per line: rolename=username",
  },
]

// --- API Gate ---

const API_BASE = "/api/plugin/reviewflow/v1"

interface ReviewflowStatus {
  roles: Record<string, string>
  version_tag: string
  current_page_version: number
  validated_page_version: number
  missing_roles: Record<string, string>
  deadlines?: Record<string, string>
  overdue_roles?: string[]
  is_fully_validated: boolean
  version_history?: {
    page_version: number
    timestamp: string
    confirmed_by: Record<string, string>
    version_tag: string
  }[]
}

// User display name cache
const userDisplayCache: Record<string, string> = {}

async function resolveUserLabels(usernames: string[]): Promise<void> {
  const unknown = usernames.filter(u => !(u in userDisplayCache))
  if (unknown.length === 0) return
  try {
    const resp = await fetch(`/api/users/display?users=${encodeURIComponent(unknown.join(","))}`)
    if (!resp.ok) return
    const data = await resp.json()
    for (const [name, info] of Object.entries(data.users || {})) {
      userDisplayCache[name] = (info as any).label || name
    }
  } catch { /* best effort */ }
}

function getUserLabel(username: string): string {
  return userDisplayCache[username] || username
}

const gate = {
  async getStatus(pagePath: string, version?: number | null): Promise<ReviewflowStatus> {
    const cleanPath = pagePath.replace(/^\/+/, "")
    const vParam = version ? `?v=${version}` : ""
    const resp = await fetch(`${API_BASE}/status/${cleanPath}${vParam}`)
    if (!resp.ok) throw new Error(`reviewflow status: ${resp.status}`)
    return resp.json()
  },

  async confirm(pagePath: string, role: string): Promise<ReviewflowStatus> {
    const cleanPath = pagePath.replace(/^\/+/, "")
    const resp = await fetch(`${API_BASE}/confirm/${cleanPath}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ role }),
    })
    if (!resp.ok) {
      const body = await resp.json().catch(() => ({}))
      throw new Error(body.error || `reviewflow confirm: ${resp.status}`)
    }
    return resp.json()
  },
}

// --- NodeView ---

class ReviewflowNodeView {
  dom: HTMLElement
  private node: PMNode
  private view: EditorView
  private getPos: () => number | undefined
  private status: ReviewflowStatus | null = null
  private loading = false
  private historyVersion: number | null = null

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.getPos = getPos

    // Capture history version context (set by viewVersion() in main.js).
    this.historyVersion = (window as any).__gowikiViewingVersion ?? null

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-reviewflow"
    this.dom.contentEditable = "false"

    this.render()
    this.fetchStatus()
  }

  private render() {
    this.dom.innerHTML = ""

    let roles: Record<string, string> = {}
    try {
      roles = JSON.parse(this.node.attrs.roles || "{}")
    } catch { /* ignore */ }

    const version = this.node.attrs.version || ""
    const roleEntries = Object.entries(roles).sort(([a], [b]) => a.localeCompare(b))
    // If the backend has no state yet (no roles in status), treat all roles as pending.
    const backendHasState = this.status != null && Object.keys(this.status.roles || {}).length > 0
    const missingRoles = backendHasState ? (this.status!.missing_roles || {}) : roles
    const overdueSet = new Set(this.status?.overdue_roles || [])
    const currentUser = (window as any).__gowikiCurrentUser?.username || ""
    const isValidated = backendHasState && this.status!.is_fully_validated === true

    // Check if the current version tag was already validated in a previous cycle.
    // If so, the version tag must be bumped before new approvals can proceed.
    const versionHistory = this.status?.version_history || []
    const versionTagStale = !this.historyVersion && !isValidated && version !== "" &&
      versionHistory.some(vr => vr.version_tag === version)

    // Wrapper with border color
    const wrapper = document.createElement("div")
    wrapper.className = "gowiki-rf-wrapper"
    if (isValidated) wrapper.classList.add("gowiki-rf-wrapper--validated")
    else if (overdueSet.size > 0) wrapper.classList.add("gowiki-rf-wrapper--overdue")

    // Header row
    const header = document.createElement("div")
    header.className = "gowiki-rf-header"
    const headerLabel = document.createElement("span")
    headerLabel.className = "gowiki-rf-header-label"
    headerLabel.textContent = "Reviewflow"
    header.appendChild(headerLabel)
    if (version) {
      const tag = document.createElement("span")
      tag.className = "gowiki-rf-version-tag"
      tag.textContent = version
      header.appendChild(tag)
    }
    if (isValidated) {
      const badge = document.createElement("span")
      badge.className = "gowiki-rf-validated-badge"
      badge.textContent = "\u2714 Validated"
      header.appendChild(badge)
    } else if (roleEntries.length > 0 && !this.loading) {
      const draft = document.createElement("span")
      draft.className = "gowiki-rf-draft-badge"
      draft.textContent = "DRAFT"
      header.appendChild(draft)
    }
    if (this.loading) {
      const loadEl = document.createElement("span")
      loadEl.className = "gowiki-rf-loading"
      loadEl.textContent = "Loading\u2026"
      header.appendChild(loadEl)
    }
    wrapper.appendChild(header)

    // Warning: version tag already used
    if (versionTagStale) {
      const warn = document.createElement("div")
      warn.className = "gowiki-rf-stale-warning"
      warn.textContent = `\u26A0 Version "${version}" was already validated. Update the version tag before approving.`
      wrapper.appendChild(warn)
    }

    // Table
    if (roleEntries.length > 0) {
      const table = document.createElement("table")
      table.className = "gowiki-rf-table"

      const thead = document.createElement("thead")
      const thRow = document.createElement("tr")
      for (const h of ["Role", "Assignee", "Status", ""]) {
        const th = document.createElement("th")
        th.textContent = h
        thRow.appendChild(th)
      }
      thead.appendChild(thRow)
      table.appendChild(thead)

      const tbody = document.createElement("tbody")
      for (const [role, user] of roleEntries) {
        const tr = document.createElement("tr")
        const isMissing = role in missingRoles
        const isOverdue = overdueSet.has(role)

        // Role name
        const tdRole = document.createElement("td")
        tdRole.className = "gowiki-rf-cell-role"
        tdRole.textContent = role
        tr.appendChild(tdRole)

        // Assignee
        const tdUser = document.createElement("td")
        tdUser.textContent = getUserLabel(user)
        if (getUserLabel(user) !== user) {
          tdUser.title = user // show login on hover
        }
        tr.appendChild(tdUser)

        // Status
        const tdStatus = document.createElement("td")
        if (isOverdue) {
          tdStatus.className = "gowiki-rf-status--overdue"
          tdStatus.textContent = "\u26A0 Overdue"
        } else if (isMissing) {
          tdStatus.className = "gowiki-rf-status--pending"
          tdStatus.textContent = "\u23F3 Pending"
        } else {
          tdStatus.className = "gowiki-rf-status--confirmed"
          tdStatus.textContent = "\u2714 Confirmed"
        }
        tr.appendChild(tdStatus)

        // Action
        const tdAction = document.createElement("td")
        if (isMissing && user === currentUser && !versionTagStale && !this.historyVersion) {
          const btn = document.createElement("button")
          btn.className = "gowiki-rf-confirm-btn"
          btn.textContent = "Confirm"
          btn.addEventListener("click", (e) => {
            e.preventDefault()
            e.stopPropagation()
            this.doConfirm(role)
          })
          tdAction.appendChild(btn)
        }
        tr.appendChild(tdAction)

        tbody.appendChild(tr)
      }
      table.appendChild(tbody)
      wrapper.appendChild(table)
    }

    this.dom.appendChild(wrapper)
  }

  private async fetchStatus() {
    const pagePath = window.location.pathname
    this.loading = true
    this.render()
    try {
      this.status = await gate.getStatus(pagePath, this.historyVersion)
    } catch {
      // Status not available — show roles without status
    }
    // Resolve display names for all role assignees
    let roles: Record<string, string> = {}
    try { roles = JSON.parse(this.node.attrs.roles || "{}") } catch {}
    const usernames = Object.values(roles).filter(u => u)
    if (usernames.length > 0) {
      await resolveUserLabels(usernames)
    }
    this.loading = false
    this.render()
    this.updatePageBackground()
  }

  /** Apply light red background to the content area when page is not validated. */
  private updatePageBackground() {
    // Don't change page background when viewing a historical version.
    if (this.historyVersion) return
    const contentRoot = document.getElementById("content") || document.querySelector(".ProseMirror")
    if (!contentRoot) return
    const hasRoles = Object.keys(
      (() => { try { return JSON.parse(this.node.attrs.roles || "{}") } catch { return {} } })()
    ).length > 0
    if (hasRoles && !this.status?.is_fully_validated) {
      contentRoot.classList.add("gowiki-rf-page-invalid")
    } else {
      contentRoot.classList.remove("gowiki-rf-page-invalid")
    }
  }

  private async doConfirm(role: string) {
    const pagePath = window.location.pathname
    try {
      this.status = await gate.confirm(pagePath, role)
      this.render()
      this.updatePageBackground()
    } catch (err) {
      console.error("reviewflow confirm failed:", err)
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.render()
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

  destroy() {
    const contentRoot = document.getElementById("content") || document.querySelector(".ProseMirror")
    if (contentRoot) contentRoot.classList.remove("gowiki-rf-page-invalid")
  }
}

// --- Styles ---

const reviewflowStyles = `
.gowiki-reviewflow {
  margin: 8px 0;
}

.gowiki-rf-wrapper {
  border: 2px solid #e0e0e0;
  border-radius: 8px;
  background: #fafafa;
}

.gowiki-rf-wrapper--validated {
  border-color: #4caf50;
}

.gowiki-rf-wrapper--overdue {
  border-color: #ff9800;
}

.gowiki-rf-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 14px;
  background: #f0f0f0;
  border-bottom: 1px solid #e0e0e0;
  border-radius: 6px 6px 0 0;
}

.gowiki-rf-wrapper--validated .gowiki-rf-header {
  background: #e8f5e9;
  border-bottom-color: #c8e6c9;
}

.gowiki-rf-header-label {
  font-weight: 600;
  font-size: 13px;
  color: #555;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.gowiki-rf-version-tag {
  background: #e3f2fd;
  color: #1565c0;
  padding: 1px 8px;
  border-radius: 10px;
  font-weight: 600;
  font-size: 13px;
}

.gowiki-rf-validated-badge {
  color: #2e7d32;
  font-weight: 600;
  font-size: 13px;
}

.gowiki-rf-draft-badge {
  color: #c62828;
  font-weight: 700;
  font-size: 15px;
  letter-spacing: 1px;
}

.gowiki-rf-stale-warning {
  padding: 6px 14px;
  background: #fff3e0;
  color: #e65100;
  font-size: 13px;
  border-bottom: 1px solid #ffe0b2;
}

.gowiki-rf-loading {
  color: #999;
  font-size: 12px;
  font-style: italic;
}

.gowiki-rf-table {
  border-collapse: collapse;
  font-size: 14px;
  margin: 0.3em !important;
  border-bottom: thin solid lightgray;
}

.gowiki-rf-table th {
  text-align: left;
  padding: 4px 14px;
  font-weight: 600;
  font-size: 12px;
  color: #777;
  text-transform: uppercase;
  letter-spacing: 0.3px;
  border-bottom: 1px solid #e8e8e8;
}

.gowiki-rf-table td {
  padding: 5px 14px;
  border-bottom: 1px solid #f0f0f0;
}

.gowiki-rf-table tr:last-child td {
  border-bottom: none;
}

.gowiki-rf-cell-role {
  font-weight: 500;
}

.gowiki-rf-status--confirmed {
  color: #2e7d32;
}

.gowiki-rf-status--pending {
  color: #757575;
}

.gowiki-rf-status--overdue {
  color: #c62828;
  font-weight: 600;
}

.gowiki-rf-confirm-btn {
  background: #1976d2;
  color: white;
  border: none;
  border-radius: 4px;
  padding: 3px 12px;
  cursor: pointer;
  font-size: 12px;
}

.gowiki-rf-confirm-btn:hover {
  background: #1565c0;
}

#app.gowiki-editing .gowiki-reviewflow.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}

.gowiki-rf-page-invalid {
  background: #fff5f5 !important;
}
`

// --- Plugin ---

export const reviewflowPlugin: WikiPlugin = {
  register(reg) {
    // Schema node
    reg.registerSchema({
      nodes: {
        reviewflow: {
          group: "block",
          atom: true,
          attrs: {
            version: { default: "" },
            roles: { default: "{}" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-reviewflow",
                "data-version": node.attrs.version || "",
              },
              `Reviewflow: ${node.attrs.version || "(no version)"}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-reviewflow",
              getAttrs(dom: HTMLElement) {
                return {
                  version: dom.getAttribute("data-version") || "",
                }
              },
            },
          ],
        },
      },
    })

    // Self-contained directive: {reviewflow version=X author=alice reviewer=bob}
    // collectExtra: true allows arbitrary role keys (author, reviewer, etc.)
    reg.registerSelfContainedDirective("reviewflow", {
      tokenType: "reviewflow",
      nodeType: "reviewflow",
      properties: reviewflowProperties,
      collectExtra: true,
    })

    // Markdown → PM: handle the synthetic "reviewflow" token
    reg.registerText("reviewflow", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        // Separate version from roles
        const version = attrs.version ?? ""
        const roles: Record<string, string> = {}
        for (const [k, v] of Object.entries(attrs)) {
          if (k !== "version") {
            roles[k] = String(v)
          }
        }
        ctx.push(
          ctx.schema.nodes.reviewflow.create({
            version,
            roles: JSON.stringify(roles),
          })
        )
      },
    })

    // PM → Markdown: serialize reviewflow node back to directive syntax
    reg.registerPMNode("reviewflow", {
      print(node) {
        const parts: string[] = []

        // version always first
        const version = node.attrs.version || ""
        if (version) {
          parts.push(`version=${version}`)
        }

        // roles alphabetically sorted
        let roles: Record<string, string> = {}
        try {
          roles = JSON.parse(node.attrs.roles || "{}")
        } catch { /* ignore */ }

        for (const key of Object.keys(roles).sort()) {
          parts.push(`${key}=${roles[key]}`)
        }

        if (parts.length === 0) {
          return "{reviewflow}\n\n"
        }
        return `{reviewflow ${parts.join(" ")}}\n\n`
      },
    })

    // Editor plugin: NodeView
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.reviewflow"),
        props: {
          nodeViews: {
            reviewflow(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new ReviewflowNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Command: insert a new reviewflow node and open properties panel
    reg.registerCommand("reviewflow", "insert", (state, dispatch) => {
      const rfType = reg.schema.nodes.reviewflow
      if (!rfType) return false
      if (dispatch) {
        const node = rfType.create({ version: "", roles: "{}" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 200),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === rfType) {
              insertedAt = pos
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch {
            // Leave default selection if NodeSelection fails
          }
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    // Styles
    reg.registerStyle("reviewflow", reviewflowStyles)
  },
}
