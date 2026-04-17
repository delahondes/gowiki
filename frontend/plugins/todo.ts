import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin, Registry } from "../compiler/registry"
import { enablePropertiesPanel, requestInputFocus } from "../compiler/core_ui"

// --- User display resolution ---

const todoUserDisplayCache: Record<string, string> = {}

async function resolveAssigneeLabels(raw: string): Promise<string> {
  if (!raw) return ""
  const parts = raw.split(",").map(s => s.trim()).filter(Boolean)
  const userParts = parts.filter(p => !p.startsWith("group:"))

  // Resolve user display names if needed.
  const unknown = userParts.filter(u => !(u in todoUserDisplayCache))
  if (unknown.length > 0) {
    try {
      const resp = await fetch(`/api/users/display?users=${encodeURIComponent(unknown.join(","))}`)
      if (resp.ok) {
        const data = await resp.json()
        for (const [name, info] of Object.entries(data.users || {})) {
          todoUserDisplayCache[name] = (info as any).label || name
        }
      }
    } catch { /* best effort */ }
  }

  // Build display string.
  return parts.map(p => {
    if (p.startsWith("group:")) {
      // Capitalize group name: "group:editors" → "Editors"
      const name = p.substring(6)
      return name.charAt(0).toUpperCase() + name.slice(1)
    }
    return todoUserDisplayCache[p] || p
  }).join(", ")
}

function formatAssigneeSync(raw: string): string {
  if (!raw) return ""
  return raw.split(",").map(s => s.trim()).filter(Boolean).map(p => {
    if (p.startsWith("group:")) {
      const name = p.substring(6)
      return name.charAt(0).toUpperCase() + name.slice(1)
    }
    return todoUserDisplayCache[p] || p
  }).join(", ")
}

// --- Properties ---

const todoProperties = [
  {
    name: "title",
    label: "Title",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "assign",
    label: "Assignee",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "resolution",
    label: "Resolution",
    default: "any",
    parse: (raw: string) => {
      const v = raw.trim().toLowerCase()
      return v === "all" ? "all" : "any"
    },
    serialize: (value: string | null) => String(value ?? "any"),
    options: [
      { value: "any", label: "Any member" },
      { value: "all", label: "All members" },
    ],
  },
  {
    name: "due",
    label: "Due date",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "YYYY-MM-DD",
  },
  {
    name: "recur",
    label: "Recurrence",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "e.g. daily, weekly, 3d, 2months",
  },
  {
    name: "priority",
    label: "Priority",
    default: "normal",
    parse: (raw: string) => {
      const v = raw.trim().toLowerCase()
      if (["low", "normal", "high", "urgent"].includes(v)) return v
      return "normal"
    },
    serialize: (value: string | null) => String(value ?? "normal"),
    options: [
      { value: "low", label: "Low" },
      { value: "normal", label: "Normal" },
      { value: "high", label: "High" },
      { value: "urgent", label: "Urgent" },
    ],
  },
  {
    name: "action",
    label: "Wiki action",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "e.g. read:path, edit:path",
  },
  {
    name: "tags",
    label: "Tags",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "description",
    label: "Description",
    default: "",
    wide: true,
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "Optional explanation or intention",
  },
]

// --- API Gate ---

const API_BASE = "/api/plugin/todo/v1"

interface TodoTask {
  id: string
  title: string
  description: string
  status: string
  source: string
  source_page: string
  assignee: { type: string; target: string; resolution: string }
  due_date: string
  recurrence: { type: string; days: number; every: number; unit: string }
  wiki_action: { type: string; page: string }
  tags: string
  priority: string
  created_by: string
  created_at: string
  updated_at: string
  warnings?: string[]
}

