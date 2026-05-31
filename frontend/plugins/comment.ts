import type { Plugin as WikiPlugin, Registry } from "../compiler/registry"
import { Plugin as PMPlugin, PluginKey } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"
import type { EditorView } from "prosemirror-view"
import type { Node as PMNode } from "prosemirror-model"
import {
  type AnchorRange,
  rangeFromPm,
  resolveRangeInPm,
  domSelectionToPmRange,
} from "../compiler/anchor"

const API_BASE = "/api/plugin/comment/v1"

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;")
}

// ── Data shapes ─────────────────────────────────────────────────────────────

interface CommentAnchor {
  selected: string
  before: string
  after: string
  // Structural anchor over the document model — added in v0.95.
  // Legacy comments only carry {selected, before, after} and fall back to
  // text-quote search.
  address?: AnchorRange
}

interface CommentEntry {
  id: string
  anchor: CommentAnchor
  text: string
  author: string
  created_at: string
  updated_at: string
  resolved: boolean
  ai?: boolean
  // When set, this entry is a reply to the referenced top-level comment.
  // Replies inherit the parent's anchor and have no Resolved/highlight of their own.
  parent_id?: string
}

// ── PM decoration plugin ────────────────────────────────────────────────────
// Highlights are rendered as ProseMirror inline decorations, not DOM mutations.
// This keeps comments anchored against the document model (mermaid-proof) and
// satisfies the "decorations only, never direct DOM" invariant from CLAUDE.md.

const COMMENT_KEY = new PluginKey<DecorationState>("comments")
const REFRESH = "refresh"
const TEMP_COMMENT_ID = "__comment_new__"

interface DecorationState {
  decos: DecorationSet
  orphanedIds: Set<string>
}

let currentComments: CommentEntry[] = []
let pendingTempRange: { from: number; to: number } | null = null
let activeView: EditorView | null = null

function resolveCommentToRange(doc: PMNode, c: CommentEntry): { from: number; to: number } | null {
  // 1. Try the structural address (modern comments).
  if (c.anchor.address) {
    const r = resolveRangeInPm(doc, c.anchor.address)
    if (r.confidence !== "lost") return { from: r.from, to: r.to }
  }
  // 2. Fall back to text-quote search against the document plain text.
  if (c.anchor.selected) {
    const synthetic: AnchorRange = {
      start: { nodeIndex: -1, plainOffset: 0 },
      end: { nodeIndex: -1, plainOffset: 0 },
      textQuote: {
        prefix: c.anchor.before || "",
        suffix: c.anchor.after || "",
        exact: c.anchor.selected,
      },
    }
    const r = resolveRangeInPm(doc, synthetic)
    if (r.confidence !== "lost") return { from: r.from, to: r.to }
  }
  return null
}

function buildDecorationState(doc: PMNode, comments: CommentEntry[], temp: { from: number; to: number } | null): DecorationState {
  const decos: Decoration[] = []
  const orphanedIds = new Set<string>()
  for (const c of comments) {
    if (c.resolved || c.parent_id) continue // replies don't get their own highlight
    const range = resolveCommentToRange(doc, c)
    if (range && range.to > range.from) {
      decos.push(Decoration.inline(range.from, range.to, {
        class: "comment-highlight",
        "data-comment-id": c.id,
      }))
    } else {
      orphanedIds.add(c.id)
    }
  }
  if (temp && temp.to > temp.from) {
    decos.push(Decoration.inline(temp.from, temp.to, {
      class: "comment-highlight",
      "data-comment-id": TEMP_COMMENT_ID,
    }))
  }
  return { decos: DecorationSet.create(doc, decos), orphanedIds }
}

export const commentPmPlugin: PMPlugin = new PMPlugin({
  key: COMMENT_KEY,
  state: {
    init(_, state) {
      return buildDecorationState(state.doc, currentComments, pendingTempRange)
    },
    apply(tr, old, _oldState, newState) {
      if (tr.getMeta(COMMENT_KEY) === REFRESH) {
        return buildDecorationState(newState.doc, currentComments, pendingTempRange)
      }
      if (tr.docChanged) {
        return { decos: old.decos.map(tr.mapping, tr.doc), orphanedIds: old.orphanedIds }
      }
      return old
    },
  },
  props: {
    decorations(state) {
      return COMMENT_KEY.getState(state)?.decos
    },
  },
})

function refreshDecorations(): Set<string> {
  if (!activeView) return new Set()
  activeView.dispatch(activeView.state.tr.setMeta(COMMENT_KEY, REFRESH))
  return COMMENT_KEY.getState(activeView.state)?.orphanedIds || new Set()
}

function getOrphanedIds(): Set<string> {
  if (!activeView) return new Set(currentComments.map(c => c.id))
  return COMMENT_KEY.getState(activeView.state)?.orphanedIds || new Set()
}

function findHighlightSpan(commentId: string): HTMLElement | null {
  return document.querySelector(`[data-comment-id="${cssEscape(commentId)}"]`)
}

function findAllHighlightSpans(commentId: string): NodeListOf<HTMLElement> {
  return document.querySelectorAll(`[data-comment-id="${cssEscape(commentId)}"]`)
}

function cssEscape(s: string): string {
  if (typeof (window as any).CSS?.escape === "function") return (window as any).CSS.escape(s)
  return s.replace(/[^\w-]/g, (c) => `\\${c}`)
}

