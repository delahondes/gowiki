import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin, Registry } from "../compiler/registry"
import { enablePropertiesPanel, requestInputFocus } from "../compiler/core_ui"
import { signConfirmation } from "../signing/signer"
import { hasKey } from "../signing/keystore"

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
  signing_enabled?: boolean
  signing_required?: boolean
  signed_roles?: string[]
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

  async confirm(pagePath: string, role: string, sigData?: { signature: string; certificate: string; digest: string } | null): Promise<ReviewflowStatus> {
    const cleanPath = pagePath.replace(/^\/+/, "")
    const body: any = { role }
    if (sigData) {
      body.signature = sigData.signature
      body.certificate = sigData.certificate
      body.digest = sigData.digest
    }
    const resp = await fetch(`${API_BASE}/confirm/${cleanPath}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    })
    if (!resp.ok) {
      const b = await resp.json().catch(() => ({}))
      throw new Error(b.error || `reviewflow confirm: ${resp.status}`)
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
  private userHasKey = false

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
    // Check if the current user has a signing key.
    const currentUser = (window as any).__gowikiCurrentUser?.username
    if (currentUser) {
      hasKey(currentUser).then(has => {
        this.userHasKey = has
        if (this.status) this.render() // re-render with updated button label
      }).catch(() => {})
    }
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
        tdRole.textContent = role.charAt(0).toUpperCase() + role.slice(1)
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
          const isSigned = this.status?.signed_roles?.includes(role)
          tdStatus.className = "gowiki-rf-status--confirmed"
          tdStatus.textContent = isSigned ? "\uD83D\uDD12 Signed" : "\u2714 Confirmed"
        }
        tr.appendChild(tdStatus)

        // Action
        const tdAction = document.createElement("td")
        if (isMissing && user === currentUser && !versionTagStale && !this.historyVersion) {
          const btn = document.createElement("button")
          btn.className = "gowiki-rf-confirm-btn"
          // Show "Sign & Confirm" if signing is enabled and user has a key.
          if (this.status?.signing_enabled && this.userHasKey) {
            btn.textContent = "\uD83D\uDD12 Sign & Confirm"
          } else if (this.status?.signing_required && !this.userHasKey) {
            btn.textContent = "Signing key required"
            btn.disabled = true
          } else {
            btn.textContent = "Confirm"
          }
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
      // Attempt cryptographic signing if enabled and user has a key.
      let sigData: { signature: string; certificate: string; digest: string } | null = null
      if (this.status?.signing_enabled && this.userHasKey) {
        const currentUser = (window as any).__gowikiCurrentUser?.username
        if (currentUser) {
          // Get the current page markdown for digest computation.
          const markdown = (window as any).__gowikiCurrentMarkdown?.() || ""
          sigData = await signConfirmation(currentUser, markdown)
        }
      }
      this.status = await gate.confirm(pagePath, role, sigData)
      this.render()
      this.updatePageBackground()
    } catch (err: any) {
      console.error("reviewflow confirm failed:", err)
      if (err?.message) alert("Confirm failed: " + err.message)
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

// --- Reviewflow Query NodeView ---

class ReviewflowQueryNodeView {
  dom: HTMLElement
  private node: PMNode

  constructor(node: PMNode, _view: EditorView, _getPos: () => number | undefined) {
    this.node = node
    this.dom = document.createElement("div")
    this.dom.className = "gowiki-reviewflow-query"
    this.dom.contentEditable = "false"
    this.fetchAndRender()
  }

  private resolvePathPrefix(): string {
    const raw = this.node.attrs.path || ""
    if (raw) return raw.replace(/^\/+/, "")
    // Default: namespace of the current page
    const pathname = window.location.pathname
    const loc = pathname.replace(/^\/+|\/+$/g, "")
    const parts = loc.split("/")
    if (!pathname.endsWith("/")) parts.pop() // remove page name, keep namespace
    return parts.join("/")
  }

  private async fetchAndRender() {
    const path = this.resolvePathPrefix()
    const statusFilter = this.node.attrs.status || "draft"

    this.dom.innerHTML = '<div class="gowiki-rfq-loading">Loading reviewflow status...</div>'

    try {
      // 1. Fetch all pages under the path
      const nsResp = await fetch(`/api/ai/v1/namespace/${path}?depth=0&include_meta=true`)
      if (!nsResp.ok) {
        this.dom.innerHTML = '<div class="gowiki-rfq-error">Failed to load pages</div>'
        return
      }
      const nsData = await nsResp.json()
      const pages: any[] = nsData.pages || []

      // 2. Fetch reviewflow status for each page (in parallel)
      const statusPromises = pages.map(async (p: any) => {
        const cleanPath = p.path.replace(/^\/+/, "")
        try {
          const resp = await fetch(`/api/plugin/reviewflow/v1/status/${cleanPath}`)
          if (!resp.ok) return null
          const status = await resp.json()
          if (!status.roles || Object.keys(status.roles).length === 0) return null
          return { page: p, status }
        } catch { return null }
      })
      const results = (await Promise.all(statusPromises)).filter(Boolean) as any[]

      // 3. Filter by status
      const filtered = results.filter(r => {
        const isValidated = r.status.is_fully_validated === true
        if (statusFilter === "draft") return !isValidated
        if (statusFilter === "validated") return isValidated
        return true // "all"
      })

      // 4. Sort by date (most recent first)
      filtered.sort((a, b) => {
        const da = a.page.last_modified || ""
        const db = b.page.last_modified || ""
        return db.localeCompare(da)
      })

      this.dom.innerHTML = ""

      // Header
      const header = document.createElement("div")
      header.className = "gowiki-rfq-header"
      const label = statusFilter === "draft" ? "Documents pending validation"
        : statusFilter === "validated" ? "Validated documents"
        : "All reviewflow documents"
      header.textContent = `Reviewflow: ${label} (/${path})`
      this.dom.appendChild(header)

      if (filtered.length === 0) {
        const empty = document.createElement("div")
        empty.className = "gowiki-rfq-empty"
        empty.textContent = statusFilter === "draft"
          ? "No documents pending validation."
          : "No documents found."
        this.dom.appendChild(empty)
        return
      }

      // Resolve user display names
      const allUsers = new Set<string>()
      for (const r of filtered) {
        for (const user of Object.values(r.status.roles || {})) allUsers.add(user as string)
        if (r.page.author) allUsers.add(r.page.author)
      }
      const unknownUsers = [...allUsers].filter(u => !(u in userDisplayCache))
      if (unknownUsers.length > 0) {
        await resolveUserLabels(unknownUsers)
      }

      // Table
      const table = document.createElement("table")
      table.className = "gowiki-rfq-table"
      const thead = document.createElement("thead")
      const hr = document.createElement("tr")
      for (const h of ["Page", "Version", "Date", "Author", "Status", "Confirmations"]) {
        const th = document.createElement("th")
        th.textContent = h
        hr.appendChild(th)
      }
      thead.appendChild(hr)
      table.appendChild(thead)

      const tbody = document.createElement("tbody")
      for (const r of filtered) {
        const tr = document.createElement("tr")

        // Page link
        const tdPage = document.createElement("td")
        const a = document.createElement("a")
        a.href = r.page.path
        a.textContent = r.page.title || r.page.path
        a.className = "gowiki-link-exists"
        tdPage.appendChild(a)
        tr.appendChild(tdPage)

        // Version tag
        const tdVersion = document.createElement("td")
        if (r.status.version_tag) {
          if (r.status.is_fully_validated && r.status.validated_page_version) {
            const va = document.createElement("a")
            va.href = `${r.page.path}?v=${r.status.validated_page_version}`
            va.textContent = r.status.version_tag
            va.className = "gowiki-rfq-version-link"
            tdVersion.appendChild(va)
          } else {
            tdVersion.textContent = r.status.version_tag
          }
        }
        tr.appendChild(tdVersion)

        // Date
        const tdDate = document.createElement("td")
        tdDate.textContent = r.page.last_modified
          ? new Date(r.page.last_modified).toLocaleDateString()
          : ""
        tr.appendChild(tdDate)

        // Author
        const tdAuthor = document.createElement("td")
        tdAuthor.textContent = r.page.author ? getUserLabel(r.page.author) : ""
        tr.appendChild(tdAuthor)

        // Status badge
        const tdStatus = document.createElement("td")
        const badge = document.createElement("span")
        if (r.status.is_fully_validated) {
          badge.className = "gowiki-rfq-badge gowiki-rfq-badge-validated"
          badge.textContent = "Validated"
        } else {
          badge.className = "gowiki-rfq-badge gowiki-rfq-badge-draft"
          badge.textContent = "Draft"
        }
        tdStatus.appendChild(badge)
        tr.appendChild(tdStatus)

        // Confirmations
        const tdConf = document.createElement("td")
        const roles = r.status.roles || {}
        const missingRoles = r.status.missing_roles || {}
        const confirmations = r.status.version_history || []
        // Find confirmations for this version from the raw status
        const fragments: string[] = []
        for (const [role, user] of Object.entries(roles)) {
          const isMissing = role in missingRoles
          const label = getUserLabel(user as string)
          if (isMissing) {
            fragments.push(`${role}: ${label} \u23F3`)
          } else {
            fragments.push(`${role}: ${label} \u2713`)
          }
        }
        tdConf.textContent = fragments.join(", ")
        tdConf.style.fontSize = "0.85em"
        tr.appendChild(tdConf)

        tbody.appendChild(tr)
      }
      table.appendChild(tbody)
      this.dom.appendChild(table)

    } catch (err) {
      this.dom.innerHTML = '<div class="gowiki-rfq-error">Failed to load reviewflow data</div>'
    }
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    if (node.attrs.path !== this.node.attrs.path || node.attrs.status !== this.node.attrs.status) {
      this.node = node
      this.fetchAndRender()
      return true
    }
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

// --- Reviewflow Query Styles ---

const rfqStyles = `
.gowiki-reviewflow-query {
  margin: 0.5em 0;
}

.gowiki-rfq-loading {
  color: #999;
  font-style: italic;
  padding: 8px;
}

.gowiki-rfq-error {
  color: #c62828;
  padding: 8px;
}

.gowiki-rfq-empty {
  color: #666;
  padding: 8px;
  font-style: italic;
}

.gowiki-rfq-header {
  font-size: 11px;
  color: #636e72;
  margin-bottom: 4px;
  font-family: monospace;
}

.gowiki-rfq-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

.gowiki-rfq-table th {
  background: #f1f3f5;
  text-align: left;
  padding: 6px 10px;
  font-weight: 600;
  font-size: 13px;
  border-bottom: 2px solid #dee2e6;
}

.gowiki-rfq-table td {
  padding: 6px 10px;
  border-bottom: 1px solid #eee;
}

.gowiki-rfq-badge {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 12px;
  font-weight: 600;
}

.gowiki-rfq-badge-draft {
  background: #fff3e0;
  color: #e65100;
}

.gowiki-rfq-badge-validated {
  background: #e8f5e9;
  color: #2e7d32;
}

.gowiki-rfq-version-link {
  display: inline-block;
  background: #e3f2fd;
  color: #1565c0;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 12px;
  font-weight: 600;
  text-decoration: none;
  cursor: pointer;
}

.gowiki-rfq-version-link:hover {
  background: #bbdefb;
}

#app.gowiki-editing .gowiki-reviewflow-query {
  background: #f8f9fa;
  border: 1px solid #dee2e6;
  border-radius: 4px;
  padding: 8px;
}

#app.gowiki-editing .gowiki-reviewflow-query.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
`

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
          if (k !== "version" && k !== "_args") {
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

    // ────────────────────────────────────────────────────
    // Reviewflow Query — {reviewflow-query path=... status=...}
    // ────────────────────────────────────────────────────

    const rfqProperties = [
      {
        name: "path",
        label: "Path prefix",
        default: "",
        parse: (raw: string) => raw.trim(),
        serialize: (value: string | null) => String(value ?? ""),
        helpText: "Namespace to scan (empty = current page's namespace)",
      },
      {
        name: "status",
        label: "Status filter",
        default: "draft",
        parse: (raw: string) => {
          const v = raw.trim().toLowerCase()
          return (v === "draft" || v === "validated" || v === "all") ? v : "draft"
        },
        serialize: (value: string | null) => String(value ?? "draft"),
        options: [
          { value: "draft", label: "Draft (pending validation)" },
          { value: "validated", label: "Validated" },
          { value: "all", label: "All" },
        ],
      },
    ]

    reg.registerSchema({
      nodes: {
        reviewflow_query: {
          group: "block",
          atom: true,
          attrs: {
            path: { default: "" },
            status: { default: "draft" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-reviewflow-query",
                "data-path": node.attrs.path || "",
                "data-status": node.attrs.status || "draft",
              },
              `Reviewflow query: ${node.attrs.status || "draft"}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-reviewflow-query",
              getAttrs(dom: HTMLElement) {
                return {
                  path: dom.getAttribute("data-path") || "",
                  status: dom.getAttribute("data-status") || "draft",
                }
              },
            },
          ],
        },
      },
    })

    reg.registerSelfContainedDirective("reviewflow-query", {
      tokenType: "reviewflow_query",
      nodeType: "reviewflow_query",
      properties: rfqProperties,
    })

    reg.registerText("reviewflow_query", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.reviewflow_query.create({
            path: attrs.path ?? "",
            status: attrs.status ?? "draft",
          })
        )
      },
    })

    reg.registerPMNode("reviewflow_query", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.path) parts.push(`path=${node.attrs.path}`)
        if (node.attrs.status && node.attrs.status !== "draft") {
          parts.push(`status=${node.attrs.status}`)
        }
        return parts.length
          ? `{reviewflow-query ${parts.join(" ")}}\n\n`
          : `{reviewflow-query}\n\n`
      },
    })

    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.reviewflow-query"),
        props: {
          nodeViews: {
            reviewflow_query(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new ReviewflowQueryNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    reg.registerCommand("reviewflow-query", "insert", (state, dispatch) => {
      const queryType = reg.schema.nodes.reviewflow_query
      if (!queryType) return false
      if (dispatch) {
        requestInputFocus("path")
        const node = queryType.create({ path: "", status: "draft" })
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 5),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === queryType && insertedAt === null) {
              insertedAt = pos
              return false
            }
          }
        )
        if (insertedAt !== null) {
          try {
            tr = tr.setSelection(NodeSelection.create(tr.doc, insertedAt))
            tr = enablePropertiesPanel(tr)
          } catch { /* leave default selection */ }
        }
        dispatch(tr.scrollIntoView())
      }
      return true
    })

    // Styles
    reg.registerStyle("reviewflow", reviewflowStyles)
    reg.registerStyle("reviewflow-query", rfqStyles)
  },
}