const gate = {
  async list(params?: Record<string, string>): Promise<{ tasks: TodoTask[]; cursor: string }> {
    const qs = params ? "?" + new URLSearchParams(params).toString() : ""
    const resp = await fetch(`${API_BASE}/tasks${qs}`)
    if (!resp.ok) throw new Error(`list tasks: ${resp.status}`)
    return resp.json()
  },

  async create(task: Partial<TodoTask>): Promise<TodoTask> {
    const resp = await fetch(`${API_BASE}/tasks`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(task),
    })
    if (!resp.ok) throw new Error(`create task: ${resp.status}`)
    return resp.json()
  },

  async get(id: string): Promise<TodoTask> {
    const resp = await fetch(`${API_BASE}/tasks/${id}`)
    if (!resp.ok) throw new Error(`get task: ${resp.status}`)
    return resp.json()
  },

  async patch(id: string, patch: Partial<TodoTask>): Promise<TodoTask> {
    const resp = await fetch(`${API_BASE}/tasks/${id}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    })
    if (!resp.ok) throw new Error(`patch task: ${resp.status}`)
    return resp.json()
  },

  async complete(id: string): Promise<TodoTask> {
    const resp = await fetch(`${API_BASE}/tasks/${id}/complete`, { method: "POST" })
    if (!resp.ok) throw new Error(`complete task: ${resp.status}`)
    return resp.json()
  },

  async reopen(id: string): Promise<TodoTask> {
    const resp = await fetch(`${API_BASE}/tasks/${id}/reopen`, { method: "POST" })
    if (!resp.ok) throw new Error(`reopen task: ${resp.status}`)
    return resp.json()
  },

  async remove(id: string): Promise<void> {
    const resp = await fetch(`${API_BASE}/tasks/${id}`, { method: "DELETE" })
    if (!resp.ok) throw new Error(`delete task: ${resp.status}`)
  },

  async mine(): Promise<{ tasks: TodoTask[] }> {
    const resp = await fetch(`${API_BASE}/tasks/mine`)
    if (!resp.ok) throw new Error(`my tasks: ${resp.status}`)
    return resp.json()
  },

  async byPage(path: string): Promise<{ tasks: TodoTask[] }> {
    const cleanPath = path.replace(/^\/+/, "")
    const resp = await fetch(`${API_BASE}/tasks/page/${cleanPath}`)
    if (resp.status === 404) throw new Error("todo_not_available")
    if (!resp.ok) throw new Error(`page tasks: ${resp.status}`)
    return resp.json()
  },

  connectSSE(onEvent: (event: { type: string; task: TodoTask }) => void): EventSource | null {
    try {
      const es = new EventSource(`${API_BASE}/stream`)
      const handler = (e: MessageEvent) => {
        try {
          onEvent(JSON.parse(e.data))
        } catch { /* ignore parse errors */ }
      }
      es.addEventListener("task.created", handler)
      es.addEventListener("task.updated", handler)
      es.addEventListener("task.completed", handler)
      es.addEventListener("task.reopened", handler)
      return es
    } catch {
      return null
    }
  },
}

// --- Priority colors ---

const PRIORITY_COLORS: Record<string, { bg: string; fg: string }> = {
  low:    { bg: "#e8f5e9", fg: "#2e7d32" },
  normal: { bg: "#e3f2fd", fg: "#1565c0" },
  high:   { bg: "#fff8e1", fg: "#f57f17" },
  urgent: { bg: "#ffebee", fg: "#c62828" },
}

// --- NodeView ---

class TodoNodeView {
  dom: HTMLElement
  private node: PMNode
  private view: EditorView
  private getPos: () => number | undefined
  private taskId: string | null = null
  private taskStatus: string = "open"
  private taskData: TodoTask | null = null
  private taskInactive: boolean = false
  private eventSource: EventSource | null = null
  private unavailable: boolean = false

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.getPos = getPos

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-todo"
    this.dom.contentEditable = "false"

    this.render()
    this.fetchTask()
    this.connectSSE()
  }

  private render() {
    const node = this.node
    this.dom.innerHTML = ""

    if (this.unavailable) {
      const err = document.createElement("div")
      err.className = "gowiki-todo-chip gowiki-todo-unavailable"
      err.textContent = `Todo: ${node.attrs.title || "(untitled)"} — todo plugin requires a database connection`
      this.dom.appendChild(err)
      return
    }

    const chip = document.createElement("div")
    chip.className = "gowiki-todo-chip"

    // Checkbox
    const cb = document.createElement("input")
    cb.type = "checkbox"
    cb.className = "gowiki-todo-checkbox"
    const status = this.taskStatus
    cb.checked = status === "done"
    // Disable checkbox if no task ID, inactive (page review pending),
    // or if a wiki action is set and user is not admin.
    const hasWikiAction = !!(node.attrs.action)
    const user = (window as any).__gowikiCurrentUser
    const isAdmin = user?.is_admin === true
    cb.disabled = !this.taskId || this.taskInactive || (hasWikiAction && !isAdmin)
    if (this.taskInactive) {
      cb.title = "Page review is pending — task is inactive"
    } else if (hasWikiAction && !isAdmin) {
      cb.title = "This task is completed automatically by a wiki action"
    }
    cb.addEventListener("change", (e) => {
      e.preventDefault()
      e.stopPropagation()
      if (this.taskId) {
        if (cb.checked) {
          gate.complete(this.taskId).then((t) => {
            this.taskStatus = t.status
            this.render()
          }).catch(() => { cb.checked = false })
        } else {
          gate.reopen(this.taskId).then((t) => {
            this.taskStatus = t.status
            this.render()
          }).catch(() => { cb.checked = true })
        }
      }
    })
    chip.appendChild(cb)

    // Title
    const title = document.createElement("span")
    title.className = "gowiki-todo-title"
    title.textContent = node.attrs.title || "(untitled)"
    if (status === "done") title.classList.add("gowiki-todo-done")
    chip.appendChild(title)

    // Assignee
    const assign = node.attrs.assign
    if (assign) {
      const badge = document.createElement("span")
      badge.className = "gowiki-todo-assignee"
      badge.textContent = formatAssigneeSync(assign)
      // Resolve display names asynchronously and update.
      resolveAssigneeLabels(assign).then(label => { badge.textContent = label })
      chip.appendChild(badge)
    }

    // Due date
    const due = node.attrs.due
    if (due) {
      const pill = document.createElement("span")
      pill.className = "gowiki-todo-due"
      const today = new Date().toISOString().slice(0, 10)
      if (due < today && status !== "done") {
        pill.classList.add("gowiki-todo-overdue")
      }
      pill.textContent = due
      chip.appendChild(pill)
    }

    // Priority badge
    const priority = node.attrs.priority || "normal"
    if (priority !== "normal") {
      const pBadge = document.createElement("span")
      pBadge.className = "gowiki-todo-priority"
      const colors = PRIORITY_COLORS[priority] || PRIORITY_COLORS.normal
      pBadge.style.background = colors.bg
      pBadge.style.color = colors.fg
      pBadge.textContent = priority
      chip.appendChild(pBadge)
    }

    // Wiki action link
    const actionAttr = node.attrs.action
    if (actionAttr) {
      const actionBadge = document.createElement("span")
      actionBadge.className = "gowiki-todo-action"
      const colonIdx = actionAttr.indexOf(":")
      const actionType = colonIdx >= 0 ? actionAttr.slice(0, colonIdx) : actionAttr
      const actionPage = colonIdx >= 0 ? actionAttr.slice(colonIdx + 1) : ""
      if (actionPage) {
        const label = actionType.charAt(0).toUpperCase() + actionType.slice(1)
        actionBadge.textContent = label + " "
        const link = document.createElement("a")
        link.href = actionPage
        link.textContent = actionPage
        link.className = "gowiki-todo-action-link"
        link.addEventListener("click", (e) => {
          e.preventDefault()
          e.stopPropagation()
          window.location.href = actionPage
        })
        actionBadge.appendChild(link)
      } else {
        actionBadge.textContent = actionAttr
      }
      chip.appendChild(actionBadge)
    }

    // Status indicator for done/cancelled/inactive
    if (this.taskInactive) {
      chip.classList.add("gowiki-todo-inactive")
    } else if (status === "done") {
      chip.style.opacity = "0.7"
    } else if (status === "cancelled") {
      chip.style.opacity = "0.5"
      chip.style.textDecoration = "line-through"
    }

    this.dom.appendChild(chip)

    // Description (optional)
    const desc = node.attrs.description
    if (desc) {
      const descEl = document.createElement("div")
      descEl.className = "gowiki-todo-description"
      descEl.textContent = desc
      this.dom.appendChild(descEl)
    }

    // Warnings from backend validation
    if (this.taskData?.warnings?.length) {
      for (const w of this.taskData.warnings) {
        const warn = document.createElement("div")
        warn.className = "gowiki-todo-warning"
        warn.textContent = "\u26A0 " + w
        this.dom.appendChild(warn)
      }
    }
  }

  private async fetchTask() {
    // Try to find the task by page path
    const pagePath = window.location.pathname.replace(/^\//, "").replace(/\/$/, "")
    try {
      const data = await gate.byPage(pagePath)
      const title = this.node.attrs.title || ""
      const match = data.tasks.find((t: TodoTask) => t.title === title)
      if (match) {
        this.taskId = match.id
        this.taskStatus = match.status
        this.taskData = match
        this.taskInactive = !!(match as any).inactive
        this.render()
      }
    } catch (err: any) {
      if (err?.message === "todo_not_available") {
        this.unavailable = true
        this.render()
      }
      // Otherwise: task not found in backend — possibly not synced yet
    }
  }

  private connectSSE() {
    this.eventSource = gate.connectSSE((event) => {
      if (this.taskId && event.task?.id === this.taskId) {
        this.taskStatus = event.task.status
        this.render()
      }
    })
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    this.node = node
    this.render()
    if (!this.taskId) this.fetchTask()
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
    if (this.eventSource) {
      this.eventSource.close()
      this.eventSource = null
    }
  }
}

// --- Todo List Properties ---

const todoListProperties = [
  {
    name: "assign",
    label: "Assignee filter",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "e.g. @alice or @quality-team (empty = current user)",
  },
  {
    name: "status",
    label: "Status filter",
    default: "open,in_progress",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? "open,in_progress"),
    helpText: "Comma-separated: open, in_progress, done, cancelled",
  },
  {
    name: "priority",
    label: "Priority filter",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "Comma-separated: low, normal, high, urgent",
  },
  {
    name: "tag",
    label: "Tag filter",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
  },
  {
    name: "due_before",
    label: "Due before",
    default: "",
    parse: (raw: string) => raw.trim(),
    serialize: (value: string | null) => String(value ?? ""),
    helpText: "YYYY-MM-DD",
  },
  {
    name: "limit",
    label: "Max items",
    default: "20",
    parse: (raw: string) => {
      const n = parseInt(raw.trim(), 10)
      return (isNaN(n) || n < 1) ? "20" : n > 100 ? "100" : String(n)
    },
    serialize: (value: string | null) => String(value ?? "20"),
  },
]

// --- Todo List NodeView ---

class TodoListNodeView {
  dom: HTMLElement
  private node: PMNode
  private view: EditorView
  private getPos: () => number | undefined
  private eventSource: EventSource | null = null

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.getPos = getPos

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-todo-list"
    this.dom.contentEditable = "false"

    this.fetchAndRender()
  }

  private async fetchAndRender() {
    const user = (window as any).__gowikiCurrentUser
    if (!user?.username) {
      this.dom.innerHTML = ""
      return
    }

    const attrs = this.node.attrs
    const params: Record<string, string> = {}
    params.assignee = attrs.assign ? attrs.assign.replace(/^@/, "") : user.username
    if (attrs.status) params.status = attrs.status
    if (attrs.priority) params.priority = attrs.priority
    if (attrs.tag) params.tag = attrs.tag
    if (attrs.due_before) params.due_before = attrs.due_before
    params.limit = attrs.limit || "20"

    try {
      const data = await gate.list(params)
      this.renderTasks(data.tasks || [])
    } catch {
      // Plugin unavailable — show message
      this.dom.innerHTML = ""
      const msg = document.createElement("div")
      msg.style.cssText = "padding:8px 12px;background:#fff3e0;border:1px solid #ffb74d;border-radius:6px;color:#e65100;font-size:13px"
      msg.textContent = "Todo list: requires a database connection"
      this.dom.appendChild(msg)
    }

    // Connect SSE for live updates
    if (!this.eventSource) {
      this.eventSource = gate.connectSSE(() => {
        this.fetchAndRender()
      })
    }
  }

  private renderTasks(tasks: TodoTask[]) {
    this.dom.innerHTML = ""

    if (tasks.length === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-todo-list-empty"
      empty.textContent = "No pending tasks"
      this.dom.appendChild(empty)
      return
    }

    const table = document.createElement("table")
    table.className = "gowiki-todo-list-table"

    const thead = document.createElement("thead")
    const hr = document.createElement("tr")
    for (const col of ["TODO", "Title", "Assignee", "Due", "Priority", "Status"]) {
      const th = document.createElement("th")
      th.textContent = col
      hr.appendChild(th)
    }
    thead.appendChild(hr)
    table.appendChild(thead)

    const tbody = document.createElement("tbody")
    for (const task of tasks) {
      const tr = document.createElement("tr")
      tr.className = "gowiki-todo-list-row"

      // Checkbox
      const tdCb = document.createElement("td")
      const cb = document.createElement("input")
      cb.type = "checkbox"
      cb.checked = task.status === "done"
      cb.disabled = task.status === "done" || task.status === "cancelled"
      cb.addEventListener("change", async (e) => {
        e.preventDefault()
        try {
          if (cb.checked) {
            await gate.complete(task.id)
          } else {
            await gate.reopen(task.id)
          }
          this.fetchAndRender()
        } catch {
          cb.checked = !cb.checked
        }
      })
      tdCb.appendChild(cb)
      tr.appendChild(tdCb)

      // Title (linked to source page)
      const tdTitle = document.createElement("td")
      if (task.source_page) {
        const link = document.createElement("a")
        const pagePath = task.source_page.startsWith("/") ? task.source_page : "/" + task.source_page
        link.href = pagePath
        link.textContent = task.title
        link.addEventListener("click", (e) => {
          e.preventDefault()
          window.location.href = pagePath
        })
        tdTitle.appendChild(link)
      } else {
        tdTitle.textContent = task.title
      }
      if (task.status === "done") tdTitle.style.textDecoration = "line-through"
      tr.appendChild(tdTitle)

      // Assignee
      const tdAssign = document.createElement("td")
      const assignTarget = task.assignee?.target || ""
      tdAssign.textContent = formatAssigneeSync(assignTarget)
      if (assignTarget) {
        resolveAssigneeLabels(assignTarget).then(label => { tdAssign.textContent = label })
      }
      tr.appendChild(tdAssign)

      // Due date
      const tdDue = document.createElement("td")
      if (task.due_date) {
        const dateStr = task.due_date.slice(0, 10)
        tdDue.textContent = dateStr
        const today = new Date().toISOString().slice(0, 10)
        if (dateStr < today && task.status !== "done") {
          tdDue.className = "gowiki-todo-list-overdue"
        }
      }
      tr.appendChild(tdDue)

      // Priority
      const tdPri = document.createElement("td")
      const priority = task.priority || "normal"
      if (priority !== "normal") {
        const badge = document.createElement("span")
        badge.className = "gowiki-todo-priority"
        const colors = PRIORITY_COLORS[priority] || PRIORITY_COLORS.normal
        badge.style.background = colors.bg
        badge.style.color = colors.fg
        badge.textContent = priority
        tdPri.appendChild(badge)
      }
      tr.appendChild(tdPri)

      // Status
      const tdStatus = document.createElement("td")
      tdStatus.textContent = task.status
      tr.appendChild(tdStatus)

      tbody.appendChild(tr)
    }
    table.appendChild(tbody)
    this.dom.appendChild(table)
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    const attrsChanged = JSON.stringify(node.attrs) !== JSON.stringify(this.node.attrs)
    this.node = node
    if (attrsChanged) this.fetchAndRender()
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
    if (this.eventSource) {
      this.eventSource.close()
      this.eventSource = null
    }
  }
}

// --- Todo Calendar NodeView ---

const STATUS_ICONS: Record<string, string> = {
  open: "○",
  in_progress: "◐",
  done: "●",
  cancelled: "✕",
}
const STATUS_COLORS: Record<string, string> = {
  open: "#1565c0",
  in_progress: "#f57f17",
  done: "#2e7d32",
  cancelled: "#9e9e9e",
}

const MONTH_NAMES = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

class TodoCalendarNodeView {
  dom: HTMLElement
  private node: PMNode
  private view: EditorView
  private getPos: () => number | undefined
  private currentYear: number

  constructor(node: PMNode, view: EditorView, getPos: () => number | undefined) {
    this.node = node
    this.view = view
    this.getPos = getPos
    this.currentYear = parseInt(node.attrs.year) || new Date().getFullYear()

    this.dom = document.createElement("div")
    this.dom.className = "gowiki-todo-calendar"
    this.dom.contentEditable = "false"

    this.fetchAndRender()
  }

  private async fetchAndRender() {
    const user = (window as any).__gowikiCurrentUser
    if (!user?.username) {
      this.dom.innerHTML = ""
      return
    }

    const ns = this.node.attrs.namespace || "/"
    const depth = parseInt(this.node.attrs.depth) || 0

    const params = new URLSearchParams()
    params.set("page_prefix", ns)
    params.set("due_after", `${this.currentYear}-01-01`)
    params.set("due_before", `${this.currentYear}-12-31`)
    params.set("status", "open,in_progress,done")
    params.set("limit", "200")

    let allTasks: TodoTask[] = []
    let cursor = ""
    try {
      // Paginate to get all tasks for the year.
      do {
        if (cursor) params.set("cursor", cursor)
        const data = await gate.list(Object.fromEntries(params))
        allTasks = allTasks.concat(data.tasks || [])
        cursor = data.cursor || ""
      } while (cursor && allTasks.length < 2000)
    } catch {
      this.dom.innerHTML = ""
      const msg = document.createElement("div")
      msg.style.cssText = "padding:8px 12px;background:#fff3e0;border:1px solid #ffb74d;border-radius:6px;color:#e65100;font-size:13px"
      msg.textContent = "Todo calendar: requires a database connection"
      this.dom.appendChild(msg)
      return
    }

    // Filter to only tasks with due dates.
    const tasks = allTasks.filter(t => t.due_date)

    // Group by page, then by month.
    const pageMonthMap = new Map<string, Map<number, TodoTask[]>>()
    for (const t of tasks) {
      const page = t.source_page || "(no page)"
      const month = parseInt(t.due_date.slice(5, 7)) - 1 // 0-indexed
      if (!pageMonthMap.has(page)) pageMonthMap.set(page, new Map())
      const months = pageMonthMap.get(page)!
      if (!months.has(month)) months.set(month, [])
      months.get(month)!.push(t)
    }

    // Build folder tree for grouping.
    const nsClean = ns.replace(/\/+$/, "")
    const pages = Array.from(pageMonthMap.keys()).sort()

    interface FolderNode { label: string; pages: string[]; children: Map<string, FolderNode> }
    const root: FolderNode = { label: "", pages: [], children: new Map() }

    for (const page of pages) {
      const rel = page.startsWith(nsClean) ? page.slice(nsClean.length) : page
      const parts = rel.replace(/^\/+/, "").split("/")

      let maxParts = depth > 0 ? depth : parts.length
      const folderParts = parts.slice(0, Math.min(maxParts, parts.length - 1))
      let current = root
      for (const part of folderParts) {
        if (!current.children.has(part)) {
          current.children.set(part, { label: part, pages: [], children: new Map() })
        }
        current = current.children.get(part)!
      }
      current.pages.push(page)
    }

    this.renderCalendar(root, pageMonthMap, nsClean)
  }

  private renderCalendar(
    root: { label: string; pages: string[]; children: Map<string, any> },
    pageMonthMap: Map<string, Map<number, TodoTask[]>>,
    nsClean: string,
  ) {
    this.dom.innerHTML = ""

    // Year navigation.
    const nav = document.createElement("div")
    nav.className = "gowiki-todo-cal-nav"
    const prevBtn = document.createElement("button")
    prevBtn.textContent = "◀"
    prevBtn.addEventListener("click", () => { this.currentYear--; this.fetchAndRender() })
    const yearLabel = document.createElement("span")
    yearLabel.className = "gowiki-todo-cal-year"
    yearLabel.textContent = String(this.currentYear)
    const nextBtn = document.createElement("button")
    nextBtn.textContent = "▶"
    nextBtn.addEventListener("click", () => { this.currentYear++; this.fetchAndRender() })
    nav.appendChild(prevBtn)
    nav.appendChild(yearLabel)
    nav.appendChild(nextBtn)
    this.dom.appendChild(nav)

    if (pageMonthMap.size === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-todo-cal-empty"
      empty.textContent = "No todos with due dates in " + this.currentYear
      this.dom.appendChild(empty)
      return
    }

    // Table.
    const table = document.createElement("table")
    table.className = "gowiki-todo-cal-table"

    // Header row.
    const thead = document.createElement("thead")
    const hr = document.createElement("tr")
    const thPage = document.createElement("th")
    thPage.textContent = "Page"
    thPage.className = "gowiki-todo-cal-page-col"
    hr.appendChild(thPage)
    for (const m of MONTH_NAMES) {
      const th = document.createElement("th")
      th.textContent = m
      th.className = "gowiki-todo-cal-month-col"
      hr.appendChild(th)
    }
    thead.appendChild(hr)
    table.appendChild(thead)

    const tbody = document.createElement("tbody")

    const renderPageRow = (page: string, indentLevel: number) => {
      const months = pageMonthMap.get(page)
      if (!months) return

      const tr = document.createElement("tr")

      // Page cell.
      const tdPage = document.createElement("td")
      tdPage.className = "gowiki-todo-cal-page-cell"
      tdPage.style.paddingLeft = (8 + indentLevel * 16) + "px"
      const pageLink = document.createElement("a")
      const displayPath = page.startsWith(nsClean) ? page.slice(nsClean.length) : page
      pageLink.textContent = displayPath.replace(/^\/+/, "") || page
      pageLink.href = page
      pageLink.addEventListener("click", (e) => {
        e.preventDefault()
        window.location.href = page
      })
      tdPage.appendChild(pageLink)
      tr.appendChild(tdPage)

      // Month cells.
      const today = new Date().toISOString().slice(0, 10)
      for (let m = 0; m < 12; m++) {
        const td = document.createElement("td")
        td.className = "gowiki-todo-cal-cell"
        const cellTasks = months.get(m)
        if (cellTasks && cellTasks.length > 0) {
          for (const t of cellTasks) {
            const chip = document.createElement("div")
            chip.className = "gowiki-todo-cal-chip"
            const overdue = (t.status === "open" || t.status === "in_progress") &&
              t.due_date && t.due_date.slice(0, 10) < today
            if (overdue) chip.classList.add("gowiki-todo-cal-chip-overdue")
            if (t.status === "done") chip.classList.add("gowiki-todo-cal-chip-done")
            const icon = document.createElement("span")
            icon.textContent = STATUS_ICONS[t.status] || "○"
            icon.style.color = overdue ? "#c62828" : (STATUS_COLORS[t.status] || "#666")
            chip.appendChild(icon)
            const label = document.createElement("span")
            label.className = "gowiki-todo-cal-chip-label"
            label.textContent = t.title
            chip.appendChild(label)
            const assignee = formatAssigneeSync(t.assignee?.target || "")
            chip.title = `${t.title} (${t.status})\n${assignee}`
            if (t.assignee?.target) {
              resolveAssigneeLabels(t.assignee.target).then(resolved => {
                chip.title = `${t.title} (${t.status})\n${resolved}`
              })
            }
            td.appendChild(chip)
          }
        }
        tr.appendChild(td)
      }

      tbody.appendChild(tr)
    }

    const renderFolder = (folder: { label: string; pages: string[]; children: Map<string, any> }, indentLevel: number) => {
      const sortedPages = folder.pages.slice().sort()
      // Pages ending with a trailing slash are the folder's own index —
      // render them first inside the folder.
      const indexPages = sortedPages.filter(p => p.endsWith("/"))
      const leafPages = sortedPages.filter(p => !p.endsWith("/"))

      for (const page of indexPages) {
        renderPageRow(page, indentLevel)
      }

      // For each child folder, check whether a sibling leaf page has the
      // same name (DokuWiki-style "eponym" namespace index). If so, render
      // that leaf right before the folder header to keep them grouped.
      const usedLeaves = new Set<string>()
      const sortedChildren = Array.from(folder.children.entries()).sort(([a], [b]) => a.localeCompare(b))

      for (const [childName, child] of sortedChildren) {
        const eponym = leafPages.find(p => {
          const segs = p.replace(/\/+$/, "").split("/")
          return segs[segs.length - 1] === childName
        })
        if (eponym) {
          renderPageRow(eponym, indentLevel)
          usedLeaves.add(eponym)
        }

        const folderRow = document.createElement("tr")
        folderRow.className = "gowiki-todo-cal-folder-row"
        const folderTd = document.createElement("td")
        folderTd.colSpan = 13
        folderTd.style.paddingLeft = (8 + indentLevel * 16) + "px"
        folderTd.textContent = "📁 " + child.label
        folderRow.appendChild(folderTd)
        tbody.appendChild(folderRow)

        renderFolder(child, indentLevel + 1)
      }

      // Remaining leaves (no eponym folder).
      for (const page of leafPages) {
        if (!usedLeaves.has(page)) renderPageRow(page, indentLevel)
      }
    }

    renderFolder(root, 0)
    table.appendChild(tbody)
    this.dom.appendChild(table)
  }

  update(node: PMNode): boolean {
    if (node.type !== this.node.type) return false
    const attrsChanged = JSON.stringify(node.attrs) !== JSON.stringify(this.node.attrs)
    this.node = node
    if (attrsChanged) {
      this.currentYear = parseInt(node.attrs.year) || new Date().getFullYear()
      this.fetchAndRender()
    }
    return true
  }

  stopEvent(event: Event): boolean {
    const type = event.type
    if (type === "mousedown" || type === "mouseup" || type === "click") return false
    return true
  }

  ignoreMutation(): boolean { return true }

  destroy() {}
}

// --- Styles ---

const todoStyles = `
.gowiki-todo {
  margin: 4px 0;
}

.gowiki-todo-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 12px;
  border: 1px solid #e0e0e0;
  border-radius: 6px;
  background: #fafafa;
  font-size: 14px;
  line-height: 1.4;
}

.gowiki-todo-checkbox {
  cursor: pointer;
  width: 16px;
  height: 16px;
  margin: 0;
  flex-shrink: 0;
}

.gowiki-todo-title {
  font-weight: 500;
}

.gowiki-todo-title.gowiki-todo-done {
  text-decoration: line-through;
  color: #999;
}

.gowiki-todo-assignee {
  background: #e8f0fe;
  color: #1a56db;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 12px;
  font-weight: 500;
}

.gowiki-todo-due {
  background: #f0f0f0;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 12px;
  color: #555;
}

.gowiki-todo-due.gowiki-todo-overdue {
  background: #ffebee;
  color: #c62828;
  font-weight: 600;
}

.gowiki-todo-priority {
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
}

.gowiki-todo-description {
  margin: 2px 0 0 28px;
  font-size: 13px;
  color: #666;
  line-height: 1.4;
}

.gowiki-todo-action {
  background: #f3e8ff;
  color: #7c3aed;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 12px;
  font-weight: 500;
}

.gowiki-todo-action-link {
  color: #5b21b6;
  text-decoration: underline;
  cursor: pointer;
}

.gowiki-todo-action-link:hover {
  color: #4c1d95;
}

.gowiki-todo-warning {
  margin: 2px 0 0 28px;
  font-size: 12px;
  color: #e65100;
  line-height: 1.4;
}

.gowiki-todo-unavailable {
  background: #fff3e0;
  border-color: #ffb74d;
  color: #e65100;
  font-style: italic;
}

.gowiki-todo-inactive {
  opacity: 0.5;
  border-style: dashed;
}

#app.gowiki-editing .gowiki-todo.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}

/* Panel styles */
.gowiki-todo-panel {
  padding: 12px;
  font-size: 14px;
}

.gowiki-todo-panel h3 {
  margin: 0 0 8px 0;
  font-size: 15px;
  font-weight: 600;
}

.gowiki-todo-panel-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.gowiki-todo-panel-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  border-bottom: 1px solid #f0f0f0;
}