// ── Anchor extraction from the live PM selection ────────────────────────────

function extractAnchorFromView(): { anchor: CommentAnchor; range: { from: number; to: number } } | null {
  if (!activeView) return null
  const sel = window.getSelection()
  const pmRange = domSelectionToPmRange(activeView, sel)
  if (!pmRange) return null

  const range = rangeFromPm(activeView.state.doc, pmRange.from, pmRange.to, { withTextQuote: true })
  const tq = range.textQuote
  const exact = tq?.exact || ""
  if (!exact.trim()) return null

  const anchor: CommentAnchor = {
    selected: exact.length > 200 ? exact.slice(0, 200) : exact,
    before: tq?.prefix || "",
    after: tq?.suffix || "",
    address: range,
  }
  return { anchor, range: pmRange }
}

// ── Sidebar state ───────────────────────────────────────────────────────────

let sidebarEl: HTMLDivElement | null = null
let collapsedToggle: HTMLDivElement | null = null
let currentPagePath = ""
let currentAuthFetch: ((url: string, init?: RequestInit) => Promise<Response>) | null = null
let currentUser: string | null = null
let currentIsAdmin = false
let sidebarHidden = false

// ── Sidebar lifecycle ───────────────────────────────────────────────────────

function ensureSidebar(): HTMLDivElement {
  if (sidebarEl && document.body.contains(sidebarEl)) return sidebarEl

  sidebarEl = document.createElement("div")
  sidebarEl.id = "comment-sidebar"
  const main = document.getElementById("main")
  if (main) {
    main.appendChild(sidebarEl)
    main.classList.add("has-comments")
  } else {
    document.body.appendChild(sidebarEl)
  }
  return sidebarEl
}

function removeSidebar() {
  if (sidebarEl && sidebarEl.parentNode) {
    sidebarEl.parentNode.removeChild(sidebarEl)
    sidebarEl = null
  }
  const main = document.getElementById("main")
  if (main) main.classList.remove("has-comments")
}

// ── Collapsed toggle ────────────────────────────────────────────────────────

function showCollapsedToggle() {
  removeCollapsedToggle()
  const main = document.getElementById("main")
  if (!main) return

  collapsedToggle = document.createElement("div")
  collapsedToggle.className = "comment-collapsed-toggle"
  collapsedToggle.title = "Show comments"

  const n = currentComments.filter(c => !c.resolved).length
  const arrow = document.createElement("span")
  arrow.className = "comment-header-arrow"
  arrow.textContent = "▶"
  collapsedToggle.appendChild(arrow)
  collapsedToggle.appendChild(document.createTextNode(` ${n}`))

  collapsedToggle.addEventListener("click", () => {
    sidebarHidden = false
    removeCollapsedToggle()
    if (currentComments.length > 0) renderAll()
  })

  main.appendChild(collapsedToggle)
}

function removeCollapsedToggle() {
  if (collapsedToggle && collapsedToggle.parentNode) {
    collapsedToggle.parentNode.removeChild(collapsedToggle)
    collapsedToggle = null
  }
}

// ── Sidebar rendering ───────────────────────────────────────────────────────

function groupReplies(comments: CommentEntry[]): Map<string, CommentEntry[]> {
  const groups = new Map<string, CommentEntry[]>()
  for (const c of comments) {
    if (!c.parent_id) continue
    if (!groups.has(c.parent_id)) groups.set(c.parent_id, [])
    groups.get(c.parent_id)!.push(c)
  }
  for (const list of groups.values()) {
    list.sort((a, b) => a.created_at.localeCompare(b.created_at))
  }
  return groups
}

function renderSidebar(comments: CommentEntry[], orphanedIds: Set<string>) {
  const sidebar = ensureSidebar()
  sidebar.innerHTML = ""
  removeCollapsedToggle()

  const header = document.createElement("div")
  header.className = "comment-sidebar-header"
  header.title = "Click to hide comments"

  const arrow = document.createElement("span")
  arrow.className = "comment-header-arrow"
  arrow.textContent = "▼"
  header.appendChild(arrow)

  const title = document.createElement("span")
  // Header count tracks unresolved top-level threads (replies don't count separately).
  const unresolvedCount = comments.filter(c => !c.resolved && !c.parent_id).length
  title.textContent = ` Comments (${unresolvedCount})`
  header.appendChild(title)

  header.addEventListener("click", () => {
    sidebarHidden = true
    removeSidebar()
    showCollapsedToggle()
  })
  sidebar.appendChild(header)

  const replies = groupReplies(comments)
  const tops = comments.filter(c => !c.parent_id)
  const anchored = tops.filter(c => !c.resolved && !orphanedIds.has(c.id)).slice()
  const orphaned = tops.filter(c => !c.resolved && orphanedIds.has(c.id))
  const resolved = tops.filter(c => c.resolved)

  anchored.sort((a, b) => {
    const spanA = findHighlightSpan(a.id)
    const spanB = findHighlightSpan(b.id)
    if (!spanA && !spanB) return 0
    if (!spanA) return 1
    if (!spanB) return -1
    return spanA.getBoundingClientRect().top - spanB.getBoundingClientRect().top
  })

  for (const c of anchored) sidebar.appendChild(renderCommentBox(c, false, replies.get(c.id) || []))

  if (orphaned.length > 0) {
    const spacer = document.createElement("div")
    spacer.className = "comment-orphaned-spacer"
    sidebar.appendChild(spacer)

    const label = document.createElement("div")
    label.className = "comment-orphaned-label-header"
    label.textContent = `Orphaned (${orphaned.length})`
    sidebar.appendChild(label)

    for (const c of orphaned) sidebar.appendChild(renderCommentBox(c, true, replies.get(c.id) || []))
  }

  if (resolved.length > 0) {
    const toggle = document.createElement("div")
    toggle.className = "comment-resolved-toggle"
    toggle.textContent = `Resolved (${resolved.length})`
    let expanded = false
    const resolvedContainer = document.createElement("div")
    resolvedContainer.style.display = "none"
    for (const c of resolved) resolvedContainer.appendChild(renderCommentBox(c, orphanedIds.has(c.id), replies.get(c.id) || []))
    toggle.addEventListener("click", () => {
      expanded = !expanded
      resolvedContainer.style.display = expanded ? "block" : "none"
      toggle.textContent = `${expanded ? "Hide" : "Show"} resolved (${resolved.length})`
    })
    sidebar.appendChild(toggle)
    sidebar.appendChild(resolvedContainer)
  }

  requestAnimationFrame(() => positionComments())
}

