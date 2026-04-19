import type { Plugin as WikiPlugin, Registry } from "../compiler/registry"

const API_BASE = "/api/plugin/comment/v1"

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;")
}

interface CommentAnchor {
  selected: string
  before: string
  after: string
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
}

// --- Anchoring ---

function findAnchorRange(root: HTMLElement, anchor: CommentAnchor): Range | null {
  const fullText = root.textContent || ""
  const selected = anchor.selected

  const positions: number[] = []
  let searchFrom = 0
  while (true) {
    const idx = fullText.indexOf(selected, searchFrom)
    if (idx === -1) break
    positions.push(idx)
    searchFrom = idx + 1
  }

  if (positions.length === 0) {
    const normalizedFull = fullText.replace(/\s+/g, " ")
    const normalizedSelected = selected.replace(/\s+/g, " ")
    const idx = normalizedFull.indexOf(normalizedSelected)
    if (idx === -1) return null
    positions.push(idx)
  }

  let bestPos = positions[0]
  if (positions.length > 1 && (anchor.before || anchor.after)) {
    let bestScore = -1
    for (const pos of positions) {
      let score = 0
      if (anchor.before) {
        const preceding = fullText.slice(Math.max(0, pos - anchor.before.length), pos)
        if (preceding.endsWith(anchor.before)) score += 2
        else if (preceding.includes(anchor.before)) score += 1
      }
      if (anchor.after) {
        const following = fullText.slice(pos + selected.length, pos + selected.length + anchor.after.length)
        if (following.startsWith(anchor.after)) score += 2
        else if (following.includes(anchor.after)) score += 1
      }
      if (score > bestScore) { bestScore = score; bestPos = pos }
    }
  }

  return textOffsetToRange(root, bestPos, bestPos + selected.length)
}

function textOffsetToRange(root: HTMLElement, start: number, end: number): Range | null {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  let offset = 0
  let startNode: Text | null = null, startOffset = 0
  let endNode: Text | null = null, endOffset = 0

  while (walker.nextNode()) {
    const node = walker.currentNode as Text
    const len = node.length
    if (!startNode && offset + len > start) { startNode = node; startOffset = start - offset }
    if (offset + len >= end) { endNode = node; endOffset = end - offset; break }
    offset += len
  }

  if (!startNode || !endNode) return null
  const range = document.createRange()
  range.setStart(startNode, startOffset)
  range.setEnd(endNode, endOffset)
  return range
}

// --- Highlight injection ---

function injectHighlight(range: Range, commentId: string): HTMLSpanElement[] {
  const spans: HTMLSpanElement[] = []
  const textNodes: Text[] = []

  const walker = document.createTreeWalker(
    range.commonAncestorContainer.nodeType === Node.TEXT_NODE
      ? range.commonAncestorContainer.parentNode!
      : range.commonAncestorContainer,
    NodeFilter.SHOW_TEXT,
    {
      acceptNode: (node) =>
        range.intersectsNode(node) && node.nodeValue?.trim()
          ? NodeFilter.FILTER_ACCEPT
          : NodeFilter.FILTER_REJECT,
    }
  )
  while (walker.nextNode()) textNodes.push(walker.currentNode as Text)

  let count = 0
  for (const textNode of textNodes) {
    const subrange = document.createRange()
    subrange.selectNodeContents(textNode)
    if (subrange.compareBoundaryPoints(Range.START_TO_START, range) < 0)
      subrange.setStart(range.startContainer, range.startOffset)
    if (subrange.compareBoundaryPoints(Range.END_TO_END, range) > 0)
      subrange.setEnd(range.endContainer, range.endOffset)

    const selectedText = subrange.toString()
    if (!selectedText) continue

    const span = document.createElement("span")
    span.className = "comment-highlight"
    span.dataset.commentId = commentId
    span.id = count === 0 ? commentId : `${commentId}_${count}`
    subrange.surroundContents(span)
    spans.push(span)
    count++
  }
  return spans
}

function removeHighlights(commentId: string) {
  const spans = document.querySelectorAll(`[data-comment-id="${commentId}"]`)
  spans.forEach((span) => {
    const parent = span.parentNode
    if (!parent) return
    while (span.firstChild) parent.insertBefore(span.firstChild, span)
    parent.removeChild(span)
    parent.normalize()
  })
}