.gowiki-todo-panel-item:last-child {
  border-bottom: none;
}

.gowiki-todo-panel-empty {
  color: #999;
  font-style: italic;
}

/* Todo list */
.gowiki-todo-list {
  margin: 8px 0;
}

.gowiki-todo-list-empty {
  color: #999;
  font-style: italic;
  font-size: 13px;
  padding: 4px 0;
}

.gowiki-todo-list-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

.gowiki-todo-list-table th {
  text-align: left;
  font-weight: 600;
  font-size: 12px;
  color: #666;
  padding: 4px 8px;
  border-bottom: 2px solid #e0e0e0;
}

.gowiki-todo-list-table td {
  padding: 4px 8px;
  border-bottom: 1px solid #f0f0f0;
}

.gowiki-todo-list-table a {
  color: #1a56db;
  text-decoration: none;
}

.gowiki-todo-list-table a:hover {
  text-decoration: underline;
}

.gowiki-todo-list-overdue {
  color: #c62828;
  font-weight: 600;
}

.gowiki-todo-list-row input[type="checkbox"] {
  cursor: pointer;
  width: 15px;
  height: 15px;
}

#app.gowiki-editing .gowiki-todo-list.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}

/* Badge */
.gowiki-todo-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #e53935;
  color: white;
  font-size: 11px;
  font-weight: 700;
  min-width: 18px;
  height: 18px;
  border-radius: 9px;
  padding: 0 4px;
}