function renderCommentBox(c: CommentEntry, orphaned: boolean, replies: CommentEntry[] = []): HTMLDivElement {
  const box = document.createElement("div")
  box.className = "comment-box" + (c.resolved ? " comment-resolved" : "") + (orphaned ? " comment-orphaned" : "") + (c.ai ? " comment-ai" : "")

  const tooltip = document.createElement("div")
  tooltip.className = "comment-tooltip"
  const sel = c.anchor.selected
  const truncated = sel.length > 120 ? sel.slice(0, 120) + "…" : sel
  tooltip.innerHTML = `Comment on “<i>${escapeHtml(truncated)}</i>”`
  box.appendChild(tooltip)
  box.dataset.commentId = c.id

  box.addEventListener("click", () => {
    const span = findHighlightSpan(c.id)
    if (span) {
      const main = document.getElementById("main")
      if (main) {
        const mainRect = main.getBoundingClientRect()
        const spanRect = span.getBoundingClientRect()
        main.scrollTo({ top: main.scrollTop + spanRect.top - mainRect.top - mainRect.height / 3, behavior: "smooth" })
      }
      span.classList.add("comment-highlight-flash")
      setTimeout(() => span.classList.remove("comment-highlight-flash"), 1000)
    }
  })

  box.addEventListener("mouseenter", () => {
    findAllHighlightSpans(c.id).forEach(el => el.classList.add("comment-highlight-active"))
    box.classList.add("comment-box-active")
  })
  box.addEventListener("mouseleave", () => {
    findAllHighlightSpans(c.id).forEach(el => el.classList.remove("comment-highlight-active"))
    box.classList.remove("comment-box-active")
  })

  appendEntryBody(box, c, orphaned)

  // Replies, oldest first, rendered as nested mini-entries.
  if (replies.length > 0) {
    const repliesEl = document.createElement("div")
    repliesEl.className = "comment-replies"
    for (const r of replies) {
      repliesEl.appendChild(renderReplyEntry(r))
    }
    box.appendChild(repliesEl)
  }

  const actions = document.createElement("div")
  actions.className = "comment-actions"

  if (currentUser) {
    const resolveBtn = document.createElement("button")
    resolveBtn.className = "comment-action-btn"
    resolveBtn.textContent = c.resolved ? "Unresolve" : "Resolve"
    resolveBtn.addEventListener("click", async (e) => { e.stopPropagation(); await resolveComment(c.id) })
    actions.appendChild(resolveBtn)

    if (!c.resolved) {
      const replyBtn = document.createElement("button")
      replyBtn.className = "comment-action-btn"
      replyBtn.textContent = "Reply"
      replyBtn.addEventListener("click", (e) => { e.stopPropagation(); startReplyForm(box, c.id) })
      actions.appendChild(replyBtn)
    }

    const toggleAIBtn = document.createElement("button")
    toggleAIBtn.className = "comment-action-btn"
    toggleAIBtn.textContent = c.ai ? "Keep on publish" : "Mark as AI"
    toggleAIBtn.title = c.ai ? "Convert to regular comment (will survive publish)" : "Mark as AI comment (will be stripped on publish)"
    toggleAIBtn.addEventListener("click", async (e) => { e.stopPropagation(); await toggleAIComment(c.id) })
    actions.appendChild(toggleAIBtn)
  }

  if (currentUser === c.author || currentIsAdmin) {
    const editBtn = document.createElement("button")
    editBtn.className = "comment-action-btn"
    editBtn.textContent = "Edit"
    editBtn.addEventListener("click", (e) => { e.stopPropagation(); startEditComment(box, c) })
    actions.appendChild(editBtn)

    const deleteBtn = document.createElement("button")
    deleteBtn.className = "comment-action-btn comment-action-delete"
    deleteBtn.textContent = "Delete"
    deleteBtn.addEventListener("click", async (e) => {
      e.stopPropagation()
      const msg = replies.length > 0
        ? `Delete this comment and its ${replies.length} repl${replies.length === 1 ? "y" : "ies"}?`
        : "Delete this comment?"
      if (confirm(msg)) await deleteComment(c.id)
    })
    actions.appendChild(deleteBtn)
  }

  box.appendChild(actions)
  return box
}

