import { Plugin as PMPlugin, PluginKey, NodeSelection } from "prosemirror-state"
import type { Node as PMNode, Schema } from "prosemirror-model"
import { EditorView } from "prosemirror-view"
import type { Plugin as WikiPlugin, Registry } from "../compiler/registry"
import { enablePropertiesPanel } from "../compiler/core_ui"

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
      badge.textContent = assign
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
      // Plugin unavailable or error — render nothing
      this.dom.innerHTML = ""
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
    for (const col of ["", "Title", "Assignee", "Due", "Priority", "Status"]) {
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
        link.href = "/" + task.source_page
        link.textContent = task.title
        link.addEventListener("click", (e) => {
          e.preventDefault()
          window.location.href = "/" + task.source_page
        })
        tdTitle.appendChild(link)
      } else {
        tdTitle.textContent = task.title
      }
      if (task.status === "done") tdTitle.style.textDecoration = "line-through"
      tr.appendChild(tdTitle)

      // Assignee
      const tdAssign = document.createElement("td")
      tdAssign.textContent = task.assignee?.target || ""
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

    // Styles
    reg.registerStyle("todo", todoStyles)

    // Expose gate for external use
    if (typeof window !== "undefined") {
      ;(window as any).__gowiki_plugins = (window as any).__gowiki_plugins || {}
      ;(window as any).__gowiki_plugins.todo = { gate }
    }
  },
}