/* Calendar */
.gowiki-todo-calendar {
  margin: 12px 0;
}

.gowiki-todo-cal-nav {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}

.gowiki-todo-cal-nav button {
  background: none;
  border: 1px solid #ccc;
  border-radius: 4px;
  cursor: pointer;
  padding: 2px 8px;
  font-size: 14px;
}

.gowiki-todo-cal-nav button:hover {
  background: #f0f0f0;
}

.gowiki-todo-cal-year {
  font-weight: 700;
  font-size: 16px;
}

.gowiki-todo-cal-empty {
  color: #999;
  font-style: italic;
  font-size: 13px;
  padding: 4px 0;
}

.gowiki-todo-cal-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
  table-layout: fixed;
}

.gowiki-todo-cal-page-col {
  width: 200px;
  text-align: left;
  font-weight: 600;
  padding: 4px 8px;
  border-bottom: 2px solid #e0e0e0;
}

.gowiki-todo-cal-month-col {
  text-align: center;
  font-weight: 600;
  font-size: 11px;
  color: #666;
  padding: 4px 2px;
  border-bottom: 2px solid #e0e0e0;
}

.gowiki-todo-cal-page-cell {
  padding: 4px 8px;
  border-bottom: 1px solid #f0f0f0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.gowiki-todo-cal-page-cell a {
  color: #1a56db;
  text-decoration: none;
}