function appendEntryBody(parent: HTMLElement, c: CommentEntry, orphaned: boolean) {
  const authorEl = document.createElement("div")
  authorEl.className = "comment-author"
  if (c.ai) {
    const aiBadge = document.createElement("span")
    aiBadge.className = "comment-ai-badge"
    aiBadge.textContent = "AI"
    authorEl.appendChild(aiBadge)
    authorEl.appendChild(document.createTextNode(` ${c.author} · ${new Date(c.created_at).toLocaleDateString()}`))
  } else {
    authorEl.textContent = `${c.author} · ${new Date(c.created_at).toLocaleDateString()}`
  }
  parent.appendChild(authorEl)

  if (orphaned) {
    const lbl = document.createElement("span")
    lbl.className = "comment-orphan-label"
    lbl.textContent = " (text not found)"
    authorEl.appendChild(lbl)
  }

  const textEl = document.createElement("div")
  textEl.className = "comment-text"
  textEl.textContent = c.text
  parent.appendChild(textEl)
}

function renderReplyEntry(r: CommentEntry): HTMLDivElement {
  const entry = document.createElement("div")
  entry.className = "comment-reply" + (r.ai ? " comment-ai" : "")
  entry.dataset.commentId = r.id
  appendEntryBody(entry, r, false)

  if (currentUser === r.author || currentIsAdmin) {
    const actions = document.createElement("div")
    actions.className = "comment-actions"
    const editBtn = document.createElement("button")
    editBtn.className = "comment-action-btn"
    editBtn.textContent = "Edit"
    editBtn.addEventListener("click", (e) => { e.stopPropagation(); startEditComment(entry, r) })
    actions.appendChild(editBtn)
    const deleteBtn = document.createElement("button")
    deleteBtn.className = "comment-action-btn comment-action-delete"
    deleteBtn.textContent = "Delete"
    deleteBtn.addEventListener("click", async (e) => {
      e.stopPropagation()
      if (confirm("Delete this reply?")) await deleteComment(r.id)
    })
    actions.appendChild(deleteBtn)
    entry.appendChild(actions)
  }
  return entry
}

function startReplyForm(box: HTMLDivElement, parentId: string) {
  // Don't open two reply forms on the same box.
  if (box.querySelector(".comment-reply-form")) return

  const form = document.createElement("div")
  form.className = "comment-reply-form"

  const textarea = document.createElement("textarea")
  textarea.className = "comment-edit-textarea"
  textarea.placeholder = "Reply…"
  textarea.rows = 2
  form.appendChild(textarea)

  const btnRow = document.createElement("div")
  btnRow.className = "comment-create-buttons"

  const submitBtn = document.createElement("button")
  submitBtn.className = "comment-action-btn comment-action-submit"
  submitBtn.textContent = "Reply"
  submitBtn.addEventListener("click", async () => {
    const text = textarea.value.trim()
    if (!text) return
    submitBtn.disabled = true
    await createReply(parentId, text)
  })
  btnRow.appendChild(submitBtn)

  const cancelBtn = document.createElement("button")
  cancelBtn.className = "comment-action-btn"
  cancelBtn.textContent = "Cancel"
  cancelBtn.addEventListener("click", () => form.remove())
  btnRow.appendChild(cancelBtn)

  form.appendChild(btnRow)

  const actions = box.querySelector(".comment-actions")
  if (actions) box.insertBefore(form, actions)
  else box.appendChild(form)

  requestAnimationFrame(() => {
    textarea.focus()
    requestAnimationFrame(() => positionComments())
  })
}

function startEditComment(box: HTMLDivElement, c: CommentEntry) {
  const textEl = box.querySelector(".comment-text") as HTMLDivElement
  if (!textEl) return
  const actionsEl = box.querySelector(".comment-actions") as HTMLDivElement

  const textarea = document.createElement("textarea")
  textarea.className = "comment-edit-textarea"
  textarea.value = c.text
  textarea.rows = 3

  const saveBtn = document.createElement("button")
  saveBtn.className = "comment-action-btn"
  saveBtn.textContent = "Save"
  const cancelBtn = document.createElement("button")
  cancelBtn.className = "comment-action-btn"
  cancelBtn.textContent = "Cancel"

  textEl.replaceWith(textarea)
  if (actionsEl) actionsEl.replaceWith(saveBtn, cancelBtn)
  textarea.focus()

  saveBtn.addEventListener("click", async () => {
    const newText = textarea.value.trim()
    if (!newText) return
    await updateComment(c.id, newText)
  })
  cancelBtn.addEventListener("click", () => void refreshComments())
}

// ── Vertical positioning ────────────────────────────────────────────────────

let scrollListener: (() => void) | null = null
let nodeRenderedListener: (() => void) | null = null
let viewResizeObserver: ResizeObserver | null = null
let repositionTimer: number | null = null

function schedulePositionComments() {
  if (repositionTimer !== null) return
  repositionTimer = window.setTimeout(() => {
    repositionTimer = null
    if (sidebarEl && !sidebarHidden) positionComments()
  }, 50)
}