// --- Context extraction ---

function extractAnchor(root: HTMLElement): CommentAnchor | null {
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0) return null

  const range = sel.getRangeAt(0)
  if (!root.contains(range.startContainer) && !root.contains(range.endContainer)) return null

  const selected = range.toString().trim()
  if (!selected) return null

  const fullText = root.textContent || ""
  const idx = fullText.indexOf(selected)
  let before = "", after = ""
  if (idx !== -1) {
    before = fullText.slice(Math.max(0, idx - 20), idx)
    after = fullText.slice(idx + selected.length, idx + selected.length + 20)
  }

  return {
    selected: selected.length > 200 ? selected.slice(0, 200) : selected,
    before,
    after,
  }
}

// --- State ---

let sidebarEl: HTMLDivElement | null = null
let collapsedToggle: HTMLDivElement | null = null
let currentComments: CommentEntry[] = []
let currentPagePath = ""
let currentContentRoot: HTMLElement | null = null
let currentAuthFetch: ((url: string, init?: RequestInit) => Promise<Response>) | null = null
let currentUser: string | null = null
let currentIsAdmin = false
let sidebarHidden = false

// Temp state for the create form: a temporary highlight ID and the anchor data.
const TEMP_COMMENT_ID = "__comment_new__"

// --- Sidebar lifecycle ---

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

// --- Collapsed toggle (floating, no layout impact) ---

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
  arrow.textContent = "\u25B6"
  collapsedToggle.appendChild(arrow)
  collapsedToggle.appendChild(document.createTextNode(` ${n}`))

  collapsedToggle.addEventListener("click", () => {
    sidebarHidden = false
    removeCollapsedToggle()
    if (currentContentRoot && currentComments.length > 0) {
      applyComments(currentContentRoot, currentComments)
    }
  })

  main.appendChild(collapsedToggle)
}

function removeCollapsedToggle() {
  if (collapsedToggle && collapsedToggle.parentNode) {
    collapsedToggle.parentNode.removeChild(collapsedToggle)
    collapsedToggle = null
  }
}

// --- Sidebar rendering ---