.gowiki-todo-cal-page-cell a:hover {
  text-decoration: underline;
}

.gowiki-todo-cal-cell {
  padding: 2px;
  border-bottom: 1px solid #f0f0f0;
  vertical-align: top;
}

.gowiki-todo-cal-chip {
  display: flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
}

.gowiki-todo-cal-chip-label {
  overflow: hidden;
  text-overflow: ellipsis;
  color: #555;
}

.gowiki-todo-cal-chip-overdue .gowiki-todo-cal-chip-label {
  color: #c62828;
  font-weight: 600;
}

.gowiki-todo-cal-chip-done {
  opacity: 0.45;
}

.gowiki-todo-cal-chip-done .gowiki-todo-cal-chip-label {
  color: #888;
  text-decoration: line-through;
}

.gowiki-todo-cal-folder-row td {
  font-weight: 600;
  font-size: 12px;
  color: #444;
  padding: 6px 8px 2px;
  border-bottom: 1px solid #e0e0e0;
  background: #fafafa;
}

#app.gowiki-editing .gowiki-todo-calendar.ProseMirror-selectednode {
  outline: 2px solid #ffd43b;
  outline-offset: 1px;
}
`

// --- Plugin ---

export const todoPlugin: WikiPlugin = {
  register(reg) {
    // Schema node
    reg.registerSchema({
      nodes: {
        todo: {
          group: "block",
          atom: true,
          attrs: {
            title: { default: "" },
            assign: { default: "" },
            resolution: { default: "any" },
            due: { default: "" },
            recur: { default: "" },
            priority: { default: "normal" },
            action: { default: "" },
            tags: { default: "" },
            description: { default: "" },
            task_id: { default: "" },
            task_status: { default: "open" },
          },
          toDOM(node: PMNode) {
            return [
              "div",
              {
                class: "gowiki-todo",
                "data-title": node.attrs.title || "",
                "data-assign": node.attrs.assign || "",
                "data-priority": node.attrs.priority || "normal",
              },
              `Todo: ${node.attrs.title || "(untitled)"}`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-todo",
              getAttrs(dom: HTMLElement) {
                return {
                  title: dom.getAttribute("data-title") || "",
                  assign: dom.getAttribute("data-assign") || "",
                  priority: dom.getAttribute("data-priority") || "normal",
                }
              },
            },
          ],
        },
      },
    })

    // Self-contained directive: {todo title="..." assign="@..." ...}
    reg.registerSelfContainedDirective("todo", {
      tokenType: "todo",
      nodeType: "todo",
      properties: todoProperties,
    })

    // Markdown → PM: handle the synthetic "todo" token
    reg.registerText("todo", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.todo.create({
            title: attrs.title ?? "",
            assign: attrs.assign ?? "",
            resolution: attrs.resolution ?? "any",
            due: attrs.due ?? "",
            recur: attrs.recur ?? "",
            priority: attrs.priority ?? "normal",
            action: attrs.action ?? "",
            tags: attrs.tags ?? "",
            description: attrs.description ?? "",
          })
        )
      },
    })

    // PM → Markdown: serialize todo node back to directive syntax (bijective)
    reg.registerPMNode("todo", {
      print(node) {
        const parts: string[] = []

        // title is always emitted (required)
        const title = node.attrs.title || ""
        parts.push(`title="${title}"`)

        // Only emit non-default values
        if (node.attrs.assign) parts.push(`assign="${node.attrs.assign}"`)
        if (node.attrs.resolution && node.attrs.resolution !== "any") {
          parts.push(`resolution=${node.attrs.resolution}`)
        }
        if (node.attrs.due) parts.push(`due=${node.attrs.due}`)
        if (node.attrs.recur) parts.push(`recur=${node.attrs.recur}`)
        if (node.attrs.priority && node.attrs.priority !== "normal") {
          parts.push(`priority=${node.attrs.priority}`)
        }
        if (node.attrs.action) parts.push(`action="${node.attrs.action}"`)
        if (node.attrs.tags) parts.push(`tags="${node.attrs.tags}"`)
        if (node.attrs.description) parts.push(`description="${node.attrs.description}"`)

        return `{todo ${parts.join(" ")}}\n\n`
      },
    })

    // Editor plugin: NodeView
    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.todo"),
        props: {
          nodeViews: {
            todo(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TodoNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    // Command: insert a new todo node and open properties panel
    reg.registerCommand("todo", "insert", (state, dispatch) => {
      const todoType = reg.schema.nodes.todo
      if (!todoType) return false
      if (dispatch) {
        const node = todoType.create({ title: "" })
        requestInputFocus("title")
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        // Search backward from the mapped position to find the just-inserted todo.
        // We search backward because replaceSelectionWith places the cursor after
        // the inserted node, so the todo is just before approxPos.
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 200),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === todoType) {
              // Keep updating — we want the LAST (closest to approxPos) todo, not the first
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

    // --- Todo List node ---

    reg.registerSchema({
      nodes: {
        todo_list: {
          group: "block",
          atom: true,
          attrs: {
            assign: { default: "" },
            status: { default: "open,in_progress" },
            priority: { default: "" },
            tag: { default: "" },
            due_before: { default: "" },
            limit: { default: "20" },
          },
          toDOM(node: PMNode) {
            const summary = node.attrs.assign ? `for ${node.attrs.assign}` : "for current user"
            return [
              "div",
              { class: "gowiki-todo-list", "data-assign": node.attrs.assign || "" },
              `Todo List (${summary})`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-todo-list",
              getAttrs(dom: HTMLElement) {
                return { assign: dom.getAttribute("data-assign") || "" }
              },
            },
          ],
        },
      },
    })

    reg.registerSelfContainedDirective("todo-list", {
      tokenType: "todo_list",
      nodeType: "todo_list",
      properties: todoListProperties,
    })

    reg.registerText("todo_list", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.todo_list.create({
            assign: attrs.assign ?? "",
            status: attrs.status ?? "open,in_progress",
            priority: attrs.priority ?? "",
            tag: attrs.tag ?? "",
            due_before: attrs.due_before ?? "",
            limit: attrs.limit ?? "20",
          })
        )
      },
    })

    reg.registerPMNode("todo_list", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.assign) parts.push(`assign="${node.attrs.assign}"`)
        if (node.attrs.status && node.attrs.status !== "open,in_progress") parts.push(`status="${node.attrs.status}"`)
        if (node.attrs.priority) parts.push(`priority="${node.attrs.priority}"`)
        if (node.attrs.tag) parts.push(`tag="${node.attrs.tag}"`)
        if (node.attrs.due_before) parts.push(`due_before=${node.attrs.due_before}`)
        if (node.attrs.limit && node.attrs.limit !== "20") parts.push(`limit=${node.attrs.limit}`)
        return `{todo-list${parts.length ? " " + parts.join(" ") : ""}}\n\n`
      },
    })

    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.todolist"),
        props: {
          nodeViews: {
            todo_list(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TodoListNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    reg.registerCommand("todo-list", "insert", (state, dispatch) => {
      const listType = reg.schema.nodes.todo_list
      if (!listType) return false
      if (dispatch) {
        const node = listType.create({})
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 200),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === listType) insertedAt = pos
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

    // --- Todo Calendar node ---

    const todoCalendarProperties = [
      {
        name: "namespace",
        label: "Namespace",
        default: "/",
        parse: (raw: string) => raw.trim(),
        serialize: (value: string | null) => String(value ?? "/"),
        helpText: "Root namespace, e.g. /projects/",
      },
      {
        name: "year",
        label: "Year",
        default: "",
        parse: (raw: string) => raw.trim(),
        serialize: (value: string | null) => String(value ?? ""),
        helpText: "Default: current year",
      },
      {
        name: "depth",
        label: "Folder depth",
        default: "",
        parse: (raw: string) => raw.trim(),
        serialize: (value: string | null) => String(value ?? ""),
        helpText: "Max subfolder levels (0 = unlimited)",
      },
    ]

    reg.registerSchema({
      nodes: {
        todo_calendar: {
          group: "block",
          atom: true,
          attrs: {
            namespace: { default: "/" },
            year: { default: "" },
            depth: { default: "" },
          },
          toDOM(node: PMNode) {
            const ns = node.attrs.namespace || "/"
            return [
              "div",
              { class: "gowiki-todo-calendar", "data-namespace": ns },
              `Todo Calendar (${ns})`,
            ]
          },
          parseDOM: [
            {
              tag: "div.gowiki-todo-calendar",
              getAttrs(dom: HTMLElement) {
                return { namespace: dom.getAttribute("data-namespace") || "/" }
              },
            },
          ],
        },
      },
    })

    reg.registerSelfContainedDirective("todo-calendar", {
      tokenType: "todo_calendar",
      nodeType: "todo_calendar",
      properties: todoCalendarProperties,
    })

    reg.registerText("todo_calendar", {
      run(ctx, tok) {
        const attrs = tok.meta?.attrs ?? {}
        ctx.push(
          ctx.schema.nodes.todo_calendar.create({
            namespace: attrs.namespace ?? "/",
            year: attrs.year ?? "",
            depth: attrs.depth ?? "",
          })
        )
      },
    })

    reg.registerPMNode("todo_calendar", {
      print(node) {
        const parts: string[] = []
        if (node.attrs.namespace && node.attrs.namespace !== "/") parts.push(`namespace="${node.attrs.namespace}"`)
        if (node.attrs.year) parts.push(`year=${node.attrs.year}`)
        if (node.attrs.depth) parts.push(`depth=${node.attrs.depth}`)
        return `{todo-calendar${parts.length ? " " + parts.join(" ") : ""}}\n\n`
      },
    })

    reg.registerEditorPlugin((_schema: Schema) => {
      return new PMPlugin({
        key: new PluginKey("gowiki.todocalendar"),
        props: {
          nodeViews: {
            todo_calendar(node: PMNode, view: EditorView, getPos: () => number | undefined) {
              return new TodoCalendarNodeView(node, view, getPos)
            },
          },
        },
      })
    })

    reg.registerCommand("todo-calendar", "insert", (state, dispatch) => {
      const calType = reg.schema.nodes.todo_calendar
      if (!calType) return false
      if (dispatch) {
        const node = calType.create({})
        let tr = state.tr.replaceSelectionWith(node)
        const approxPos = tr.mapping.map(state.selection.from)
        let insertedAt: number | null = null
        tr.doc.nodesBetween(
          Math.max(0, approxPos - 200),
          Math.min(tr.doc.content.size, approxPos + 5),
          (n, pos) => {
            if (n.type === calType) insertedAt = pos
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
    reg.registerStyle("todo", todoStyles)

    // Expose gate for external use
    if (typeof window !== "undefined") {
      ;(window as any).__gowiki_plugins = (window as any).__gowiki_plugins || {}
      ;(window as any).__gowiki_plugins.todo = { gate }
    }
  },
}