function positionComments() {
  if (!sidebarEl) return

  const boxes = Array.from(sidebarEl.querySelectorAll<HTMLDivElement>(".comment-box"))
  if (boxes.length === 0) return

  boxes.forEach(b => { b.style.marginTop = "" })

  const header = sidebarEl.querySelector(".comment-sidebar-header")
  const MIN_GAP = 4

  let lastBottom = header ? header.getBoundingClientRect().bottom : sidebarEl.getBoundingClientRect().top

  for (const box of boxes) {
    const commentId = box.dataset.commentId
    if (!commentId) continue
    const anchorEl = findHighlightSpan(commentId)
    if (!anchorEl) { lastBottom = box.getBoundingClientRect().bottom + MIN_GAP; continue }

    const desiredTop = anchorEl.getBoundingClientRect().top
    const currentTop = box.getBoundingClientRect().top
    const earliestTop = lastBottom + MIN_GAP
    const targetTop = Math.max(desiredTop, earliestTop)
    const offset = targetTop - currentTop
    if (Math.abs(offset) > 1) box.style.marginTop = `${offset}px`

    lastBottom = box.getBoundingClientRect().top + box.offsetHeight
  }
}

function setupScrollListener() {
  const main = document.getElementById("main")
  if (!main || scrollListener) return
  scrollListener = () => {
    if (sidebarEl && !sidebarHidden) requestAnimationFrame(() => positionComments())
  }
  main.addEventListener("scroll", scrollListener, { passive: true })
}

function removeScrollListener() {
  if (!scrollListener) return
  const main = document.getElementById("main")
  if (main) main.removeEventListener("scroll", scrollListener)
  scrollListener = null
}

// Async content (mermaid, queries, includes, images) re-flows the page after
// initial render. Re-position whenever a node finishes rendering or the view
// changes size.
function setupReflowListeners() {
  if (!nodeRenderedListener) {
    nodeRenderedListener = () => schedulePositionComments()
    document.addEventListener("gowiki:node-rendered", nodeRenderedListener)
  }
  if (!viewResizeObserver && activeView) {
    viewResizeObserver = new ResizeObserver(() => schedulePositionComments())
    viewResizeObserver.observe(activeView.dom)
  }
}

function removeReflowListeners() {
  if (nodeRenderedListener) {
    document.removeEventListener("gowiki:node-rendered", nodeRenderedListener)
    nodeRenderedListener = null
  }
  if (viewResizeObserver) {
    viewResizeObserver.disconnect()
    viewResizeObserver = null
  }
  if (repositionTimer !== null) {
    clearTimeout(repositionTimer)
    repositionTimer = null
  }
}

// ── Create form ─────────────────────────────────────────────────────────────

function cleanupTempHighlight() {
  if (!pendingTempRange) return
  pendingTempRange = null
  refreshDecorations()
}

function showCreateForm() {
  const result = extractAnchorFromView()
  if (!result) {
    alert("Please select some text first.")
    return
  }
  const { anchor, range } = result

  // Clear the browser selection.
  const sel = window.getSelection()
  if (sel) sel.removeAllRanges()

  // Make sure the sidebar is visible and contains the existing comments.
  if (sidebarHidden) {
    sidebarHidden = false
    removeCollapsedToggle()
  }
  if (currentComments.length > 0) {
    renderSidebar(currentComments, getOrphanedIds())
  } else {
    ensureMinimalSidebar()
  }

  const sidebar = ensureSidebar()

  const existing = sidebar.querySelector(".comment-create-form")
  if (existing) existing.remove()

  // Show a temporary highlight at the selection.
  pendingTempRange = range
  refreshDecorations()

  const form = document.createElement("div")
  form.className = "comment-box comment-create-form"
  form.dataset.commentId = TEMP_COMMENT_ID

  const selectedText = anchor.selected
  const label = document.createElement("div")
  label.className = "comment-create-label"
  label.textContent = `“${selectedText.length > 60 ? selectedText.slice(0, 60) + "…" : selectedText}”`
  form.appendChild(label)

  const textarea = document.createElement("textarea")
  textarea.className = "comment-create-textarea"
  textarea.placeholder = "Write your comment…"
  textarea.rows = 2
  form.appendChild(textarea)

  const btnRow = document.createElement("div")
  btnRow.className = "comment-create-buttons"

  const submitBtn = document.createElement("button")
  submitBtn.className = "comment-action-btn comment-action-submit"
  submitBtn.textContent = "Comment"
  submitBtn.addEventListener("click", async () => {
    const text = textarea.value.trim()
    if (!text) return
    submitBtn.disabled = true
    cleanupTempHighlight()
    await createComment(anchor, text)
  })
  btnRow.appendChild(submitBtn)

  const cancelBtn = document.createElement("button")
  cancelBtn.className = "comment-action-btn"
  cancelBtn.textContent = "Cancel"
  cancelBtn.addEventListener("click", () => {
    cleanupTempHighlight()
    form.remove()
    if (currentComments.length === 0) removeSidebar()
    else requestAnimationFrame(() => positionComments())
  })
  btnRow.appendChild(cancelBtn)

  form.appendChild(btnRow)

  // Insert the form at the right vertical position based on the temp highlight.
  const tempAnchor = findHighlightSpan(TEMP_COMMENT_ID)
  if (tempAnchor) {
    const anchorTop = tempAnchor.getBoundingClientRect().top
    const boxes = sidebar.querySelectorAll<HTMLDivElement>(".comment-box")
    let insertBefore: Element | null = null
    for (const box of boxes) {
      const cid = box.dataset.commentId
      if (!cid || cid === TEMP_COMMENT_ID) continue
      const cAnchor = findHighlightSpan(cid)
      if (cAnchor && cAnchor.getBoundingClientRect().top > anchorTop) { insertBefore = box; break }
    }
    if (insertBefore) sidebar.insertBefore(form, insertBefore)
    else {
      const resolvedToggle = sidebar.querySelector(".comment-resolved-toggle")
      if (resolvedToggle) sidebar.insertBefore(form, resolvedToggle)
      else sidebar.appendChild(form)
    }
  } else {
    sidebar.appendChild(form)
  }

  requestAnimationFrame(() => {
    positionComments()
    textarea.focus()
    form.scrollIntoView({ block: "nearest", behavior: "smooth" })
  })
}