function renderSidebar(comments: CommentEntry[], orphanedIds: Set<string>) {
  const sidebar = ensureSidebar()
  sidebar.innerHTML = ""
  removeCollapsedToggle()

  // Header doubles as fold toggle.
  const header = document.createElement("div")
  header.className = "comment-sidebar-header"
  header.title = "Click to hide comments"

  const arrow = document.createElement("span")
  arrow.className = "comment-header-arrow"
  arrow.textContent = "\u25BC"
  header.appendChild(arrow)

  const title = document.createElement("span")
  const unresolvedCount = comments.filter(c => !c.resolved).length
  title.textContent = ` Comments (${unresolvedCount})`
  header.appendChild(title)

  header.addEventListener("click", () => {
    sidebarHidden = true
    removeSidebar()
    showCollapsedToggle()
  })
  sidebar.appendChild(header)

  // Split unresolved into anchored vs orphaned.
  const anchored = comments.filter(c => !c.resolved && !orphanedIds.has(c.id)).slice()
  const orphaned = comments.filter(c => !c.resolved && orphanedIds.has(c.id))
  const resolved = comments.filter(c => c.resolved)

  // Sort anchored by position in the document (top to bottom).
  anchored.sort((a, b) => {
    const spanA = document.getElementById(a.id)
    const spanB = document.getElementById(b.id)
    if (!spanA && !spanB) return 0
    if (!spanA) return 1
    if (!spanB) return -1
    return spanA.getBoundingClientRect().top - spanB.getBoundingClientRect().top
  })

  for (const c of anchored) {
    sidebar.appendChild(renderCommentBox(c, false))
  }

  // Orphaned comments: visible but pushed to the very bottom with a spacer.
  if (orphaned.length > 0) {
    const spacer = document.createElement("div")
    spacer.className = "comment-orphaned-spacer"
    sidebar.appendChild(spacer)

    const label = document.createElement("div")
    label.className = "comment-orphaned-label-header"
    label.textContent = `Orphaned (${orphaned.length})`
    sidebar.appendChild(label)

    for (const c of orphaned) {
      sidebar.appendChild(renderCommentBox(c, true))
    }
  }

  // Resolved comments: collapsed section at the very bottom.
  if (resolved.length > 0) {
    const toggle = document.createElement("div")
    toggle.className = "comment-resolved-toggle"
    toggle.textContent = `Resolved (${resolved.length})`
    let expanded = false
    const resolvedContainer = document.createElement("div")
    resolvedContainer.style.display = "none"
    for (const c of resolved) {
      resolvedContainer.appendChild(renderCommentBox(c, orphanedIds.has(c.id)))
    }
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

function renderCommentBox(c: CommentEntry, orphaned: boolean): HTMLDivElement {
  const box = document.createElement("div")
  box.className = "comment-box" + (c.resolved ? " comment-resolved" : "") + (orphaned ? " comment-orphaned" : "") + (c.ai ? " comment-ai" : "")

  // Tooltip showing the anchored text.
  const tooltip = document.createElement("div")
  tooltip.className = "comment-tooltip"
  const sel = c.anchor.selected
  const truncated = sel.length > 120 ? sel.slice(0, 120) + "\u2026" : sel
  tooltip.innerHTML = `Comment on \u201C<i>${escapeHtml(truncated)}</i>\u201D`
  box.appendChild(tooltip)
  box.dataset.commentId = c.id

  box.addEventListener("click", () => {
    const span = document.getElementById(c.id)
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
    document.querySelectorAll(`[data-comment-id="${c.id}"]`).forEach(el => el.classList.add("comment-highlight-active"))
    box.classList.add("comment-box-active")
  })
  box.addEventListener("mouseleave", () => {
    document.querySelectorAll(`[data-comment-id="${c.id}"]`).forEach(el => el.classList.remove("comment-highlight-active"))
    box.classList.remove("comment-box-active")
  })

  const authorEl = document.createElement("div")
  authorEl.className = "comment-author"
  if (c.ai) {
    const aiBadge = document.createElement("span")
    aiBadge.className = "comment-ai-badge"
    aiBadge.textContent = "AI"
    authorEl.appendChild(aiBadge)
    authorEl.appendChild(document.createTextNode(` ${c.author} \u00B7 ${new Date(c.created_at).toLocaleDateString()}`))
  } else {
    authorEl.textContent = `${c.author} \u00B7 ${new Date(c.created_at).toLocaleDateString()}`
  }
  box.appendChild(authorEl)

  if (orphaned) {
    const lbl = document.createElement("span")
    lbl.className = "comment-orphan-label"
    lbl.textContent = " (text not found)"
    authorEl.appendChild(lbl)
  }

  const textEl = document.createElement("div")
  textEl.className = "comment-text"
  textEl.textContent = c.text
  box.appendChild(textEl)

  const actions = document.createElement("div")
  actions.className = "comment-actions"

  if (currentUser) {
    const resolveBtn = document.createElement("button")
    resolveBtn.className = "comment-action-btn"
    resolveBtn.textContent = c.resolved ? "Unresolve" : "Resolve"
    resolveBtn.addEventListener("click", async (e) => { e.stopPropagation(); await resolveComment(c.id) })
    actions.appendChild(resolveBtn)

    // AI toggle: lets user convert AI comment to regular (survives publish) or vice versa.
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
      if (confirm("Delete this comment?")) await deleteComment(c.id)
    })
    actions.appendChild(deleteBtn)
  }

  box.appendChild(actions)
  return box
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

// --- Vertical positioning ---

let scrollListener: (() => void) | null = null

function positionComments() {
  if (!sidebarEl || !currentContentRoot) return

  const boxes = Array.from(sidebarEl.querySelectorAll<HTMLDivElement>(".comment-box"))
  if (boxes.length === 0) return

  // Reset all positioning.
  boxes.forEach(b => { b.style.marginTop = "" })

  const header = sidebarEl.querySelector(".comment-sidebar-header")
  const MIN_GAP = 4

  // For each box, compute its desired Y (= its anchor highlight's Y).
  // Then resolve collisions: if a box would overlap the previous one,
  // push it just below. This keeps each box as close to its anchor as
  // possible while preventing overlap.
  let lastBottom = header ? header.getBoundingClientRect().bottom : sidebarEl.getBoundingClientRect().top

  for (const box of boxes) {
    const commentId = box.dataset.commentId
    if (!commentId) continue
    const anchorEl = document.getElementById(commentId)
    if (!anchorEl) { lastBottom = box.getBoundingClientRect().bottom + MIN_GAP; continue }

    // Where we want the box: aligned with the anchor highlight.
    const desiredTop = anchorEl.getBoundingClientRect().top
    // Where the box currently sits (with default margin only).
    const currentTop = box.getBoundingClientRect().top
    // The earliest we can place it (just below the previous element).
    const earliestTop = lastBottom + MIN_GAP

    // Target = max(desired position, earliest non-overlapping position).
    const targetTop = Math.max(desiredTop, earliestTop)
    const offset = targetTop - currentTop
    if (Math.abs(offset) > 1) {
      box.style.marginTop = `${offset}px`
    }

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

// --- Create form in sidebar (positioned like a real comment) ---

function cleanupTempHighlight() {
  removeHighlights(TEMP_COMMENT_ID)
}

function showCreateForm(root: HTMLElement) {
  const anchor = extractAnchor(root)
  if (!anchor) {
    alert("Please select some text first.")
    return
  }

  const sel = window.getSelection()
  let selRange: Range | null = null
  if (sel && sel.rangeCount > 0) selRange = sel.getRangeAt(0).cloneRange()

  // Clear the browser selection.
  if (sel) sel.removeAllRanges()

  // If sidebar is hidden, un-hide it.
  if (sidebarHidden) {
    sidebarHidden = false
    removeCollapsedToggle()
    // Re-inject highlights and build sidebar from existing comments.
    currentComments.forEach(c => removeHighlights(c.id))
    if (currentComments.length > 0) {
      const orphanedIds = new Set<string>()
      for (const c of currentComments) {
        const range = findAnchorRange(root, c.anchor)
        if (range) injectHighlight(range, c.id)
        else orphanedIds.add(c.id)
      }
      renderSidebar(currentComments, orphanedIds)
      setupScrollListener()
    } else {
      // No comments yet — create a minimal sidebar.
      const sidebar = ensureSidebar()
      sidebar.innerHTML = ""
      const header = document.createElement("div")
      header.className = "comment-sidebar-header"
      header.title = "Click to hide comments"
      const arrowSpan = document.createElement("span")
      arrowSpan.className = "comment-header-arrow"
      arrowSpan.textContent = "\u25BC"
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
  } else if (currentComments.length === 0) {
    // Sidebar isn't hidden but no comments — make sure sidebar exists with header.
    const sidebar = ensureSidebar()
    if (!sidebar.querySelector(".comment-sidebar-header")) {
      const header = document.createElement("div")
      header.className = "comment-sidebar-header"
      const arrowSpan = document.createElement("span")
      arrowSpan.className = "comment-header-arrow"
      arrowSpan.textContent = "\u25BC"
      header.appendChild(arrowSpan)
      const titleSpan = document.createElement("span")
      titleSpan.textContent = " Comments (0)"
      header.appendChild(titleSpan)
      header.addEventListener("click", () => {
        sidebarHidden = true
        removeSidebar()
      })
      sidebar.appendChild(header)
    }
  }

  const sidebar = ensureSidebar()

  // Remove any previous create form and temp highlight.
  const existing = sidebar.querySelector(".comment-create-form")
  if (existing) existing.remove()
  cleanupTempHighlight()

  // Inject a temporary highlight at the selection so positionComments() can align the form.
  if (selRange) {
    injectHighlight(selRange, TEMP_COMMENT_ID)
  }

  // Build the form element (styled as a comment box).
  const form = document.createElement("div")
  form.className = "comment-box comment-create-form"
  form.dataset.commentId = TEMP_COMMENT_ID

  const selectedText = anchor.selected
  const label = document.createElement("div")
  label.className = "comment-create-label"
  label.textContent = `\u201C${selectedText.length > 60 ? selectedText.slice(0, 60) + "\u2026" : selectedText}\u201D`
  form.appendChild(label)

  const textarea = document.createElement("textarea")
  textarea.className = "comment-create-textarea"
  textarea.placeholder = "Write your comment\u2026"
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
    // If no real comments, remove sidebar entirely.
    if (currentComments.length === 0) removeSidebar()
    else requestAnimationFrame(() => positionComments())
  })
  btnRow.appendChild(cancelBtn)

  form.appendChild(btnRow)

  // Insert the form among existing comment boxes at the correct vertical
  // position (based on the temp highlight's position in the document).
  // positionComments() will then align it precisely.
  const tempAnchor = document.getElementById(TEMP_COMMENT_ID)
  if (tempAnchor) {
    const anchorTop = tempAnchor.getBoundingClientRect().top
    const boxes = sidebar.querySelectorAll<HTMLDivElement>(".comment-box")
    let insertBefore: Element | null = null
    for (const box of boxes) {
      const cid = box.dataset.commentId
      if (!cid || cid === TEMP_COMMENT_ID) continue
      const cAnchor = document.getElementById(cid)
      if (cAnchor && cAnchor.getBoundingClientRect().top > anchorTop) {
        insertBefore = box
        break
      }
    }
    if (insertBefore) {
      sidebar.insertBefore(form, insertBefore)
    } else {
      // Append before the resolved toggle if present, else at end.
      const resolvedToggle = sidebar.querySelector(".comment-resolved-toggle")
      if (resolvedToggle) sidebar.insertBefore(form, resolvedToggle)
      else sidebar.appendChild(form)
    }
  } else {
    sidebar.appendChild(form)
  }

  // Run positioning so the form aligns with its temp highlight.
  requestAnimationFrame(() => {
    positionComments()
    // Focus textarea and scroll it into view.
    textarea.focus()
    form.scrollIntoView({ block: "nearest", behavior: "smooth" })
  })
}

// --- API calls ---

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
  if (!currentContentRoot) return
  currentComments.forEach((c) => removeHighlights(c.id))
  cleanupTempHighlight()

  currentComments = await fetchComments(currentPagePath)

  if (currentComments.length > 0) {
    applyComments(currentContentRoot, currentComments)
  } else {
    removeSidebar()
    removeCollapsedToggle()
  }
}

function applyComments(root: HTMLElement, comments: CommentEntry[]) {
  const orphanedIds = new Set<string>()
  for (const c of comments) {
    const range = findAnchorRange(root, c.anchor)
    if (range) injectHighlight(range, c.id)
    else orphanedIds.add(c.id)
  }
  renderSidebar(comments, orphanedIds)
  setupScrollListener()
}

// --- Public API ---

export async function initComments(opts: {
  pagePath: string
  contentRoot: HTMLElement
  authFetch: (url: string, init?: RequestInit) => Promise<Response>
  username: string | null
  isAdmin: boolean
}) {
  currentPagePath = opts.pagePath
  currentContentRoot = opts.contentRoot
  currentAuthFetch = opts.authFetch
  currentUser = opts.username
  currentIsAdmin = opts.isAdmin
  sidebarHidden = false

  currentComments = await fetchComments(opts.pagePath)

  if (currentComments.length > 0) {
    applyComments(opts.contentRoot, currentComments)
  }
}

export function destroyComments() {
  currentComments.forEach((c) => removeHighlights(c.id))
  cleanupTempHighlight()
  removeSidebar()
  removeCollapsedToggle()
  removeScrollListener()
  currentComments = []
  currentContentRoot = null
}

export function reapplyComments() {
  if (!currentContentRoot || currentComments.length === 0) return
  // Re-try anchoring for all unresolved comments: remove existing highlights, re-anchor.
  currentComments.forEach((c) => removeHighlights(c.id))
  removeSidebar()
  removeCollapsedToggle()
  applyComments(currentContentRoot, currentComments)
}

export function addComment() {
  if (!currentContentRoot) return
  showCreateForm(currentContentRoot)
}

export function getCommentCount(): number {
  return currentComments.filter((c) => !c.resolved).length
}

export { clearAIComments }

export async function createAIComment(selectedText: string, beforeContext: string, afterContext: string, commentText: string): Promise<boolean> {
  if (!currentAuthFetch || !currentPagePath) return false
  const resp = await currentAuthFetch(`${API_BASE}${encodePagePath(currentPagePath)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      anchor: { selected: selectedText, before: beforeContext, after: afterContext },
      text: commentText,
      ai: true,
    }),
  })
  if (resp.ok) {
    await refreshComments()
    return true
  }
  return false
}

// --- Plugin registration ---

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