function ensureMinimalSidebar() {
  const sidebar = ensureSidebar()
  if (sidebar.querySelector(".comment-sidebar-header")) return
  const header = document.createElement("div")
  header.className = "comment-sidebar-header"
  header.title = "Click to hide comments"
  const arrowSpan = document.createElement("span")
  arrowSpan.className = "comment-header-arrow"
  arrowSpan.textContent = "▼"
  header.appendChild(arrowSpan)
  const titleSpan = document.createElement("span")
  titleSpan.textContent = " Comments (0)"
  header.appendChild(titleSpan)
  header.addEventListener("click", () => {
    sidebarHidden = true
    removeSidebar()
    if (currentComments.length > 0) showCollapsedToggle()
  })
  sidebar.appendChild(header)
}

// ── API calls ───────────────────────────────────────────────────────────────

async function fetchComments(pagePath: string): Promise<CommentEntry[]> {
  const resp = await fetch(`${API_BASE}/${encodePagePath(pagePath)}`)
  if (!resp.ok) return []
  const data = await resp.json()
  return data.comments || []
}

async function createComment(anchor: CommentAnchor, text: string) {
  if (!currentAuthFetch) return
  const resp = await currentAuthFetch(`${API_BASE}/${encodePagePath(currentPagePath)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ anchor, text }),
  })
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({}))
    alert((err as any).error || "Failed to create comment")
    return
  }
  await refreshComments()
}

async function createReply(parentId: string, text: string) {
  if (!currentAuthFetch) return
  const resp = await currentAuthFetch(`${API_BASE}/${encodePagePath(currentPagePath)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ anchor: { selected: "", before: "", after: "" }, text, parent_id: parentId }),
  })
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({}))
    alert((err as any).error || "Failed to post reply")
    return
  }
  await refreshComments()
}

async function updateComment(commentId: string, newText: string) {
  if (!currentAuthFetch) return
  const resp = await currentAuthFetch(
    `${API_BASE}/${commentId}?page=${encodePagePath(currentPagePath)}`,
    { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ text: newText }) }
  )
  if (!resp.ok) { const err = await resp.json().catch(() => ({})); alert((err as any).error || "Failed to update comment"); return }
  await refreshComments()
}

async function resolveComment(commentId: string) {
  if (!currentAuthFetch) return
  const resp = await currentAuthFetch(
    `${API_BASE}/${commentId}/resolve?page=${encodePagePath(currentPagePath)}`,
    { method: "PATCH" }
  )
  if (!resp.ok) { const err = await resp.json().catch(() => ({})); alert((err as any).error || "Failed to resolve comment"); return }
  await refreshComments()
}

async function deleteComment(commentId: string) {
  if (!currentAuthFetch) return
  const resp = await currentAuthFetch(
    `${API_BASE}/${commentId}?page=${encodePagePath(currentPagePath)}`,
    { method: "DELETE" }
  )
  if (!resp.ok) { const err = await resp.json().catch(() => ({})); alert((err as any).error || "Failed to delete comment"); return }
  await refreshComments()
}

async function toggleAIComment(commentId: string) {
  if (!currentAuthFetch) return
  const resp = await currentAuthFetch(
    `${API_BASE}/${commentId}/toggle-ai?page=${encodePagePath(currentPagePath)}`,
    { method: "PATCH" }
  )
  if (!resp.ok) { const err = await resp.json().catch(() => ({})); alert((err as any).error || "Failed to toggle AI flag"); return }
  await refreshComments()
}

async function clearAIComments() {
  if (!currentAuthFetch) return
  const resp = await currentAuthFetch(
    `${API_BASE}/?page=${encodePagePath(currentPagePath)}`,
    { method: "DELETE" }
  )
  if (!resp.ok) { const err = await resp.json().catch(() => ({})); alert((err as any).error || "Failed to clear AI comments"); return }
  await refreshComments()
}

function encodePagePath(p: string): string {
  return p.split("/").map(encodeURIComponent).join("/")
}

async function refreshComments() {
  currentComments = await fetchComments(currentPagePath)
  cleanupTempHighlight() // also triggers refreshDecorations
  renderAll()
}

function renderAll() {
  if (currentComments.length === 0) {
    refreshDecorations()
    removeSidebar()
    removeCollapsedToggle()
    return
  }
  const orphanedIds = refreshDecorations()
  renderSidebar(currentComments, orphanedIds)
  setupScrollListener()
  setupReflowListeners()
}

// ── Public API ──────────────────────────────────────────────────────────────

export async function initComments(opts: {
  pagePath: string
  view: EditorView
  authFetch: (url: string, init?: RequestInit) => Promise<Response>
  username: string | null
  isAdmin: boolean
}) {
  currentPagePath = opts.pagePath
  activeView = opts.view
  currentAuthFetch = opts.authFetch
  currentUser = opts.username
  currentIsAdmin = opts.isAdmin
  sidebarHidden = false

  currentComments = await fetchComments(opts.pagePath)
  renderAll()
}

export function destroyComments() {
  currentComments = []
  pendingTempRange = null
  if (activeView) {
    try { refreshDecorations() } catch {}
  }
  activeView = null
  removeSidebar()
  removeCollapsedToggle()
  removeScrollListener()
  removeReflowListeners()
}

export function reapplyComments() {
  if (currentComments.length === 0) return
  renderAll()
}

export function addComment() {
  showCreateForm()
}

export function getCommentCount(): number {
  return currentComments.filter((c) => !c.resolved).length
}

export { clearAIComments }

export async function createAIComment(selectedText: string, beforeContext: string, afterContext: string, commentText: string): Promise<boolean> {
  if (!currentAuthFetch || !currentPagePath) return false
  // AI-created comments use text-quote only (the AI doesn't have a structural address).
  const resp = await currentAuthFetch(`${API_BASE}${encodePagePath(currentPagePath)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      anchor: { selected: selectedText, before: beforeContext, after: afterContext },
      text: commentText,
      ai: true,
    }),
  })
  if (resp.ok) { await refreshComments(); return true }
  return false
}

// ── Plugin registration ─────────────────────────────────────────────────────

const commentStyles = `
/* --- Highlights --- */
.comment-highlight {
  background-color: rgba(255, 220, 100, 0.35);
  border-bottom: 2px solid rgba(255, 180, 0, 0.5);
  cursor: pointer;
  transition: background-color 0.15s;
}
.comment-highlight:hover,
.comment-highlight-active {
  background-color: rgba(255, 200, 50, 0.55);
}
.comment-highlight-flash {
  animation: comment-flash 1s ease;
}
@keyframes comment-flash {
  0%, 100% { background-color: rgba(255, 220, 100, 0.35); }
  50% { background-color: rgba(255, 160, 0, 0.6); }
}

/* --- Layout: #main gets 2 columns when sidebar present --- */
#main.has-comments {
  display: grid;
  grid-template-columns: 1fr 220px;
  grid-template-rows: 1fr auto;
  column-gap: 8px;
}
#main.has-comments #content { grid-column: 1; grid-row: 1; }
#main.has-comments #footer  { grid-column: 1; grid-row: 2; }
#comment-sidebar {
  grid-column: 2;
  grid-row: 1 / -1;
  display: flex;
  flex-direction: column;
  padding: 4px;
  font-size: 0.82em;
  scrollbar-width: thin;
  align-self: stretch;
}
@media (max-width: 900px) {
  #comment-sidebar { display: none; }
  .comment-collapsed-toggle { display: none; }
  #main.has-comments { display: flex; flex-direction: column; }
}

/* --- Collapsed toggle (floats at top-right, no layout impact) --- */
.comment-collapsed-toggle {
  position: absolute;
  top: 4px;
  right: 4px;
  font-size: 0.82em;
  padding: 3px 8px;
  cursor: pointer;
  color: var(--gw-color-muted);
  background: var(--gw-color-bg);
  border: 1px solid var(--gw-color-border);
  border-radius: 3px;
  user-select: none;
  z-index: 5;
}
.comment-collapsed-toggle:hover {
  background: var(--gw-color-surface-alt);
  color: var(--gw-color-text);
  border-color: var(--gw-color-border);
}
.comment-header-arrow {
  font-size: 0.75em;
  margin-right: 2px;
}

/* --- Sidebar header (fold toggle) --- */
.comment-sidebar-header {
  display: flex;
  align-items: center;
  font-weight: 600;
  font-size: 0.95em;
  padding: 4px 6px 8px;
  color: var(--gw-color-muted);
  border-bottom: 1px solid var(--gw-color-border);
  margin-bottom: 6px;
  cursor: pointer;
  user-select: none;
  border-radius: 3px;
}
.comment-sidebar-header:hover {
  background: var(--gw-color-surface-alt);
  color: var(--gw-color-text);
}

/* --- Comment boxes ---
   The yellow-parchment look is a signal "these are reviewer notes" that
   the user relies on. We keep its light-mode palette hardcoded and only
   override in dark mode below. */
.comment-box {
  padding: 8px 8px 6px;
  margin-bottom: 4px;
  border: 1px solid #e0d8b0;
  border-radius: var(--gw-radius-sm);
  background: #fffde7;
  color: #222;
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.comment-box:hover, .comment-box-active {
  border-color: #d4c878;
  box-shadow: var(--gw-shadow-sm);
}
.comment-resolved { opacity: 0.6; background: #f8f8f5; border-color: #ddd; }
.comment-orphaned { border-left: 3px solid var(--gw-color-error); }
.comment-orphan-label { color: var(--gw-color-error); font-style: italic; font-size: 0.85em; }
.comment-author { font-size: 0.85em; color: #777; margin-bottom: 4px; }
.comment-text { white-space: pre-wrap; word-break: break-word; line-height: 1.4; margin-bottom: 6px; }
.comment-actions { display: flex; gap: 6px; flex-wrap: wrap; }
.comment-action-btn {
  font-size: 0.8em; padding: 2px 8px; border: 1px solid var(--gw-color-border); border-radius: 3px;
  background: var(--gw-color-bg); cursor: pointer; color: var(--gw-color-muted);
}
.comment-action-btn:hover { background: var(--gw-color-border-soft); border-color: var(--gw-color-muted); }
.comment-action-delete { color: var(--gw-color-error); }
.comment-action-delete:hover { background: var(--gw-color-error-bg); border-color: var(--gw-color-error); }
.comment-action-submit { background: var(--gw-color-success-bg); border-color: var(--gw-color-success); color: var(--gw-color-success); }
.comment-action-submit:hover { background: var(--gw-color-success-bg); filter: brightness(1.1); }

/* --- AI comments --- */
.comment-ai { background: #e8eaf6; border-color: #9fa8da; }
.comment-ai:hover, .comment-ai.comment-box-active { border-color: #7986cb; }
.comment-ai-badge {
  display: inline-block;
  background: #5c6bc0;
  color: #fff;
  font-size: 0.7em;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 3px;
  margin-right: 4px;
  vertical-align: middle;
  letter-spacing: 0.5px;
}

/* --- Tooltip --- */
.comment-tooltip {
  display: none;
  position: absolute;
  left: 0; right: 0; bottom: 100%;
  margin-bottom: 4px;
  padding: 5px 8px;
  font-size: 0.82em;
  color: var(--gw-color-text);
  background: var(--gw-color-bg);
  border: 1px solid var(--gw-color-border);
  border-radius: 3px;
  box-shadow: var(--gw-shadow-sm);
  white-space: normal;
  word-break: break-word;
  line-height: 1.35;
  z-index: 10;
  pointer-events: none;
}
.comment-box { position: relative; }
.comment-box:hover > .comment-tooltip { display: block; }

/* --- Orphaned: floated to bottom --- */
.comment-orphaned-spacer {
  flex-grow: 1;
  min-height: 80px;
}
.comment-orphaned-label-header {
  font-size: 0.82em;
  color: var(--gw-color-error);
  font-style: italic;
  padding: 4px 8px;
  margin-bottom: 4px;
  border-top: 1px dashed var(--gw-color-error);
}

.comment-resolved-toggle {
  font-size: 0.85em; color: var(--gw-color-muted); cursor: pointer; padding: 4px 8px; margin: 4px 0; border-radius: 3px;
}
.comment-resolved-toggle:hover { background: var(--gw-color-surface-alt); color: var(--gw-color-text); }

/* --- Replies (stacked inside a parent comment box) --- */
.comment-replies {
  margin: 4px 0 6px 0;
  border-left: 2px solid #d4c878;
  padding-left: 6px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.comment-reply {
  padding: 4px 6px;
  background: rgba(255, 255, 255, 0.35);
  border-radius: 2px;
  font-size: 0.95em;
}
.comment-reply .comment-author { font-size: 0.8em; margin-bottom: 2px; }
.comment-reply .comment-text { margin-bottom: 2px; }
.comment-reply.comment-ai { background: rgba(92, 107, 192, 0.08); }
html[data-theme="dark"] .comment-replies { border-left-color: #7a6c40; }
html[data-theme="dark"] .comment-reply { background: rgba(0, 0, 0, 0.20); }
html[data-theme="dark"] .comment-reply.comment-ai { background: rgba(92, 107, 192, 0.18); }

.comment-reply-form {
  margin: 4px 0 6px 0;
  padding-left: 6px;
  border-left: 2px solid var(--gw-color-success);
}

/* --- Create form in sidebar --- */
.comment-create-form {
  background: var(--gw-color-success-bg);
  border-color: var(--gw-color-success);
  cursor: default;
}
.comment-create-label {
  font-size: 0.85em; color: var(--gw-color-muted); margin-bottom: 6px; font-style: italic;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.comment-create-textarea, .comment-edit-textarea {
  width: 100%; box-sizing: border-box; padding: 6px; border: 1px solid var(--gw-color-border);
  background: var(--gw-color-bg); color: var(--gw-color-text);
  border-radius: 3px; font-size: 0.9em; font-family: inherit; resize: vertical; margin-bottom: 6px;
}

/* --- Dark-mode adjustments for the yellow-parchment comment boxes --- */
html[data-theme="dark"] .comment-box {
  background: #3a3520;
  border-color: #5a4f30;
  color: var(--gw-color-text);
}
html[data-theme="dark"] .comment-box:hover,
html[data-theme="dark"] .comment-box-active {
  border-color: #7a6c40;
}
html[data-theme="dark"] .comment-resolved {
  background: #2a2a28;
  border-color: var(--gw-color-border);
}
html[data-theme="dark"] .comment-author {
  color: var(--gw-color-muted);
}
html[data-theme="dark"] .comment-ai {
  background: #2a2c40;
  border-color: #3f4a6e;
}
html[data-theme="dark"] .comment-ai:hover,
html[data-theme="dark"] .comment-ai.comment-box-active {
  border-color: #5c6bc0;
}
.comment-create-buttons { display: flex; gap: 6px; }

/* --- Allow text selection in ProseMirror view mode --- */
.gowiki-view .ProseMirror {
  user-select: text !important;
  -webkit-user-select: text !important;
}
.gowiki-view .ProseMirror *::selection {
  background: highlight !important;
}
.gowiki-view .ProseMirror *::-moz-selection {
  background: highlight !important;
}
`

export const commentPlugin: WikiPlugin = {
  register(reg: Registry) {
    reg.registerStyle("comment", commentStyles)
  },
}
