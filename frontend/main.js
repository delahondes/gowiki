import { EditorState, TextSelection, Plugin } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { Slice } from "prosemirror-model"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { keymap } from "prosemirror-keymap"
import { baseKeymap, setBlockType, toggleMark, wrapIn } from "prosemirror-commands"
import { history, undo, redo } from "prosemirror-history"
import { icons } from "prosemirror-menu"
import { splitListItem, sinkListItem, liftListItem, wrapInList } from "prosemirror-schema-list"
import { markdownToPM } from "./compiler/markdown_to_pm.ts"
import { pmToMarkdown } from "./compiler/pm_to_markdown.ts"
import { buildRegistry } from "./compiler/build_registry.ts"
import { isPropertiesPanelEnabled } from "./compiler/core_ui.ts"
import { openMediaManager } from "./media_manager.js"
import { highlightCodeBlocks } from "./highlight.ts"
import { adjustFormula } from "./plugins/table.ts"
import { initComments, destroyComments, addComment, getCommentCount, reapplyComments } from "./plugins/comment.ts"
const HLJS_THEMES = [
  "github", "atom-one-light", "vs", "xcode", "idea",
  "github-dark", "atom-one-dark", "monokai", "nord", "vs2015", "tokyo-night-dark",
]

function loadHighlightTheme(theme) {
  if (!HLJS_THEMES.includes(theme)) theme = "github"
  let link = document.getElementById("gowiki-hljs-theme")
  if (!link) {
    link = document.createElement("link")
    link.id = "gowiki-hljs-theme"
    link.rel = "stylesheet"
    document.head.appendChild(link)
  }
  link.href = `/hljs/${theme}.css`
}

loadHighlightTheme("github")

const registry = buildRegistry(basicSchema)
const schema = registry.buildSchema()
registry.bindSchema(schema)

const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform)
function shortcutHint(label, key) {
  const mod = isMac ? "\u2318" : "Ctrl+"
  return `${label} (${mod}${key})`
}

function resolvePagePathFromLocation(loc) {
  const path = decodeURIComponent(loc.pathname || "/")
  if (path === "/") return "index"
  let trimmed = path.replace(/^\/+|\/+$/g, "")
  if (!trimmed) return "index"
  // Canonical: /foo/index → foo (namespace index page)
  if (trimmed.endsWith("/index")) trimmed = trimmed.slice(0, -6)
  else if (trimmed === "index") return "index"
  return trimmed
}

const pagePath = resolvePagePathFromLocation(window.location)
const pageDisplayPath = pagePath === "index" ? "/" : `/${pagePath}`
// For namespace index pages (URL ends with /), the namespace IS the pagePath.
// For leaf pages, the namespace is the parent directory.
const urlIsNamespaceIndex = (() => {
  const raw = decodeURIComponent(window.location.pathname || "/")
  return raw.endsWith("/") || raw.endsWith("/index")
})()
let pageNamespace = urlIsNamespaceIndex
  ? (pagePath === "index" ? "" : pagePath)
  : pagePath.includes("/")
    ? pagePath.split("/").slice(0, -1).join("/")
    : ""
const defaultMarkdown = `
## Gowiki

This is editable text.

- One
- Two
`

const appRoot = document.querySelector("#app")
const contentRoot = document.querySelector("#content")
const actionsRoot = document.querySelector("#actions")
const sidebarRoot = document.querySelector("#left")
const footerRoot = document.querySelector("#footer")

let currentUser = null // { username } or null
let editToken = null
let autoSaveTimer = null
let pageLockInfo = null // { locked_by, is_draft }
let currentPageVersion = 0
let currentPageMeta = null // full meta object from API: { version, author, created_by, updated_at, ... }

// Global media version cache: maps relative media paths (as in node attrs) to their max version.
window.__gowikiMediaVersions = new Map()
window.__gowikiUpdateMediaVersionCache = function(absPath, version) {
  const relPath = buildMediaReferencePath(pageNamespace, absPath)
  window.__gowikiMediaVersions.set(relPath, version)
}
let stashedEditorState = null // ProseMirror EditorState preserved across draft exit/resume
let historyLatestVersion = null // latest version number from history listing
let draftSavedThisSession = false // true once any draft save succeeds in this edit session
let lastSavedDraftMarkdown = null // markdown from the last successful draft save

let mode = "view"
let editMode = "visual"
let currentMarkdown = defaultMarkdown
let currentDoc = markdownToPM(defaultMarkdown, registry)
let editBaselineMarkdown = defaultMarkdown
let editorView = null
let rawEditor = null
let statusText = ""
let isNewPage = false
let hasTemplate = false
let isNamespaceIndex = false
let siteTitle = "Gowiki"
let siteVersion = ""
let tocMaxLevel = 3
let viewView = null
let sidebarView = null
let footerView = null
let isFullscreen = false

let reapplyTimer = null
const debouncedReapplyComments = () => {
  if (reapplyTimer) clearTimeout(reapplyTimer)
  reapplyTimer = setTimeout(reapplyComments, 100)
}

const tableCommands = new Map()
const extraCommands = []
let togglePropertiesCommand = null
let includeInsertCommand = null
let databaseInsertQueryCommand = null
let databaseInsertNewRowCommand = null
let databaseInsertRowCommand = null
let databaseInsertVarCommand = null
let tagInsertCommand = null
let tagQueryInsertCommand = null
let captionInsertRefCommand = null
let footnoteInsertCommand = null
let spoilerInsertCommand = null
let chartInsertCommand = null
let slidesInsertCommand = null
let todoInsertCommand = null
let todoListInsertCommand = null
let reviewflowInsertCommand = null
let reviewflowLinkInsertCommand = null
let versionLinkInsertCommand = null
let changesInsertCommand = null

registry.onCommand((namespace, name, cmd) => {
  if (namespace === "table") {
    tableCommands.set(name, cmd)
    return
  }

  if (namespace === "ui" && name === "properties.toggle") {
    togglePropertiesCommand = cmd
    return
  }

  if (namespace === "include") {
    if (name === "insert") includeInsertCommand = cmd
    return
  }

  if (namespace === "database") {
    if (name === "insertQuery") databaseInsertQueryCommand = cmd
    if (name === "insertNewRow") databaseInsertNewRowCommand = cmd
    if (name === "insertRow") databaseInsertRowCommand = cmd
    if (name === "insertVar") databaseInsertVarCommand = cmd
    return
  }

  if (namespace === "tag") {
    if (name === "insert") tagInsertCommand = cmd
    return
  }

  if (namespace === "tag-query") {
    if (name === "insert") tagQueryInsertCommand = cmd
    return
  }

  if (namespace === "caption") {
    if (name === "insertRef") captionInsertRefCommand = cmd
    return
  }

  if (namespace === "footnote") {
    if (name === "insert") footnoteInsertCommand = cmd
    return
  }

  if (namespace === "spoiler") {
    if (name === "insert") spoilerInsertCommand = cmd
    return
  }

  if (namespace === "chart") {
    if (name === "insert") chartInsertCommand = cmd
    return
  }

  if (namespace === "slides") {
    if (name === "insert") slidesInsertCommand = cmd
    return
  }

  if (namespace === "todo") {
    if (name === "insert") todoInsertCommand = cmd
    return
  }

  if (namespace === "todo-list") {
    if (name === "insert") todoListInsertCommand = cmd
    return
  }

  if (namespace === "reviewflow") {
    if (name === "insert") reviewflowInsertCommand = cmd
    return
  }

  if (namespace === "reviewflow-link") {
    if (name === "insert") reviewflowLinkInsertCommand = cmd
    return
  }

  if (namespace === "version-link") {
    if (name === "insert") versionLinkInsertCommand = cmd
    return
  }

  if (namespace === "changes") {
    if (name === "insert") changesInsertCommand = cmd
    return
  }

  extraCommands.push({ label: `${namespace}:${name}`, cmd })
})

function findLinkMarkInMarks(marks, linkType) {
  return marks.find(mark => mark.type === linkType) ?? null
}

function findLinkRangeAtPosition(doc, linkType, pos) {
  const $from = doc.resolve(pos)
  const parent = $from.parent
  const parentStart = $from.start()

  let childInfo = parent.childAfter($from.parentOffset)
  if (!childInfo.node && $from.parentOffset > 0) {
    childInfo = parent.childBefore($from.parentOffset)
  }
  if (!childInfo.node) return null

  const link = findLinkMarkInMarks(childInfo.node.marks, linkType)
  if (!link) return null

  let from = parentStart + childInfo.offset
  let to = from + childInfo.node.nodeSize

  for (let i = childInfo.index - 1; i >= 0; i--) {
    const prev = parent.child(i)
    if (!findLinkMarkInMarks(prev.marks, linkType)) break
    from -= prev.nodeSize
  }

  for (let i = childInfo.index + 1; i < parent.childCount; i++) {
    const next = parent.child(i)
    if (!findLinkMarkInMarks(next.marks, linkType)) break
    to += next.nodeSize
  }

  return { from, to, mark: link }
}

function classifyLinkTarget(rawTarget) {
  const target = (rawTarget ?? "").trim()
  if (target.length === 0) {
    return { ok: false, error: "URL/path cannot be empty." }
  }
  if (/^https?:\/\//i.test(target)) {
    return { ok: true, normalized: target, kind: "external" }
  }
  // Encode spaces as %20 so users can type paths with spaces.
  const normalized = target.replace(/ /g, "%20")
  // Accept #fragment (same-page anchor) or internal paths starting with /, ./, ../
  if (/^(#\S+|(\/(?!\/)|\.\/|\.\.\/)\S*)$/.test(normalized)) {
    return { ok: true, normalized, kind: "internal" }
  }
  return {
    ok: false,
    error:
      "Use http://, https://, #anchor, or an internal path starting with '/', './', or '../'.",
  }
}

function defaultLinkTextForTarget(target) {
  if (/^https?:\/\//i.test(target)) return target
  if (/^mailto:/i.test(target)) return target.replace(/^mailto:/i, "")
  const pathOnly = target.split(/[?#]/)[0]
  const clean = pathOnly.replace(/\/+$/, "")
  const parts = clean.split("/").filter(Boolean).filter(p => p !== "." && p !== "..")
  const raw = parts[parts.length - 1] ?? "index"
  try { return decodeURIComponent(raw) } catch { return raw }
}

function promptLinkForm(initialTarget, initialText) {
  return new Promise(resolve => {
    const overlay = document.createElement("div")
    overlay.className = "gowiki-link-modal-overlay"

    const dialog = document.createElement("div")
    dialog.className = "gowiki-link-modal"

    const title = document.createElement("div")
    title.className = "gowiki-link-modal-title"
    title.textContent = "Set link target"

    const textLabel = document.createElement("label")
    textLabel.className = "gowiki-link-modal-label"
    textLabel.textContent = "Link text"

    const textInput = document.createElement("input")
    textInput.type = "text"
    textInput.className = "gowiki-link-modal-input"
    textInput.value = initialText ?? ""
    textInput.placeholder = "(optional)"

    const targetLabel = document.createElement("label")
    targetLabel.className = "gowiki-link-modal-label"
    targetLabel.textContent = "Link target"

    const targetInput = document.createElement("input")
    targetInput.type = "text"
    targetInput.className = "gowiki-link-modal-input"
    targetInput.value = initialTarget
    targetInput.placeholder = "https://example.org or /namespace/page"

    const warning = document.createElement("div")
    warning.className = "gowiki-link-modal-warning"

    const buttons = document.createElement("div")
    buttons.className = "gowiki-link-modal-actions"

    const cancelBtn = document.createElement("button")
    cancelBtn.type = "button"
    cancelBtn.textContent = "Cancel"
    cancelBtn.className = "gowiki-link-modal-btn"

    const okBtn = document.createElement("button")
    okBtn.type = "button"
    okBtn.textContent = "OK"
    okBtn.className = "gowiki-link-modal-btn"

    function close(value) {
      overlay.remove()
      resolve(value)
    }

    function submit() {
      const verdict = classifyLinkTarget(targetInput.value)
      if (!verdict.ok) {
        warning.textContent = verdict.error
        targetInput.focus()
        return
      }
      close({
        target: verdict.normalized,
        text: textInput.value,
      })
    }

    cancelBtn.addEventListener("click", () => close(null))
    okBtn.addEventListener("click", submit)
    overlay.addEventListener("click", event => {
      if (event.target === overlay) close(null)
    })
    targetInput.addEventListener("keydown", event => {
      if (event.key === "Enter") {
        event.preventDefault()
        submit()
      } else if (event.key === "Escape") {
        event.preventDefault()
        close(null)
      }
    })
    textInput.addEventListener("keydown", event => {
      if (event.key === "Enter") {
        event.preventDefault()
        submit()
      } else if (event.key === "Escape") {
        event.preventDefault()
        close(null)
      }
    })

    buttons.appendChild(cancelBtn)
    buttons.appendChild(okBtn)
    dialog.appendChild(title)
    dialog.appendChild(textLabel)
    dialog.appendChild(textInput)
    dialog.appendChild(targetLabel)
    dialog.appendChild(targetInput)
    dialog.appendChild(warning)
    dialog.appendChild(buttons)
    overlay.appendChild(dialog)
    document.body.appendChild(overlay)
    targetInput.focus()
    targetInput.select()
  })
}

function findLinkRangeAtCursor(state) {
  const linkType = state.schema.marks.link
  if (!linkType) return null
  if (!state.selection.empty) return null
  return findLinkRangeAtPosition(state.doc, linkType, state.selection.from)
}

function isSameLinkMark(a, b) {
  if (!a || !b) return false
  return a.type === b.type && (a.attrs?.href ?? "") === (b.attrs?.href ?? "")
}

function findLinkRangeForSelection(state) {
  const linkType = state.schema.marks.link
  if (!linkType) return null
  if (state.selection.empty) return findLinkRangeAtCursor(state)

  const { from, to } = state.selection
  let selectedMark = null
  let hasNonWhitespace = false
  let invalid = false

  state.doc.nodesBetween(from, to, (node, pos) => {
    if (!node.isText || invalid) return
    const start = Math.max(from, pos)
    const end = Math.min(to, pos + node.nodeSize)
    if (end <= start) return

    const chunk = (node.text ?? "").slice(start - pos, end - pos)
    if (chunk.trim().length === 0) return
    hasNonWhitespace = true

    const mark = findLinkMarkInMarks(node.marks, linkType)
    if (!mark) {
      invalid = true
      return
    }
    if (!selectedMark) {
      selectedMark = mark
      return
    }
    if (!isSameLinkMark(selectedMark, mark)) {
      invalid = true
    }
  })

  if (invalid || !hasNonWhitespace || !selectedMark) return null

  let anchor = null
  state.doc.nodesBetween(from, to, (node, pos) => {
    if (!node.isText || anchor !== null) return
    const start = Math.max(from, pos)
    const end = Math.min(to, pos + node.nodeSize)
    if (end <= start) return

    const chunk = (node.text ?? "").slice(start - pos, end - pos)
    if (chunk.trim().length === 0) return
    const mark = findLinkMarkInMarks(node.marks, linkType)
    if (mark && isSameLinkMark(selectedMark, mark)) {
      anchor = start
    }
  })
  if (anchor === null) return null

  const linkAtAnchor = findLinkRangeAtPosition(state.doc, linkType, anchor)
  if (!linkAtAnchor || !isSameLinkMark(selectedMark, linkAtAnchor.mark)) return null
  return linkAtAnchor
}

function findWordRangeAtCursor(state) {
  if (!state.selection.empty) return null

  const $from = state.selection.$from
  const parent = $from.parent
  const parentStart = $from.start()
  const isWordChar = ch => /[A-Za-z0-9_]/.test(ch)

  let childInfo = parent.childAfter($from.parentOffset)
  if (!childInfo.node && $from.parentOffset > 0) {
    childInfo = parent.childBefore($from.parentOffset)
  }
  if (!childInfo.node || !childInfo.node.isText) return null

  const text = childInfo.node.text ?? ""
  const textOffset = Math.max(0, $from.parentOffset - childInfo.offset)

  let start = textOffset
  while (start > 0 && isWordChar(text[start - 1])) start--

  let end = textOffset
  while (end < text.length && isWordChar(text[end])) end++

  if (start === end) return null

  return {
    from: parentStart + childInfo.offset + start,
    to: parentStart + childInfo.offset + end,
  }
}

function setExternalLinkCommand() {
  return (state, dispatch, view) => {
    const linkType = state.schema.marks.link
    if (!linkType) return false

    let target = null
    let currentHref = ""
    let currentText = ""

    const linkForSelection = findLinkRangeForSelection(state)
    if (linkForSelection) {
      target = { from: linkForSelection.from, to: linkForSelection.to }
      currentHref = linkForSelection.mark.attrs.href ?? ""
      currentText = state.doc.textBetween(
        linkForSelection.from,
        linkForSelection.to,
        ""
      )
    } else if (!state.selection.empty) {
      target = { from: state.selection.from, to: state.selection.to }
      currentText = state.doc.textBetween(state.selection.from, state.selection.to, "")
    } else {
      const wordRange = findWordRangeAtCursor(state)
      if (wordRange) {
        target = wordRange
        currentText = state.doc.textBetween(wordRange.from, wordRange.to, "")
      }
    }

    const promptSeed = currentHref || "https://"
    void promptLinkForm(promptSeed, currentText).then(form => {
      if (form === null) return
      const activeState = view.state
      const activeDispatch = view.dispatch
      const activeLinkType = activeState.schema.marks.link
      if (!activeLinkType) return

      let activeTarget = null
      const activeLinkForSelection = findLinkRangeForSelection(activeState)
      if (activeLinkForSelection) {
        activeTarget = {
          from: activeLinkForSelection.from,
          to: activeLinkForSelection.to,
        }
      } else if (!activeState.selection.empty) {
        activeTarget = {
          from: activeState.selection.from,
          to: activeState.selection.to,
        }
      } else {
        activeTarget = findWordRangeAtCursor(activeState)
      }

      const normalized = form.target
      const isAutoText = form.text.trim().length === 0
      const displayText = isAutoText
        ? defaultLinkTextForTarget(normalized)
        : form.text

      const tr = activeState.tr
      const linkMark = activeLinkType.create({
        href: normalized,
        autoText: isAutoText,
      })
      let markFrom = activeState.selection.from
      let markTo = markFrom
      if (activeTarget) {
        tr.insertText(displayText, activeTarget.from, activeTarget.to)
        markFrom = activeTarget.from
        markTo = activeTarget.from + displayText.length
      } else {
        tr.insertText(displayText, activeState.selection.from, activeState.selection.to)
        markFrom = activeState.selection.from
        markTo = activeState.selection.from + displayText.length
      }
      tr.removeMark(markFrom, markTo, activeLinkType)
      tr.addMark(markFrom, markTo, linkMark)
      tr.setSelection(TextSelection.create(tr.doc, markTo))
      setStatus("Link updated")
      activeDispatch(tr.scrollIntoView())
      view.focus()
    })
    return true
  }
}

function applyStyles(styles) {
  for (const { id, css } of styles) {
    const styleId = `gowiki-style-${id}`
    const existing = document.getElementById(styleId)
    if (existing) {
      // Update if content changed (e.g. after HMR).
      if (existing.textContent !== css) existing.textContent = css
      continue
    }
    const style = document.createElement("style")
    style.id = styleId
    style.textContent = css
    document.head.appendChild(style)
  }
}

function encodePagePath(path) {
  return path
    .split("/")
    .filter(Boolean)
    .map(part => encodeURIComponent(part))
    .join("/")
}

function splitPathParts(raw) {
  return String(raw ?? "")
    .split("/")
    .filter(Boolean)
}

function encodePathSegments(segments) {
  return segments.map(s => encodeURIComponent(s)).join("/")
}

function buildMediaReferencePath(currentNamespace, mediaPath) {
  const from = splitPathParts(currentNamespace)
  const to = splitPathParts(mediaPath)

  let idx = 0
  while (idx < from.length && idx < to.length && from[idx] === to[idx]) {
    idx += 1
  }

  const upCount = from.length - idx
  const down = encodePathSegments(to.slice(idx))

  if (upCount <= 2) {
    if (upCount === 0) return "./" + down
    return "../".repeat(upCount) + down
  }

  return "/" + encodePathSegments(to)
}

function appendMediaVersion(target, version) {
  if (version && version > 1) return `${target}?v=${version}`
  return target
}

function mediaLabelFromPath(mediaPath) {
  const parts = splitPathParts(mediaPath)
  const raw = parts[parts.length - 1] ?? "file"
  try { return decodeURIComponent(raw) } catch { return raw }
}

function rawInsertMediaReference(kind, mediaEntry) {
  const textarea = document.querySelector(".gowiki-raw-editor")
  if (!textarea) return

  const target = appendMediaVersion(buildMediaReferencePath(pageNamespace, mediaEntry.path), mediaEntry.version)
  const label = mediaLabelFromPath(mediaEntry.path)

  let snippet
  if (kind === "image") {
    snippet = `![${label}](${target})`
  } else {
    snippet = `[${label}](${target})`
  }

  textarea.focus()
  const start = textarea.selectionStart
  const end = textarea.selectionEnd
  textarea.setSelectionRange(start, end)
  rawInsertText(textarea, snippet)
  setStatus("Inserted " + (kind === "image" ? "image" : "link") + " " + target)
}

function insertMediaReference(kind, mediaEntry) {
  if (!editorView || mode !== "edit" || editMode !== "visual") return

  const basePath = buildMediaReferencePath(pageNamespace, mediaEntry.path)
  const label = mediaLabelFromPath(mediaEntry.path)
  const version = (mediaEntry.version && mediaEntry.version > 1) ? String(mediaEntry.version) : null

  // Update media version cache so the property panel dropdown knows the max version.
  if (mediaEntry.version > 0) {
    window.__gowikiMediaVersions.set(basePath, mediaEntry.version)
  }
  const state = editorView.state
  const tr = state.tr

  if (kind === "image" && state.schema.nodes.image) {
    const imageNode = state.schema.nodes.image.create({
      src: basePath,
      alt: label,
      title: null,
      version,
    })
    tr.replaceSelectionWith(imageNode, false)
    editorView.dispatch(tr.scrollIntoView())
    editorView.focus()
    setStatus("Inserted image " + basePath)
    return
  }

  // Use medialink node for non-image media files
  if (state.schema.nodes.medialink) {
    const medialinkNode = state.schema.nodes.medialink.create({
      href: basePath,
      label,
      version,
      title: null,
      autoText: false,
    })
    tr.replaceSelectionWith(medialinkNode, false)
    editorView.dispatch(tr.scrollIntoView())
    editorView.focus()
    setStatus("Inserted media link " + basePath)
    return
  }

  // Fallback: use link mark if medialink node not available
  const target = appendMediaVersion(basePath, mediaEntry.version)
  const linkType = state.schema.marks.link
  if (!linkType) return

  const from = state.selection.from
  const to = state.selection.to
  tr.insertText(label, from, to)
  const end = from + label.length
  tr.addMark(from, end, linkType.create({ href: target, autoText: false }))
  tr.setSelection(TextSelection.create(tr.doc, from, end))
  editorView.dispatch(tr.scrollIntoView())
  editorView.focus()
  setStatus("Inserted link " + target)
}

class AccessDeniedError extends Error {
  constructor(path) {
    super(`Access denied: ${path}`)
    this.name = "AccessDeniedError"
  }
}

async function fetchPage(path) {
  const resp = await fetch(`/api/pages/${encodePagePath(path)}`)
  if (resp.status === 404) return null
  if (resp.status === 403) throw new AccessDeniedError(path)
  if (!resp.ok) {
    throw new Error(`Failed to load page ${path}: ${resp.status}`)
  }
  return await resp.json()
}

class CircularIncludeError extends Error {
  constructor(cycle) {
    super(`Circular include detected: ${cycle.join(" → ")}`)
    this.name = "CircularIncludeError"
    this.cycle = cycle
  }
}

async function savePage(path, markdown) {
  const resp = await authFetch(`/api/pages/${encodePagePath(path)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ markdown }),
  })
  if (resp.status === 422) {
    const body = await resp.json()
    if (body.cycle) {
      throw new CircularIncludeError(body.cycle)
    }
    throw new Error(body.error || `Save rejected: ${resp.status}`)
  }
  if (!resp.ok) {
    throw new Error(`Failed to save page ${path}: ${resp.status}`)
  }
  return await resp.json()
}

async function deleteMedia(mediaPath) {
  const resp = await authFetch(`/api/media/${encodePagePath(mediaPath)}`, {
    method: "DELETE",
  })
  return resp.ok
}

function promptOrphanDeletion(orphanedMedia) {
  const names = orphanedMedia.map((p) => p.split("/").pop())
  const msg =
    orphanedMedia.length === 1
      ? `"${names[0]}" is no longer referenced. Delete it?`
      : `${orphanedMedia.length} files are no longer referenced:\n${names.join(", ")}\nDelete them?`
  if (!confirm(msg)) return

  Promise.all(orphanedMedia.map(deleteMedia)).then((results) => {
    const deleted = results.filter(Boolean).length
    if (deleted > 0) {
      setStatus(`Deleted ${deleted} orphaned file${deleted > 1 ? "s" : ""}`)
    }
  })
}

// ── Paste helpers ─────────────────────────────────────

function buildMediaApiUrl(namespacePath) {
  const encoded = encodePagePath(namespacePath)
  return encoded ? `/api/media/${encoded}` : "/api/media"
}

function generatePasteFilename(originalName) {
  const ext = (originalName.match(/\.([^.]+)$/) ?? [])[1] ?? "png"
  const now = new Date()
  const ts = now.getFullYear().toString()
    + String(now.getMonth() + 1).padStart(2, "0")
    + String(now.getDate()).padStart(2, "0")
    + "-"
    + String(now.getHours()).padStart(2, "0")
    + String(now.getMinutes()).padStart(2, "0")
    + String(now.getSeconds()).padStart(2, "0")
  const rand = Math.random().toString(36).slice(2, 6)
  return `paste-${ts}-${rand}.${ext}`
}

async function uploadMediaFile(file) {
  // Rename to a unique timestamped name to avoid collisions
  const uniqueName = generatePasteFilename(file.name)
  const renamedFile = new File([file], uniqueName, { type: file.type })
  const form = new FormData()
  form.append("file", renamedFile)
  form.append("overwrite", "false")
  const resp = await authFetch(buildMediaApiUrl(pageNamespace), {
    method: "POST",
    body: form,
  })
  if (!resp.ok) {
    throw new Error(`Upload failed: ${resp.status}`)
  }
  const data = await resp.json()
  return data.entry
}

function extractImageFiles(clipboardData) {
  if (!clipboardData) return []
  // Check items first (broader coverage than files in some browsers)
  const items = clipboardData.items
  if (items) {
    const found = []
    for (let i = 0; i < items.length; i++) {
      const item = items[i]
      if (item.kind === "file" && /^image\//.test(item.type)) {
        const file = item.getAsFile()
        if (file) found.push(file)
      }
    }
    if (found.length > 0) return found
  }
  // Fallback to files
  const files = clipboardData.files
  if (files && files.length > 0) {
    return Array.from(files).filter(f => /^image\//.test(f.type))
  }
  return []
}

async function handleImageFilePaste(view, imageFiles) {
  for (const file of imageFiles) {
    try {
      const entry = await uploadMediaFile(file)
      const basePath = buildMediaReferencePath(pageNamespace, entry.path)
      const version = (entry.version && entry.version > 1) ? String(entry.version) : null
      const label = mediaLabelFromPath(entry.path)
      if (entry.version > 0) {
        window.__gowikiMediaVersions.set(basePath, entry.version)
      }

      const imageNode = schema.nodes.image.create({
        src: basePath,
        alt: label,
        title: null,
        version,
      })
      const tr = view.state.tr.replaceSelectionWith(imageNode, false)
      view.dispatch(tr.scrollIntoView())
      setStatus("Pasted image " + basePath)
    } catch (err) {
      console.error("Image paste upload failed", err)
      setStatus("Image paste failed: " + err.message)
    }
  }
}

function dataUrlToFile(dataUrl, baseName) {
  const commaIdx = dataUrl.indexOf(",")
  const header = dataUrl.slice(0, commaIdx)
  const base64 = dataUrl.slice(commaIdx + 1)
  const mime = (header.match(/data:([^;]+)/) ?? [])[1] ?? "application/octet-stream"
  const ext = mime.split("/")[1] ?? "bin"
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return new File([bytes], `${baseName}.${ext}`, { type: mime })
}

// Detect if markdown contains images with non-storable src (data URLs, file://, blob://)
// External http(s):// images are kept as-is — they render fine in <img> tags.
function hasNonLocalImages(md) {
  return /!\[[^\]]*\]\((data:|file:\/\/|blob:)/.test(md)
}

// Load an image URL into a canvas and return a blob
function fetchImageAsBlob(url) {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.crossOrigin = "anonymous"
    img.onload = () => {
      try {
        const canvas = document.createElement("canvas")
        canvas.width = img.naturalWidth
        canvas.height = img.naturalHeight
        const ctx = canvas.getContext("2d")
        ctx.drawImage(img, 0, 0)
        canvas.toBlob(blob => {
          if (blob) resolve(blob)
          else reject(new Error("Canvas toBlob failed"))
        }, "image/png")
      } catch (err) {
        reject(err)
      }
    }
    img.onerror = () => reject(new Error("Image load failed: " + url))
    img.src = url
  })
}

// Upload a single image from its URL (data:, file://, http(s)://) and return wiki path
async function uploadImageFromUrl(url) {
  let blob
  if (url.startsWith("data:")) {
    const file = dataUrlToFile(url, "pasted-image-" + Date.now())
    const entry = await uploadMediaFile(file)
    return appendMediaVersion(buildMediaReferencePath(pageNamespace, entry.path), entry.version)
  }
  // For file://, blob://, http(s):// — try to load via canvas
  blob = await fetchImageAsBlob(url)
  const ext = blob.type?.split("/")[1] ?? "png"
  const file = new File([blob], "pasted-image-" + Date.now() + "." + ext, { type: blob.type })
  const entry = await uploadMediaFile(file)
  return appendMediaVersion(buildMediaReferencePath(pageNamespace, entry.path), entry.version)
}

// Process markdown that contains non-storable image URLs: upload each and rewrite src
async function handleNonLocalImagePaste(view, md, slice, plainText) {
  const re = /!\[([^\]]*)\]\(((data:|file:\/\/|blob:)[^)]*)\)/g
  let result = md
  let match
  const replacements = []
  while ((match = re.exec(md)) !== null) {
    replacements.push({ fullMatch: match[0], alt: match[1], url: match[2] })
  }

  let uploadedCount = 0
  for (const { fullMatch, alt, url } of replacements) {
    try {
      const target = await uploadImageFromUrl(url)
      const newAlt = alt && !alt.startsWith("file://") && !alt.startsWith("http") ? alt : mediaLabelFromPath(target)
      result = result.replace(fullMatch, `![${newAlt}](${target})`)
      uploadedCount++
    } catch (err) {
      console.warn("Could not upload pasted image:", url, err)
      // Remove the un-uploadable image
      result = result.replace(fullMatch, "")
    }
  }

  try {
    const cleanDoc = markdownToPM(result, registry)
    let openStart = 0
    let openEnd = 0
    const fc = cleanDoc.content.firstChild
    if (cleanDoc.content.childCount === 1 && fc && fc.type === schema.nodes.paragraph) {
      openStart = Math.min(slice.openStart, 1)
      openEnd = Math.min(slice.openEnd, 1)
    }
    const tr = view.state.tr.replaceSelection(
      new Slice(cleanDoc.content, openStart, openEnd)
    )
    view.dispatch(tr)
    if (uploadedCount > 0) {
      setStatus(`Uploaded ${uploadedCount} pasted image${uploadedCount > 1 ? "s" : ""}`)
    }
  } catch {
    view.dispatch(view.state.tr.insertText(plainText))
  }
}

function cleanupPastedMarkdown(md) {
  // Convert common bullet-like characters at line starts to markdown list markers
  // ·(middle dot) •(bullet) ◦(white bullet) ▪(small square) ‣(triangular) ►(pointer) –(en dash) —(em dash)
  md = md.replace(/^([ \t]*)[·•◦▪‣►–—]\s+/gm, "$1- ")

  // Collapse excessive whitespace after numbered list markers: "1.       text" → "1. text"
  md = md.replace(/^(\d+\.)\s{2,}/gm, "$1 ")

  return md
}

function normalizeMarkdownForStorage(markdown) {
  const doc = markdownToPM(markdown, registry)
  const normalizedMarkdown = pmToMarkdown(doc, registry)

  // Second round-trip: verify stability
  let roundTripError = false
  try {
    const doc2 = markdownToPM(normalizedMarkdown, registry)
    const md3 = pmToMarkdown(doc2, registry)
    if (md3 !== normalizedMarkdown) {
      console.error("Round-trip validation failed: serialize→parse→serialize not stable")
      // Log the diff to help diagnose which construct is unstable.
      const lines1 = normalizedMarkdown.split("\n")
      const lines2 = md3.split("\n")
      const maxLines = Math.max(lines1.length, lines2.length)
      let firstDiff = -1
      for (let i = 0; i < maxLines; i++) {
        const a = lines1[i] ?? "(missing)"
        const b = lines2[i] ?? "(missing)"
        if (a !== b) {
          if (firstDiff === -1) {
            firstDiff = i
            // Show context: 5 lines before the first difference
            console.error("  --- context before first diff ---")
            for (let j = Math.max(0, i - 5); j < i; j++) {
              console.error(`  line ${j + 1}: ${JSON.stringify(lines1[j])}`)
            }
            console.error("  --- differences ---")
          }
          console.error(`  line ${i + 1} differs:`)
          console.error(`    pass1: ${JSON.stringify(a)}`)
          console.error(`    pass2: ${JSON.stringify(b)}`)
        }
      }
      if (lines1.length !== lines2.length) {
        console.error(`  line count: pass1=${lines1.length}, pass2=${lines2.length}`)
      }
      roundTripError = true
    }
  } catch (err) {
    console.error("Round-trip validation threw:", err)
    roundTripError = true
  }

  return {
    markdown: normalizedMarkdown,
    doc,
    changed: normalizedMarkdown !== markdown,
    roundTripError,
  }
}

// ── Database-row raw-mode validation ──

function extractDatabaseRowBlocks(markdown) {
  const lines = markdown.split("\n")
  const blocks = []
  const directiveRe = /^\{database-row\s+table=(?:"([^"]+)"|'([^']+)'|(\S+?))\s*\}$/
  const rowRe = /^\|(.+)\|(.+)\|$/
  const sepRe = /^\|[\s-]+\|[\s-]+\|$/
  let inCode = false
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].trimStart().startsWith("```")) { inCode = !inCode; continue }
    if (inCode) continue
    const m = lines[i].trim().match(directiveRe)
    if (!m) continue
    const table = m[1] || m[2] || m[3]
    const fields = {}
    i++
    while (i < lines.length && lines[i].trim() === "") i++
    if (i < lines.length && rowRe.test(lines[i])) i++ // header
    if (i < lines.length && sepRe.test(lines[i])) i++ // separator
    while (i < lines.length && rowRe.test(lines[i])) {
      const rm = lines[i].match(rowRe)
      if (rm) fields[rm[1].trim()] = rm[2].trim()
      i++
    }
    i--
    blocks.push({ table, fields })
  }
  return blocks
}

const databaseSchemaCache = new Map()

async function getDatabaseSchema(tableName) {
  const cached = databaseSchemaCache.get(tableName)
  if (cached && Date.now() - cached.fetchedAt < 60000) return cached.schema
  const resp = await fetch(`/api/database/${encodeURIComponent(tableName)}/schema`)
  if (!resp.ok) return null
  const schema = await resp.json()
  databaseSchemaCache.set(tableName, { schema, fetchedAt: Date.now() })
  return schema
}

async function validateDatabaseRows(markdown) {
  const errors = []
  const baselineBlocks = extractDatabaseRowBlocks(editBaselineMarkdown)
  const currentBlocks = extractDatabaseRowBlocks(markdown)

  // Reject multiple bound database-row blocks
  if (currentBlocks.length > 1) {
    errors.push("A page cannot contain more than one bound database-row block")
  }

  // On non-row-bound pages, reject any bound database-row block
  if (baselineBlocks.length === 0 && currentBlocks.length > 0) {
    errors.push("Cannot add a bound database-row block to a non-row-bound page — only the {database-row} placeholder is allowed here")
  }

  // Check that every baseline block still exists
  for (const base of baselineBlocks) {
    const match = currentBlocks.find(b => b.table === base.table)
    if (!match) {
      errors.push(`Cannot remove database-row block (table: ${base.table})`)
      continue
    }
    // Check that every baseline field still exists
    for (const fieldName of Object.keys(base.fields)) {
      if (!(fieldName in match.fields)) {
        errors.push(`Cannot remove field '${fieldName}' from database-row (table: ${base.table})`)
      }
    }
  }

  // Validate enum values against schema
  for (const block of currentBlocks) {
    const tableSchema = await getDatabaseSchema(block.table)
    if (!tableSchema || !tableSchema.fields) continue
    for (const field of tableSchema.fields) {
      if (!(field.name in block.fields)) continue
      const value = block.fields[field.name]
      if (field.type === "enum" && field.enum_values) {
        if (value !== "" && !field.enum_values.includes(value)) {
          errors.push(`Invalid value '${value}' for enum field '${field.name}' (table: ${block.table}). Allowed: ${field.enum_values.join(", ")}`)
        }
      } else if (field.type === "multi_enum" && field.enum_values) {
        if (value !== "") {
          const tokens = value.split(",").map(t => t.trim()).filter(Boolean)
          for (const token of tokens) {
            if (!field.enum_values.includes(token)) {
              errors.push(`Invalid value '${token}' in multi-enum field '${field.name}' (table: ${block.table}). Allowed: ${field.enum_values.join(", ")}`)
            }
          }
        }
      }
    }
  }

  return { valid: errors.length === 0, errors }
}

function applyNormalizedEditState(normalized) {
  currentMarkdown = normalized.markdown
  currentDoc = normalized.doc

  if (mode !== "edit") return

  if (editMode === "raw" && rawEditor) {
    if (rawEditor.value !== normalized.markdown) {
      rawEditor.value = normalized.markdown
    }
    autoResizeRawEditor(rawEditor)
    return
  }

  if (editMode === "visual" && editorView && normalized.changed) {
    const tr = editorView.state.tr
    tr.replaceWith(0, editorView.state.doc.content.size, normalized.doc.content)
    tr.setMeta("addToHistory", false)
    editorView.dispatch(tr)
  }
}

let statusToastEl = null
let statusToastTimer = null

function setStatus(text) {
  statusText = text
  if (!text) return
  if (!statusToastEl) {
    statusToastEl = document.createElement("div")
    statusToastEl.className = "gowiki-action-toast"
    document.body.appendChild(statusToastEl)
  }
  statusToastEl.textContent = text
  statusToastEl.classList.add("visible")
  clearTimeout(statusToastTimer)
  statusToastTimer = setTimeout(() => {
    statusToastEl.classList.remove("visible")
  }, 3000)
}

function setMode(nextMode) {
  if (mode !== "edit" && nextMode === "edit") {
    editBaselineMarkdown = currentMarkdown
  }

  if (mode === "edit" && nextMode !== "edit") {
    editBaselineMarkdown = currentMarkdown
    stopAutoSave()
  }

  mode = nextMode
  if (mode === "edit") {
    appRoot.classList.add("gowiki-editing")
    renderEdit(editMode)
    startAutoSave()
  } else {
    appRoot.classList.remove("gowiki-editing")
    renderView()
  }
  renderActions()
}

async function setEditMode(nextEditMode) {
  if (mode !== "edit") return
  if (nextEditMode === editMode) return

  let markdown = currentMarkdown
  if (editMode === "visual" && editorView) {
    markdown = pmToMarkdown(editorView.state.doc, registry)
  } else if (editMode === "raw" && rawEditor) {
    markdown = rawEditor.value
  }

  // Validate database-row blocks when leaving raw mode
  if (editMode === "raw") {
    const dbValidation = await validateDatabaseRows(markdown)
    if (!dbValidation.valid) {
      setStatus(dbValidation.errors.join("; "))
      return
    }
  }

  try {
    const normalized = normalizeMarkdownForStorage(markdown)
    currentMarkdown = normalized.markdown
    currentDoc = normalized.doc
  } catch (err) {
    console.error("Switch mode failed", err)
    setStatus("Invalid Markdown")
    return
  }

  editMode = nextEditMode
  renderEdit(editMode)
  renderActions()
}

function clearContent() {
  if (editorView) {
    editorView.destroy()
    editorView = null
  }
  if (viewView) {
    viewView.destroy()
    viewView = null
  }
  rawEditor = null
  destroyComments()
  document.removeEventListener("gowiki:node-rendered", debouncedReapplyComments)
  if (reapplyTimer) { clearTimeout(reapplyTimer); reapplyTimer = null }
  if (CSS.highlights) CSS.highlights.delete("search-highlight")
  contentRoot.innerHTML = ""
}

function isInCodeBlock(state) {
  const $from = state.selection.$from
  for (let d = $from.depth; d >= 0; d--) {
    if ($from.node(d).type.name === "code_block") return true
  }
  return false
}

function isInTableCell(state) {
  const $from = state.selection.$from
  for (let d = $from.depth; d >= 0; d--) {
    const name = $from.node(d).type.name
    if (name === "table_cell" || name === "table_header") return true
  }
  return false
}

function findAncestorOfType($pos, typeName) {
  for (let d = $pos.depth; d >= 0; d--) {
    const node = $pos.node(d)
    if (node.type.name === typeName) {
      return { depth: d, node, start: $pos.start(d) }
    }
  }
  return null
}

function getCodeBlockSelectionInfo(state) {
  const sel = state.selection
  const fromBlock = findAncestorOfType(sel.$from, "code_block")
  const toBlock = findAncestorOfType(sel.$to, "code_block")
  if (!fromBlock || !toBlock) return null
  if (fromBlock.start !== toBlock.start) return null

  const text = fromBlock.node.textContent ?? ""
  return {
    text,
    blockStart: fromBlock.start,
    from: sel.from - fromBlock.start,
    to: sel.to - fromBlock.start,
    empty: sel.empty,
  }
}

function collectTargetLineStarts(text, from, to, empty) {
  const starts = [0]
  for (let i = 0; i < text.length; i++) {
    if (text.charCodeAt(i) === 10) starts.push(i + 1)
  }

  const lineStartAt = pos => {
    let lo = 0
    let hi = starts.length - 1
    while (lo <= hi) {
      const mid = (lo + hi) >> 1
      if (starts[mid] <= pos) lo = mid + 1
      else hi = mid - 1
    }
    return starts[Math.max(0, hi)]
  }

  if (empty) {
    return [lineStartAt(from)]
  }

  const effectiveTo = to > from && text.charCodeAt(to - 1) === 10 ? to - 1 : to
  const first = lineStartAt(from)
  const last = lineStartAt(effectiveTo)
  return starts.filter(pos => pos >= first && pos <= last)
}

function remapPos(pos, changes) {
  let mapped = pos
  for (const change of changes) {
    const { at, delta } = change
    if (delta >= 0) {
      if (mapped >= at) mapped += delta
      continue
    }

    const removed = -delta
    if (mapped <= at) continue
    if (mapped <= at + removed) mapped = at
    else mapped += delta
  }
  return mapped
}

function applyCodeBlockIndent(direction) {
  return (state, dispatch) => {
    const info = getCodeBlockSelectionInfo(state)
    if (!info) return false

    const lineStarts = collectTargetLineStarts(info.text, info.from, info.to, info.empty)
    if (lineStarts.length === 0) return true

    const changes = []
    for (const at of lineStarts) {
      if (direction === "in") {
        changes.push({ at, remove: 0, insert: "  ", delta: 2 })
        continue
      }
      let remove = 0
      if (info.text.charCodeAt(at) === 32) remove++
      if (info.text.charCodeAt(at + 1) === 32) remove++
      if (remove > 0) changes.push({ at, remove, insert: "", delta: -remove })
    }

    if (changes.length === 0) return true

    let nextText = info.text
    for (let i = changes.length - 1; i >= 0; i--) {
      const c = changes[i]
      nextText = nextText.slice(0, c.at) + c.insert + nextText.slice(c.at + c.remove)
    }

    if (!dispatch) return true

    const mappedFrom = remapPos(info.from, changes)
    const mappedTo = remapPos(info.to, changes)

    let tr = state.tr.insertText(
      nextText,
      info.blockStart,
      info.blockStart + info.text.length
    )

    tr = tr.setSelection(
      TextSelection.create(tr.doc, info.blockStart + mappedFrom, info.blockStart + mappedTo)
    )

    dispatch(tr.scrollIntoView())
    return true
  }
}

function tabKeyCommand(direction) {
  const codeIndent = applyCodeBlockIndent(direction)

  return (state, dispatch) => {
    // Let table plugin own Tab behavior inside table cells.
    if (isInTableCell(state)) return false

    // In code blocks, Tab/Shift-Tab always indent/dedent line starts.
    if (isInCodeBlock(state)) {
      return codeIndent(state, dispatch)
    }

    // In headings, Tab/Shift-Tab adjusts heading level.
    const { $from } = state.selection
    if ($from.parent.type === state.schema.nodes.heading) {
      const node = $from.parent
      const level = node.attrs.level
      const newLevel = direction === "in" ? Math.min(level + 1, 6) : Math.max(level - 1, 1)
      if (newLevel !== level) {
        return setBlockType(state.schema.nodes.heading, {
          level: newLevel, numbered: node.attrs.numbered,
        })(state, dispatch)
      }
      return true
    }

    const itemType = state.schema.nodes.list_item
    if (itemType) {
      const listCmd = direction === "in" ? sinkListItem(itemType) : liftListItem(itemType)
      if (listCmd(state, dispatch)) return true
    }

    // Outside list/table/code block, do nothing but keep focus in editor.
    return true
  }
}


function backspaceEmptyListItemCommand() {
  return (state, dispatch) => {
    const sel = state.selection
    if (!sel.empty) return false

    const listItemType = state.schema.nodes.list_item
    if (!listItemType) return false

    const $from = sel.$from
    if ($from.parentOffset !== 0) return false

    let depth = $from.depth
    while (depth > 0 && $from.node(depth).type !== listItemType) depth--
    if (depth <= 0) return false

    const item = $from.node(depth)
    if (item.textContent.trim().length !== 0) return false

    const listDepth = depth - 1
    const indexInList = $from.index(listDepth)
    if (indexInList <= 0) return false

    const itemPos = $from.before(depth)
    const prevItem = $from.node(listDepth).child(indexInList - 1)
    const prevItemPos = itemPos - prevItem.nodeSize

    if (!dispatch) return true

    let tr = state.tr.delete(itemPos, itemPos + item.nodeSize)
    const cursorPos = Math.max(prevItemPos + 1, prevItemPos + prevItem.nodeSize - 2)
    tr = tr.setSelection(TextSelection.create(tr.doc, cursorPos))
    dispatch(tr.scrollIntoView())
    return true
  }
}

function insertHardBreakCommand() {
  return (state, dispatch) => {
    const hardBreak = state.schema.nodes.hard_break
    if (!hardBreak) return false
    if (!dispatch) return true
    dispatch(
      state.tr
        .replaceSelectionWith(hardBreak.create())
        .scrollIntoView()
    )
    return true
  }
}

function scrollSelectionIntoContentView(view) {
  const scroller = document.querySelector("#app") || contentRoot
  if (!(scroller instanceof HTMLElement)) return false

  const menu = view.dom.closest(".gowiki-raw-wrapper")?.querySelector(".gowiki-raw-menubar")
  const topInset = (menu instanceof HTMLElement ? menu.offsetHeight : 0) + 12
  const bottomInset = 10

  let fromCoords
  let toCoords
  try {
    fromCoords = view.coordsAtPos(view.state.selection.from)
    toCoords = view.coordsAtPos(view.state.selection.to)
  } catch {
    return false
  }

  const scrollerRect = scroller.getBoundingClientRect()
  const topLimit = scrollerRect.top + topInset
  const bottomLimit = scrollerRect.bottom - bottomInset
  const selectionTop = Math.min(fromCoords.top, toCoords.top)
  const selectionBottom = Math.max(fromCoords.bottom, toCoords.bottom)

  let delta = 0
  if (selectionTop < topLimit) {
    delta = selectionTop - topLimit
  } else if (selectionBottom > bottomLimit) {
    delta = selectionBottom - bottomLimit
  }

  if (delta !== 0) {
    scroller.scrollTop += delta
  }
  return true
}

const COPY_ICON = '<svg viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>'
const CHECK_ICON = '<svg viewBox="0 0 24 24"><polyline points="20 6 9 17 4 12"/></svg>'

function addCodeCopyButtons(container) {
  container.querySelectorAll("pre").forEach(pre => {
    const btn = document.createElement("button")
    btn.className = "gowiki-code-copy-btn"
    btn.title = "Copy code"
    btn.innerHTML = COPY_ICON
    btn.addEventListener("click", (e) => {
      e.preventDefault()
      e.stopPropagation()
      const code = pre.querySelector("code")
      if (!code) return
      navigator.clipboard.writeText(code.textContent || "").then(() => {
        btn.innerHTML = CHECK_ICON
        btn.classList.add("gowiki-code-copy-btn--copied")
        setTimeout(() => {
          btn.innerHTML = COPY_ICON
          btn.classList.remove("gowiki-code-copy-btn--copied")
        }, 1500)
      })
    })
    pre.appendChild(btn)
  })
}

function mountReadOnlyView(container, markdown, className) {
  const doc = markdownToPM(markdown, registry)
  const wrapper = document.createElement("div")
  if (className) wrapper.className = className
  container.appendChild(wrapper)
  const state = EditorState.create({
    doc,
    schema,
    plugins: registry.getEditorPlugins(),
  })
  const view = new EditorView(wrapper, {
    state,
    editable: () => false,
    // Override dispatchTransaction so that after every state update (which
    // internally calls domObserver.start()), we immediately re-stop the
    // observer.  Without this, async plugin transactions (e.g. link status
    // check) restart the observer and ProseMirror's selectionchange handler
    // fights the native browser selection — causing code-block selection to
    // blink and fail.
    dispatchTransaction(tr) {
      const newState = view.state.apply(tr)
      view.updateState(newState)
      view.dom.removeAttribute("contenteditable")
      if (view.domObserver && view.domObserver.stop) view.domObserver.stop()
    },
  })
  highlightCodeBlocks(wrapper)
  addCodeCopyButtons(wrapper)
  // Fold spoilers by default in view mode
  wrapper.querySelectorAll("details.gowiki-spoiler[open]").forEach(d => d.removeAttribute("open"))
  view.dom.removeAttribute("contenteditable")
  if (view.domObserver && view.domObserver.stop) view.domObserver.stop()
  // ProseMirror registers copy/cut/paste handlers on view.dom that call
  // preventDefault(), which suppresses the browser's native copy even when
  // the editor is non-editable.  Add a capture-phase handler that stops
  // the event from reaching ProseMirror's handler.
  for (const evt of ["copy", "cut", "paste"]) {
    view.dom.addEventListener(evt, (e) => e.stopImmediatePropagation(), true)
  }
  return view
}

async function fetchAndMountZone(path, container, className) {
  try {
    const page = await fetchPage(path)
    if (!page) return null
    return mountReadOnlyView(container, page.markdown, className)
  } catch {
    return null
  }
}

function buildTOC(container) {
  if (tocMaxLevel <= 0) return

  const selector = Array.from({ length: tocMaxLevel }, (_, i) => `h${i + 1}`).join(", ")
  const headings = container.querySelectorAll(selector)
  if (headings.length <= 1) return

  const toc = document.createElement("div")
  toc.className = "gowiki-toc"
  const title = document.createElement("div")
  title.className = "gowiki-toc-title"
  title.textContent = "Contents"
  toc.appendChild(title)

  const minLevel = Math.min(...Array.from(headings, h => parseInt(h.tagName.slice(1), 10)))

  const list = document.createElement("ul")
  for (const h of headings) {
    const level = parseInt(h.tagName.slice(1), 10)
    const id = h.id
    if (!id) continue
    const li = document.createElement("li")
    li.className = `gowiki-toc-level-${level - minLevel}`
    const a = document.createElement("a")
    a.href = `#${id}`
    const num = h.getAttribute("data-heading-number")
    a.textContent = (num ? num + " " : "") + h.textContent
    a.addEventListener("click", e => {
      e.preventDefault()
      const target = document.getElementById(id)
      if (target) {
        target.scrollIntoView({ behavior: "smooth" })
        window.history.replaceState(null, "", `#${id}`)
      }
    })
    li.appendChild(a)
    list.appendChild(li)
  }
  toc.appendChild(list)
  container.insertBefore(toc, container.firstChild)
}

function renderView() {
  clearContent()

  if (isNewPage) {
    const banner = document.createElement("div")
    banner.className = "gowiki-new-page-banner"
    banner.textContent = "This page does not exist. Switch to Edit mode to create it."
    contentRoot.appendChild(banner)
    return
  }

  viewView = mountReadOnlyView(contentRoot, currentMarkdown, "gowiki-view")

  // Highlight searched terms if navigating from search.
  const highlight = new URLSearchParams(window.location.search).get("highlight")
  if (highlight) {
    highlightTermsInView(contentRoot, highlight)
  }

  // Build table of contents for view mode.
  buildTOC(contentRoot)

  // Check for pending read acknowledgements.
  if (currentUser && !isNewPage) {
    checkReadAck(pagePath, currentPageVersion, contentRoot)
  }

  // Scroll to fragment anchor if present.
  if (window.location.hash) {
    const id = window.location.hash.slice(1)
    if (id) {
      const el = document.getElementById(id)
      if (el) el.scrollIntoView({ behavior: "smooth" })
    }
  }

  updatePageTitle()

  // Initialize comments overlay.
  if (!isNewPage) {
    initComments({
      pagePath: `/${pagePath}`,
      contentRoot,
      authFetch,
      username: currentUser?.username || null,
      isAdmin: currentUser?.groups?.includes("admin") || false,
    })
  }

  // Re-anchor comments and re-apply highlights when async nodes finish rendering.
  document.addEventListener("gowiki:node-rendered", debouncedReapplyComments)
  if (highlight) {
    document.addEventListener("gowiki:node-rendered", () => highlightTermsInView(contentRoot, highlight))
  }
}

async function checkReadAck(path, currentVersion, container) {
  try {
    const resp = await fetch(`/api/plugin/todo/v1/tasks/ack/${encodePagePath(path)}`)
    if (!resp.ok) return
    const data = await resp.json()
    const tasks = data.tasks || []
    if (tasks.length === 0) return

    for (const task of tasks) {
      // Skip tasks the user already acknowledged at the current (or later) version.
      if (task.previous_ack_version >= currentVersion) continue

      const banner = document.createElement("div")
      banner.className = "gowiki-ack-banner"

      const title = document.createElement("div")
      title.className = "gowiki-ack-title"
      title.textContent = task.title || "Read acknowledgement required"
      banner.appendChild(title)

      if (task.previous_ack_version > 0 && task.previous_ack_version < currentVersion) {
        const info = document.createElement("div")
        info.className = "gowiki-ack-info"
        info.textContent = `You previously acknowledged version ${task.previous_ack_version}. `
        const diffLink = document.createElement("a")
        diffLink.href = "#"
        diffLink.textContent = "View changes since your last acknowledgement"
        diffLink.addEventListener("click", (e) => {
          e.preventDefault()
          showDiff(task.previous_ack_version, 0)
        })
        info.appendChild(diffLink)
        banner.appendChild(info)
      }

      const row = document.createElement("div")
      row.className = "gowiki-ack-row"

      const label = document.createElement("label")
      label.className = "gowiki-ack-label"
      const checkbox = document.createElement("input")
      checkbox.type = "checkbox"
      label.appendChild(checkbox)
      const labelText = document.createTextNode(" I confirm that I have read and understood the contents of this page")
      label.appendChild(labelText)

      const btn = document.createElement("button")
      btn.className = "gowiki-ack-btn"
      btn.textContent = "Acknowledge"
      btn.disabled = true

      checkbox.addEventListener("change", () => { btn.disabled = !checkbox.checked })
      btn.addEventListener("click", async () => {
        btn.disabled = true
        btn.textContent = "Acknowledging…"
        try {
          const ackResp = await authFetch(`/api/plugin/todo/v1/tasks/${task.task_id}/acknowledge`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ version: currentVersion }),
          })
          if (ackResp.ok) {
            banner.className = "gowiki-ack-banner gowiki-ack-done"
            banner.textContent = "Acknowledged"
            setTimeout(() => banner.remove(), 2000)
          } else {
            btn.textContent = "Acknowledge"
            btn.disabled = false
            setStatus("Failed to acknowledge")
          }
        } catch {
          btn.textContent = "Acknowledge"
          btn.disabled = false
          setStatus("Failed to acknowledge")
        }
      })

      row.appendChild(label)
      row.appendChild(btn)
      banner.appendChild(row)
      container.appendChild(banner)
    }
  } catch {
    // Silently ignore ack check failures.
  }
}

function highlightTermsInView(container, query) {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return

  // Use CSS Custom Highlight API — it styles ranges without modifying the DOM,
  // so ProseMirror's async DOM reconciliation cannot discard our highlights.
  if (!CSS.highlights) return

  const ranges = []
  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT)

  while (walker.nextNode()) {
    const textNode = walker.currentNode
    const parent = textNode.parentElement
    if (!parent) continue
    if (parent.closest("pre, code, script, style")) continue

    const text = textNode.textContent.toLowerCase()

    const hits = []
    for (const term of terms) {
      let idx = 0
      while ((idx = text.indexOf(term, idx)) !== -1) {
        hits.push({ start: idx, length: term.length })
        idx += term.length
      }
    }
    if (hits.length === 0) continue

    hits.sort((a, b) => a.start - b.start)
    const merged = [hits[0]]
    for (let i = 1; i < hits.length; i++) {
      const prev = merged[merged.length - 1]
      if (hits[i].start < prev.start + prev.length) continue
      merged.push(hits[i])
    }

    for (const { start, length } of merged) {
      const range = new Range()
      range.setStart(textNode, start)
      range.setEnd(textNode, start + length)
      ranges.push(range)
    }
  }

  if (ranges.length > 0) {
    const hl = new Highlight(...ranges)
    CSS.highlights.set("search-highlight", hl)
  }
}

function autoResizeRawEditor(editorEl) {
  const scroller = document.querySelector("#app")
  const scrollTop = scroller ? scroller.scrollTop : 0
  editorEl.style.height = "0"
  editorEl.style.height = `${Math.max(editorEl.scrollHeight, 360)}px`
  if (scroller) scroller.scrollTop = scrollTop
}

// --- Raw mode menubar helpers ---

function rawInsertText(textarea, text) {
  textarea.focus()
  document.execCommand("insertText", false, text)
}

function rawWrapSelection(textarea, before, after) {
  const start = textarea.selectionStart
  const end = textarea.selectionEnd
  const selected = textarea.value.substring(start, end)

  if (start === end) {
    // No selection: insert syntax with cursor between markers
    textarea.focus()
    textarea.setSelectionRange(start, start)
    rawInsertText(textarea, before + after)
    textarea.setSelectionRange(start + before.length, start + before.length)
  } else {
    // Wrap selection
    textarea.focus()
    textarea.setSelectionRange(start, end)
    rawInsertText(textarea, before + selected + after)
    textarea.setSelectionRange(start + before.length, start + before.length + selected.length)
  }
}

function rawGetCurrentLineRange(textarea) {
  const val = textarea.value
  const pos = textarea.selectionStart
  let lineStart = pos
  while (lineStart > 0 && val[lineStart - 1] !== "\n") lineStart--
  let lineEnd = pos
  while (lineEnd < val.length && val[lineEnd] !== "\n") lineEnd++
  return { lineStart, lineEnd }
}

function rawGetSelectedLineRanges(textarea) {
  const val = textarea.value
  const selStart = textarea.selectionStart
  const selEnd = textarea.selectionEnd

  let lineStart = selStart
  while (lineStart > 0 && val[lineStart - 1] !== "\n") lineStart--

  let lineEnd = selEnd
  if (selEnd > selStart && val[selEnd - 1] === "\n") {
    lineEnd = selEnd - 1
  }
  while (lineEnd < val.length && val[lineEnd] !== "\n") lineEnd++

  // Split into individual line ranges
  const ranges = []
  let cur = lineStart
  while (cur <= lineEnd) {
    let end = val.indexOf("\n", cur)
    if (end === -1 || end > lineEnd) end = lineEnd
    ranges.push({ lineStart: cur, lineEnd: end })
    cur = end + 1
  }
  return ranges
}

function rawToggleLinePrefix(textarea, prefix) {
  const ranges = rawGetSelectedLineRanges(textarea)
  const val = textarea.value

  // Check if all lines already have the prefix
  const allHavePrefix = ranges.every(r =>
    val.substring(r.lineStart, r.lineStart + prefix.length) === prefix
  )

  textarea.focus()

  if (allHavePrefix) {
    // Remove prefix from all lines, working backwards to preserve positions
    for (let i = ranges.length - 1; i >= 0; i--) {
      const r = ranges[i]
      textarea.setSelectionRange(r.lineStart, r.lineStart + prefix.length)
      rawInsertText(textarea, "")
    }
  } else {
    // Add prefix to lines that don't have it, working backwards
    for (let i = ranges.length - 1; i >= 0; i--) {
      const r = ranges[i]
      if (val.substring(r.lineStart, r.lineStart + prefix.length) !== prefix) {
        textarea.setSelectionRange(r.lineStart, r.lineStart)
        rawInsertText(textarea, prefix)
      }
    }
  }
}

function rawSetHeadingLevel(textarea, level, numbered) {
  const { lineStart, lineEnd } = rawGetCurrentLineRange(textarea)
  const val = textarea.value
  const line = val.substring(lineStart, lineEnd)

  // Strip any existing heading prefix (including optional `1. ` numbered marker)
  const stripped = line.replace(/^#{1,6}\s*(?:1\.\s)?/, "")
  const numPrefix = numbered ? "1. " : ""
  const newPrefix = "#".repeat(level) + " " + numPrefix

  // If line already has this exact heading level (ignoring numbered), toggle it off
  const existingMatch = line.match(/^(#{1,6})\s/)
  const alreadyThisLevel = existingMatch && existingMatch[1].length === level && !numbered

  textarea.focus()
  textarea.setSelectionRange(lineStart, lineEnd)
  if (alreadyThisLevel) {
    rawInsertText(textarea, stripped)
    textarea.setSelectionRange(lineStart, lineStart + stripped.length)
  } else {
    const newLine = newPrefix + stripped
    rawInsertText(textarea, newLine)
    textarea.setSelectionRange(lineStart, lineStart + newLine.length)
  }
}

function rawInsertHorizontalRule(textarea) {
  const { lineStart, lineEnd } = rawGetCurrentLineRange(textarea)
  const val = textarea.value
  const line = val.substring(lineStart, lineEnd)

  textarea.focus()
  if (line.trim() === "") {
    // Replace empty line with hr
    textarea.setSelectionRange(lineStart, lineEnd)
    rawInsertText(textarea, "---")
  } else {
    // Insert hr on a new line after current line
    textarea.setSelectionRange(lineEnd, lineEnd)
    rawInsertText(textarea, "\n---")
  }
}

async function rawInsertLink(textarea) {
  const start = textarea.selectionStart
  const end = textarea.selectionEnd
  const selectedText = textarea.value.substring(start, end)

  const form = await promptLinkForm("https://", selectedText)
  if (!form) return

  const displayText = form.text.trim().length === 0
    ? defaultLinkTextForTarget(form.target)
    : form.text
  const md = `[${displayText}](${form.target})`

  textarea.focus()
  textarea.setSelectionRange(start, end)
  rawInsertText(textarea, md)
}

function rawInsertCodeBlock(textarea) {
  const start = textarea.selectionStart
  const end = textarea.selectionEnd
  const selected = textarea.value.substring(start, end)

  textarea.focus()
  if (selected.length > 0) {
    textarea.setSelectionRange(start, end)
    rawInsertText(textarea, "```\n" + selected + "\n```")
  } else {
    textarea.setSelectionRange(start, start)
    rawInsertText(textarea, "```\n\n```")
    // Place cursor inside the code block
    textarea.setSelectionRange(start + 4, start + 4)
  }
}

function rawInsertSpoiler(textarea) {
  const start = textarea.selectionStart
  const end = textarea.selectionEnd
  const selected = textarea.value.substring(start, end)

  textarea.focus()
  if (selected.length > 0) {
    textarea.setSelectionRange(start, end)
    rawInsertText(textarea, "```spoiler Details\n" + selected + "\n```")
  } else {
    textarea.setSelectionRange(start, start)
    rawInsertText(textarea, "```spoiler Details\n\n```")
    // Place cursor inside the spoiler
    textarea.setSelectionRange(start + 19, start + 19)
  }
}

function rawInsertChart(textarea) {
  const start = textarea.selectionStart
  const snippet = "```chart pie\nItem 1 = 30\nItem 2 = 50\nItem 3 = 20\n```"
  textarea.focus()
  textarea.setSelectionRange(start, start)
  rawInsertText(textarea, snippet)
  // Place cursor on the first data line
  const cursorPos = start + 13 // after "```chart pie\n"
  textarea.setSelectionRange(cursorPos, cursorPos)
}

function rawInsertSlides(textarea) {
  const start = textarea.selectionStart
  const snippet = "{slides}\n\n# Title Slide\n\n---\n\n# Slide 2\n\n---\n\n# Thank You"
  textarea.focus()
  textarea.setSelectionRange(start, start)
  rawInsertText(textarea, snippet)
  // Place cursor on the title
  const cursorPos = start + 12 // after "{slides}\n\n# "
  textarea.setSelectionRange(cursorPos, cursorPos)
}

// --- Raw ordered list renumbering ---

function rawRenumberOrderedLists(textarea) {
  const val = textarea.value
  const lines = val.split("\n")
  const origStart = textarea.selectionStart
  const origEnd = textarea.selectionEnd
  const olRe = /^(\s*)(\d+)(\. .*)$/

  const stack = [] // {indent, counter}
  let changed = false
  let startDelta = 0
  let endDelta = 0
  let pos = 0

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const m = line.match(olRe)

    if (m) {
      const indent = m[1].length
      const oldNum = m[2]

      // Pop entries deeper than current indent
      while (stack.length > 0 && stack[stack.length - 1].indent > indent) {
        stack.pop()
      }

      if (stack.length > 0 && stack[stack.length - 1].indent === indent) {
        stack[stack.length - 1].counter++
      } else {
        // Pop entries at same or deeper indent (shouldn't remain, but be safe)
        while (stack.length > 0 && stack[stack.length - 1].indent >= indent) {
          stack.pop()
        }
        stack.push({ indent, counter: 1 })
      }

      const newNum = String(stack[stack.length - 1].counter)
      if (oldNum !== newNum) {
        changed = true
        const delta = newNum.length - oldNum.length
        lines[i] = m[1] + newNum + m[3]
        const numEnd = pos + indent + oldNum.length
        if (numEnd <= origStart) startDelta += delta
        if (numEnd <= origEnd) endDelta += delta
      }
    } else if (/^\s*$/.test(line)) {
      // Blank line: don't break list continuity
    } else if (/^\s*- /.test(line)) {
      // Bullet list: clear ordered counters at this indent and deeper
      const indent = line.match(/^(\s*)/)[1].length
      while (stack.length > 0 && stack[stack.length - 1].indent >= indent) {
        stack.pop()
      }
    } else {
      // Non-list content: clear entire stack
      stack.length = 0
    }

    pos += line.length + 1
  }

  if (!changed) return

  const newText = lines.join("\n")
  textarea.focus()
  textarea.setSelectionRange(0, val.length)
  rawInsertText(textarea, newText)
  const newStart = Math.max(0, origStart + startDelta)
  const newEnd = Math.max(newStart, origEnd + endDelta)
  textarea.setSelectionRange(newStart, newEnd)
}

// --- Raw list keyboard handling ---

const rawListRe = /^(\s*)(- |\d+\. )/

function rawGetListPrefix(textarea) {
  const { lineStart, lineEnd } = rawGetCurrentLineRange(textarea)
  const line = textarea.value.substring(lineStart, lineEnd)
  const m = line.match(rawListRe)
  if (!m) return null
  return { lineStart, lineEnd, line, indent: m[1], marker: m[2], contentStart: m[1].length + m[2].length }
}

function rawHandleEnterInList(textarea) {
  const info = rawGetListPrefix(textarea)
  if (!info) return false

  const pos = textarea.selectionStart
  const contentAfterCursor = textarea.value.substring(pos, info.lineEnd)
  const contentBeforeCursor = textarea.value.substring(info.lineStart + info.contentStart, pos)

  // Empty list item: remove the prefix (exit list)
  if (contentBeforeCursor.trim() === "" && contentAfterCursor.trim() === "") {
    textarea.focus()
    textarea.setSelectionRange(info.lineStart, info.lineEnd)
    rawInsertText(textarea, "")
    return true
  }

  // Build the next list prefix at the same indent level
  let nextMarker = info.marker
  const orderedMatch = info.marker.match(/^(\d+)\. $/)
  if (orderedMatch) {
    nextMarker = (Number(orderedMatch[1]) + 1) + ". "
  }
  const insertion = "\n" + info.indent + nextMarker

  textarea.focus()
  textarea.setSelectionRange(pos, pos)
  rawInsertText(textarea, insertion)
  rawRenumberOrderedLists(textarea)
  return true
}

function rawListIndentWidth(line) {
  // Ordered lists (1. ) need 3-space indent per CommonMark; unordered (- ) need 2
  const stripped = line.replace(/^\s*/, "")
  return /^\d+\. /.test(stripped) ? 3 : 2
}

function rawHandleTabInList(textarea, direction) {
  const ranges = rawGetSelectedLineRanges(textarea)
  const val = textarea.value
  const listLines = ranges.filter(r => rawListRe.test(val.substring(r.lineStart, r.lineEnd)))
  if (listLines.length === 0) return false

  // Expand each selected root line to include its sub-items (deeper-indented lines below it).
  // Build all line ranges for the entire textarea to scan forward.
  const allRanges = []
  {
    let cur = 0
    while (cur < val.length) {
      let end = val.indexOf("\n", cur)
      if (end === -1) end = val.length
      allRanges.push({ lineStart: cur, lineEnd: end })
      cur = end + 1
    }
  }

  // When indenting, filter out roots that are the first item in their list —
  // you can't sublevel the first item since there's no sibling above it.
  let effectiveRoots = listLines
  if (direction === "in") {
    effectiveRoots = listLines.filter(root => {
      const rootLine = val.substring(root.lineStart, root.lineEnd)
      const rootIndent = rootLine.match(/^(\s*)/)[1].length
      const rootIsOrdered = /^\s*\d+\. /.test(rootLine)

      // Find this root's index in allRanges and scan backward
      let rootIdx = -1
      for (let i = 0; i < allRanges.length; i++) {
        if (allRanges[i].lineStart === root.lineStart) { rootIdx = i; break }
      }
      if (rootIdx <= 0) return false // first line of file, definitely first item

      for (let i = rootIdx - 1; i >= 0; i--) {
        const prevLine = val.substring(allRanges[i].lineStart, allRanges[i].lineEnd)
        if (prevLine.trim() === "") continue // blank line, keep scanning
        const prevIndent = prevLine.match(/^(\s*)/)[1].length
        if (prevIndent > rootIndent) continue // continuation or sub-item, keep scanning
        if (prevIndent < rootIndent) return false // shallower level: root is first at its level
        // Same indent: must be a list item of the same type (both ordered or both unordered)
        if (!rawListRe.test(prevLine)) return false // not a list item
        const prevIsOrdered = /^\s*\d+\. /.test(prevLine)
        if (prevIsOrdered !== rootIsOrdered) return false // different list type = first item of new list
        return true // same-type sibling found, can indent
      }
      return false // reached start of file, first item
    })
    if (effectiveRoots.length === 0) return false
  }

  // Collect the full set of lines to indent: roots + their sub-items
  const linesToProcess = new Set()
  for (const root of effectiveRoots) {
    linesToProcess.add(root.lineStart)
    const rootLine = val.substring(root.lineStart, root.lineEnd)
    const rootIndent = rootLine.match(/^(\s*)/)[1].length

    // Find this root's index in allRanges
    let rootIdx = -1
    for (let i = 0; i < allRanges.length; i++) {
      if (allRanges[i].lineStart === root.lineStart) { rootIdx = i; break }
    }
    if (rootIdx === -1) continue

    // Scan forward: collect all subsequent lines at deeper indent (sub-items)
    for (let i = rootIdx + 1; i < allRanges.length; i++) {
      const subLine = val.substring(allRanges[i].lineStart, allRanges[i].lineEnd)
      if (subLine.trim() === "") {
        // Blank line: include it (may be inside a list), keep scanning
        linesToProcess.add(allRanges[i].lineStart)
        continue
      }
      const subIndent = subLine.match(/^(\s*)/)[1].length
      if (subIndent > rootIndent) {
        linesToProcess.add(allRanges[i].lineStart)
      } else {
        break
      }
    }
  }

  // Remove trailing blank lines that aren't actually sub-items
  // (they were speculatively included while scanning)

  // Build the final sorted list of line ranges to process
  const processRanges = allRanges.filter(r => linesToProcess.has(r.lineStart))

  const origStart = textarea.selectionStart
  const origEnd = textarea.selectionEnd
  textarea.focus()

  let startDelta = 0
  let endDelta = 0

  if (direction === "in") {
    // Use the indent width of the first root for all lines in this operation
    const firstRootLine = val.substring(effectiveRoots[0].lineStart, effectiveRoots[0].lineEnd)
    const indentStr = " ".repeat(rawListIndentWidth(firstRootLine))

    for (let i = processRanges.length - 1; i >= 0; i--) {
      const r = processRanges[i]
      const line = val.substring(r.lineStart, r.lineEnd)
      if (line.trim() === "") continue // skip blank lines
      textarea.setSelectionRange(r.lineStart, r.lineStart)
      rawInsertText(textarea, indentStr)
      if (r.lineStart <= origStart) startDelta += indentStr.length
      if (r.lineStart <= origEnd) endDelta += indentStr.length
    }
  } else {
    const firstRootLine = val.substring(listLines[0].lineStart, listLines[0].lineEnd)
    const width = rawListIndentWidth(firstRootLine)

    for (let i = processRanges.length - 1; i >= 0; i--) {
      const r = processRanges[i]
      const line = val.substring(r.lineStart, r.lineEnd)
      if (line.trim() === "") continue // skip blank lines
      let remove = 0
      for (let j = 0; j < width && j < line.length; j++) {
        if (line.charCodeAt(j) === 32) remove++
        else break
      }
      if (remove > 0) {
        textarea.setSelectionRange(r.lineStart, r.lineStart + remove)
        rawInsertText(textarea, "")
        if (r.lineStart < origStart) {
          startDelta -= Math.min(remove, origStart - r.lineStart)
        }
        if (r.lineStart < origEnd) {
          endDelta -= Math.min(remove, origEnd - r.lineStart)
        }
      }
    }
  }

  const newStart = Math.max(0, origStart + startDelta)
  const newEnd = Math.max(newStart, origEnd + endDelta)
  textarea.setSelectionRange(newStart, newEnd)
  rawRenumberOrderedLists(textarea)
  return true
}

function rawInsertInclude(textarea) {
  const snippet = "{include path=}"
  const start = textarea.selectionStart
  textarea.focus()
  textarea.setSelectionRange(start, textarea.selectionEnd)
  rawInsertText(textarea, snippet)
  // Place cursor between "=" and "}", ready to type the path
  const cursorPos = start + snippet.length - 1
  textarea.setSelectionRange(cursorPos, cursorPos)
}

// --- Raw table helpers ---

function rawGetTableContext(textarea) {
  const val = textarea.value
  const pos = textarea.selectionStart
  const pipeRe = /^\s*\|/

  // Find current line
  let lineStart = pos
  while (lineStart > 0 && val[lineStart - 1] !== "\n") lineStart--
  let lineEnd = pos
  while (lineEnd < val.length && val[lineEnd] !== "\n") lineEnd++
  const curLine = val.substring(lineStart, lineEnd)
  if (!pipeRe.test(curLine)) return null

  // Find table boundaries (contiguous pipe lines)
  const lines = val.split("\n")
  let offset = 0
  let curLineIdx = -1
  for (let i = 0; i < lines.length; i++) {
    if (offset === lineStart) { curLineIdx = i; break }
    offset += lines[i].length + 1
  }
  if (curLineIdx < 0) return null

  let tableFirstLine = curLineIdx
  while (tableFirstLine > 0 && pipeRe.test(lines[tableFirstLine - 1])) tableFirstLine--
  // Skip a {table ...} directive just above the table
  if (tableFirstLine > 0 && /^\s*\{table\s/.test(lines[tableFirstLine - 1])) tableFirstLine--

  let tableLastLine = curLineIdx
  while (tableLastLine < lines.length - 1 && pipeRe.test(lines[tableLastLine + 1])) tableLastLine++

  return { lines, curLineIdx, tableFirstLine, tableLastLine }
}

function rawParsePipeRow(line) {
  const trimmed = line.trim()
  if (!trimmed.startsWith("|") || !trimmed.endsWith("|")) return null
  const inner = trimmed.slice(1, -1)
  return inner.split("|").map(c => c.trim())
}

function rawBuildPipeRow(cells) {
  return "| " + cells.join(" | ") + " |"
}

function rawIsSeparatorRow(line) {
  const trimmed = line.trim()
  if (!trimmed.startsWith("|") || !trimmed.endsWith("|")) return false
  const cells = trimmed.slice(1, -1).split("|")
  // Every cell must contain at least one dash
  return cells.length > 0 && cells.every(c => /^\s*:?-+:?\s*$/.test(c))
}

// Convert a table line index to a 0-based formula row (skipping separator)
function rawLineToFormulaRow(ctx, lineIdx) {
  const firstData = ctx.tableFirstLine + (/^\s*\{table\s/.test(ctx.lines[ctx.tableFirstLine]) ? 1 : 0)
  let formulaRow = 0
  for (let i = firstData; i <= ctx.tableLastLine; i++) {
    if (rawIsSeparatorRow(ctx.lines[i])) continue
    if (i === lineIdx) return formulaRow
    formulaRow++
  }
  return formulaRow
}

// Scan all cells in the table for formulas and adjust their references
function rawAdjustFormulas(ctx, changeType, changeIndex) {
  const firstData = ctx.tableFirstLine + (/^\s*\{table\s/.test(ctx.lines[ctx.tableFirstLine]) ? 1 : 0)

  for (let i = firstData; i <= ctx.tableLastLine; i++) {
    if (rawIsSeparatorRow(ctx.lines[i])) continue
    const cells = rawParsePipeRow(ctx.lines[i])
    if (!cells) continue

    let changed = false
    for (let c = 0; c < cells.length; c++) {
      const cell = cells[c].trim()
      if (cell.startsWith("=") && cell.length > 1) {
        const formula = cell.slice(1)
        const adjusted = adjustFormula(formula, changeType, changeIndex)
        if (adjusted !== formula) {
          cells[c] = "=" + adjusted
          changed = true
        }
      }
    }

    if (changed) {
      ctx.lines[i] = rawBuildPipeRow(cells)
    }
  }
}

// Adjust column specs (col1.align, col2-5.width, etc.) in the {table ...} directive line
function rawAdjustColumnSpecs(ctx, changeType, changeIndex) {
  const directiveLine = ctx.lines[ctx.tableFirstLine]
  if (!/^\s*\{table\s/.test(directiveLine)) return

  // changeIndex is 0-based column index; col specs use 1-based numbering
  const oneBasedIdx = changeIndex + 1
  const isInsert = changeType === "insertCol"

  const adjusted = directiveLine.replace(
    /col(\d+)(\+|-(?=\.))?\.(align|width|color)|col(\d+)-(\d+)\.(align|width|color)|col\.(align|width|color)/g,
    (match, singleNum, singleSuffix, _singleProp, rangeStart, rangeEnd, _rangeProp, _allProp) => {
      // col.prop — never changes
      if (match.startsWith("col.")) return match

      if (rangeStart !== undefined) {
        // Range: col3-8.prop
        let start = parseInt(rangeStart)
        let end = parseInt(rangeEnd)
        let newStart = start
        let newEnd = end
        if (isInsert) {
          // Extend range when insert is strictly inside
          if (oneBasedIdx > start && oneBasedIdx <= end) {
            newEnd++
          } else {
            if (newStart >= oneBasedIdx) newStart++
            if (newEnd >= oneBasedIdx) newEnd++
          }
        } else {
          // Delete
          if (start === end && start === oneBasedIdx) return "\x00REMOVE\x00"
          if (oneBasedIdx >= start && oneBasedIdx <= end) {
            newEnd--
          } else {
            if (newStart > oneBasedIdx) newStart--
            if (newEnd > oneBasedIdx) newEnd--
          }
        }
        if (newStart === newEnd) {
          return match.replace(/col\d+-\d+\./, `col${newStart}.`)
        }
        return match.replace(/col\d+-\d+\./, `col${newStart}-${newEnd}.`)
      }

      const num = parseInt(singleNum)
      if (singleSuffix === "+") {
        // col3+.prop — shift N when insert/delete strictly before N
        if (isInsert) {
          const newN = oneBasedIdx < num ? num + 1 : num
          return match.replace(/col\d+\+\./, `col${newN}+.`)
        } else {
          if (oneBasedIdx < num) {
            return match.replace(/col\d+\+\./, `col${num - 1}+.`)
          }
          return match
        }
      }

      if (singleSuffix === "-") {
        // col3-.prop — shift N when insert/delete at or before N
        if (isInsert) {
          const newN = oneBasedIdx <= num ? num + 1 : num
          return match.replace(/col\d+-\./, `col${newN}-.`)
        } else {
          if (oneBasedIdx <= num) {
            const newN = num - 1
            if (newN < 1) return "\x00REMOVE\x00"
            return match.replace(/col\d+-\./, `col${newN}-.`)
          }
          return match
        }
      }

      // Single: col3.prop
      if (isInsert) {
        const newIdx = num >= oneBasedIdx ? num + 1 : num
        return match.replace(/col\d+\./, `col${newIdx}.`)
      } else {
        if (num === oneBasedIdx) return "\x00REMOVE\x00"
        const newIdx = num > oneBasedIdx ? num - 1 : num
        return match.replace(/col\d+\./, `col${newIdx}.`)
      }
    }
  )

  // Remove sentinel-marked specs and clean up (handles both unquoted and quoted values)
  let cleaned = adjusted.replace(/\s*\x00REMOVE\x00=(?:"[^"]*"|\S+)/g, "")
  // If directive is now empty (only "{table }"), remove the line
  if (/^\s*\{table\s*\}\s*$/.test(cleaned)) {
    ctx.lines.splice(ctx.tableFirstLine, 1)
    ctx.tableLastLine--
  } else {
    // Collapse multiple spaces
    cleaned = cleaned.replace(/  +/g, " ")
    ctx.lines[ctx.tableFirstLine] = cleaned
  }
}

function rawTableColCount(ctx) {
  for (let i = ctx.tableFirstLine; i <= ctx.tableLastLine; i++) {
    const cells = rawParsePipeRow(ctx.lines[i])
    if (cells && !rawIsSeparatorRow(ctx.lines[i])) return cells.length
  }
  return 0
}

// Get current cell coordinates (row index within table, column index)
function rawGetCellCoords(textarea, ctx) {
  const val = textarea.value
  const pos = textarea.selectionStart
  const lineStart = ctx.lines.slice(0, ctx.curLineIdx).reduce((s, l) => s + l.length + 1, 0)
  const lineEnd = lineStart + ctx.lines[ctx.curLineIdx].length
  const pipes = rawFindPipes(val, lineStart, lineEnd)
  let col = 0
  for (let i = 0; i < pipes.length - 1; i++) {
    if (pos >= pipes[i] && pos <= pipes[i + 1]) { col = i; break }
  }
  return { row: ctx.curLineIdx, col }
}

// Position cursor at cell (row, col) in the table after replacement
function rawFocusCell(textarea, ctx, row, col) {
  const newVal = textarea.value
  // Compute offset of tableFirstLine
  let tableOffset = 0
  const allLines = newVal.split("\n")
  for (let i = 0; i < ctx.tableFirstLine; i++) tableOffset += allLines[i].length + 1

  // Find the target row offset
  let targetLineOffset = tableOffset
  for (let i = ctx.tableFirstLine; i < row && i <= ctx.tableLastLine; i++) {
    targetLineOffset += allLines[i].length + 1
  }
  if (row > ctx.tableLastLine) return

  const targetLine = allLines[row]
  if (!targetLine) return
  const lineStart = targetLineOffset
  const lineEnd = lineStart + targetLine.length
  const pipes = rawFindPipes(newVal, lineStart, lineEnd)

  // Clamp col to available cells
  const maxCol = Math.max(0, pipes.length - 2)
  const c = Math.min(col, maxCol)
  if (pipes.length >= 2 && c < pipes.length - 1) {
    rawSelectCell(textarea, pipes[c], pipes[c + 1])
  }
}

function rawReplaceLines(textarea, ctx, origTableLastLine) {
  const val = textarea.value
  const allLines = val.split("\n")
  const origLast = origTableLastLine ?? ctx.tableLastLine
  // Compute char offsets of the original table region
  let startOffset = 0
  for (let i = 0; i < ctx.tableFirstLine; i++) startOffset += allLines[i].length + 1
  let endOffset = startOffset
  for (let i = ctx.tableFirstLine; i <= origLast; i++) endOffset += allLines[i].length + 1
  if (endOffset > 0 && endOffset <= val.length + 1) endOffset-- // trim trailing newline

  const replacement = ctx.lines.slice(ctx.tableFirstLine, ctx.tableLastLine + 1).join("\n")
  const scroller = document.querySelector("#app")
  const savedScroll = scroller ? scroller.scrollTop : 0
  textarea.focus()
  textarea.setSelectionRange(startOffset, Math.min(endOffset, val.length))
  rawInsertText(textarea, replacement)
  if (scroller) scroller.scrollTop = savedScroll
}

function rawInsertTable(textarea) {
  const row1 = "| Header 1 | Header 2 | Header 3 |"
  const sep  = "| --- | --- | --- |"
  const row2 = "|  |  |  |"
  const snippet = row1 + "\n" + sep + "\n" + row2

  const { lineStart, lineEnd } = rawGetCurrentLineRange(textarea)
  const line = textarea.value.substring(lineStart, lineEnd)
  textarea.focus()
  if (line.trim() === "") {
    textarea.setSelectionRange(lineStart, lineEnd)
    rawInsertText(textarea, snippet)
  } else {
    textarea.setSelectionRange(lineEnd, lineEnd)
    rawInsertText(textarea, "\n" + snippet)
  }
}

function rawTableAddRowBelow(textarea) {
  const ctx = rawGetTableContext(textarea)
  if (!ctx) return
  const coords = rawGetCellCoords(textarea, ctx)
  const cols = rawTableColCount(ctx)
  if (cols === 0) return
  const changeIndex = rawLineToFormulaRow(ctx, ctx.curLineIdx) + 1
  const origLast = ctx.tableLastLine
  const newRow = rawBuildPipeRow(Array(cols).fill(""))
  ctx.lines.splice(ctx.curLineIdx + 1, 0, newRow)
  ctx.tableLastLine++
  rawAdjustFormulas(ctx, "insertRow", changeIndex)
  rawReplaceLines(textarea, ctx, origLast)
  rawFocusCell(textarea, ctx, coords.row, coords.col)
}

function rawTableAddRowAbove(textarea) {
  const ctx = rawGetTableContext(textarea)
  if (!ctx) return
  const coords = rawGetCellCoords(textarea, ctx)
  const cols = rawTableColCount(ctx)
  if (cols === 0) return
  const firstDataLine = ctx.tableFirstLine + (/^\s*\{table\s/.test(ctx.lines[ctx.tableFirstLine]) ? 1 : 0)
  if (ctx.curLineIdx <= firstDataLine + 1) return
  const changeIndex = rawLineToFormulaRow(ctx, ctx.curLineIdx)
  const origLast = ctx.tableLastLine
  const newRow = rawBuildPipeRow(Array(cols).fill(""))
  ctx.lines.splice(ctx.curLineIdx, 0, newRow)
  ctx.tableLastLine++
  rawAdjustFormulas(ctx, "insertRow", changeIndex)
  rawReplaceLines(textarea, ctx, origLast)
  // Cursor row shifted down by 1 due to insertion above
  rawFocusCell(textarea, ctx, coords.row + 1, coords.col)
}

function rawTableAddColumnRight(textarea) {
  const ctx = rawGetTableContext(textarea)
  if (!ctx) return
  const coords = rawGetCellCoords(textarea, ctx)
  const cols = rawTableColCount(ctx)
  const firstData = ctx.tableFirstLine + (/^\s*\{table\s/.test(ctx.lines[ctx.tableFirstLine]) ? 1 : 0)

  const insertIdx = coords.col + 1
  for (let i = firstData; i <= ctx.tableLastLine; i++) {
    const row = rawParsePipeRow(ctx.lines[i])
    if (!row) continue
    if (rawIsSeparatorRow(ctx.lines[i])) {
      row.splice(insertIdx, 0, "---")
    } else {
      row.splice(insertIdx, 0, "")
    }
    ctx.lines[i] = rawBuildPipeRow(row)
  }
  rawAdjustFormulas(ctx, "insertCol", insertIdx)
  rawAdjustColumnSpecs(ctx, "insertCol", insertIdx)
  rawReplaceLines(textarea, ctx)
  rawFocusCell(textarea, ctx, coords.row, coords.col)
}

function rawTableAddColumnLeft(textarea) {
  const ctx = rawGetTableContext(textarea)
  if (!ctx) return
  const coords = rawGetCellCoords(textarea, ctx)
  const firstData = ctx.tableFirstLine + (/^\s*\{table\s/.test(ctx.lines[ctx.tableFirstLine]) ? 1 : 0)

  const insertIdx = coords.col
  for (let i = firstData; i <= ctx.tableLastLine; i++) {
    const row = rawParsePipeRow(ctx.lines[i])
    if (!row) continue
    if (rawIsSeparatorRow(ctx.lines[i])) {
      row.splice(insertIdx, 0, "---")
    } else {
      row.splice(insertIdx, 0, "")
    }
    ctx.lines[i] = rawBuildPipeRow(row)
  }
  rawAdjustFormulas(ctx, "insertCol", insertIdx)
  rawAdjustColumnSpecs(ctx, "insertCol", insertIdx)
  rawReplaceLines(textarea, ctx)
  // Column shifted right by 1 due to insertion at left
  rawFocusCell(textarea, ctx, coords.row, coords.col + 1)
}

function rawTableDeleteRow(textarea) {
  const ctx = rawGetTableContext(textarea)
  if (!ctx) return
  const coords = rawGetCellCoords(textarea, ctx)
  const firstData = ctx.tableFirstLine + (/^\s*\{table\s/.test(ctx.lines[ctx.tableFirstLine]) ? 1 : 0)
  if (ctx.curLineIdx <= firstData + 1) return
  const changeIndex = rawLineToFormulaRow(ctx, ctx.curLineIdx)
  const origLast = ctx.tableLastLine
  ctx.lines.splice(ctx.curLineIdx, 1)
  ctx.tableLastLine--
  rawAdjustFormulas(ctx, "deleteRow", changeIndex)
  rawReplaceLines(textarea, ctx, origLast)
  // Focus cell below (same row index, since rows shifted up), or above if was last row
  const targetRow = Math.min(coords.row, ctx.tableLastLine)
  rawFocusCell(textarea, ctx, targetRow, coords.col)
}

function rawTableDeleteColumn(textarea) {
  const ctx = rawGetTableContext(textarea)
  if (!ctx) return
  const coords = rawGetCellCoords(textarea, ctx)
  const firstData = ctx.tableFirstLine + (/^\s*\{table\s/.test(ctx.lines[ctx.tableFirstLine]) ? 1 : 0)
  const cols = rawTableColCount(ctx)
  if (cols <= 1) return

  // Remove the column at cursor position
  for (let i = firstData; i <= ctx.tableLastLine; i++) {
    const row = rawParsePipeRow(ctx.lines[i])
    if (!row || row.length <= 1) continue
    row.splice(coords.col, 1)
    ctx.lines[i] = rawBuildPipeRow(row)
  }
  rawAdjustFormulas(ctx, "deleteCol", coords.col)
  rawAdjustColumnSpecs(ctx, "deleteCol", coords.col)
  rawReplaceLines(textarea, ctx)
  // Focus cell to the right (same col), or left if was last column
  const targetCol = Math.min(coords.col, cols - 2)
  rawFocusCell(textarea, ctx, coords.row, targetCol)
}

// Select a cell's content between two pipe positions.
// For empty cells, place cursor in the middle (after leading space).
function rawSelectCell(textarea, pipeLeft, pipeRight) {
  const val = textarea.value
  const cellStart = pipeLeft + 1
  let contentStart = cellStart
  while (contentStart < pipeRight && val[contentStart] === " ") contentStart++
  let contentEnd = pipeRight
  while (contentEnd > contentStart && val[contentEnd - 1] === " ") contentEnd--
  if (contentStart >= contentEnd) {
    // Empty cell: place cursor after leading space (middle of cell)
    const mid = Math.min(cellStart + 1, pipeRight)
    textarea.setSelectionRange(mid, mid)
  } else {
    textarea.setSelectionRange(contentStart, contentEnd)
  }
}

// Find all pipe positions on a line (within lineStart..lineEnd)
function rawFindPipes(val, lineStart, lineEnd) {
  const pipes = []
  for (let i = lineStart; i <= lineEnd; i++) {
    if (val[i] === "|") pipes.push(i)
  }
  return pipes
}

function rawHandleTabInTable(textarea, direction) {
  const val = textarea.value
  const pos = textarea.selectionStart
  const pipeRe = /^\s*\|/

  // Check if cursor is on a table line
  let lineStart = pos
  while (lineStart > 0 && val[lineStart - 1] !== "\n") lineStart--
  let lineEnd = pos
  while (lineEnd < val.length && val[lineEnd] !== "\n") lineEnd++
  const line = val.substring(lineStart, lineEnd)
  if (!pipeRe.test(line)) return false

  // Don't navigate on the separator row
  if (rawIsSeparatorRow(line)) return false

  const pipes = rawFindPipes(val, lineStart, lineEnd)
  if (pipes.length < 2) return false

  // Find which cell the cursor is in
  let cellIdx = -1
  for (let i = 0; i < pipes.length - 1; i++) {
    if (pos >= pipes[i] && pos <= pipes[i + 1]) { cellIdx = i; break }
  }
  if (cellIdx === -1) cellIdx = 0

  const totalCells = pipes.length - 1

  if (direction === 1) {
    // Forward
    if (cellIdx < totalCells - 1) {
      // Move to next cell on same line
      rawSelectCell(textarea, pipes[cellIdx + 1], pipes[cellIdx + 2])
      return true
    }

    // Last cell on this line — move to next data row
    let nextLineStart = lineEnd + 1
    if (nextLineStart >= val.length) return rawTabAddTableRow(textarea)
    let nextLineEnd = nextLineStart
    while (nextLineEnd < val.length && val[nextLineEnd] !== "\n") nextLineEnd++
    let nextLine = val.substring(nextLineStart, nextLineEnd)

    if (!pipeRe.test(nextLine)) return rawTabAddTableRow(textarea)

    // Skip separator rows
    if (rawIsSeparatorRow(nextLine)) {
      nextLineStart = nextLineEnd + 1
      if (nextLineStart >= val.length) return rawTabAddTableRow(textarea)
      nextLineEnd = nextLineStart
      while (nextLineEnd < val.length && val[nextLineEnd] !== "\n") nextLineEnd++
      nextLine = val.substring(nextLineStart, nextLineEnd)
      if (!pipeRe.test(nextLine) || rawIsSeparatorRow(nextLine)) return rawTabAddTableRow(textarea)
    }

    // Move to first cell of next row
    const nextPipes = rawFindPipes(val, nextLineStart, nextLineEnd)
    if (nextPipes.length < 2) return false
    rawSelectCell(textarea, nextPipes[0], nextPipes[1])
    return true
  } else {
    // Backward
    if (cellIdx > 0) {
      // Move to previous cell on same line
      rawSelectCell(textarea, pipes[cellIdx - 1], pipes[cellIdx])
      return true
    }

    // First cell — move to previous row's last cell
    if (lineStart === 0) return false
    let prevLineEnd = lineStart - 1
    let prevLineStart = prevLineEnd
    while (prevLineStart > 0 && val[prevLineStart - 1] !== "\n") prevLineStart--
    let prevLine = val.substring(prevLineStart, prevLineEnd)

    // Skip separator rows
    if (rawIsSeparatorRow(prevLine)) {
      if (prevLineStart === 0) return false
      prevLineEnd = prevLineStart - 1
      prevLineStart = prevLineEnd
      while (prevLineStart > 0 && val[prevLineStart - 1] !== "\n") prevLineStart--
      prevLine = val.substring(prevLineStart, prevLineEnd)
    }

    if (!pipeRe.test(prevLine)) return false
    const prevPipes = rawFindPipes(val, prevLineStart, prevLineEnd)
    if (prevPipes.length < 2) return false
    // Select last cell
    rawSelectCell(textarea, prevPipes[prevPipes.length - 2], prevPipes[prevPipes.length - 1])
    return true
  }
}

function rawTabAddTableRow(textarea) {
  const ctx = rawGetTableContext(textarea)
  if (!ctx) return false
  const cols = rawTableColCount(ctx)
  if (cols === 0) return false

  const newRow = rawBuildPipeRow(Array(cols).fill(""))

  // Find the end of the last table line
  let endOffset = 0
  for (let i = 0; i <= ctx.tableLastLine; i++) endOffset += ctx.lines[i].length + 1
  endOffset-- // remove trailing newline offset

  textarea.focus()
  textarea.setSelectionRange(endOffset, endOffset)
  rawInsertText(textarea, "\n" + newRow)

  // Move cursor to first cell of the new row
  const newRowStart = endOffset + 1
  const newVal = textarea.value
  let newRowEnd = newRowStart
  while (newRowEnd < newVal.length && newVal[newRowEnd] !== "\n") newRowEnd++
  const newPipes = rawFindPipes(newVal, newRowStart, newRowEnd)
  if (newPipes.length >= 2) {
    rawSelectCell(textarea, newPipes[0], newPipes[1])
  }
  return true
}

function rawInsertProperty(textarea) {
  const val = textarea.value
  const pos = textarea.selectionStart

  // Find current line
  let lineStart = pos
  while (lineStart > 0 && val[lineStart - 1] !== "\n") lineStart--
  let lineEnd = pos
  while (lineEnd < val.length && val[lineEnd] !== "\n") lineEnd++
  const line = val.substring(lineStart, lineEnd)

  // Detect context: what directive to insert
  let directive = null
  let tableStart = lineStart
  if (/!\[.*\]\(.*\)/.test(line)) {
    // Image line — check if a {image ...} directive already exists above
    if (lineStart > 0) {
      let prevEnd = lineStart - 1
      let prevStart = prevEnd
      while (prevStart > 0 && val[prevStart - 1] !== "\n") prevStart--
      const prevLine = val.substring(prevStart, prevEnd)
      if (/^\s*\{image\s/.test(prevLine)) {
        // Already has a property directive — select its value for editing
        const eqIdx = prevLine.indexOf("=")
        if (eqIdx >= 0) {
          const valStart = prevStart + eqIdx + 1
          const valEnd = prevStart + prevLine.lastIndexOf("}")
          textarea.focus()
          textarea.setSelectionRange(valStart, Math.max(valStart, valEnd))
        }
        return
      }
    }
    directive = "{image size=}"
  } else if (/^\s*\|/.test(line)) {
    // Table line — check if a {table ...} directive already exists above the table
    // Find the first line of the table
    tableStart = lineStart
    while (tableStart > 0) {
      let pStart = tableStart - 1
      while (pStart > 0 && val[pStart - 1] !== "\n") pStart--
      const pLine = val.substring(pStart, tableStart - 1)
      if (/^\s*\|/.test(pLine)) {
        tableStart = pStart
      } else {
        break
      }
    }
    if (tableStart > 0) {
      let prevEnd = tableStart - 1
      let prevStart = prevEnd
      while (prevStart > 0 && val[prevStart - 1] !== "\n") prevStart--
      const prevLine = val.substring(prevStart, prevEnd)
      if (/^\s*\{table\s/.test(prevLine)) {
        const eqIdx = prevLine.indexOf("=")
        if (eqIdx >= 0) {
          const valStart = prevStart + eqIdx + 1
          const valEnd = prevStart + prevLine.lastIndexOf("}")
          textarea.focus()
          textarea.setSelectionRange(valStart, Math.max(valStart, valEnd))
        }
        return
      }
    }
    directive = "{table width=}"
  }

  if (!directive) return

  // For tables, insert above the table start; for images, above the current line
  const insertAt = /^\s*\|/.test(line) ? tableStart : lineStart

  // Insert directive on its own line before the target line
  const scroller = document.querySelector("#app")
  const savedScroll = scroller ? scroller.scrollTop : 0
  textarea.focus()
  textarea.setSelectionRange(insertAt, insertAt)
  rawInsertText(textarea, directive + "\n")

  // Place cursor between "=" and "}"
  const cursorPos = insertAt + directive.length - 1
  textarea.setSelectionRange(cursorPos, cursorPos)
  if (scroller) scroller.scrollTop = savedScroll
}

function markActive(state, type) {
  const { from, $from, to, empty } = state.selection
  if (empty) return Boolean(type.isInSet(state.storedMarks || $from.marks()))
  return state.doc.rangeHasMark(from, to, type)
}

function updateMenubarState(state, refs) {
  if (!refs) return
  const setActive = (btn, active) => {
    if (!btn) return
    btn.classList.toggle("gowiki-raw-menuitem-active", active)
  }
  if (schema.marks.strong && refs.bold) {
    setActive(refs.bold, markActive(state, schema.marks.strong))
  }
  if (schema.marks.em && refs.italic) {
    setActive(refs.italic, markActive(state, schema.marks.em))
  }
  if (schema.marks.code && refs.code) {
    setActive(refs.code, markActive(state, schema.marks.code))
  }
  if (schema.marks.underline && refs.underline) {
    setActive(refs.underline, markActive(state, schema.marks.underline))
  }
  if (schema.marks.strikethrough && refs.strikethrough) {
    setActive(refs.strikethrough, markActive(state, schema.marks.strikethrough))
  }
  if (schema.marks.subscript && refs.subscript) {
    setActive(refs.subscript, markActive(state, schema.marks.subscript))
  }
  if (schema.marks.superscript && refs.superscript) {
    setActive(refs.superscript, markActive(state, schema.marks.superscript))
  }
  if (refs.properties) {
    setActive(refs.properties, isPropertiesPanelEnabled(state))
  }
}

// --- Symbol picker (searchable panel) ---
const symbolCatalogue = [
  // Indicators
  { char: "\u26A0\uFE0F", label: "Warning", cat: "Indicators", aliases: "alert caution danger" },
  { char: "\u2139\uFE0F", label: "Info", cat: "Indicators", aliases: "information" },
  { char: "\u2705", label: "Check", cat: "Indicators", aliases: "yes ok done" },
  { char: "\u274C", label: "Cross", cat: "Indicators", aliases: "no wrong delete remove" },
  { char: "\u2B50", label: "Star", cat: "Indicators", aliases: "favorite" },
  { char: "\u{1F4A1}", label: "Idea", cat: "Indicators", aliases: "lightbulb tip" },
  { char: "\u{1F4CC}", label: "Pin", cat: "Indicators", aliases: "pushpin" },
  { char: "\u{1F512}", label: "Lock", cat: "Indicators", aliases: "locked secure" },
  { char: "\u{1F513}", label: "Unlock", cat: "Indicators", aliases: "unlocked open" },
  { char: "\u{1F4DD}", label: "Note", cat: "Indicators", aliases: "memo" },
  { char: "\u{1F534}", label: "Red circle", cat: "Indicators", aliases: "stop" },
  { char: "\u{1F7E2}", label: "Green circle", cat: "Indicators", aliases: "go" },
  { char: "\u{1F7E1}", label: "Yellow circle", cat: "Indicators", aliases: "caution" },
  { char: "\u{1F535}", label: "Blue circle", cat: "Indicators", aliases: "" },
  // Checkboxes
  { char: "\u2610", label: "Ballot box", cat: "Checkboxes", aliases: "unchecked empty checkbox" },
  { char: "\u2611\uFE0F", label: "Ballot check", cat: "Checkboxes", aliases: "checked done checkbox" },
  { char: "\u2612", label: "Ballot cross", cat: "Checkboxes", aliases: "crossed rejected checkbox" },
  // Arrows
  { char: "\u2192", label: "Arrow right", cat: "Arrows", aliases: "right ->" },
  { char: "\u2190", label: "Arrow left", cat: "Arrows", aliases: "left <-" },
  { char: "\u2191", label: "Arrow up", cat: "Arrows", aliases: "up" },
  { char: "\u2193", label: "Arrow down", cat: "Arrows", aliases: "down" },
  { char: "\u2194", label: "Arrow left-right", cat: "Arrows", aliases: "horizontal both" },
  { char: "\u2195", label: "Arrow up-down", cat: "Arrows", aliases: "vertical both" },
  { char: "\u21D2", label: "Double arrow right", cat: "Arrows", aliases: "implies =>" },
  { char: "\u21D0", label: "Double arrow left", cat: "Arrows", aliases: "implied by <=" },
  { char: "\u21D4", label: "Double arrow both", cat: "Arrows", aliases: "equivalent <=>" },
  { char: "\u21B5", label: "Return arrow", cat: "Arrows", aliases: "enter newline cr" },
  // Math
  { char: "\u00B1", label: "Plus-minus", cat: "Math", aliases: "plusminus +/-" },
  { char: "\u00D7", label: "Multiply", cat: "Math", aliases: "times cross x" },
  { char: "\u00F7", label: "Divide", cat: "Math", aliases: "division" },
  { char: "\u2260", label: "Not equal", cat: "Math", aliases: "ne !=" },
  { char: "\u2264", label: "Less or equal", cat: "Math", aliases: "le <=" },
  { char: "\u2265", label: "Greater or equal", cat: "Math", aliases: "ge >=" },
  { char: "\u2248", label: "Approximately", cat: "Math", aliases: "approx almost ~" },
  { char: "\u221E", label: "Infinity", cat: "Math", aliases: "inf" },
  { char: "\u221A", label: "Square root", cat: "Math", aliases: "sqrt radical" },
  { char: "\u2211", label: "Summation", cat: "Math", aliases: "sum sigma" },
  { char: "\u220F", label: "Product", cat: "Math", aliases: "prod pi" },
  { char: "\u2202", label: "Partial", cat: "Math", aliases: "partial derivative" },
  { char: "\u2208", label: "Element of", cat: "Math", aliases: "in belongs member" },
  { char: "\u2209", label: "Not element of", cat: "Math", aliases: "notin" },
  { char: "\u2282", label: "Subset", cat: "Math", aliases: "subset" },
  { char: "\u2283", label: "Superset", cat: "Math", aliases: "superset" },
  { char: "\u2229", label: "Intersection", cat: "Math", aliases: "cap and" },
  { char: "\u222A", label: "Union", cat: "Math", aliases: "cup or" },
  { char: "\u2205", label: "Empty set", cat: "Math", aliases: "null void" },
  { char: "\u2234", label: "Therefore", cat: "Math", aliases: "so hence" },
  // Greek
  { char: "\u03B1", label: "Alpha", cat: "Greek", aliases: "" },
  { char: "\u03B2", label: "Beta", cat: "Greek", aliases: "" },
  { char: "\u03B3", label: "Gamma", cat: "Greek", aliases: "" },
  { char: "\u03B4", label: "Delta", cat: "Greek", aliases: "" },
  { char: "\u03B5", label: "Epsilon", cat: "Greek", aliases: "" },
  { char: "\u03B6", label: "Zeta", cat: "Greek", aliases: "" },
  { char: "\u03B7", label: "Eta", cat: "Greek", aliases: "" },
  { char: "\u03B8", label: "Theta", cat: "Greek", aliases: "" },
  { char: "\u03BB", label: "Lambda", cat: "Greek", aliases: "" },
  { char: "\u03BC", label: "Mu", cat: "Greek", aliases: "micro" },
  { char: "\u03C0", label: "Pi", cat: "Greek", aliases: "" },
  { char: "\u03C1", label: "Rho", cat: "Greek", aliases: "" },
  { char: "\u03C3", label: "Sigma", cat: "Greek", aliases: "" },
  { char: "\u03C4", label: "Tau", cat: "Greek", aliases: "" },
  { char: "\u03C6", label: "Phi", cat: "Greek", aliases: "" },
  { char: "\u03C9", label: "Omega", cat: "Greek", aliases: "" },
  { char: "\u0394", label: "Delta (upper)", cat: "Greek", aliases: "capital" },
  { char: "\u03A3", label: "Sigma (upper)", cat: "Greek", aliases: "capital" },
  { char: "\u03A9", label: "Omega (upper)", cat: "Greek", aliases: "capital ohm" },
  // Common
  { char: "\u00A9", label: "Copyright", cat: "Common", aliases: "(c)" },
  { char: "\u00AE", label: "Registered", cat: "Common", aliases: "(r)" },
  { char: "\u2122", label: "Trademark", cat: "Common", aliases: "tm" },
  { char: "\u00B0", label: "Degree", cat: "Common", aliases: "temperature" },
  { char: "\u00B5", label: "Micro", cat: "Common", aliases: "mu" },
  { char: "\u00B6", label: "Pilcrow", cat: "Common", aliases: "paragraph" },
  { char: "\u00A7", label: "Section", cat: "Common", aliases: "paragraph" },
  { char: "\u2020", label: "Dagger", cat: "Common", aliases: "cross obelisk" },
  { char: "\u2021", label: "Double dagger", cat: "Common", aliases: "diesis" },
  { char: "\u2022", label: "Bullet", cat: "Common", aliases: "dot" },
  { char: "\u2026", label: "Ellipsis", cat: "Common", aliases: "dots ..." },
  { char: "\u2013", label: "En dash", cat: "Common", aliases: "ndash" },
  { char: "\u2014", label: "Em dash", cat: "Common", aliases: "mdash" },
  { char: "\u00A0", label: "Non-breaking space", cat: "Common", aliases: "nbsp" },
  // Superscript digits
  { char: "\u2070", label: "Superscript 0", cat: "Superscript", aliases: "sup power" },
  { char: "\u00B9", label: "Superscript 1", cat: "Superscript", aliases: "sup power" },
  { char: "\u00B2", label: "Superscript 2", cat: "Superscript", aliases: "sup power squared" },
  { char: "\u00B3", label: "Superscript 3", cat: "Superscript", aliases: "sup power cubed" },
  { char: "\u2074", label: "Superscript 4", cat: "Superscript", aliases: "sup power" },
  { char: "\u2075", label: "Superscript 5", cat: "Superscript", aliases: "sup power" },
  { char: "\u2076", label: "Superscript 6", cat: "Superscript", aliases: "sup power" },
  { char: "\u2077", label: "Superscript 7", cat: "Superscript", aliases: "sup power" },
  { char: "\u2078", label: "Superscript 8", cat: "Superscript", aliases: "sup power" },
  { char: "\u2079", label: "Superscript 9", cat: "Superscript", aliases: "sup power" },
]

let symbolPanelEl = null
let symbolPanelAnchor = null // "toolbar" or "cursor"
let symbolToolbarAnchor = null // DOM element of the toolbar button

function insertSymbolChar(char) {
  if (editMode === "visual" && editorView) {
    const tr = editorView.state.tr.insertText(char)
    editorView.dispatch(tr)
    editorView.focus()
  } else if (editMode === "raw" && rawEditor) {
    rawEditor.focus()
    rawInsertText(rawEditor, char)
  }
}

function buildSymbolPanel() {
  const panel = document.createElement("div")
  panel.className = "gowiki-symbol-panel"

  const search = document.createElement("input")
  search.className = "gowiki-symbol-search"
  search.type = "text"
  search.placeholder = "Search symbols\u2026"
  panel.appendChild(search)

  const body = document.createElement("div")
  body.className = "gowiki-symbol-body"
  panel.appendChild(body)

  let selectedIdx = -1
  let visibleItems = []

  function renderSymbols(filter) {
    body.innerHTML = ""
    visibleItems = []
    selectedIdx = -1
    const q = filter.toLowerCase()

    if (q) {
      // Flat filtered list
      const matches = symbolCatalogue.filter(s => {
        const hay = (s.label + " " + s.aliases + " " + s.cat).toLowerCase()
        return hay.includes(q)
      })
      if (matches.length === 0) {
        const empty = document.createElement("div")
        empty.className = "gowiki-symbol-empty"
        empty.textContent = "No matches"
        body.appendChild(empty)
        return
      }
      const grid = document.createElement("div")
      grid.className = "gowiki-symbol-grid-inner"
      for (const sym of matches) {
        const item = createSymbolItem(sym)
        grid.appendChild(item)
        visibleItems.push({ el: item, sym })
      }
      body.appendChild(grid)
      // Auto-select first item
      if (visibleItems.length > 0) {
        selectedIdx = 0
        visibleItems[0].el.classList.add("gowiki-symbol-item--selected")
      }
    } else {
      // Categorized view
      const cats = []
      const catMap = new Map()
      for (const sym of symbolCatalogue) {
        if (!catMap.has(sym.cat)) {
          catMap.set(sym.cat, [])
          cats.push(sym.cat)
        }
        catMap.get(sym.cat).push(sym)
      }
      for (const cat of cats) {
        const header = document.createElement("div")
        header.className = "gowiki-symbol-cat"
        header.textContent = cat
        body.appendChild(header)
        const grid = document.createElement("div")
        grid.className = "gowiki-symbol-grid-inner"
        for (const sym of catMap.get(cat)) {
          const item = createSymbolItem(sym)
          grid.appendChild(item)
          visibleItems.push({ el: item, sym })
        }
        body.appendChild(grid)
      }
    }
  }

  function createSymbolItem(sym) {
    const item = document.createElement("span")
    item.className = "gowiki-symbol-item"
    item.textContent = sym.char
    item.title = sym.label
    item.addEventListener("mousedown", e => {
      e.preventDefault()
      closeSymbolPanel()
      insertSymbolChar(sym.char)
    })
    return item
  }

  function moveSelection(delta) {
    if (visibleItems.length === 0) return
    if (selectedIdx >= 0) {
      visibleItems[selectedIdx].el.classList.remove("gowiki-symbol-item--selected")
    }
    if (selectedIdx < 0) {
      selectedIdx = delta > 0 ? 0 : visibleItems.length - 1
    } else {
      selectedIdx = (selectedIdx + delta + visibleItems.length) % visibleItems.length
    }
    visibleItems[selectedIdx].el.classList.add("gowiki-symbol-item--selected")
    visibleItems[selectedIdx].el.scrollIntoView({ block: "nearest" })
  }

  search.addEventListener("input", () => renderSymbols(search.value))
  search.addEventListener("keydown", e => {
    const cols = 8 // grid columns
    if (e.key === "ArrowDown") {
      e.preventDefault()
      moveSelection(cols)
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      moveSelection(-cols)
    } else if (e.key === "ArrowRight") {
      // Only navigate grid if cursor is at end of search text
      if (search.selectionStart === search.value.length) {
        e.preventDefault()
        moveSelection(1)
      }
    } else if (e.key === "ArrowLeft") {
      if (search.selectionStart === 0 && search.selectionEnd === 0) {
        e.preventDefault()
        moveSelection(-1)
      }
    } else if (e.key === "Enter" || e.key === "Tab") {
      e.preventDefault()
      if (selectedIdx >= 0 && selectedIdx < visibleItems.length) {
        closeSymbolPanel()
        insertSymbolChar(visibleItems[selectedIdx].sym.char)
      }
    } else if (e.key === "Escape") {
      e.preventDefault()
      closeSymbolPanel()
    }
  })

  renderSymbols("")
  return { panel, search }
}

function openSymbolPanel(anchor) {
  if (symbolPanelEl) closeSymbolPanel()
  const { panel, search } = buildSymbolPanel()
  symbolPanelEl = panel
  symbolPanelAnchor = anchor

  if (anchor === "toolbar" && symbolToolbarAnchor) {
    symbolToolbarAnchor.appendChild(panel)
  } else {
    // Position near cursor in the editor
    document.body.appendChild(panel)
    let rect
    if (editMode === "visual" && editorView) {
      const coords = editorView.coordsAtPos(editorView.state.selection.from)
      rect = { left: coords.left, top: coords.bottom }
    } else if (editMode === "raw" && rawEditor) {
      const r = rawEditor.getBoundingClientRect()
      rect = { left: r.left + 20, top: r.top + 20 }
    } else {
      rect = { left: window.innerWidth / 2 - 150, top: window.innerHeight / 3 }
    }
    panel.style.position = "fixed"
    panel.style.left = Math.min(rect.left, window.innerWidth - 320) + "px"
    panel.style.top = Math.min(rect.top + 4, window.innerHeight - 400) + "px"
  }

  // Focus the search field after a microtask so it doesn't lose focus immediately
  requestAnimationFrame(() => search.focus())
}

function closeSymbolPanel() {
  if (!symbolPanelEl) return
  symbolPanelEl.remove()
  symbolPanelEl = null
  symbolPanelAnchor = null
}

function buildMenubar() {
  const bar = document.createElement("div")
  bar.className = "gowiki-raw-menubar"
  const refs = {}

  function addButton(label, title, onClick, refName) {
    const btn = document.createElement("span")
    btn.className = "gowiki-raw-menuitem"
    btn.textContent = label
    btn.title = title
    btn.addEventListener("mousedown", e => {
      e.preventDefault()
      onClick()
    })
    bar.appendChild(btn)
    if (refName) refs[refName] = btn
    return btn
  }

  function addIconButton(iconData, title, onClick, refName) {
    const btn = document.createElement("span")
    btn.className = "gowiki-raw-menuitem"
    btn.title = title
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg")
    svg.setAttribute("viewBox", `0 0 ${iconData.width} ${iconData.height}`)
    svg.style.width = (iconData.width / iconData.height) + "em"
    svg.style.height = "1em"
    svg.style.verticalAlign = "middle"
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path")
    path.setAttribute("d", iconData.path)
    svg.appendChild(path)
    btn.appendChild(svg)
    btn.addEventListener("mousedown", e => {
      e.preventDefault()
      onClick()
    })
    bar.appendChild(btn)
    if (refName) refs[refName] = btn
    return btn
  }

  function addImgButton(src, title, onClick, refName) {
    const btn = document.createElement("span")
    btn.className = "gowiki-raw-menuitem"
    btn.title = title
    const img = document.createElement("img")
    img.src = src
    img.alt = title
    img.width = 14
    img.height = 14
    img.className = "gowiki-menu-icon"
    img.style.verticalAlign = "middle"
    btn.appendChild(img)
    btn.addEventListener("mousedown", e => {
      e.preventDefault()
      onClick()
    })
    bar.appendChild(btn)
    if (refName) refs[refName] = btn
    return btn
  }

  function addSeparator() {
    const sep = document.createElement("span")
    sep.className = "gowiki-raw-menusep"
    bar.appendChild(sep)
  }

  // Properties
  addImgButton("/icons/tools.svg", "Toggle properties", () => {
    if (editMode === "visual" && editorView) {
      if (togglePropertiesCommand) togglePropertiesCommand(editorView.state, editorView.dispatch, editorView)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawInsertProperty(rawEditor)
    }
  }, "properties")

  // Heading dropdown
  const headingWrap = document.createElement("span")
  headingWrap.className = "gowiki-raw-menuitem gowiki-raw-menu-dropdown-wrap"
  headingWrap.title = "Headings"
  headingWrap.textContent = "H#"
  const headingMenu = document.createElement("div")
  headingMenu.className = "gowiki-raw-dropdown-menu"
  const headingItems = []
  for (let level = 1; level <= 5; level++) {
    const item = document.createElement("div")
    item.className = "gowiki-raw-dropdown-item"
    item.textContent = `H${level}`
    item.dataset.level = String(level)
    item.addEventListener("mousedown", e => {
      e.preventDefault()
      headingMenu.style.display = "none"
      const numbered = e.shiftKey
      if (editMode === "visual" && editorView) {
        setBlockType(schema.nodes.heading, { level, numbered })(editorView.state, editorView.dispatch)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        rawSetHeadingLevel(rawEditor, level, numbered)
      }
    })
    headingMenu.appendChild(item)
    headingItems.push(item)
  }
  const headingHint = document.createElement("div")
  headingHint.className = "gowiki-raw-dropdown-item gowiki-raw-dropdown-item--hint"
  headingHint.textContent = "Hold shift for numbered"
  headingMenu.appendChild(headingHint)

  function updateHeadingMenuShift(shift) {
    for (const item of headingItems) {
      const level = item.dataset.level
      item.textContent = shift ? `H${level} numbered` : `H${level}`
    }
  }
  headingMenu.addEventListener("keydown", e => { if (e.key === "Shift") updateHeadingMenuShift(true) })
  headingMenu.addEventListener("keyup", e => { if (e.key === "Shift") updateHeadingMenuShift(false) })
  document.addEventListener("keydown", e => {
    if (e.key === "Shift" && headingMenu.style.display === "block") updateHeadingMenuShift(true)
  })
  document.addEventListener("keyup", e => {
    if (e.key === "Shift" && headingMenu.style.display === "block") updateHeadingMenuShift(false)
  })

  headingWrap.appendChild(headingMenu)
  headingWrap.addEventListener("mousedown", e => {
    e.preventDefault()
    const isOpen = headingMenu.style.display === "block"
    headingMenu.style.display = isOpen ? "none" : "block"
    if (!isOpen) updateHeadingMenuShift(false)
  })
  bar.appendChild(headingWrap)

  document.addEventListener("mousedown", e => {
    if (!headingWrap.contains(e.target)) {
      headingMenu.style.display = "none"
    }
  })

  addSeparator()

  // HR
  const hrBtn = addButton("---", "Insert horizontal rule", () => {
    if (editMode === "visual" && editorView) {
      const hr = schema.nodes.horizontal_rule
      if (hr) editorView.dispatch(editorView.state.tr.replaceSelectionWith(hr.create()).scrollIntoView())
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawInsertHorizontalRule(rawEditor)
    }
  })
  hrBtn.style.fontWeight = "700"
  hrBtn.style.letterSpacing = "-1px"

  addSeparator()

  // Bold
  addButton("B", shortcutHint("Bold", "B"), () => {
    if (editMode === "visual" && editorView) {
      toggleMark(schema.marks.strong)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawWrapSelection(rawEditor, "**", "**")
    }
  }, "bold").style.fontWeight = "700"

  // Italic
  addButton("I", shortcutHint("Italic", "I"), () => {
    if (editMode === "visual" && editorView) {
      toggleMark(schema.marks.em)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawWrapSelection(rawEditor, "*", "*")
    }
  }, "italic").style.fontStyle = "italic"

  // Underline
  const uBtn = addButton("U", shortcutHint("Underline", "U"), () => {
    if (editMode === "visual" && editorView) {
      toggleMark(schema.marks.underline)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawWrapSelection(rawEditor, "_", "_")
    }
  }, "underline")
  uBtn.style.textDecoration = "underline"

  // Strikethrough
  const sBtn = addButton("S", shortcutHint("Strikethrough", "Shift-S"), () => {
    if (editMode === "visual" && editorView) {
      toggleMark(schema.marks.strikethrough)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawWrapSelection(rawEditor, "~~", "~~")
    }
  }, "strikethrough")
  sBtn.style.textDecoration = "line-through"

  // Subscript
  addButton("x\u2082", "Subscript", () => {
    if (editMode === "visual" && editorView) {
      toggleMark(schema.marks.subscript)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawWrapSelection(rawEditor, "~", "~")
    }
  }, "subscript")

  // Superscript
  addButton("x\u00B2", "Superscript", () => {
    if (editMode === "visual" && editorView) {
      toggleMark(schema.marks.superscript)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawWrapSelection(rawEditor, "^", "^")
    }
  }, "superscript")

  // Footnote
  if (footnoteInsertCommand) {
    addButton("fn", "Insert footnote", () => {
      if (editMode === "visual" && editorView) {
        footnoteInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        rawWrapSelection(rawEditor, "^[", "]")
      }
    })
  }

  // Inline code
  addButton("</>", shortcutHint("Inline code", "E"), () => {
    if (editMode === "visual" && editorView) {
      toggleMark(schema.marks.code)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawWrapSelection(rawEditor, "`", "`")
    }
  }, "code")

  // Symbol picker toolbar button
  {
    const symWrap = document.createElement("span")
    symWrap.className = "gowiki-raw-menuitem gowiki-raw-menu-dropdown-wrap"
    {
      const mod = isMac ? "\u2318" : "Ctrl+"
      symWrap.title = `Insert symbol (${mod}:)`
    }
    symWrap.textContent = "\u263A"
    symbolToolbarAnchor = symWrap
    symWrap.addEventListener("mousedown", e => {
      e.preventDefault()
      if (symbolPanelEl && symbolPanelAnchor === "toolbar") {
        closeSymbolPanel()
      } else {
        openSymbolPanel("toolbar")
      }
    })
    bar.appendChild(symWrap)
    document.addEventListener("mousedown", e => {
      if (symbolPanelEl && !symbolPanelEl.contains(e.target) && !symWrap.contains(e.target)) {
        closeSymbolPanel()
      }
    })
  }

  addSeparator()

  // Link
  addIconButton(icons.link, shortcutHint("Set or edit link", "K"), () => {
    if (editMode === "visual" && editorView) {
      setExternalLinkCommand()(editorView.state, editorView.dispatch, editorView)
    } else if (editMode === "raw" && rawEditor) {
      void rawInsertLink(rawEditor)
    }
  })

  addSeparator()

  // Undo
  addIconButton(icons.undo, "Undo", () => {
    if (editMode === "visual" && editorView) {
      undo(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawEditor.focus()
      document.execCommand("undo")
    }
  })

  // Redo
  addIconButton(icons.redo, "Redo", () => {
    if (editMode === "visual" && editorView) {
      redo(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawEditor.focus()
      document.execCommand("redo")
    }
  })

  addSeparator()

  // Table dropdown
  const tableWrap = document.createElement("span")
  tableWrap.className = "gowiki-raw-menuitem gowiki-raw-menu-dropdown-wrap"
  tableWrap.title = "Table"
  const tableIcon = document.createElement("img")
  tableIcon.src = "/icons/table.svg"
  tableIcon.alt = "Table"
  tableIcon.width = 14
  tableIcon.height = 14
  tableIcon.className = "gowiki-menu-icon"
  tableIcon.style.verticalAlign = "middle"
  tableWrap.appendChild(tableIcon)
  const tableDropMenu = document.createElement("div")
  tableDropMenu.className = "gowiki-raw-dropdown-menu"
  tableDropMenu.dataset.menuId = "table"
  const tableActions = [
    { name: "insert", label: "Insert table" },
    { name: "row.addBefore", label: "Add row above" },
    { name: "row.addAfter", label: "Add row below" },
    { name: "column.addBefore", label: "Add column left" },
    { name: "column.addAfter", label: "Add column right" },
    { name: "row.delete", label: "Delete row" },
    { name: "column.delete", label: "Delete column" },
    { name: "cell.properties", label: "Cell properties" },
    { name: "cell.merge", label: "Merge cells" },
    { name: "cell.split", label: "Unmerge cell" },
  ]
  const rawTableFns = {
    insert: rawInsertTable,
    "row.addBefore": rawTableAddRowAbove,
    "row.addAfter": rawTableAddRowBelow,
    "column.addBefore": rawTableAddColumnLeft,
    "column.addAfter": rawTableAddColumnRight,
    "row.delete": rawTableDeleteRow,
    "column.delete": rawTableDeleteColumn,
  }
  const visualOnlyActions = new Set(["cell.merge", "cell.split"])
  for (const action of tableActions) {
    const item = document.createElement("div")
    item.className = "gowiki-raw-dropdown-item"
    item.textContent = action.label
    if (visualOnlyActions.has(action.name)) item.dataset.visualOnly = "1"
    item.addEventListener("mousedown", e => {
      e.preventDefault()
      if (item.classList.contains("gowiki-raw-dropdown-item--disabled")) return
      tableDropMenu.style.display = "none"
      if (editMode === "visual" && editorView) {
        const cmd = tableCommands.get(action.name)
        if (cmd) cmd(editorView.state, editorView.dispatch)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const fn = rawTableFns[action.name]
        if (fn) fn(rawEditor)
      }
    })
    tableDropMenu.appendChild(item)
  }
  tableWrap.appendChild(tableDropMenu)
  tableWrap.addEventListener("mousedown", e => {
    e.preventDefault()
    const isOpen = tableDropMenu.style.display === "block"
    if (!isOpen) {
      for (const el of tableDropMenu.querySelectorAll("[data-visual-only]")) {
        el.classList.toggle("gowiki-raw-dropdown-item--disabled", editMode !== "visual")
      }
    }
    tableDropMenu.style.display = isOpen ? "none" : "block"
  })
  bar.appendChild(tableWrap)
  document.addEventListener("mousedown", e => {
    if (!tableWrap.contains(e.target)) {
      tableDropMenu.style.display = "none"
    }
  })

  // Unordered list
  addIconButton(icons.bulletList, "Unordered list", () => {
    if (editMode === "visual" && editorView) {
      wrapInList(schema.nodes.bullet_list)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawToggleLinePrefix(rawEditor, "- ")
    }
  })

  // Ordered list
  addIconButton(icons.orderedList, "Ordered list", () => {
    if (editMode === "visual" && editorView) {
      wrapInList(schema.nodes.ordered_list)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawToggleLinePrefix(rawEditor, "1. ")
      rawRenumberOrderedLists(rawEditor)
    }
  })

  // Code block
  addImgButton("/icons/codeblock.svg", "Code block", () => {
    if (editMode === "visual" && editorView) {
      const codeBlockType = schema.nodes.code_block
      if (codeBlockType) {
        const { state } = editorView
        const { from, to } = state.selection
        const text = from < to ? state.doc.textBetween(from, to, "\n") : ""
        const node = codeBlockType.create(null, text ? schema.text(text) : null)
        // Use delete + insert: replaceSelectionWith/replaceRangeWith
        // lose text content when selection spans block boundaries
        const tr = state.tr
        tr.deleteSelection()
        const pos = tr.selection.from
        tr.replaceWith(pos, pos, node)
        editorView.dispatch(tr.scrollIntoView())
      }
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawInsertCodeBlock(rawEditor)
    }
  })

  // Spoiler
  if (spoilerInsertCommand) {
    addImgButton("/icons/spoiler.svg", "Spoiler", () => {
      if (editMode === "visual" && editorView) {
        spoilerInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        rawInsertSpoiler(rawEditor)
      }
    })
  }

  // Chart
  if (chartInsertCommand) {
    addImgButton("/icons/chart.svg", "Chart", () => {
      if (editMode === "visual" && editorView) {
        chartInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        rawInsertChart(rawEditor)
      }
    })
  }

  // Slides
  if (slidesInsertCommand) {
    addImgButton("/icons/slides.svg", "Slides", () => {
      if (editMode === "visual" && editorView) {
        slidesInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        rawInsertSlides(rawEditor)
      }
    })
  }

  // Include
  if (includeInsertCommand) {
    const includeBtn = addImgButton("/icons/include.svg", "Include", () => {
      if (editMode === "visual" && editorView) {
        includeInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        rawInsertInclude(rawEditor)
      }
    })
    includeBtn.querySelector(".gowiki-menu-icon").classList.add("gowiki-menu-icon--lg")
  }

  // Database Query
  if (databaseInsertQueryCommand) {
    const btn = addImgButton("/icons/database-query.svg", "Database Query", () => {
      if (editMode === "visual" && editorView) {
        databaseInsertQueryCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{database-query table=}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
    btn.querySelector(".gowiki-menu-icon").classList.add("gowiki-menu-icon--lg")
  }

  // Database New Row
  if (databaseInsertNewRowCommand) {
    const btn = addImgButton("/icons/database-newrow.svg", "Database New Row", () => {
      if (editMode === "visual" && editorView) {
        databaseInsertNewRowCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{database-newrow table=}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
    btn.querySelector(".gowiki-menu-icon").classList.add("gowiki-menu-icon--lg")
  }

  // Database Row
  if (databaseInsertRowCommand) {
    const btn = addImgButton("/icons/database-row.svg", "Database Row", () => {
      if (editMode === "visual" && editorView) {
        databaseInsertRowCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{database-row table=}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
    btn.querySelector(".gowiki-menu-icon").classList.add("gowiki-menu-icon--lg")
  }

  // Template Variable
  if (databaseInsertVarCommand) {
    addImgButton("/icons/variable.svg", "Template Variable", () => {
      if (editMode === "visual" && editorView) {
        databaseInsertVarCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{{NAME}}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet)
        rawEditor.setSelectionRange(start + 2, start + 6)
      }
    })
  }

  // Blockquote
  addIconButton(icons.blockquote, "Quote block", () => {
    if (editMode === "visual" && editorView) {
      wrapIn(schema.nodes.blockquote)(editorView.state, editorView.dispatch)
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawToggleLinePrefix(rawEditor, "> ")
    }
  })

  // Tag
  if (tagInsertCommand) {
    addButton("#", "Insert tag", () => {
      if (editMode === "visual" && editorView) {
        tagInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{tag }"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
  }

  // Tag query
  if (tagQueryInsertCommand) {
    addButton("Q#", "Insert tag query", () => {
      if (editMode === "visual" && editorView) {
        tagQueryInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{tag-query tag=}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
  }

  // Caption reference
  if (captionInsertRefCommand) {
    addButton("Ref", "Insert caption reference", () => {
      if (editMode === "visual" && editorView) {
        captionInsertRefCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{ref }"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet)
        const cursorPos = start + 5
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
  }

  // Paragraph below
  addImgButton("/icons/paragraphdown.svg", "Add paragraph below", () => {
    if (editMode === "visual" && editorView) {
      const paragraphType = schema.nodes.paragraph
      if (paragraphType) {
        const state = editorView.state
        const $from = state.selection.$from
        // Find the top-level block containing the cursor.
        const depth = $from.depth > 0 ? 1 : 0
        try {
          const afterBlock = $from.after(depth)
          const tr = state.tr.insert(afterBlock, paragraphType.create())
          tr.setSelection(TextSelection.near(tr.doc.resolve(afterBlock + 1)))
          editorView.dispatch(tr.scrollIntoView())
        } catch {
          // At end of document — append a paragraph.
          const tr = state.tr.insert(state.doc.content.size, paragraphType.create())
          tr.setSelection(TextSelection.near(tr.doc.resolve(tr.doc.content.size - 1)))
          editorView.dispatch(tr.scrollIntoView())
        }
      }
      editorView.focus()
    } else if (editMode === "raw" && rawEditor) {
      rawEditor.focus()
      // Insert a blank line after the current line.
      const val = rawEditor.value
      const pos = rawEditor.selectionStart
      const lineEnd = val.indexOf("\n", pos)
      const insertAt = lineEnd === -1 ? val.length : lineEnd
      rawEditor.setSelectionRange(insertAt, insertAt)
      rawInsertText(rawEditor, "\n\n")
    }
  })

  // Todo
  if (todoInsertCommand) {
    addImgButton("/icons/todo.svg", "Insert todo", () => {
      if (editMode === "visual" && editorView) {
        todoInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{todo title=}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
  }

  // Todo List
  if (todoListInsertCommand) {
    addImgButton("/icons/todo.svg", "Insert todo list", () => {
      if (editMode === "visual" && editorView) {
        todoListInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        rawEditor.focus()
        rawInsertText(rawEditor, "{todo-list}\n\n")
      }
    })
  }

  // Reviewflow
  if (reviewflowInsertCommand) {
    addImgButton("/icons/reviewflow.svg", "Insert reviewflow", () => {
      if (editMode === "visual" && editorView) {
        reviewflowInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{reviewflow version=}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
  }

  // Reviewflow Link
  if (reviewflowLinkInsertCommand) {
    addImgButton("/icons/reviewflow-link.svg", "Insert reviewflow link", () => {
      if (editMode === "visual" && editorView) {
        reviewflowLinkInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{reviewflow-link version=}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
  }

  // Version Link
  if (versionLinkInsertCommand) {
    addImgButton("/icons/version-link.svg", "Insert version link", () => {
      if (editMode === "visual" && editorView) {
        versionLinkInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{version-link version=}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        const cursorPos = start + snippet.length - 1
        rawEditor.setSelectionRange(cursorPos, cursorPos)
      }
    })
  }

  // Changes
  if (changesInsertCommand) {
    addImgButton("/icons/changes.svg", "Insert latest changes", () => {
      if (editMode === "visual" && editorView) {
        changesInsertCommand(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      } else if (editMode === "raw" && rawEditor) {
        const snippet = "{changes count=10}"
        const start = rawEditor.selectionStart
        rawEditor.focus()
        rawInsertText(rawEditor, snippet + "\n\n")
        rawEditor.setSelectionRange(start + snippet.length, start + snippet.length)
      }
    })
  }

  // Extra commands from plugins
  for (const { label, cmd } of extraCommands) {
    addButton(label, label, () => {
      if (editMode === "visual" && editorView) {
        cmd(editorView.state, editorView.dispatch, editorView)
        editorView.focus()
      }
    })
  }

  return { dom: bar, refs }
}

function renderEdit(nextEditMode) {
  clearContent()

  const wrapper = document.createElement("div")
  wrapper.className = "gowiki-raw-wrapper"

  const { dom: menubarDom, refs: menubarRefs } = buildMenubar()
  wrapper.appendChild(menubarDom)

  if (nextEditMode === "raw") {
    const editorEl = document.createElement("textarea")
    editorEl.id = "gowiki-raw-editor"
    editorEl.className = "gowiki-raw-editor"
    editorEl.value = currentMarkdown

    editorEl.addEventListener("keydown", e => {
      const isMod = e.metaKey || e.ctrlKey
      // Save/publish shortcuts
      if ((e.key === "s" || e.key === "S") && isMod) {
        e.preventDefault()
        e.stopPropagation()
        if (e.shiftKey) {
          void publishDraft()
        } else {
          void saveDraftExplicit()
        }
        return
      }
      if (e.key === "Enter" && !e.shiftKey && !isMod) {
        if (rawHandleEnterInList(editorEl)) {
          e.preventDefault()
          autoResizeRawEditor(editorEl)
        }
      } else if (e.key === "h" && isMod && !e.altKey) {
        e.preventDefault()
        const { lineStart, lineEnd } = rawGetCurrentLineRange(editorEl)
        const line = editorEl.value.substring(lineStart, lineEnd)
        const headMatch = line.match(/^(#{1,6})\s/)
        if (e.shiftKey) {
          // Shift+Cmd+H: toggle numbered
          if (headMatch) {
            const level = headMatch[1].length
            const isNumbered = /^#{1,6}\s+1\.\s/.test(line)
            rawSetHeadingLevel(editorEl, level, !isNumbered)
          } else {
            rawSetHeadingLevel(editorEl, 2, true)
          }
        } else {
          // Cmd+H: toggle heading
          if (headMatch) {
            // Remove heading entirely
            const stripped = line.replace(/^#{1,6}\s*(?:1\.\s)?/, "")
            editorEl.focus()
            editorEl.setSelectionRange(lineStart, lineEnd)
            rawInsertText(editorEl, stripped)
          } else {
            rawSetHeadingLevel(editorEl, 2, false)
          }
        }
        autoResizeRawEditor(editorEl)
      } else if (e.key === ";" && e.shiftKey && isMod) {
        e.preventDefault()
        openSymbolPanel("cursor")
      } else if (e.key === ":" && isMod) {
        e.preventDefault()
        openSymbolPanel("cursor")
      } else if (e.key === "Tab" && !e.ctrlKey && !e.metaKey) {
        // Tab on heading lines adjusts level
        const { lineStart, lineEnd } = rawGetCurrentLineRange(editorEl)
        const line = editorEl.value.substring(lineStart, lineEnd)
        const headMatch = line.match(/^(#{1,6})\s/)
        if (headMatch) {
          const level = headMatch[1].length
          const newLevel = e.shiftKey ? Math.max(level - 1, 1) : Math.min(level + 1, 6)
          if (newLevel !== level) {
            const isNumbered = /^#{1,6}\s+1\.\s/.test(line)
            rawSetHeadingLevel(editorEl, newLevel, isNumbered)
          }
          e.preventDefault()
          autoResizeRawEditor(editorEl)
        } else if (rawHandleTabInTable(editorEl, e.shiftKey ? -1 : 1)) {
          e.preventDefault()
          autoResizeRawEditor(editorEl)
        } else if (rawHandleTabInList(editorEl, e.shiftKey ? "out" : "in")) {
          e.preventDefault()
          autoResizeRawEditor(editorEl)
        }
      }
    })

    editorEl.addEventListener("input", () => {
      autoResizeRawEditor(editorEl)
    })

    editorEl.addEventListener("paste", e => {
      const imageFiles = extractImageFiles(e.clipboardData)
      if (imageFiles.length === 0) return
      e.preventDefault()
      void (async () => {
        for (const file of imageFiles) {
          try {
            const entry = await uploadMediaFile(file)
            const target = appendMediaVersion(buildMediaReferencePath(pageNamespace, entry.path), entry.version)
            const label = mediaLabelFromPath(entry.path)
            const snippet = `![${label}](${target})`
            editorEl.focus()
            const start = editorEl.selectionStart
            const end = editorEl.selectionEnd
            editorEl.setSelectionRange(start, end)
            rawInsertText(editorEl, snippet)
            setStatus("Pasted image " + target)
          } catch (err) {
            console.error("Image paste upload failed", err)
            setStatus("Image paste failed: " + err.message)
          }
        }
        autoResizeRawEditor(editorEl)
      })()
    })

    editorEl.addEventListener("blur", () => {
      if (mode !== "edit" || editMode !== "raw") return
      try {
        const normalized = normalizeMarkdownForStorage(editorEl.value)
        applyNormalizedEditState(normalized)
      } catch {
        // Keep invalid in-progress raw text unchanged.
      }
      validateDatabaseRows(editorEl.value).then(result => {
        if (!result.valid) setStatus(result.errors.join("; "))
      })
    })

    wrapper.appendChild(editorEl)
    contentRoot.appendChild(wrapper)
    autoResizeRawEditor(editorEl)
    rawEditor = editorEl
    return
  }

  // Visual mode
  const editorEl = document.createElement("div")
  wrapper.appendChild(editorEl)
  contentRoot.appendChild(wrapper)

  const menubarStatePlugin = new Plugin({
    view() {
      return {
        update(view) { updateMenubarState(view.state, menubarRefs) }
      }
    }
  })

  const listKeymap = keymap({
    Enter: splitListItem(schema.nodes.list_item),
    Backspace: backspaceEmptyListItemCommand(),
    Tab: tabKeyCommand("in"),
    "Shift-Tab": tabKeyCommand("out"),
    "Alt-Enter": insertHardBreakCommand(),
  })

  const shortcutKeymap = keymap({
    "Mod-b": toggleMark(schema.marks.strong),
    "Mod-i": toggleMark(schema.marks.em),
    "Mod-u": toggleMark(schema.marks.underline),
    "Mod-Shift-s": toggleMark(schema.marks.strikethrough),
    "Mod-e": toggleMark(schema.marks.code),
    "Mod-k": (state, dispatch, view) => {
      setExternalLinkCommand()(state, dispatch, view)
      return true
    },
    "Mod-h": (state, dispatch) => {
      const node = state.selection.$from.parent
      if (node.type === schema.nodes.heading) {
        return setBlockType(schema.nodes.paragraph)(state, dispatch)
      }
      return setBlockType(schema.nodes.heading, { level: 2 })(state, dispatch)
    },
    "Mod-Shift-h": (state, dispatch) => {
      const node = state.selection.$from.parent
      if (node.type === schema.nodes.heading) {
        return setBlockType(schema.nodes.heading, {
          level: node.attrs.level,
          numbered: !node.attrs.numbered,
        })(state, dispatch)
      }
      return setBlockType(schema.nodes.heading, { level: 2, numbered: true })(state, dispatch)
    },
    "Mod-s": () => {
      void saveDraftExplicit()
      return true
    },
    "Mod-Shift-s": () => {
      void publishDraft()
      return true
    },
    "Mod-Shift-;": () => {
      openSymbolPanel("cursor")
      return true
    },
  })

  const plugins = [
    shortcutKeymap,
    listKeymap,
    history(),
    keymap(baseKeymap),
    ...registry.getEditorPlugins(),
    menubarStatePlugin,
  ]

  // Restore stashed state (preserves undo history) or create fresh.
  let state
  if (stashedEditorState) {
    // Reconfigure with fresh plugins (new menubar refs) but keep doc + history.
    state = stashedEditorState.reconfigure({ plugins })
    stashedEditorState = null
  } else {
    state = EditorState.create({ doc: currentDoc, schema, plugins })
  }

  editorView = new EditorView(editorEl, {
    state,
    handleScrollToSelection(view) {
      return scrollSelectionIntoContentView(view)
    },
    handlePaste(view, event, slice) {
      const plainText = event.clipboardData?.getData("text/plain") ?? ""

      // When pasting table content (rows/cells) inside a table, let
      // ProseMirror's default handling (prosemirror-tables) manage it
      // so rows are inserted as siblings rather than nested.
      const { $from } = view.state.selection
      if (isInTableCell(view.state)) {
        let hasTableContent = false
        slice.content.forEach(node => {
          if (node.type.name === "table" || node.type.name === "table_row") hasTableContent = true
        })
        if (hasTableContent) return false
      }

      // Inside a code_block: insert plain text literally
      if ($from.parent.type === schema.nodes.code_block) {
        view.dispatch(view.state.tr.insertText(plainText))
        return true
      }

      // Direct image paste (screenshot, "Copy Image" from browser)
      // Check both files and items for image blobs.
      // Skip if clipboard also has HTML with table content (e.g. Excel paste)
      // — prefer structured table data over the image representation.
      const imageFiles = extractImageFiles(event.clipboardData)
      if (imageFiles.length > 0) {
        const clipHtml = event.clipboardData?.getData("text/html") ?? ""
        if (!/<table[\s>]/i.test(clipHtml)) {
          void handleImageFilePaste(view, imageFiles)
          return true
        }
      }

      // Sanitize through serialize → parse → insert pipeline
      let md
      try {
        const tempDoc = schema.nodes.doc.create(null, slice.content)
        md = pmToMarkdown(tempDoc, registry)
      } catch {
        // Serialization failed (unknown node types) — use plain text as markdown
        md = plainText
      }

      // Clean up list-like patterns (·, •, numbered) from pasted content
      md = cleanupPastedMarkdown(md)

      // Handle images with non-local src (data:, file://, blob://, http(s)://)
      if (hasNonLocalImages(md)) {
        void handleNonLocalImagePaste(view, md, slice, plainText)
        return true
      }

      try {
        const cleanDoc = markdownToPM(md, registry)
        // Use openStart/openEnd=0 so block-level content (tables, lists, etc.)
        // is inserted as a complete block rather than being flattened.
        // Only preserve the original open depths for purely inline content
        // (single paragraph whose content can merge into the cursor paragraph).
        let openStart = 0
        let openEnd = 0
        const fc = cleanDoc.content.firstChild
        if (cleanDoc.content.childCount === 1 && fc && fc.type === schema.nodes.paragraph) {
          openStart = Math.min(slice.openStart, 1)
          openEnd = Math.min(slice.openEnd, 1)
        }
        const tr = view.state.tr.replaceSelection(
          new Slice(cleanDoc.content, openStart, openEnd)
        )
        view.dispatch(tr)
      } catch {
        // Parse failed — insert as plain text
        view.dispatch(view.state.tr.insertText(plainText))
      }
      return true
    },
    handleDrop(view, event, slice, moved) {
      // Internal drag — let ProseMirror handle it
      if (moved) return false

      const plainText = event.dataTransfer?.getData("text/plain") ?? ""

      let md
      try {
        const tempDoc = schema.nodes.doc.create(null, slice.content)
        md = pmToMarkdown(tempDoc, registry)
      } catch {
        md = plainText
      }

      md = cleanupPastedMarkdown(md)

      try {
        const cleanDoc = markdownToPM(md, registry)
        let openStart = 0
        let openEnd = 0
        const fc = cleanDoc.content.firstChild
        if (cleanDoc.content.childCount === 1 && fc && fc.type === schema.nodes.paragraph) {
          openStart = Math.min(slice.openStart, 1)
          openEnd = Math.min(slice.openEnd, 1)
        }
        const tr = view.state.tr.replaceSelection(
          new Slice(cleanDoc.content, openStart, openEnd)
        )
        view.dispatch(tr)
      } catch {
        view.dispatch(view.state.tr.insertText(plainText))
      }
      return true
    },
    handleDOMEvents: {
      blur(view) {
        if (mode !== "edit" || editMode !== "visual") return false
        try {
          const serialized = pmToMarkdown(view.state.doc, registry)
          const normalized = normalizeMarkdownForStorage(serialized)
          if (normalized.roundTripError) {
            console.error("Round-trip validation failed on blur, skipping normalization")
            return false
          }
          applyNormalizedEditState(normalized)
        } catch {
          // Keep edit session live while user content is in progress.
        }
        return false
      },
      contextmenu(view, event) {
        // Show table context menu when right-clicking inside a table.
        const $pos = view.state.selection.$from
        let inTable = false
        for (let d = $pos.depth; d > 0; d--) {
          if ($pos.node(d).type.name === "table") { inTable = true; break }
        }
        if (!inTable) return false
        event.preventDefault()

        // Remove any previous context menu clone.
        document.querySelector(".gowiki-ctx-menu")?.remove()

        // Build a standalone context menu (not a child of the toolbar).
        const ctxMenu = document.createElement("div")
        ctxMenu.className = "gowiki-raw-dropdown-menu gowiki-ctx-menu"
        ctxMenu.style.position = "fixed"
        ctxMenu.style.left = event.clientX + "px"
        ctxMenu.style.top = event.clientY + "px"
        ctxMenu.style.display = "block"
        ctxMenu.style.zIndex = "9999"

        const ctxActions = [
          { name: "row.addBefore", label: "Add row above" },
          { name: "row.addAfter", label: "Add row below" },
          { name: "column.addBefore", label: "Add column left" },
          { name: "column.addAfter", label: "Add column right" },
          { name: "row.delete", label: "Delete row" },
          { name: "column.delete", label: "Delete column" },
          { name: "cell.properties", label: "Cell properties" },
          { name: "cell.merge", label: "Merge cells" },
          { name: "cell.split", label: "Unmerge cell" },
          { name: "delete", label: "Delete table" },
        ]
        const visualOnly = new Set(["cell.merge", "cell.split"])
        for (const action of ctxActions) {
          const item = document.createElement("div")
          item.className = "gowiki-raw-dropdown-item"
          item.textContent = action.label
          if (visualOnly.has(action.name) && editMode !== "visual") {
            item.classList.add("gowiki-raw-dropdown-item--disabled")
          }
          item.addEventListener("mousedown", (e) => {
            e.preventDefault()
            e.stopPropagation()
            if (item.classList.contains("gowiki-raw-dropdown-item--disabled")) return
            ctxMenu.remove()
            if (editMode === "visual" && editorView) {
              const cmd = tableCommands.get(action.name)
              if (cmd) cmd(editorView.state, editorView.dispatch)
              editorView.focus()
            }
          })
          ctxMenu.appendChild(item)
        }

        document.body.appendChild(ctxMenu)
        // Close on any outside click.
        const closeCtx = (e) => {
          if (!ctxMenu.contains(e.target)) {
            ctxMenu.remove()
            document.removeEventListener("mousedown", closeCtx, true)
          }
        }
        document.addEventListener("mousedown", closeCtx, true)
        return true
      },
    },
  })

  // Auto-focus the editor so the user can start typing immediately.
  if (nextEditMode === "visual" && editorView) {
    editorView.focus()
  } else if (nextEditMode === "raw") {
    const ta = document.querySelector(".gowiki-raw-editor")
    if (ta) ta.focus()
  }
}

// ── Action bar icon SVG paths (stroke-based, 24x24 viewBox) ──

const actionIcons = {
  // Pencil
  edit: "M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z",
  // Clock with arrow
  history: "M12 8v4l3 3m6-3a9 9 0 1 1-2.64-6.36",
  // Arrow bend left
  backlinks: "M9 17H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4m6 16 4-4-4-4m4 4H9",
  // File with plus
  newPage: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8ZM14 2v6h6M12 18v-6m-3 3h6",
  // Tree/sitemap
  siteMap: "M6 9H4.5a2 2 0 0 1-2-2V5.5a2 2 0 0 1 2-2h3a2 2 0 0 1 2 2V7a2 2 0 0 1-2 2H6Zm0 0v3m0 0c0 1.1.9 2 2 2h4c1.1 0 2-.9 2-2m-8 0c0 1.1-.9 2-2 2H4.5a2 2 0 0 0-2 2V18a2 2 0 0 0 2 2h3a2 2 0 0 0 2-2v-1.5a2 2 0 0 0-2-2H6m8-2.5v3.5a2 2 0 0 0 2 2h1.5a2 2 0 0 0 2-2V17a2 2 0 0 0-2-2h-1.5a2 2 0 0 0-2 2Z",
  // File arrow down
  exportPdf: "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8ZM14 2v6h6M12 18v-6m-3 3 3 3 3-3",
  // Chat bubble
  comment: "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2Z",
  // Arrows move
  move: "M5 9l-3 3 3 3M9 5l3-3 3 3M15 19l-3 3-3-3M19 9l3 3-3 3M2 12h20M12 2v20",
  // Folder arrows
  convert: "M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2ZM9 15l3-3 3 3",
  // Trash
  delete: "M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6Z",
  // Image/photo
  media: "M21 3H3a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h18a1 1 0 0 0 1-1V4a1 1 0 0 0-1-1Zm-3 5a1.5 1.5 0 1 1-3 0 1.5 1.5 0 0 1 3 0ZM2 20l5.5-7L11 17l3.5-4.5L22 20Z",
  // Code brackets
  switchRaw: "M16 18l6-6-6-6M8 6l-6 6 6 6",
  // Eye
  switchVisual: "M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8Z M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z",
  // Floppy disk
  save: "M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2ZM17 21v-8H7v8M7 3v5h8",
  // Upload / cloud up
  publish: "M12 16V4m-5 4 5-5 5 5M20 21H4",
  // X circle
  cancel: "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20Zm3.54 6.46L9.46 14.54m0-6.08 6.08 6.08",
  // Hourglass (main, shifted up-left) + small trash (nudged down-right)
  discard: "M2 1h10M2 17h10M7 9l2.5-3.5V2H4.5v3.5L7 9Zm0 0-2.5 3.5V16h5v-3.5L7 9ZM14 10h8M17 10V8.5a1.5 1.5 0 0 1 3 0V10m2.5 0v8.5a1.5 1.5 0 0 1-1.5 1.5h-5a1.5 1.5 0 0 1-1.5-1.5V10Z",
  // Expand arrows
  fullscreen: "M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3",
  // Contract arrows
  exitFullscreen: "M4 14h4v4M20 10h-4V6M14 10h4V6M10 14H6v4",
  // Lock
  lock: "M19 11H5a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7a2 2 0 0 0-2-2ZM7 11V7a5 5 0 0 1 10 0v4",
  // Hourglass (main, shifted up-left) + small floppy (nudged down-right)
  saveDraft: "M2 1h10M2 17h10M7 9l2.5-3.5V2H4.5v3.5L7 9Zm0 0-2.5 3.5V16h5v-3.5L7 9ZM14 12h6.5l3.5 3.5v5a1.5 1.5 0 0 1-1.5 1.5h-7a1.5 1.5 0 0 1-1.5-1.5v-7a1.5 1.5 0 0 1 1.5-1.5ZM20.5 12v3.5H24M15 22v-4h5v4M15 12v3h4",
}

function makeActionIconBtn(iconName, tooltip, onClick, extraClass) {
  const btn = document.createElement("button")
  btn.type = "button"
  btn.className = "gowiki-action-btn gowiki-action-btn--stroke" + (extraClass ? " " + extraClass : "")
  btn.title = tooltip
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg")
  svg.setAttribute("viewBox", "0 0 24 24")
  const pathData = actionIcons[iconName]
  if (pathData) {
    for (const d of pathData.split(/(?=M)/)) {
      // For multi-path icons split at each M, but actually just use one path
    }
    const p = document.createElementNS("http://www.w3.org/2000/svg", "path")
    p.setAttribute("d", pathData)
    svg.appendChild(p)
  }
  btn.appendChild(svg)
  btn.addEventListener("click", onClick)
  return btn
}

function makeActionSep() {
  const sep = document.createElement("div")
  sep.className = "gowiki-action-sep"
  return sep
}

// Text-button factory for in-content buttons (history Back, etc.)
function makeContentButton(label, onClick) {
  const btn = document.createElement("button")
  btn.type = "button"
  btn.className = "gowiki-content-btn"
  btn.textContent = label
  btn.addEventListener("click", onClick)
  return btn
}

function promptNewPage() {
  return new Promise(resolve => {
    const overlay = document.createElement("div")
    overlay.className = "gowiki-link-modal-overlay"

    const dialog = document.createElement("div")
    dialog.className = "gowiki-link-modal"

    const title = document.createElement("div")
    title.className = "gowiki-link-modal-title"
    title.textContent = "Create new page"

    const pathLabel = document.createElement("label")
    pathLabel.className = "gowiki-link-modal-label"
    pathLabel.textContent = "Page path"

    const pathInput = document.createElement("input")
    pathInput.type = "text"
    pathInput.className = "gowiki-link-modal-input"
    pathInput.placeholder = "/namespace/page-name"

    const warning = document.createElement("div")
    warning.className = "gowiki-link-modal-warning"

    const buttons = document.createElement("div")
    buttons.className = "gowiki-link-modal-actions"

    const cancelBtn = document.createElement("button")
    cancelBtn.type = "button"
    cancelBtn.textContent = "Cancel"
    cancelBtn.className = "gowiki-link-modal-btn"

    const okBtn = document.createElement("button")
    okBtn.type = "button"
    okBtn.textContent = "Create"
    okBtn.className = "gowiki-link-modal-btn"

    function close(value) {
      overlay.remove()
      resolve(value)
    }

    function submit() {
      const raw = pathInput.value.trim()
      if (!raw) {
        warning.textContent = "Path cannot be empty."
        pathInput.focus()
        return
      }
      if (/\.\w+$/.test(raw)) {
        warning.textContent = "Page paths must not have a file extension."
        pathInput.focus()
        return
      }
      const cleaned = raw.replace(/^\/+/, "").replace(/\/+$/, "")
      if (!cleaned) {
        warning.textContent = "Invalid path."
        pathInput.focus()
        return
      }
      close(cleaned)
    }

    cancelBtn.addEventListener("click", () => close(null))
    okBtn.addEventListener("click", submit)
    overlay.addEventListener("click", event => {
      if (event.target === overlay) close(null)
    })
    pathInput.addEventListener("keydown", event => {
      if (event.key === "Enter") {
        event.preventDefault()
        submit()
      } else if (event.key === "Escape") {
        event.preventDefault()
        close(null)
      }
    })

    buttons.appendChild(cancelBtn)
    buttons.appendChild(okBtn)
    dialog.appendChild(title)
    dialog.appendChild(pathLabel)
    dialog.appendChild(pathInput)
    dialog.appendChild(warning)
    dialog.appendChild(buttons)
    overlay.appendChild(dialog)
    document.body.appendChild(overlay)
    pathInput.focus()
  }).then(pagePath => {
    if (pagePath) {
      window.location.href = "/" + pagePath + "?action=create"
    }
  })
}

function toggleFullscreen() {
  isFullscreen = !isFullscreen
  document.body.classList.toggle("gowiki-fullscreen", isFullscreen)
  if (isFullscreen) {
    const exitBtn = document.createElement("button")
    exitBtn.className = "gowiki-fullscreen-exit"
    exitBtn.title = "Exit fullscreen (F11 or Esc)"
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg")
    svg.setAttribute("viewBox", "0 0 24 24")
    const p = document.createElementNS("http://www.w3.org/2000/svg", "path")
    p.setAttribute("d", actionIcons.exitFullscreen)
    svg.appendChild(p)
    exitBtn.appendChild(svg)
    exitBtn.addEventListener("click", toggleFullscreen)
    document.body.appendChild(exitBtn)
  } else {
    const existing = document.querySelector(".gowiki-fullscreen-exit")
    if (existing) existing.remove()
  }
}

function renderActions() {
  actionsRoot.innerHTML = ""

  if (mode === "edit") {
    // ── Edit mode actions ──
    actionsRoot.appendChild(makeActionIconBtn("media", "Media manager", () => {
      openMediaManager(
        pageNamespace,
        text => setStatus(text),
        (kind, entry) => {
          if (editMode === "raw") {
            rawInsertMediaReference(kind, entry)
          } else {
            insertMediaReference(kind, entry)
          }
        }
      )
    }))

    if (editMode === "visual") {
      actionsRoot.appendChild(makeActionIconBtn("switchRaw", "Switch to raw", () => setEditMode("raw")))
    } else {
      actionsRoot.appendChild(makeActionIconBtn("switchVisual", "Switch to visual", () => setEditMode("visual")))
    }

    actionsRoot.appendChild(makeActionSep())

    const saveHint = isMac ? "\u2318S" : "Ctrl+S"
    actionsRoot.appendChild(makeActionIconBtn("save", `Save & continue (${saveHint})`, () => void saveDraftExplicit()))
    actionsRoot.appendChild(makeActionIconBtn("saveDraft", "Save to draft", () => void saveDraftAndExit()))

    const pubHint = isMac ? "\u21E7\u2318S" : "Ctrl+Shift+S"
    actionsRoot.appendChild(makeActionIconBtn("publish", `Publish (${pubHint})`, () => void publishDraft()))

    actionsRoot.appendChild(makeActionSep())
    actionsRoot.appendChild(makeActionIconBtn("cancel", "Cancel editing", () => cancelEdit()))
    actionsRoot.appendChild(makeActionIconBtn("discard", "Discard draft", () => void discardDraft(), "gowiki-action-delete"))

    actionsRoot.appendChild(makeActionSep())
    actionsRoot.appendChild(makeActionIconBtn("fullscreen", "Fullscreen (F11)", () => toggleFullscreen()))

  } else {
    // ── View mode actions ──

    // Edit button — varies by lock/draft state
    if (pageLockInfo && pageLockInfo.is_draft && pageLockInfo.locked_by === currentUser?.username) {
      const editBtn = makeActionIconBtn("edit", "Resume editing \u2014 unpublished draft", () => void enterEditMode(true))
      const dot = document.createElement("span")
      dot.className = "gowiki-action-draft-dot"
      editBtn.appendChild(dot)
      actionsRoot.appendChild(editBtn)
      actionsRoot.appendChild(makeActionIconBtn("publish", "Publish draft", () => void publishFromView()))
      actionsRoot.appendChild(makeActionIconBtn("discard", "Discard draft", () => void discardDraft(), "gowiki-action-delete"))
    } else if (pageLockInfo && pageLockInfo.locked_by && pageLockInfo.locked_by !== currentUser?.username) {
      const editBtn = makeActionIconBtn("lock", `Locked by ${pageLockInfo.locked_by}`, () => {})
      editBtn.disabled = true
      actionsRoot.appendChild(editBtn)
    } else {
      const editHint = isMac ? "\u2318E" : "Ctrl+E"
      actionsRoot.appendChild(makeActionIconBtn("edit", `Edit (${editHint})`, () => void enterEditMode(false)))
    }

    actionsRoot.appendChild(makeActionSep())

    actionsRoot.appendChild(makeActionIconBtn("history", "History", () => void showHistory()))
    actionsRoot.appendChild(makeActionIconBtn("backlinks", "Backlinks", () => void showBacklinks()))

    actionsRoot.appendChild(makeActionSep())

    actionsRoot.appendChild(makeActionIconBtn("newPage", "New page", () => void promptNewPage()))
    actionsRoot.appendChild(makeActionIconBtn("siteMap", "Site map", () => { window.location.href = "/_sitemap" }))
    actionsRoot.appendChild(makeActionIconBtn("exportPdf", "Export PDF", () => window.open(`/api/export/pdf/${pagePath}`, "_blank")))

    if (currentUser && !isNewPage) {
      actionsRoot.appendChild(makeActionSep())
      const commentCount = getCommentCount()
      const commentTip = commentCount > 0 ? `Comments (${commentCount})` : "Comment"
      const commentBtn = makeActionIconBtn("comment", commentTip, () => addComment())
      if (commentCount > 0) {
        const badge = document.createElement("span")
        badge.className = "gowiki-action-badge"
        badge.textContent = String(commentCount)
        commentBtn.appendChild(badge)
      }
      actionsRoot.appendChild(commentBtn)
      actionsRoot.appendChild(makeActionIconBtn("move", "Move page", () => void movePage()))
      if (isNamespaceIndex) {
        actionsRoot.appendChild(makeActionIconBtn("convert", "Convert to regular page", () => void convertPageType("to_regular_page")))
      } else {
        actionsRoot.appendChild(makeActionIconBtn("convert", "Convert to namespace", () => void convertPageType("to_namespace_index")))
      }
      actionsRoot.appendChild(makeActionIconBtn("delete", "Delete page", () => void deletePage(), "gowiki-action-delete"))
    }

    actionsRoot.appendChild(makeActionSep())
    actionsRoot.appendChild(makeActionIconBtn("fullscreen", "Fullscreen (F11)", () => toggleFullscreen()))
  }
}

async function publishFromView() {
  // Quick-publish: enter edit, then immediately publish.
  // Use force=true since the user owns the draft and is explicitly publishing.
  const ok = await enterEditMode(true)
  if (!ok) return
  await publishDraft()
}

async function deletePage() {
  if (!confirm(`Delete page "${pageDisplayPath}"? This will archive the content.`)) {
    return
  }

  const resp = await authFetch(`/api/pages/${pagePath}`, { method: "DELETE" })
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({}))
    const msg = data.error || `Delete failed (${resp.status})`
    setStatus(msg)
    return
  }

  const data = await resp.json()

  // Warn about pages that include this page.
  if (data.included_by && data.included_by.length > 0) {
    alert(
      `Warning: this page was included by:\n${data.included_by.join("\n")}\n\nThose pages may now have broken includes.`
    )
  }

  // Navigate to parent namespace or root.
  const parent = pagePath.includes("/")
    ? "/" + pagePath.split("/").slice(0, -1).join("/")
    : "/"
  setStatus("Page deleted.")
  window.location.href = parent
}

async function movePage() {
  const newPath = prompt("Move page to:", `/${pagePath}`)
  if (!newPath || newPath.trim() === "" || newPath.trim() === `/${pagePath}`) return

  // Dry run first: always request preview with move_media=true to show all options.
  let preview
  try {
    const previewResp = await authFetch(`/api/move/${pagePath}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ to: newPath.trim(), move_media: true, dry_run: true }),
    })
    if (!previewResp.ok) {
      const data = await previewResp.json().catch(() => ({}))
      setStatus(data.error || `Move preview failed (${previewResp.status})`)
      return
    }
    preview = await previewResp.json()
  } catch {
    setStatus("Move preview failed")
    return
  }

  const hasAffectedPages = preview.affected_pages && preview.affected_pages.length > 0
  const hasMedia = preview.media_to_move && preview.media_to_move.length > 0

  // Ask about optional actions first, before the final confirmation.
  let updateLinks = false
  if (hasAffectedPages) {
    updateLinks = confirm(
      `${preview.affected_pages.length} page(s) contain links to this page:\n\n` +
      preview.affected_pages.map(p => `  - ${p}`).join("\n") +
      "\n\nUpdate their links to point to the new location?"
    )
  }

  let moveMedia = false
  if (hasMedia) {
    moveMedia = confirm(
      `${preview.media_to_move.length} media file(s) are exclusively referenced by this page:\n\n` +
      preview.media_to_move.map(m => `  - ${m}`).join("\n") +
      "\n\nMove them alongside the page?"
    )
  }

  // Build final confirmation summarizing all actions.
  const lines = [`Move page "${pageDisplayPath}" to "${preview.new_path}"`, ""]
  lines.push("Actions:")
  lines.push(`  - Move page and its full history`)
  if (updateLinks) {
    lines.push(`  - Update links in ${preview.affected_pages.length} page(s)`)
  }
  if (moveMedia) {
    lines.push(`  - Co-move ${preview.media_to_move.length} media file(s)`)
  }
  lines.push("")
  lines.push("Apply?")
  if (!confirm(lines.join("\n"))) return

  // Execute the move.
  const resp = await authFetch(`/api/move/${pagePath}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      to: newPath.trim(),
      move_media: moveMedia,
      update_links: updateLinks,
    }),
  })
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({}))
    setStatus(data.error || `Move failed (${resp.status})`)
    return
  }

  const result = await resp.json()
  setStatus("Page moved.")
  window.location.href = result.new_path
}

async function convertPageType(flag) {
  const label = flag === "to_namespace_index"
    ? "Convert to namespace index?"
    : "Convert to regular page?"

  if (!confirm(label)) return

  const body = {}
  body[flag] = true

  const resp = await authFetch(`/api/move/${pagePath}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!resp.ok) {
    const data = await resp.json().catch(() => ({}))
    setStatus(data.error || `Conversion failed (${resp.status})`)
    return
  }

  setStatus("Page converted.")
  window.location.reload()
}

// ── History and Diff UI ──────────────────────────────

let inHistoryView = false // true when showing history/version/diff

function enterHistoryView() {
  if (!inHistoryView) {
    // Push a state so the browser back button returns to the page.
    window.history.pushState({ gowikiHistory: true }, "", window.location.href)
    inHistoryView = true
  }
}

function exitHistoryView() {
  inHistoryView = false
  currentDoc = markdownToPM(currentMarkdown, registry)
  setMode("view")
}

window.addEventListener("popstate", (e) => {
  if (inHistoryView) {
    inHistoryView = false
    currentDoc = markdownToPM(currentMarkdown, registry)
    setMode("view")
  }
})

async function showHistory() {
  try {
    const resp = await fetch(`/api/history/${encodePagePath(pagePath)}`)
    if (!resp.ok) {
      setStatus("Failed to load history")
      return
    }
    const data = await resp.json()
    const versions = data.versions || []
    enterHistoryView()
    renderHistoryPage(versions, data.draft || null)
  } catch {
    setStatus("Failed to load history")
  }
}

async function showBacklinks() {
  try {
    const resp = await fetch(`/api/backlinks/${encodePagePath(pagePath)}`)
    if (!resp.ok) {
      setStatus("Failed to load backlinks")
      return
    }
    const data = await resp.json()
    const backlinks = data.backlinks || []
    enterHistoryView()
    renderBacklinksPage(backlinks)
  } catch {
    setStatus("Failed to load backlinks")
  }
}

function renderBacklinksPage(backlinks) {
  clearContent()
  mode = "view"
  appRoot.classList.remove("gowiki-editing")

  const container = document.createElement("div")
  container.className = "gowiki-history"

  const title = document.createElement("h2")
  title.textContent = `Backlinks: ${pageDisplayPath}`
  container.appendChild(title)

  if (backlinks.length === 0) {
    const empty = document.createElement("p")
    empty.textContent = "No pages link to this page."
    empty.style.color = "#666"
    container.appendChild(empty)
  } else {
    const list = document.createElement("ul")
    list.className = "gowiki-backlinks-list"
    for (const entry of backlinks) {
      const li = document.createElement("li")
      const link = document.createElement("a")
      const href = entry.path.startsWith("/") ? entry.path : "/" + entry.path
      link.href = href
      link.textContent = entry.path
      link.addEventListener("click", e => {
        e.preventDefault()
        window.location.href = href
      })
      li.appendChild(link)
      if (entry.title && entry.title !== entry.path) {
        const titleSpan = document.createElement("span")
        titleSpan.textContent = ` — ${entry.title}`
        titleSpan.style.color = "#888"
        li.appendChild(titleSpan)
      }
      list.appendChild(li)
    }
    container.appendChild(list)
  }

  const backBtn = document.createElement("button")
  backBtn.textContent = "Back to page"
  backBtn.className = "gowiki-content-btn"
  backBtn.style.marginTop = "16px"
  backBtn.addEventListener("click", () => {
    window.history.back()
  })
  container.appendChild(backBtn)

  contentRoot.appendChild(container)
}

function renderHistoryPage(versions, draft) {
  clearContent()
  mode = "view"
  appRoot.classList.remove("gowiki-editing")

  const container = document.createElement("div")
  container.className = "gowiki-history"

  const title = document.createElement("h2")
  title.textContent = `History: ${pageDisplayPath}`
  container.appendChild(title)

  // Draft notice for non-owners.
  if (draft && draft.is_own === false) {
    const notice = document.createElement("div")
    notice.className = "gowiki-history-draft-notice"
    notice.textContent = `A draft by ${draft.owner} exists`

    // Admin users get a force-discard button.
    if (currentUser && currentUser.is_admin) {
      const forceBtn = document.createElement("button")
      forceBtn.textContent = "Force discard"
      forceBtn.className = "gowiki-history-btn gowiki-history-btn-discard"
      forceBtn.style.marginLeft = "12px"
      forceBtn.addEventListener("click", () => void adminForceDiscardDraft(draft.owner))
      notice.appendChild(forceBtn)
    }

    container.appendChild(notice)
  }

  if (versions.length === 0 && !(draft && draft.is_own)) {
    const empty = document.createElement("p")
    empty.textContent = "No version history available."
    empty.style.color = "#666"
    container.appendChild(empty)
  } else {
    const table = document.createElement("table")
    table.className = "gowiki-history-table"
    const thead = document.createElement("thead")
    thead.innerHTML = "<tr><th>Version</th><th>Date</th><th>Author</th><th>Status</th><th>Actions</th></tr>"
    table.appendChild(thead)
    const tbody = document.createElement("tbody")

    // Draft row for owner.
    if (draft && draft.is_own === true) {
      const tr = document.createElement("tr")
      tr.className = "gowiki-history-draft-row"

      const tdVer = document.createElement("td")
      tdVer.innerHTML = "<em>Draft</em>"
      tr.appendChild(tdVer)

      const tdDate = document.createElement("td")
      tdDate.textContent = draft.since ? new Date(draft.since).toLocaleString() : "—"
      tr.appendChild(tdDate)

      const tdAuthor = document.createElement("td")
      tdAuthor.textContent = draft.owner
      tr.appendChild(tdAuthor)

      const tdStatus = document.createElement("td")
      tdStatus.textContent = "—"
      tr.appendChild(tdStatus)

      const tdActions = document.createElement("td")
      tdActions.className = "gowiki-history-actions"

      const diffBtn = document.createElement("button")
      diffBtn.textContent = "Diff vs published"
      diffBtn.className = "gowiki-history-btn"
      diffBtn.addEventListener("click", () => void showDiff(0, -1))
      tdActions.appendChild(diffBtn)

      const publishBtn = document.createElement("button")
      publishBtn.textContent = "Publish"
      publishBtn.className = "gowiki-history-btn gowiki-history-btn-restore"
      publishBtn.addEventListener("click", () => void publishDraftFromHistory())
      tdActions.appendChild(publishBtn)

      const discardBtn = document.createElement("button")
      discardBtn.textContent = "Discard"
      discardBtn.className = "gowiki-history-btn gowiki-history-btn-discard"
      discardBtn.addEventListener("click", () => void discardDraftFromHistory(draft.has_changes))
      tdActions.appendChild(discardBtn)

      tr.appendChild(tdActions)
      tbody.appendChild(tr)
    }

    // Show newest first.
    if (versions.length > 0) {
      const sorted = [...versions].reverse()
      const latestVersion = sorted[0].version
      const oldestVersion = sorted[sorted.length - 1].version
      historyLatestVersion = latestVersion
      for (const v of sorted) {
        const tr = document.createElement("tr")

        const tdVer = document.createElement("td")
        tdVer.textContent = `v${v.version}`
        tr.appendChild(tdVer)

        const tdDate = document.createElement("td")
        tdDate.textContent = new Date(v.timestamp).toLocaleString()
        tr.appendChild(tdDate)

        const tdAuthor = document.createElement("td")
        tdAuthor.textContent = v.author || "—"
        tr.appendChild(tdAuthor)

        const tdStatus = document.createElement("td")
        const rfMeta = v.plugin_meta?.reviewflow
        if (rfMeta) {
          if (rfMeta.is_validated) {
            const badge = document.createElement("span")
            badge.className = "gowiki-history-validated-badge"
            badge.textContent = "Validated"
            tdStatus.appendChild(badge)
            if (rfMeta.version_tag) {
              const tag = document.createElement("a")
              tag.className = "gowiki-history-version-tag"
              tag.textContent = rfMeta.version_tag
              tag.href = `${window.location.pathname}?v=${v.version}`
              tag.title = `View validated version ${rfMeta.version_tag}`
              tag.addEventListener("click", (e) => {
                e.preventDefault()
                void viewVersion(v.version)
              })
              tdStatus.appendChild(tag)
            }
          } else {
            tdStatus.textContent = "—"
          }
        } else {
          tdStatus.textContent = "—"
        }
        tr.appendChild(tdStatus)

        const tdActions = document.createElement("td")
        tdActions.className = "gowiki-history-actions"

        const viewBtn = document.createElement("button")
        viewBtn.textContent = "View"
        viewBtn.className = "gowiki-history-btn"
        viewBtn.addEventListener("click", () => void viewVersion(v.version))
        tdActions.appendChild(viewBtn)

        if (v.version > oldestVersion) {
          const diffPrevBtn = document.createElement("button")
          diffPrevBtn.textContent = "Diff vs prev"
          diffPrevBtn.className = "gowiki-history-btn"
          const prevVersion = sorted[sorted.indexOf(v) + 1].version
          diffPrevBtn.addEventListener("click", () => void showDiff(prevVersion, v.version))
          tdActions.appendChild(diffPrevBtn)
        }

        if (v.version < latestVersion) {
          const diffCurBtn = document.createElement("button")
          diffCurBtn.textContent = "Diff vs current"
          diffCurBtn.className = "gowiki-history-btn"
          diffCurBtn.addEventListener("click", () => void showDiff(v.version, 0))
          tdActions.appendChild(diffCurBtn)

          const restoreBtn = document.createElement("button")
          restoreBtn.textContent = "Restore"
          restoreBtn.className = "gowiki-history-btn gowiki-history-btn-restore"
          restoreBtn.addEventListener("click", () => void restoreVersion(v.version))
          tdActions.appendChild(restoreBtn)
        }

        tr.appendChild(tdActions)
        tbody.appendChild(tr)
      }
    }
    table.appendChild(tbody)
    container.appendChild(table)
  }

  const backBtn = document.createElement("button")
  backBtn.textContent = "Back to page"
  backBtn.className = "gowiki-content-btn"
  backBtn.style.marginTop = "16px"
  backBtn.addEventListener("click", () => {
    window.history.back()
  })
  container.appendChild(backBtn)

  contentRoot.appendChild(container)
}

// rewriteMediaToVersioned rewrites <img> src attributes in a container
// to use versioned media URLs based on the media_refs map from the attic entry.
// media_refs maps media path (e.g. "images/photo.png") to version number.
// resolveMediaSrc resolves an image src (as it appears in the DOM) to
// the normalized media path used as key in media_refs.
// Handles: /media/path, ./relative, ../relative, /absolute
function resolveMediaSrc(src) {
  // Absolute /media/ URL
  const mediaMatch = src.match(/^\/media\/(.+)$/)
  if (mediaMatch) return mediaMatch[1]

  // Skip external URLs
  if (/^https?:\/\//i.test(src)) return null

  // Relative path (./file, ../dir/file) — resolve against current page namespace
  if (src.startsWith("./") || src.startsWith("../")) {
    const ns = pageNamespace // e.g. "" for root, "ns/sub" for nested
    const nsParts = ns ? ns.split("/") : []
    const refParts = src.split("/")
    const resolved = [...nsParts]
    for (const part of refParts) {
      if (part === ".") continue
      if (part === "..") { resolved.pop(); continue }
      resolved.push(part)
    }
    return resolved.join("/")
  }

  // Absolute path starting with /
  if (src.startsWith("/")) return src.slice(1)

  return null
}

// rewriteMediaToVersioned rewrites <img> src attributes in a container
// to use versioned media URLs based on the media_refs map from an attic entry.
// Used for backward compat when viewing old archived page versions that have
// frozen media_refs but bare image URLs in their markdown.
function rewriteMediaToVersioned(container, mediaRefs) {
  if (!mediaRefs || typeof mediaRefs !== "object") return
  const imgs = container.querySelectorAll("img[src]")
  for (const img of imgs) {
    const src = img.getAttribute("src") || ""
    // Skip images that already have a ?v= parameter (new-style versioned URLs).
    if (/[?&]v=\d/.test(src)) continue
    const mediaPath = resolveMediaSrc(src)
    if (!mediaPath) continue
    const ver = mediaRefs[mediaPath]
    if (ver != null) {
      img.setAttribute("src", `/media/${mediaPath}?v=${ver}`)
    }
  }
}

async function viewVersion(version) {
  try {
    const resp = await fetch(`/api/versions/${encodePagePath(pagePath)}?v=${version}`)
    if (!resp.ok) {
      setStatus("Failed to load version")
      return
    }
    const data = await resp.json()
    clearContent()

    const container = document.createElement("div")
    container.className = "gowiki-version-view"

    const header = document.createElement("div")
    header.className = "gowiki-version-header"
    header.textContent = `Viewing version ${version} of ${pageDisplayPath}`

    const content = document.createElement("div")
    content.className = "gowiki-version-content"
    window.__gowikiViewingVersion = version
    mountReadOnlyView(content, data.markdown, "gowiki-view")
    window.__gowikiViewingVersion = null
    rewriteMediaToVersioned(content, data.media_refs)

    const actions = document.createElement("div")
    actions.style.marginTop = "16px"
    actions.style.display = "flex"
    actions.style.gap = "8px"

    const backBtn = document.createElement("button")
    backBtn.textContent = "Back to history"
    backBtn.className = "gowiki-content-btn"
    backBtn.addEventListener("click", () => void showHistory())
    actions.appendChild(backBtn)

    if (historyLatestVersion == null || version < historyLatestVersion) {
      const restoreBtn = document.createElement("button")
      restoreBtn.textContent = "Restore this version"
      restoreBtn.className = "gowiki-content-btn"
      restoreBtn.addEventListener("click", () => void restoreVersion(version))
      actions.appendChild(restoreBtn)
    }

    container.append(header, content, actions)
    contentRoot.appendChild(container)
  } catch {
    setStatus("Failed to load version")
  }
}

async function showDiff(fromVersion, toVersion) {
  try {
    const resp = await fetch(`/api/diff/${encodePagePath(pagePath)}?from=${fromVersion}&to=${toVersion}`)
    if (!resp.ok) {
      setStatus("Failed to load diff")
      return
    }
    const data = await resp.json()
    clearContent()
    renderDiffView(data.hunks, fromVersion, toVersion, data.from_media_refs, data.to_media_refs)
  } catch {
    setStatus("Failed to load diff")
  }
}

function renderDiffView(hunks, fromVersion, toVersion, fromMediaRefs, toMediaRefs) {
  const container = document.createElement("div")
  container.className = "gowiki-diff"

  const header = document.createElement("div")
  header.className = "gowiki-diff-header"
  const fromLabel = fromVersion === 0 ? "current" : `v${fromVersion}`
  const toLabel = toVersion === -1 ? "draft" : toVersion === 0 ? "current" : `v${toVersion}`
  header.textContent = `Diff: ${fromLabel} → ${toLabel} — ${pageDisplayPath}`

  const diffContent = document.createElement("div")
  diffContent.className = "gowiki-diff-content"

  for (const hunk of hunks) {
    const line = document.createElement("div")
    line.className = `gowiki-diff-line gowiki-diff-${hunk.op}`
    const prefix = hunk.op === "insert" ? "+" : hunk.op === "delete" ? "-" : " "
    line.textContent = prefix + (hunk.content.endsWith("\n") ? hunk.content.slice(0, -1) : hunk.content)
    diffContent.appendChild(line)
  }

  container.append(header, diffContent)

  // Show media version changes between the two versions.
  const mediaChanges = buildMediaChanges(fromMediaRefs, toMediaRefs)
  if (mediaChanges) container.appendChild(mediaChanges)

  const actions = document.createElement("div")
  actions.style.marginTop = "16px"
  const backBtn = document.createElement("button")
  backBtn.textContent = "Back to history"
  backBtn.className = "gowiki-content-btn"
  backBtn.addEventListener("click", () => void showHistory())
  actions.appendChild(backBtn)

  container.appendChild(actions)
  contentRoot.appendChild(container)
}

function buildMediaChanges(fromRefs, toRefs) {
  if (!fromRefs && !toRefs) return null
  const from = fromRefs || {}
  const to = toRefs || {}

  // Collect all media paths that differ between versions.
  const allPaths = new Set([...Object.keys(from), ...Object.keys(to)])
  const changed = []
  for (const p of allPaths) {
    const fv = from[p] ?? null
    const tv = to[p] ?? null
    if (fv !== tv) changed.push({ path: p, fromVersion: fv, toVersion: tv })
  }
  if (changed.length === 0) return null

  const section = document.createElement("div")
  section.className = "gowiki-diff-media"

  const heading = document.createElement("div")
  heading.className = "gowiki-diff-media-heading"
  heading.textContent = "Media changes"
  section.appendChild(heading)

  for (const ch of changed) {
    const row = document.createElement("div")
    row.className = "gowiki-diff-media-row"

    const label = document.createElement("div")
    label.className = "gowiki-diff-media-label"
    label.textContent = ch.path
    row.appendChild(label)

    const compare = document.createElement("div")
    compare.className = "gowiki-diff-media-compare"

    if (ch.fromVersion != null) {
      const cell = document.createElement("div")
      cell.className = "gowiki-diff-media-cell gowiki-diff-media-old"
      const caption = document.createElement("div")
      caption.className = "gowiki-diff-media-caption"
      caption.textContent = `v${ch.fromVersion}`
      const img = document.createElement("img")
      img.src = `/media/${ch.path}?v=${ch.fromVersion}`
      img.alt = `${ch.path} v${ch.fromVersion}`
      cell.append(caption, img)
      compare.appendChild(cell)
    } else {
      const cell = document.createElement("div")
      cell.className = "gowiki-diff-media-cell gowiki-diff-media-old"
      cell.textContent = "(not present)"
      compare.appendChild(cell)
    }

    const arrow = document.createElement("div")
    arrow.className = "gowiki-diff-media-arrow"
    arrow.textContent = "→"
    compare.appendChild(arrow)

    if (ch.toVersion != null) {
      const cell = document.createElement("div")
      cell.className = "gowiki-diff-media-cell gowiki-diff-media-new"
      const caption = document.createElement("div")
      caption.className = "gowiki-diff-media-caption"
      caption.textContent = `v${ch.toVersion}`
      const img = document.createElement("img")
      img.src = `/media/${ch.path}?v=${ch.toVersion}`
      img.alt = `${ch.path} v${ch.toVersion}`
      cell.append(caption, img)
      compare.appendChild(cell)
    } else {
      const cell = document.createElement("div")
      cell.className = "gowiki-diff-media-cell gowiki-diff-media-new"
      cell.textContent = "(removed)"
      compare.appendChild(cell)
    }

    row.appendChild(compare)
    section.appendChild(row)
  }

  return section
}

async function restoreVersion(version) {
  try {
    stopAutoSave()
    const resp = await fetch(`/api/versions/${encodePagePath(pagePath)}?v=${version}`)
    if (!resp.ok) {
      setStatus("Failed to load version for restore")
      return
    }
    const data = await resp.json()
    const markdown = data.markdown

    // Acquire edit token (force to supersede any existing session).
    const editResp = await authFetch(`/api/edit/${encodePagePath(pagePath)}?force=true`, {
      method: "POST",
    })
    if (!editResp.ok) {
      setStatus("Failed to acquire edit lock for restore")
      return
    }
    const token = (await editResp.json()).edit_token

    // Save restored content as draft.
    const draftResp = await authFetch(`/api/draft/${encodePagePath(pagePath)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ markdown, edit_token: token }),
    })
    if (!draftResp.ok) {
      setStatus("Failed to save restored version as draft")
      return
    }

    // Publish immediately.
    const pubResp = await authFetch(`/api/publish/${encodePagePath(pagePath)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ edit_token: token }),
    })
    if (!pubResp.ok) {
      const body = await pubResp.json().catch(() => ({}))
      setStatus(body.error || "Failed to publish restored version")
      return
    }

    // Clean up edit state and show restored page.
    editToken = null
    pageLockInfo = null
    stashedEditorState = null
    inHistoryView = false
    await reloadPageContent()
    setStatus(`Restored version ${version}`)
  } catch {
    setStatus("Failed to restore version")
  }
}

function cancelEdit() {
  if (mode !== "edit") return

  if (draftSavedThisSession) {
    // Draft was explicitly saved — preserve it, show saved draft content.
    stashEditorState()
    currentMarkdown = lastSavedDraftMarkdown || editBaselineMarkdown
    try {
      currentDoc = markdownToPM(currentMarkdown, registry)
    } catch (err) {
      console.error("Cancel failed while rebuilding document", err)
      setStatus("Cancel failed")
      return
    }
    if (currentUser) {
      pageLockInfo = { locked_by: currentUser.username, is_draft: true }
    }
    setStatus("Draft preserved, exiting edit mode")
  } else {
    // No save happened — draft is just a copy of published content, discard it.
    if (editToken) {
      authFetch(`/api/draft/${encodePagePath(pagePath)}?edit_token=${encodeURIComponent(editToken)}`, { method: "DELETE" }).catch(() => {})
    }
    editToken = null
    stashedEditorState = null
    pageLockInfo = null
    currentMarkdown = editBaselineMarkdown
    try {
      currentDoc = markdownToPM(currentMarkdown, registry)
    } catch (err) {
      console.error("Cancel failed while rebuilding document", err)
      setStatus("Cancel failed")
      return
    }
    setStatus("Edit cancelled")
  }
  setMode("view")
}

// ── Banner: logo resolution ──────────────────────────

async function resolveSiteInfo() {
  try {
    const resp = await fetch("/api/site/info")
    if (resp.ok) {
      const data = await resp.json()
      if (data.title) {
        siteTitle = data.title
        document.getElementById("banner-title").textContent = siteTitle
        updatePageTitle()
      }
      if (data.version) {
        siteVersion = data.version
      }
      if (typeof data.toc_max_level === "number") {
        tocMaxLevel = data.toc_max_level
      }
      if (data.code_theme) {
        loadHighlightTheme(data.code_theme)
      }
    }
  } catch { /* keep default */ }
}

function updatePageTitle() {
  document.title = siteTitle
  if (!contentRoot) return
  const h = contentRoot.querySelector("h1, h2, h3, h4, h5, h6")
  if (h && h.textContent.trim()) {
    document.title = `[${siteTitle}] ${h.textContent.trim()}`
  }
}

async function resolveLogo() {
  try {
    const resp = await fetch("/api/site/logo")
    if (resp.ok) {
      const data = await resp.json()
      if (data.path) {
        document.getElementById("banner-logo-img").src = "/media/" + data.path
      }
    }
  } catch { /* keep default */ }
}

// ── Banner: search ──────────────────────────────────

function escapeHtml(str) {
  return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;")
}

function initSearch() {
  const input = document.getElementById("search-input")
  const resultsEl = document.getElementById("search-results")
  let debounceTimer = null
  let activeIndex = -1
  let currentResults = []

  input.addEventListener("input", () => {
    clearTimeout(debounceTimer)
    const query = input.value.trim()
    if (!query) { hideResults(); return }
    debounceTimer = setTimeout(() => runSearch(query), 200)
  })

  input.addEventListener("keydown", (e) => {
    if (resultsEl.classList.contains("search-results-hidden")) return

    if (e.key === "ArrowDown") {
      e.preventDefault()
      activeIndex = Math.min(activeIndex + 1, currentResults.length - 1)
      updateActiveResult()
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      activeIndex = Math.max(activeIndex - 1, 0)
      updateActiveResult()
    } else if (e.key === "Enter") {
      e.preventDefault()
      if (activeIndex >= 0 && activeIndex < currentResults.length) {
        navigateToSearchResult(currentResults[activeIndex].path)
      } else {
        const query = input.value.trim()
        if (query) {
          hideResults()
          window.location.href = "/?q=" + encodeURIComponent(query)
        }
      }
    } else if (e.key === "Escape") {
      hideResults()
      input.blur()
    }
  })

  document.addEventListener("click", (e) => {
    if (!e.target.closest("#banner-search")) hideResults()
  })

  function updateActiveResult() {
    const items = resultsEl.querySelectorAll(".search-result-item")
    items.forEach((el, i) => {
      el.classList.toggle("search-result-active", i === activeIndex)
    })
    if (items[activeIndex]) {
      items[activeIndex].scrollIntoView({ block: "nearest" })
    }
  }

  async function runSearch(query) {
    try {
      const resp = await fetch(`/api/search?q=${encodeURIComponent(query)}&limit=8`)
      if (!resp.ok) return
      const data = await resp.json()
      currentResults = data.results || []
      activeIndex = -1
      renderSearchResults(currentResults)
    } catch { /* ignore */ }
  }

  function renderSearchResults(results) {
    resultsEl.innerHTML = ""
    if (results.length === 0) {
      resultsEl.innerHTML = '<div class="search-no-results">No results</div>'
      resultsEl.classList.remove("search-results-hidden")
      return
    }
    for (const r of results) {
      const item = document.createElement("a")
      const displayPath = r.path.startsWith("/") ? r.path : "/" + r.path
      item.href = displayPath
      item.className = "search-result-item gowiki-link-exists"
      item.innerHTML =
        `<div class="search-result-title">${escapeHtml(r.title || displayPath)}</div>` +
        `<div class="search-result-path">${escapeHtml(displayPath)}</div>` +
        (r.snippet ? `<div class="search-result-snippet">${r.snippet}</div>` : "")
      item.addEventListener("click", (e) => {
        e.preventDefault()
        navigateToSearchResult(r.path)
      })
      resultsEl.appendChild(item)
    }
    resultsEl.classList.remove("search-results-hidden")
  }

  function hideResults() {
    resultsEl.classList.add("search-results-hidden")
    activeIndex = -1
    currentResults = []
  }

  function navigateToSearchResult(path) {
    const query = input.value.trim()
    const qs = query ? "?highlight=" + encodeURIComponent(query) : ""
    const url = path.startsWith("/") ? path : "/" + path
    window.location.href = url + qs
  }
}

// ── Bootstrap ───────────────────────────────────────

async function renderSearchResultsPage(query) {
  clearContent()

  const heading = document.createElement("h1")
  heading.textContent = `Search results for "${query}"`
  heading.className = "search-page-heading"
  contentRoot.appendChild(heading)

  const container = document.createElement("div")
  container.className = "search-page-results"
  contentRoot.appendChild(container)

  try {
    const resp = await fetch(`/api/search?q=${encodeURIComponent(query)}&limit=50`)
    if (!resp.ok) {
      container.textContent = "Search failed."
      return
    }
    const data = await resp.json()
    const results = data.results || []

    if (results.length === 0) {
      container.innerHTML = '<div class="search-page-empty">No results found.</div>'
      return
    }

    for (const r of results) {
      const item = document.createElement("a")
      const displayPath = r.path.startsWith("/") ? r.path : "/" + r.path
      const qs = query ? "?highlight=" + encodeURIComponent(query) : ""
      item.href = displayPath + qs
      item.className = "search-page-item gowiki-link-exists"
      item.innerHTML =
        `<div class="search-page-item-title">${escapeHtml(r.title || displayPath)}</div>` +
        `<div class="search-page-item-path">${escapeHtml(displayPath)}</div>` +
        (r.snippet ? `<div class="search-page-item-snippet">${r.snippet}</div>` : "")
      container.appendChild(item)
    }
  } catch {
    container.textContent = "Search failed."
  }
}

// ── Auth ─────────────────────────────────────────────

async function checkAuth() {
  try {
    const resp = await fetch("/api/auth/me")
    if (resp.ok) {
      currentUser = await resp.json()
    } else {
      currentUser = null
    }
  } catch {
    currentUser = null
  }
  window.__gowikiCurrentUser = currentUser
  renderBannerUser()
}

function renderBannerUser() {
  const el = document.getElementById("banner-user")
  if (!el) return
  el.innerHTML = ""
  if (currentUser) {
    const span = document.createElement("span")
    span.textContent = currentUser.username
    el.appendChild(span)
    if (currentUser.is_admin) {
      const adminLink = document.createElement("a")
      adminLink.textContent = "Admin"
      adminLink.href = "/_admin"
      adminLink.addEventListener("click", (e) => {
        e.preventDefault()
        window.location.href = "/_admin"
      })
      el.appendChild(adminLink)
    }
    const tokensLink = document.createElement("a")
    tokensLink.textContent = "API Tokens"
    tokensLink.addEventListener("click", (e) => {
      e.preventDefault()
      showMyTokensModal()
    })
    el.appendChild(tokensLink)
    const logout = document.createElement("a")
    logout.textContent = "Logout"
    logout.addEventListener("click", async (e) => {
      e.preventDefault()
      await fetch("/api/auth/logout", { method: "POST" })
      currentUser = null
      window.__gowikiCurrentUser = null
      pageLockInfo = null
      editToken = null
      stashedEditorState = null
      renderBannerUser()
      await reloadPageContent()
    })
    el.appendChild(logout)
  } else {
    const login = document.createElement("a")
    login.textContent = "Login"
    login.addEventListener("click", (e) => {
      e.preventDefault()
      showLoginDialog()
    })
    el.appendChild(login)
  }
}

async function showMyTokensModal() {
  const overlay = document.createElement("div")
  overlay.className = "gowiki-login-overlay"
  const dialog = document.createElement("div")
  dialog.className = "gowiki-login-dialog"
  dialog.style.maxWidth = "500px"

  const title = document.createElement("h3")
  title.textContent = "API Tokens"
  dialog.appendChild(title)

  const content = document.createElement("div")
  dialog.appendChild(content)

  async function loadTokens() {
    content.innerHTML = "Loading..."
    try {
      const resp = await authFetch("/api/tokens")
      if (!resp.ok) { content.textContent = "Failed to load tokens."; return }
      const data = await resp.json()
      content.innerHTML = ""

      const tokens = data.tokens || []
      if (tokens.length === 0) {
        const p = document.createElement("p")
        p.textContent = "No API tokens yet."
        p.style.color = "#666"
        content.appendChild(p)
      } else {
        for (const token of tokens) {
          const row = document.createElement("div")
          row.style.cssText = "display:flex;align-items:center;gap:8px;padding:4px 0;border-bottom:1px solid #eee"
          const info = document.createElement("span")
          info.style.flex = "1"
          info.innerHTML = "<b>" + (token.name || "unnamed") + "</b><br><small style='color:#888'>" +
            (token.last_used_at ? "Last used: " + new Date(token.last_used_at).toLocaleString() : "Never used") + "</small>"
          row.appendChild(info)
          const del = document.createElement("button")
          del.className = "gowiki-admin-btn-small gowiki-admin-btn-danger"
          del.textContent = "Revoke"
          del.addEventListener("click", async () => {
            if (!confirm("Revoke token \"" + token.name + "\"?")) return
            const r = await authFetch("/api/tokens/" + token.id, { method: "DELETE" })
            if (r.ok) loadTokens()
            else alert("Failed to revoke token.")
          })
          row.appendChild(del)
          content.appendChild(row)
        }
      }

      // Create button
      const createBtn = document.createElement("button")
      createBtn.className = "gowiki-content-btn"
      createBtn.textContent = "Create New Token"
      createBtn.style.marginTop = "12px"
      createBtn.addEventListener("click", async () => {
        const name = prompt("Token name (e.g. 'Claude assistant'):")
        if (!name) return
        const r = await authFetch("/api/tokens", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name }),
        })
        if (!r.ok) {
          const err = await r.json().catch(() => ({}))
          alert(err.error || "Failed to create token.")
          return
        }
        const result = await r.json()
        // Show the plaintext token
        const tokenDisplay = document.createElement("div")
        tokenDisplay.style.cssText = "margin-top:12px;padding:12px;background:#e8f5e9;border:1px solid #c8e6c9;border-radius:6px"
        tokenDisplay.innerHTML = "<b>Token created! Copy it now — it will not be shown again:</b>"
        const code = document.createElement("code")
        code.style.cssText = "display:block;margin-top:8px;padding:8px;background:#fff;border:1px solid #ddd;border-radius:4px;word-break:break-all;font-size:13px;user-select:all"
        code.textContent = result.token
        tokenDisplay.appendChild(code)
        content.appendChild(tokenDisplay)
        // Reload the list above (but keep the token display visible)
        setTimeout(() => loadTokens(), 0)
        // Re-append the display since loadTokens clears content
        setTimeout(() => content.appendChild(tokenDisplay), 100)
      })
      content.appendChild(createBtn)
    } catch {
      content.textContent = "Failed to load tokens."
    }
  }

  await loadTokens()

  const closeBtn = document.createElement("button")
  closeBtn.className = "gowiki-content-btn"
  closeBtn.textContent = "Close"
  closeBtn.style.marginTop = "12px"
  closeBtn.addEventListener("click", () => overlay.remove())
  dialog.appendChild(closeBtn)

  overlay.appendChild(dialog)
  overlay.addEventListener("click", (e) => { if (e.target === overlay) overlay.remove() })
  document.body.appendChild(overlay)
}

function showLoginDialog(onSuccess) {
  return new Promise(resolve => {
    const overlay = document.createElement("div")
    overlay.className = "gowiki-login-overlay"

    const dialog = document.createElement("div")
    dialog.className = "gowiki-login-dialog"

    const title = document.createElement("h3")
    title.textContent = "Login"

    const errorEl = document.createElement("div")
    errorEl.className = "gowiki-login-error"

    const userLabel = document.createElement("label")
    userLabel.textContent = "Username"
    const userInput = document.createElement("input")
    userInput.type = "text"
    userInput.autocomplete = "username"

    const passLabel = document.createElement("label")
    passLabel.textContent = "Password"
    const passInput = document.createElement("input")
    passInput.type = "password"
    passInput.autocomplete = "current-password"

    const actions = document.createElement("div")
    actions.className = "gowiki-login-actions"
    const cancelBtn = document.createElement("button")
    cancelBtn.textContent = "Cancel"
    const loginBtn = document.createElement("button")
    loginBtn.textContent = "Login"
    loginBtn.className = "primary"

    function close(success) {
      overlay.remove()
      resolve(success)
    }

    async function submit() {
      errorEl.style.display = "none"
      const username = userInput.value.trim()
      const password = passInput.value
      if (!username || !password) {
        errorEl.textContent = "Username and password required"
        errorEl.style.display = "block"
        return
      }
      try {
        const resp = await fetch("/api/auth/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ username, password }),
        })
        if (resp.ok) {
          await checkAuth()
          if (onSuccess) onSuccess()
          close(true)
          await reloadPageContent()
        } else {
          errorEl.textContent = "Invalid credentials"
          errorEl.style.display = "block"
          passInput.value = ""
          passInput.focus()
        }
      } catch {
        errorEl.textContent = "Login failed"
        errorEl.style.display = "block"
      }
    }

    cancelBtn.addEventListener("click", () => close(false))
    loginBtn.addEventListener("click", submit)
    passInput.addEventListener("keydown", e => { if (e.key === "Enter") submit() })
    userInput.addEventListener("keydown", e => { if (e.key === "Enter") passInput.focus() })
    overlay.addEventListener("click", e => { if (e.target === overlay) close(false) })

    actions.append(cancelBtn, loginBtn)
    dialog.append(title, errorEl, userLabel, userInput, passLabel, passInput, actions)
    overlay.appendChild(dialog)
    document.body.appendChild(overlay)

    // Fetch OAuth providers and add buttons if available.
    fetch("/api/auth/providers").then(r => r.json()).then(data => {
      if (data.providers && data.providers.length > 0) {
        const oauthSection = document.createElement("div")
        oauthSection.className = "gowiki-login-oauth"
        for (const provider of data.providers) {
          const btn = document.createElement("button")
          btn.className = "gowiki-login-oauth-btn"
          btn.textContent = "Sign in with " + provider.label
          btn.addEventListener("click", () => {
            const returnTo = encodeURIComponent(window.location.pathname)
            window.location.href = "/api/auth/oauth/login?return_to=" + returnTo
          })
          oauthSection.appendChild(btn)
        }
        const divider = document.createElement("div")
        divider.className = "gowiki-login-divider"
        divider.textContent = "or sign in with username"
        // Insert OAuth buttons before the local login form.
        dialog.insertBefore(oauthSection, errorEl)
        dialog.insertBefore(divider, errorEl)
      }
    }).catch(() => { /* OAuth not available, local-only login */ })

    userInput.focus()
  })
}

// Wraps a write API call: if it returns 401, shows login dialog and retries.
async function authFetch(url, options) {
  let resp = await fetch(url, options)
  if (resp.status === 401) {
    const loggedIn = await showLoginDialog()
    if (loggedIn) {
      resp = await fetch(url, options)
    }
  }
  return resp
}
// Expose for media_manager.js
window.__gowikiAuthFetch = authFetch
window.__gowikiCurrentUser = null

// Expose global variable context for the template_var resolver.
window.__gowikiGlobalVarContext = () => ({
  pagePath: "/" + pagePath,
  pageNamespace: pageNamespace ? "/" + pageNamespace : "/",
  pageName: pagePath.includes("/") ? pagePath.split("/").pop().replace(/_/g, " ") : pagePath.replace(/_/g, " "),
  pageMeta: currentPageMeta,
  siteTitle,
  siteVersion,
})

async function reloadPageContent() {
  // Admin page is a virtual route — re-render it instead of fetching a wiki page.
  if (pagePath === "_admin") {
    if (currentUser && currentUser.is_admin) {
      renderAdminPage()
    } else {
      contentRoot.innerHTML = ""
      actionsRoot.innerHTML = ""
      const msg = document.createElement("div")
      msg.style.padding = "40px 20px"
      msg.style.textAlign = "center"
      msg.style.color = "#666"
      msg.style.fontSize = "16px"
      msg.textContent = currentUser
        ? "Access denied. Admin privileges required."
        : "Please log in with an admin account to access this page."
      contentRoot.appendChild(msg)
    }
    return
  }

  const page = await fetchPage(pagePath)
  if (page) {
    currentMarkdown = page.markdown
    isNewPage = false
    hasTemplate = false
    currentPageVersion = page.meta?.version || 0
    currentPageMeta = page.meta || null
    pageLockInfo = null
    if (page.locked_by) {
      pageLockInfo = { locked_by: page.locked_by, is_draft: !!page.is_draft }
    }
    // Populate media version cache from page response.
    if (page.media_versions) {
      for (const [absPath, ver] of Object.entries(page.media_versions)) {
        window.__gowikiUpdateMediaVersionCache(absPath, ver)
      }
    }
  } else {
    // Page doesn't exist — try to fetch a template.
    let templateMarkdown = null
    try {
      const tmplUrl = `/api/template/${encodePagePath(pagePath)}`
      console.log("[gowiki] reloadPageContent fetching template:", tmplUrl)
      const tmplResp = await fetch(tmplUrl)
      console.log("[gowiki] reloadPageContent template response:", tmplResp.status)
      if (tmplResp.ok) {
        const tmplData = await tmplResp.json()
        console.log("[gowiki] reloadPageContent template data:", tmplData)
        templateMarkdown = tmplData.markdown
      }
    } catch (err) {
      console.error("[gowiki] reloadPageContent template fetch error:", err)
    }
    currentMarkdown = templateMarkdown || defaultMarkdown
    hasTemplate = !!templateMarkdown
    isNewPage = true
    currentPageVersion = 0
    currentPageMeta = null
    pageLockInfo = null
  }

  isNamespaceIndex = !!(page && page.is_namespace_index)
  // Update pageNamespace based on authoritative backend flag.
  if (isNamespaceIndex) {
    pageNamespace = pagePath === "index" ? "" : pagePath
  }

  currentDoc = markdownToPM(currentMarkdown, registry)
  setMode("view")

  // Re-mount sidebar and footer so dynamic content (e.g. recent changes) refreshes.
  refreshZones()
}

// ── Draft / Edit mode API ────────────────────────────

function stashEditorState() {
  if (editorView) {
    stashedEditorState = editorView.state
  }
}

async function enterEditMode(force) {
  // If we still have a valid edit token (saved-to-draft without exiting the session),
  // resume directly with the stashed editor state — no API call needed.
  if (editToken && stashedEditorState) {
    draftSavedThisSession = true
    setMode("edit")
    return true
  }

  const forceParam = force ? "?force=true" : ""
  const resp = await authFetch(`/api/edit/${encodePagePath(pagePath)}${forceParam}`, {
    method: "POST",
  })
  if (resp.status === 423) {
    const body = await resp.json()
    setStatus(`Page locked by ${body.locked_by}`)
    return false
  }
  if (resp.status === 409) {
    const body = await resp.json().catch(() => ({}))
    if (body.error && body.error.includes("conflict")) {
      setStatus(body.error)
      return false
    }
    const ok = confirm("You already have this page open in another session. Force edit?")
    if (ok) return enterEditMode(true)
    return false
  }
  if (resp.status === 401) {
    return false
  }
  if (!resp.ok) {
    setStatus("Failed to enter edit mode")
    return false
  }
  const data = await resp.json()
  editToken = data.edit_token
  stashedEditorState = null // new session — discard any old stash
  draftSavedThisSession = false
  lastSavedDraftMarkdown = null
  currentMarkdown = data.markdown
  currentDoc = markdownToPM(currentMarkdown, registry)
  setMode("edit")
  return true
}

function getCurrentMarkdown() {
  if (mode !== "edit") return currentMarkdown
  if (editMode === "visual" && editorView) {
    return pmToMarkdown(editorView.state.doc, registry)
  }
  if (editMode === "raw" && rawEditor) {
    return rawEditor.value
  }
  return currentMarkdown
}

// Shows a modal when the edit session has been superseded by another tab.
// Returns "force" if the user wants to retake editing, "discard" otherwise.
function promptSupersededDialog() {
  return new Promise(resolve => {
    const overlay = document.createElement("div")
    overlay.className = "gowiki-link-modal-overlay"

    const dialog = document.createElement("div")
    dialog.className = "gowiki-link-modal"

    const title = document.createElement("div")
    title.className = "gowiki-link-modal-title"
    title.textContent = "Edit session superseded"

    const msg = document.createElement("div")
    msg.style.cssText = "margin: 8px 0 16px; color: #b00020;"
    msg.textContent = "Another tab took over editing this page. Your changes here may be lost."

    const buttons = document.createElement("div")
    buttons.className = "gowiki-link-modal-actions"

    const forceBtn = document.createElement("button")
    forceBtn.type = "button"
    forceBtn.className = "gowiki-link-modal-btn"
    forceBtn.textContent = "Force save & continue"

    const discardBtn = document.createElement("button")
    discardBtn.type = "button"
    discardBtn.className = "gowiki-link-modal-btn"
    discardBtn.textContent = "Discard my changes"

    function close(value) {
      overlay.remove()
      resolve(value)
    }

    forceBtn.addEventListener("click", () => close("force"))
    discardBtn.addEventListener("click", () => close("discard"))
    overlay.addEventListener("click", event => {
      if (event.target === overlay) close("discard")
    })

    buttons.appendChild(forceBtn)
    buttons.appendChild(discardBtn)
    dialog.appendChild(title)
    dialog.appendChild(msg)
    dialog.appendChild(buttons)
    overlay.appendChild(dialog)
    document.body.appendChild(overlay)
  })
}

// Handles a 409 superseded response during editing.
// If the user chooses to force, retakes the edit token and saves the current
// editor content (without rebuilding the editor or losing unsaved work).
// Returns true if the user forced and the save succeeded, false if discarded.
async function handleSuperseded() {
  stopAutoSave()
  const choice = await promptSupersededDialog()
  if (choice !== "force") {
    editToken = null
    stashedEditorState = null
    await reloadPageContent()
    setMode("view")
    return false
  }
  // Force-retake the edit token via API — do NOT call enterEditMode()
  // which would rebuild the editor and lose the current unsaved content.
  const resp = await authFetch(`/api/edit/${encodePagePath(pagePath)}?force=true`, {
    method: "POST",
  })
  if (!resp.ok) {
    setStatus("Failed to retake edit session")
    editToken = null
    setMode("view")
    return false
  }
  const data = await resp.json()
  editToken = data.edit_token

  // Save the current editor content with the new token.
  const markdown = getCurrentMarkdown()
  const saveResp = await authFetch(`/api/draft/${encodePagePath(pagePath)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ markdown, edit_token: editToken }),
  })
  if (!saveResp.ok) {
    setStatus("Failed to save after retaking edit session")
    editToken = null
    setMode("view")
    return false
  }
  draftSavedThisSession = true
  lastSavedDraftMarkdown = markdown
  startAutoSave()
  setStatus(`Edit session retaken — draft saved ${new Date().toLocaleTimeString()}`)
  return true
}

async function autoSaveDraft() {
  if (mode !== "edit" || !editToken) return
  const markdown = getCurrentMarkdown()
  try {
    const resp = await fetch(`/api/draft/${encodePagePath(pagePath)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ markdown, edit_token: editToken }),
    })
    if (resp.status === 409) {
      await handleSuperseded()
      return
    }
    if (resp.ok) {
      draftSavedThisSession = true
      lastSavedDraftMarkdown = markdown
      setStatus(`Draft auto-saved ${new Date().toLocaleTimeString()}`)
    }
  } catch {
    // silent failure for auto-save
  }
}

function startAutoSave() {
  stopAutoSave()
  autoSaveTimer = setInterval(autoSaveDraft, 2 * 60 * 1000)
}

function stopAutoSave() {
  if (autoSaveTimer) {
    clearInterval(autoSaveTimer)
    autoSaveTimer = null
  }
}

async function saveDraftExplicit() {
  if (mode !== "edit" || !editToken) return
  const markdown = getCurrentMarkdown()
  const dbValidation = await validateDatabaseRows(markdown)
  if (!dbValidation.valid) {
    setStatus(dbValidation.errors.join("; "))
    return
  }
  const resp = await authFetch(`/api/draft/${encodePagePath(pagePath)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ markdown, edit_token: editToken }),
  })
  if (resp.status === 409) {
    await handleSuperseded()
    return
  }
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({}))
    setStatus(body.error || "Save failed")
    return
  }
  draftSavedThisSession = true
  lastSavedDraftMarkdown = markdown
  setStatus(`Draft saved ${new Date().toLocaleTimeString()}`)
}

async function publishDraft() {
  if (mode !== "edit" || !editToken) return
  // Save draft content first
  const markdown = getCurrentMarkdown()
  let normalized
  try {
    normalized = normalizeMarkdownForStorage(markdown)
  } catch (err) {
    setStatus("Invalid Markdown — cannot publish: " + (err.message || err))
    return
  }
  if (normalized.roundTripError) {
    setStatus("Document failed round-trip validation — cannot publish.")
    return
  }
  const dbValidation = await validateDatabaseRows(normalized.markdown)
  if (!dbValidation.valid) {
    setStatus(dbValidation.errors.join("; "))
    return
  }
  // Save draft with latest content
  const draftResp = await authFetch(`/api/draft/${encodePagePath(pagePath)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ markdown: normalized.markdown, edit_token: editToken }),
  })
  if (!draftResp.ok) {
    if (draftResp.status === 409) {
      await handleSuperseded()
    } else {
      setStatus("Failed to save draft before publish")
    }
    return
  }
  // Publish
  const resp = await authFetch(`/api/publish/${encodePagePath(pagePath)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ edit_token: editToken }),
  })
  if (resp.status === 409) {
    const body = await resp.json().catch(() => ({}))
    if (body.error === "database_row_conflict") {
      // A forced inline edit changed the published page while the draft was open.
      const msg = `The database row (table: ${body.table}) was modified by an inline edit while you were editing.\n\nForce publish with your values?`
      if (confirm(msg)) {
        // Retry publish with force_publish flag.
        const retryResp = await authFetch(`/api/publish/${encodePagePath(pagePath)}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ edit_token: editToken, force_publish: true }),
        })
        if (retryResp.ok) {
          const result = await retryResp.json()
          editToken = null
          pageLockInfo = null
          stashedEditorState = null
          applyNormalizedEditState(normalized)
          editBaselineMarkdown = normalized.markdown
          isNewPage = false
          hasTemplate = false
          if (result.page?.meta) {
            currentPageMeta = result.page.meta
            currentPageVersion = result.page.meta.version || 0
          }
          setStatus(`Published (forced) ${new Date().toLocaleTimeString()}`)
          if (result.orphaned_media && result.orphaned_media.length > 0) {
            promptOrphanDeletion(result.orphaned_media)
          }
          setMode("view")
        } else {
          setStatus("Force publish failed")
        }
      }
      return
    }
    // Retake token and retry publish if user chooses to force.
    const retook = await handleSuperseded()
    if (retook) {
      // Token was retaken and content saved as draft — now retry publish.
      await publishDraft()
    }
    return
  }
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({}))
    setStatus(body.error || "Publish failed")
    return
  }
  const result = await resp.json()
  editToken = null
  pageLockInfo = null
  stashedEditorState = null
  applyNormalizedEditState(normalized)
  editBaselineMarkdown = normalized.markdown
  isNewPage = false
  hasTemplate = false
  // Update metadata from the publish response so global variables resolve immediately.
  if (result.page?.meta) {
    currentPageMeta = result.page.meta
    currentPageVersion = result.page.meta.version || 0
  }
  setStatus(`Published ${new Date().toLocaleTimeString()}`)

  if (result.orphaned_media && result.orphaned_media.length > 0) {
    promptOrphanDeletion(result.orphaned_media)
  }
  setMode("view")
  refreshZones()
}

function refreshZones() {
  if (sidebarView) { sidebarView.destroy(); sidebarView = null }
  sidebarRoot.innerHTML = ""
  fetchAndMountZone("sidebar", sidebarRoot, "gowiki-sidebar").then(v => {
    sidebarView = v
  })
  if (footerView) { footerView.destroy(); footerView = null }
  footerRoot.innerHTML = ""
  fetchAndMountZone("footer", footerRoot, "gowiki-footer").then(v => {
    footerView = v
  })
}

async function discardDraft() {
  const hasChanges = currentMarkdown !== editBaselineMarkdown
  const msg = hasChanges
    ? "Discard draft and lose all unpublished changes?"
    : "The draft has no changes from the published version. Discard?"
  if (!confirm(msg)) return
  const tokenParam = editToken ? `?edit_token=${encodeURIComponent(editToken)}` : ""
  const resp = await authFetch(`/api/draft/${encodePagePath(pagePath)}${tokenParam}`, {
    method: "DELETE",
  })
  if (resp.status === 409) {
    setStatus("Another editing session is active — cannot discard")
    return
  }
  if (resp.ok) {
    editToken = null
    pageLockInfo = null
    stashedEditorState = null
    // Reload published content.
    const page = await fetchPage(pagePath)
    if (page) {
      currentMarkdown = page.markdown
      currentDoc = markdownToPM(currentMarkdown, registry)
      if (page.media_versions) {
        for (const [absPath, ver] of Object.entries(page.media_versions)) {
          window.__gowikiUpdateMediaVersionCache(absPath, ver)
        }
      }
    }
    setStatus("Draft discarded")
    setMode("view")
  }
}

async function publishDraftFromHistory() {
  // Enter edit mode to get a token, then immediately publish.
  const forceParam = "?force=true"
  const editResp = await authFetch(`/api/edit/${encodePagePath(pagePath)}${forceParam}`, {
    method: "POST",
  })
  if (!editResp.ok) {
    setStatus("Failed to enter edit mode for publish")
    return
  }
  const editData = await editResp.json()
  const token = editData.edit_token

  const resp = await authFetch(`/api/publish/${encodePagePath(pagePath)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ edit_token: token }),
  })
  if (!resp.ok) {
    const body = await resp.json().catch(() => ({}))
    setStatus(body.error || "Publish failed")
    return
  }

  editToken = null
  pageLockInfo = null
  stashedEditorState = null
  await reloadPageContent()
  setStatus("Draft published")
  showHistory()
}

async function discardDraftFromHistory(hasChanges) {
  const msg = hasChanges === false
    ? "The draft has no changes from the published version. Discard?"
    : "Discard draft and lose all unpublished changes?"
  if (!confirm(msg)) return

  const tokenParam = editToken ? `?edit_token=${encodeURIComponent(editToken)}` : ""
  const resp = await authFetch(`/api/draft/${encodePagePath(pagePath)}${tokenParam}`, {
    method: "DELETE",
  })
  if (resp.status === 409) {
    setStatus("Another editing session is active — cannot discard")
    return
  }
  if (resp.ok) {
    editToken = null
    pageLockInfo = null
    stashedEditorState = null
    await reloadPageContent()
    setStatus("Draft discarded")
    showHistory()
  }
}

async function adminForceDiscardDraft(draftOwner) {
  const msg = `Discard ${draftOwner}'s draft on ${pageDisplayPath}? This is irreversible and will be logged.`
  if (!confirm(msg)) return

  const resp = await authFetch(`/api/admin/drafts/${encodePagePath(pagePath)}`, {
    method: "DELETE",
  })
  if (resp.ok) {
    pageLockInfo = null
    await reloadPageContent()
    setStatus(`Draft by ${draftOwner} discarded (admin override)`)
    showHistory()
  } else {
    const body = await resp.json().catch(() => ({}))
    setStatus(body.error || "Force discard failed")
  }
}

async function saveDraftAndExit() {
  await saveDraftExplicit()
  // Stash editor state so undo survives resume.
  stashEditorState()
  // Keep editToken — we'll reuse it on resume.
  // Update currentMarkdown/currentDoc from editor so view mode shows latest content.
  if (lastSavedDraftMarkdown) {
    currentMarkdown = lastSavedDraftMarkdown
    try {
      currentDoc = markdownToPM(currentMarkdown, registry)
    } catch (err) {
      console.error("saveDraftAndExit: markdown parse failed", err)
      setStatus("Invalid Markdown in draft — " + (err.message || err))
      return
    }
  }
  // Update lock info so the draft banner shows immediately.
  if (currentUser) {
    pageLockInfo = { locked_by: currentUser.username, is_draft: true }
  }
  setMode("view")
}

// ── Admin Page ────────────────────────────────────────

async function renderSitemapPage() {
  clearContent()
  const container = document.createElement("div")
  container.className = "gowiki-sitemap"

  const title = document.createElement("h2")
  title.textContent = "Site map"
  container.appendChild(title)

  try {
    const resp = await fetch("/api/sitemap")
    if (!resp.ok) throw new Error("Failed to load sitemap")
    const data = await resp.json()
    const pages = data.pages || []

    function nodeUrl(node) {
      if (node.path === "/" || node.path === "/index" || node.path === "index") return "/"
      const hasChildren = node.children && node.children.length > 0
      if (node.is_namespace_index || hasChildren) return node.path + "/"
      return node.path
    }

    function leafName(node) {
      if (node.path === "/" || node.path === "/index" || node.path === "index") return "/"
      const leaf = node.path.split("/").pop()
      const hasChildren = node.children && node.children.length > 0
      if (node.is_namespace_index || hasChildren) return leaf + "/"
      return leaf
    }

    function buildTree(nodes, parentUl, depth) {
      // First pass: build all items, collect path elements for alignment.
      const pathElements = []

      for (const node of nodes) {
        const li = document.createElement("li")
        const hasChildren = node.children && node.children.length > 0
        const isPhantom = !node.has_page
        const isRoot = node.path === "/"

        if (hasChildren) {
          const toggle = document.createElement("span")
          toggle.className = "gowiki-sitemap-toggle"
          toggle.textContent = isRoot ? "\u25BE" : "\u25B8"
          toggle.addEventListener("click", () => {
            const childUl = li.querySelector(":scope > ul")
            if (childUl) {
              const hidden = childUl.style.display === "none"
              childUl.style.display = hidden ? "" : "none"
              toggle.textContent = hidden ? "\u25BE" : "\u25B8"
            }
          })
          li.appendChild(toggle)
        } else {
          const spacer = document.createElement("span")
          spacer.className = "gowiki-sitemap-spacer"
          li.appendChild(spacer)
        }

        const row = document.createElement("span")
        row.className = "gowiki-sitemap-row"

        const label = leafName(node)
        if (isPhantom) {
          const span = document.createElement("span")
          span.className = "gowiki-sitemap-path gowiki-sitemap-phantom"
          span.textContent = label
          pathElements.push(span)
          row.appendChild(span)
        } else {
          const url = nodeUrl(node)
          const a = document.createElement("a")
          a.href = url
          a.className = "gowiki-sitemap-path gowiki-link-exists"
          a.textContent = label
          a.addEventListener("click", e => {
            e.preventDefault()
            window.location.href = url
          })
          pathElements.push(a)
          row.appendChild(a)
        }

        const displayTitle = node.title || ""
        if (!isPhantom && displayTitle) {
          const titleSpan = document.createElement("span")
          titleSpan.className = "gowiki-sitemap-title"
          titleSpan.textContent = displayTitle
          row.appendChild(titleSpan)
        }

        li.appendChild(row)

        if (hasChildren) {
          const childUl = document.createElement("ul")
          if (!isRoot) childUl.style.display = "none"
          buildTree(node.children, childUl, depth + 1)
          li.appendChild(childUl)
        }
        parentUl.appendChild(li)
      }

      // Second pass: align path column within this level.
      if (pathElements.length > 0) {
        let maxLen = 0
        for (const el of pathElements) maxLen = Math.max(maxLen, el.textContent.length)
        parentUl.style.setProperty("--path-width", (maxLen + 2) + "ch")
      }
    }

    const ul = document.createElement("ul")
    buildTree(pages, ul, 0)
    container.appendChild(ul)
  } catch (err) {
    const msg = document.createElement("div")
    msg.style.color = "#d63031"
    msg.textContent = "Failed to load sitemap."
    container.appendChild(msg)
  }

  contentRoot.appendChild(container)
}

function renderAdminPage() {
  contentRoot.innerHTML = ""
  actionsRoot.innerHTML = ""

  actionsRoot.appendChild(makeActionIconBtn("backlinks", "Back to Wiki", () => { window.location.href = "/" }))

  // Tab bar
  const tabBar = document.createElement("div")
  tabBar.className = "gowiki-admin-tabs"

  const tabContent = document.createElement("div")
  tabContent.className = "gowiki-admin-content"

  const tabs = ["Users", "Groups", "ACL", "Locks", "Tokens", "Configuration", "Database", "Todo"]
  let activeTab = "Users"

  function renderTabBar() {
    tabBar.innerHTML = ""
    for (const name of tabs) {
      const btn = document.createElement("button")
      btn.className = "gowiki-admin-tab" + (name === activeTab ? " gowiki-admin-tab-active" : "")
      btn.textContent = name
      btn.addEventListener("click", () => switchTab(name))
      tabBar.appendChild(btn)
    }
  }

  function switchTab(name) {
    activeTab = name
    renderTabBar()
    loadTab(name)
  }

  function loadTab(name) {
    tabContent.innerHTML = ""
    switch (name) {
      case "Users": renderAdminUsersTab(tabContent); break
      case "Groups": renderAdminGroupsTab(tabContent); break
      case "ACL": renderAdminACLTab(tabContent); break
      case "Locks": renderAdminLocksTab(tabContent); break
      case "Tokens": renderAdminTokensTab(tabContent); break
      case "Configuration": renderAdminConfigTab(tabContent); break
      case "Database": renderAdminDatabaseTab(tabContent); break
      case "Todo": renderAdminTodoTab(tabContent); break
    }
  }

  renderTabBar()
  loadTab("Users")

  contentRoot.appendChild(tabBar)
  contentRoot.appendChild(tabContent)
}

// ── Admin: Modal helper ───────────────────────────────

function showAdminModal(title, buildContent) {
  return new Promise((resolve) => {
    const overlay = document.createElement("div")
    overlay.className = "gowiki-admin-modal-overlay"

    const dialog = document.createElement("div")
    dialog.className = "gowiki-admin-modal"

    const h3 = document.createElement("h3")
    h3.textContent = title
    dialog.appendChild(h3)

    const errorEl = document.createElement("div")
    errorEl.className = "gowiki-admin-modal-error"
    dialog.appendChild(errorEl)

    function showError(msg) {
      errorEl.textContent = msg
      errorEl.style.display = "block"
    }

    function close(result) {
      overlay.remove()
      resolve(result)
    }

    const body = document.createElement("div")
    body.className = "gowiki-admin-modal-body"
    dialog.appendChild(body)

    buildContent(body, close, showError)

    overlay.appendChild(dialog)
    overlay.addEventListener("click", (e) => { if (e.target === overlay) close(null) })
    document.addEventListener("keydown", function handler(e) {
      if (e.key === "Escape") {
        document.removeEventListener("keydown", handler)
        close(null)
      }
    })
    document.body.appendChild(overlay)
  })
}

function adminModalActions(parent, onCancel, onConfirm, confirmLabel) {
  const actions = document.createElement("div")
  actions.className = "gowiki-admin-modal-actions"
  const cancelBtn = document.createElement("button")
  cancelBtn.textContent = "Cancel"
  cancelBtn.addEventListener("click", onCancel)
  const confirmBtn = document.createElement("button")
  confirmBtn.textContent = confirmLabel || "Save"
  confirmBtn.className = "primary"
  confirmBtn.addEventListener("click", onConfirm)
  actions.append(cancelBtn, confirmBtn)
  parent.appendChild(actions)
  return { cancelBtn, confirmBtn }
}

function adminFormField(parent, labelText, type, value) {
  const label = document.createElement("label")
  label.textContent = labelText
  parent.appendChild(label)
  const input = document.createElement("input")
  input.type = type || "text"
  if (value !== undefined) input.value = value
  parent.appendChild(input)
  return input
}

function adminFormSelect(parent, labelText, options, selectedValue) {
  const label = document.createElement("label")
  label.textContent = labelText
  parent.appendChild(label)
  const select = document.createElement("select")
  for (const opt of options) {
    const o = document.createElement("option")
    o.value = opt.value
    o.textContent = opt.label
    if (opt.value === selectedValue) o.selected = true
    select.appendChild(o)
  }
  parent.appendChild(select)
  return select
}

// ── Admin: Users Tab ──────────────────────────────────

async function renderAdminUsersTab(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading users...</div>'

  try {
    const resp = await authFetch("/api/admin/users")
    if (!resp.ok) {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load users.</div>'
      return
    }
    const data = await resp.json()
    const users = data.users || []

    container.innerHTML = ""

    // Create user button
    const toolbar = document.createElement("div")
    toolbar.className = "gowiki-admin-toolbar"
    const createBtn = document.createElement("button")
    createBtn.className = "gowiki-admin-btn gowiki-admin-btn-primary"
    createBtn.textContent = "Create User"
    createBtn.addEventListener("click", async () => {
      const result = await showCreateUserModal()
      if (result) renderAdminUsersTab(container)
    })
    toolbar.appendChild(createBtn)
    container.appendChild(toolbar)

    if (users.length === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-admin-empty"
      empty.textContent = "No users found."
      container.appendChild(empty)
      return
    }

    const table = document.createElement("table")
    table.className = "gowiki-admin-table"

    const thead = document.createElement("thead")
    const headerRow = document.createElement("tr")
    for (const col of ["Username", "Display Name", "Email", "Groups", "Status", "Last Login", "Actions"]) {
      const th = document.createElement("th")
      th.textContent = col
      headerRow.appendChild(th)
    }
    thead.appendChild(headerRow)
    table.appendChild(thead)

    const tbody = document.createElement("tbody")
    for (const user of users) {
      const tr = document.createElement("tr")

      const tdUsername = document.createElement("td")
      tdUsername.textContent = user.username
      tr.appendChild(tdUsername)

      const tdDisplay = document.createElement("td")
      tdDisplay.textContent = user.display_name || ""
      tr.appendChild(tdDisplay)

      const tdEmail = document.createElement("td")
      tdEmail.textContent = user.email || ""
      tr.appendChild(tdEmail)

      const tdGroups = document.createElement("td")
      const localGroups = user.groups || []
      const oauthGroups = user.oauth_groups || []
      const allGroupParts = []
      if (localGroups.length > 0) allGroupParts.push(localGroups.join(", "))
      if (oauthGroups.length > 0) allGroupParts.push(oauthGroups.map(g => g + " (Azure)").join(", "))
      tdGroups.textContent = allGroupParts.join(", ") || ""
      tr.appendChild(tdGroups)

      const tdStatus = document.createElement("td")
      const badge = document.createElement("span")
      badge.className = user.disabled ? "gowiki-admin-badge-disabled" : "gowiki-admin-badge-active"
      badge.textContent = user.disabled ? "Disabled" : "Active"
      tdStatus.appendChild(badge)
      tr.appendChild(tdStatus)

      const tdLogin = document.createElement("td")
      tdLogin.textContent = user.last_login ? new Date(user.last_login).toLocaleString() : "Never"
      tr.appendChild(tdLogin)

      const tdActions = document.createElement("td")
      tdActions.className = "gowiki-admin-actions-cell"

      const editBtn = document.createElement("button")
      editBtn.className = "gowiki-admin-btn-small"
      editBtn.textContent = "Edit"
      editBtn.addEventListener("click", async () => {
        const result = await showEditUserModal(user)
        if (result) renderAdminUsersTab(container)
      })
      tdActions.appendChild(editBtn)

      const pwBtn = document.createElement("button")
      pwBtn.className = "gowiki-admin-btn-small"
      pwBtn.textContent = "Password"
      pwBtn.addEventListener("click", async () => {
        await showSetPasswordModal(user.username)
      })
      tdActions.appendChild(pwBtn)

      // Don't allow deleting yourself
      if (user.username !== currentUser.username) {
        const delBtn = document.createElement("button")
        delBtn.className = "gowiki-admin-btn-small gowiki-admin-btn-danger"
        delBtn.textContent = "Delete"
        delBtn.addEventListener("click", async () => {
          if (confirm("Delete user \"" + user.username + "\"? This cannot be undone.")) {
            const r = await authFetch("/api/admin/users/" + encodeURIComponent(user.username), { method: "DELETE" })
            if (r.ok) renderAdminUsersTab(container)
            else alert("Failed to delete user.")
          }
        })
        tdActions.appendChild(delBtn)
      }

      tr.appendChild(tdActions)
      tbody.appendChild(tr)
    }
    table.appendChild(tbody)
    container.appendChild(table)

  } catch (err) {
    container.innerHTML = '<div class="gowiki-admin-error">Failed to load users.</div>'
  }
}

async function showCreateUserModal() {
  return showAdminModal("Create User", (body, close, showError) => {
    const usernameInput = adminFormField(body, "Username", "text")
    const passwordInput = adminFormField(body, "Password", "password")
    const emailInput = adminFormField(body, "Email", "email")
    const displayInput = adminFormField(body, "Display Name", "text")
    const groupsInput = adminFormField(body, "Groups (comma-separated)", "text")

    adminModalActions(body, () => close(null), async () => {
      const username = usernameInput.value.trim()
      const password = passwordInput.value
      if (!username || !password) {
        showError("Username and password are required.")
        return
      }
      const groups = groupsInput.value.split(",").map(s => s.trim()).filter(Boolean)
      try {
        const resp = await authFetch("/api/admin/users", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            username,
            password,
            email: emailInput.value.trim(),
            display_name: displayInput.value.trim(),
            groups,
          }),
        })
        if (resp.ok) {
          close(true)
        } else {
          const err = await resp.json().catch(() => ({}))
          showError(err.error || "Failed to create user.")
        }
      } catch {
        showError("Network error.")
      }
    }, "Create")
  })
}

async function showEditUserModal(user) {
  return showAdminModal("Edit User: " + user.username, (body, close, showError) => {
    const emailInput = adminFormField(body, "Email", "email", user.email || "")
    const displayInput = adminFormField(body, "Display Name", "text", user.display_name || "")
    const groupsInput = adminFormField(body, "Local groups (comma-separated)", "text", (user.groups || []).join(", "))

    const oauthGroups = user.oauth_groups || []
    if (oauthGroups.length > 0) {
      const oauthField = adminFormField(body, "Azure AD groups (synced on login)", "text", oauthGroups.join(", "))
      oauthField.disabled = true
      oauthField.style.opacity = "0.6"
    }

    const disabledLabel = document.createElement("label")
    disabledLabel.className = "gowiki-admin-checkbox-label"
    const disabledCheck = document.createElement("input")
    disabledCheck.type = "checkbox"
    disabledCheck.checked = !!user.disabled
    disabledLabel.appendChild(disabledCheck)
    disabledLabel.appendChild(document.createTextNode(" Disabled"))
    body.appendChild(disabledLabel)

    adminModalActions(body, () => close(null), async () => {
      const groups = groupsInput.value.split(",").map(s => s.trim()).filter(Boolean)
      try {
        const resp = await authFetch("/api/admin/users/" + encodeURIComponent(user.username), {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            email: emailInput.value.trim(),
            display_name: displayInput.value.trim(),
            groups,
            disabled: disabledCheck.checked,
          }),
        })
        if (resp.ok) {
          close(true)
        } else {
          const err = await resp.json().catch(() => ({}))
          showError(err.error || "Failed to update user.")
        }
      } catch {
        showError("Network error.")
      }
    }, "Save")
  })
}

async function showSetPasswordModal(username) {
  return showAdminModal("Set Password: " + username, (body, close, showError) => {
    const passwordInput = adminFormField(body, "New Password", "password")
    const confirmInput = adminFormField(body, "Confirm Password", "password")

    adminModalActions(body, () => close(null), async () => {
      const password = passwordInput.value
      if (!password) {
        showError("Password is required.")
        return
      }
      if (password !== confirmInput.value) {
        showError("Passwords do not match.")
        return
      }
      try {
        const resp = await authFetch("/api/admin/users/" + encodeURIComponent(username) + "/password", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ password }),
        })
        if (resp.ok) {
          close(true)
        } else {
          const err = await resp.json().catch(() => ({}))
          showError(err.error || "Failed to set password.")
        }
      } catch {
        showError("Network error.")
      }
    }, "Set Password")
  })
}

// ── Admin: Groups Tab ─────────────────────────────────

async function renderAdminGroupsTab(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading groups...</div>'

  try {
    const resp = await authFetch("/api/admin/groups")
    if (!resp.ok) {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load groups.</div>'
      return
    }
    const data = await resp.json()
    const groups = data.groups || []

    container.innerHTML = ""

    const toolbar = document.createElement("div")
    toolbar.className = "gowiki-admin-toolbar"
    const createBtn = document.createElement("button")
    createBtn.className = "gowiki-admin-btn gowiki-admin-btn-primary"
    createBtn.textContent = "Create Group"
    createBtn.addEventListener("click", async () => {
      const result = await showCreateGroupModal()
      if (result) renderAdminGroupsTab(container)
    })
    toolbar.appendChild(createBtn)
    container.appendChild(toolbar)

    if (groups.length === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-admin-empty"
      empty.textContent = "No groups found."
      container.appendChild(empty)
      return
    }

    const table = document.createElement("table")
    table.className = "gowiki-admin-table"

    const thead = document.createElement("thead")
    const headerRow = document.createElement("tr")
    for (const col of ["Name", "Description", "Actions"]) {
      const th = document.createElement("th")
      th.textContent = col
      headerRow.appendChild(th)
    }
    thead.appendChild(headerRow)
    table.appendChild(thead)

    const tbody = document.createElement("tbody")
    for (const group of groups) {
      const tr = document.createElement("tr")

      const tdName = document.createElement("td")
      tdName.textContent = group.name
      tr.appendChild(tdName)

      const tdDesc = document.createElement("td")
      tdDesc.textContent = group.description || ""
      tr.appendChild(tdDesc)

      const tdActions = document.createElement("td")
      tdActions.className = "gowiki-admin-actions-cell"

      const editBtn = document.createElement("button")
      editBtn.className = "gowiki-admin-btn-small"
      editBtn.textContent = "Edit"
      editBtn.addEventListener("click", async () => {
        const result = await showEditGroupModal(group)
        if (result) renderAdminGroupsTab(container)
      })
      tdActions.appendChild(editBtn)

      // Protect the admin group from deletion
      if (group.name !== "admin") {
        const delBtn = document.createElement("button")
        delBtn.className = "gowiki-admin-btn-small gowiki-admin-btn-danger"
        delBtn.textContent = "Delete"
        delBtn.addEventListener("click", async () => {
          if (confirm("Delete group \"" + group.name + "\"? This cannot be undone.")) {
            const r = await authFetch("/api/admin/groups/" + encodeURIComponent(group.name), { method: "DELETE" })
            if (r.ok) renderAdminGroupsTab(container)
            else alert("Failed to delete group.")
          }
        })
        tdActions.appendChild(delBtn)
      }

      tr.appendChild(tdActions)
      tbody.appendChild(tr)
    }
    table.appendChild(tbody)
    container.appendChild(table)

  } catch {
    container.innerHTML = '<div class="gowiki-admin-error">Failed to load groups.</div>'
  }
}

async function showCreateGroupModal() {
  return showAdminModal("Create Group", (body, close, showError) => {
    const nameInput = adminFormField(body, "Name", "text")
    const descInput = adminFormField(body, "Description", "text")

    adminModalActions(body, () => close(null), async () => {
      const name = nameInput.value.trim()
      if (!name) {
        showError("Group name is required.")
        return
      }
      try {
        const resp = await authFetch("/api/admin/groups", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name, description: descInput.value.trim() }),
        })
        if (resp.ok) {
          close(true)
        } else {
          const err = await resp.json().catch(() => ({}))
          showError(err.error || "Failed to create group.")
        }
      } catch {
        showError("Network error.")
      }
    }, "Create")
  })
}

async function showEditGroupModal(group) {
  return showAdminModal("Edit Group: " + group.name, (body, close, showError) => {
    const descInput = adminFormField(body, "Description", "text", group.description || "")

    adminModalActions(body, () => close(null), async () => {
      try {
        const resp = await authFetch("/api/admin/groups/" + encodeURIComponent(group.name), {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ description: descInput.value.trim() }),
        })
        if (resp.ok) {
          close(true)
        } else {
          const err = await resp.json().catch(() => ({}))
          showError(err.error || "Failed to update group.")
        }
      } catch {
        showError("Network error.")
      }
    }, "Save")
  })
}

// ── Admin: ACL Tab ────────────────────────────────────

async function renderAdminACLTab(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading ACL rules...</div>'

  try {
    const resp = await authFetch("/api/admin/acl")
    if (!resp.ok) {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load ACL rules.</div>'
      return
    }
    const data = await resp.json()
    let rules = data.rules || []

    container.innerHTML = ""

    const toolbar = document.createElement("div")
    toolbar.className = "gowiki-admin-toolbar"

    const addBtn = document.createElement("button")
    addBtn.className = "gowiki-admin-btn gowiki-admin-btn-primary"
    addBtn.textContent = "Add Rule"
    addBtn.addEventListener("click", () => {
      collectRulesFromDOM()
      rules.push({ pattern: ".*", subject_type: "special", subject: "@all", permissions: ["view"] })
      renderRules()
    })
    toolbar.appendChild(addBtn)

    const saveBtn = document.createElement("button")
    saveBtn.className = "gowiki-admin-btn gowiki-admin-btn-primary"
    saveBtn.textContent = "Save ACL"
    saveBtn.addEventListener("click", async () => {
      collectRulesFromDOM()
      try {
        const r = await authFetch("/api/admin/acl", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ rules }),
        })
        if (r.ok) {
          statusMsg.textContent = "ACL saved."
          statusMsg.style.color = "#155724"
          statusMsg.style.display = "inline"
          setTimeout(() => { statusMsg.style.display = "none" }, 3000)
        } else {
          const err = await r.json().catch(() => ({}))
          statusMsg.textContent = err.error || "Failed to save ACL."
          statusMsg.style.color = "#c33"
          statusMsg.style.display = "inline"
        }
      } catch {
        statusMsg.textContent = "Network error."
        statusMsg.style.color = "#c33"
        statusMsg.style.display = "inline"
      }
    })
    toolbar.appendChild(saveBtn)

    const statusMsg = document.createElement("span")
    statusMsg.className = "gowiki-admin-status-msg"
    statusMsg.style.display = "none"
    toolbar.appendChild(statusMsg)

    container.appendChild(toolbar)

    const tableContainer = document.createElement("div")
    container.appendChild(tableContainer)

    function collectRulesFromDOM() {
      const rows = tableContainer.querySelectorAll("tr[data-rule-index]")
      rules = []
      rows.forEach((row) => {
        const patternInput = row.querySelector("[data-field='pattern']")
        const typeSelect = row.querySelector("[data-field='subject_type']")
        const subjectInput = row.querySelector("[data-field='subject']")
        const viewCheck = row.querySelector("[data-perm='view']")
        const editCheck = row.querySelector("[data-perm='edit']")
        const deleteCheck = row.querySelector("[data-perm='delete']")
        const perms = []
        if (viewCheck && viewCheck.checked) perms.push("view")
        if (editCheck && editCheck.checked) perms.push("edit")
        if (deleteCheck && deleteCheck.checked) perms.push("delete")
        rules.push({
          pattern: patternInput ? patternInput.value : "",
          subject_type: typeSelect ? typeSelect.value : "special",
          subject: subjectInput ? subjectInput.value : "@all",
          permissions: perms,
        })
      })
    }

    function renderRules() {
      tableContainer.innerHTML = ""

      if (rules.length === 0) {
        const empty = document.createElement("div")
        empty.className = "gowiki-admin-empty"
        empty.textContent = "No ACL rules defined."
        tableContainer.appendChild(empty)
        return
      }

      const table = document.createElement("table")
      table.className = "gowiki-admin-table"

      const thead = document.createElement("thead")
      const headerRow = document.createElement("tr")
      for (const col of ["Pattern", "Subject Type", "Subject", "View", "Edit", "Delete", ""]) {
        const th = document.createElement("th")
        th.textContent = col
        headerRow.appendChild(th)
      }
      thead.appendChild(headerRow)
      table.appendChild(thead)

      const tbody = document.createElement("tbody")
      rules.forEach((rule, index) => {
        const tr = document.createElement("tr")
        tr.setAttribute("data-rule-index", index)

        const tdPattern = document.createElement("td")
        const patternInput = document.createElement("input")
        patternInput.type = "text"
        patternInput.value = rule.pattern || ""
        patternInput.className = "gowiki-admin-input-inline"
        patternInput.setAttribute("data-field", "pattern")
        tdPattern.appendChild(patternInput)
        tr.appendChild(tdPattern)

        const tdType = document.createElement("td")
        const typeSelect = document.createElement("select")
        typeSelect.className = "gowiki-admin-input-inline"
        typeSelect.setAttribute("data-field", "subject_type")
        for (const opt of [
          { value: "user", label: "User" },
          { value: "group", label: "Group" },
          { value: "special", label: "Special" },
        ]) {
          const o = document.createElement("option")
          o.value = opt.value
          o.textContent = opt.label
          if (opt.value === rule.subject_type) o.selected = true
          typeSelect.appendChild(o)
        }
        tdType.appendChild(typeSelect)
        tr.appendChild(tdType)

        const tdSubject = document.createElement("td")
        const subjectInput = document.createElement("input")
        subjectInput.type = "text"
        subjectInput.value = rule.subject || ""
        subjectInput.className = "gowiki-admin-input-inline"
        subjectInput.setAttribute("data-field", "subject")
        tdSubject.appendChild(subjectInput)
        tr.appendChild(tdSubject)

        const allPerms = ["view", "edit", "delete"]
        for (const perm of allPerms) {
          const td = document.createElement("td")
          td.style.textAlign = "center"
          const check = document.createElement("input")
          check.type = "checkbox"
          check.checked = (rule.permissions || []).includes(perm)
          check.setAttribute("data-perm", perm)
          td.appendChild(check)
          tr.appendChild(td)
        }

        const tdRemove = document.createElement("td")
        const removeBtn = document.createElement("button")
        removeBtn.className = "gowiki-admin-btn-small gowiki-admin-btn-danger"
        removeBtn.textContent = "Remove"
        removeBtn.addEventListener("click", () => {
          collectRulesFromDOM()
          rules.splice(index, 1)
          renderRules()
        })
        tdRemove.appendChild(removeBtn)
        tr.appendChild(tdRemove)

        tbody.appendChild(tr)
      })
      table.appendChild(tbody)
      tableContainer.appendChild(table)
    }

    renderRules()

  } catch {
    container.innerHTML = '<div class="gowiki-admin-error">Failed to load ACL rules.</div>'
  }
}

// ── Admin: Locks Tab ──────────────────────────────────

async function renderAdminLocksTab(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading locks...</div>'

  try {
    const resp = await authFetch("/api/admin/locks")
    if (!resp.ok) {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load locks.</div>'
      return
    }
    const data = await resp.json()
    const locks = data.locks || []

    container.innerHTML = ""

    const toolbar = document.createElement("div")
    toolbar.className = "gowiki-admin-toolbar"
    const refreshBtn = document.createElement("button")
    refreshBtn.className = "gowiki-admin-btn"
    refreshBtn.textContent = "Refresh"
    refreshBtn.addEventListener("click", () => renderAdminLocksTab(container))
    toolbar.appendChild(refreshBtn)
    container.appendChild(toolbar)

    if (locks.length === 0) {
      const empty = document.createElement("div")
      empty.className = "gowiki-admin-empty"
      empty.textContent = "No active locks."
      container.appendChild(empty)
      return
    }

    const table = document.createElement("table")
    table.className = "gowiki-admin-table"

    const thead = document.createElement("thead")
    const headerRow = document.createElement("tr")
    for (const col of ["Page", "Owner", "Since", "Actions"]) {
      const th = document.createElement("th")
      th.textContent = col
      headerRow.appendChild(th)
    }
    thead.appendChild(headerRow)
    table.appendChild(thead)

    const tbody = document.createElement("tbody")
    for (const lock of locks) {
      const tr = document.createElement("tr")

      const tdPage = document.createElement("td")
      tdPage.textContent = lock.page
      tr.appendChild(tdPage)

      const tdOwner = document.createElement("td")
      tdOwner.textContent = lock.owner
      tr.appendChild(tdOwner)

      const tdSince = document.createElement("td")
      tdSince.textContent = lock.since ? new Date(lock.since).toLocaleString() : ""
      tr.appendChild(tdSince)

      const tdActions = document.createElement("td")
      tdActions.className = "gowiki-admin-actions-cell"
      const discardBtn = document.createElement("button")
      discardBtn.className = "gowiki-admin-btn-small gowiki-admin-btn-danger"
      discardBtn.textContent = "Force Discard"
      discardBtn.addEventListener("click", async () => {
        if (confirm("Force discard draft for \"" + lock.page + "\" owned by " + lock.owner + "?")) {
          const pagePath = lock.page
          const r = await authFetch("/api/admin/drafts/" + encodePagePath(pagePath), { method: "DELETE" })
          if (r.ok) {
            renderAdminLocksTab(container)
          } else {
            alert("Failed to discard draft.")
          }
        }
      })
      tdActions.appendChild(discardBtn)
      tr.appendChild(tdActions)

      tbody.appendChild(tr)
    }
    table.appendChild(tbody)
    container.appendChild(table)

  } catch {
    container.innerHTML = '<div class="gowiki-admin-error">Failed to load locks.</div>'
  }
}

// ── Admin: Tokens Tab ──────────────────────────────────

async function renderAdminTokensTab(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading tokens...</div>'

  try {
    const resp = await authFetch("/api/admin/tokens")
    if (!resp.ok) {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load tokens.</div>'
      return
    }
    const data = await resp.json()
    container.innerHTML = ""

    const refreshBtn = document.createElement("button")
    refreshBtn.className = "gowiki-admin-btn-small"
    refreshBtn.textContent = "Refresh"
    refreshBtn.addEventListener("click", () => renderAdminTokensTab(container))
    container.appendChild(refreshBtn)

    const tokens = data.tokens || []
    if (tokens.length === 0) {
      const p = document.createElement("p")
      p.textContent = "No API tokens."
      p.style.color = "#666"
      container.appendChild(p)
      return
    }

    const table = document.createElement("table")
    table.className = "gowiki-admin-table"
    const thead = document.createElement("thead")
    thead.innerHTML = "<tr><th>ID</th><th>User</th><th>Name</th><th>Created</th><th>Last Used</th><th>Actions</th></tr>"
    table.appendChild(thead)

    const tbody = document.createElement("tbody")
    for (const token of tokens) {
      const tr = document.createElement("tr")

      const tdID = document.createElement("td")
      tdID.textContent = token.id
      tdID.style.fontFamily = "monospace"
      tdID.style.fontSize = "0.85em"
      tr.appendChild(tdID)

      const tdUser = document.createElement("td")
      tdUser.textContent = token.user
      tr.appendChild(tdUser)

      const tdName = document.createElement("td")
      tdName.textContent = token.name
      tr.appendChild(tdName)

      const tdCreated = document.createElement("td")
      tdCreated.textContent = token.created_at ? new Date(token.created_at).toLocaleString() : ""
      tr.appendChild(tdCreated)

      const tdUsed = document.createElement("td")
      tdUsed.textContent = token.last_used_at ? new Date(token.last_used_at).toLocaleString() : "Never"
      tr.appendChild(tdUsed)

      const tdActions = document.createElement("td")
      tdActions.className = "gowiki-admin-actions-cell"
      const revokeBtn = document.createElement("button")
      revokeBtn.className = "gowiki-admin-btn-small gowiki-admin-btn-danger"
      revokeBtn.textContent = "Revoke"
      revokeBtn.addEventListener("click", async () => {
        if (confirm("Revoke token \"" + token.name + "\" for user " + token.user + "?")) {
          const r = await authFetch("/api/admin/tokens/" + token.id, { method: "DELETE" })
          if (r.ok) {
            renderAdminTokensTab(container)
          } else {
            alert("Failed to revoke token.")
          }
        }
      })
      tdActions.appendChild(revokeBtn)
      tr.appendChild(tdActions)

      tbody.appendChild(tr)
    }
    table.appendChild(tbody)
    container.appendChild(table)

  } catch {
    container.innerHTML = '<div class="gowiki-admin-error">Failed to load tokens.</div>'
  }
}

// ── Admin: Configuration Tab ──────────────────────────

async function renderAdminConfigTab(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading configuration...</div>'

  try {
    const resp = await authFetch("/api/admin/config")
    if (!resp.ok) {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load configuration.</div>'
      return
    }
    const config = await resp.json()

    container.innerHTML = ""

    const form = document.createElement("div")
    form.className = "gowiki-admin-config-form"

    // Site section
    const siteHeading = document.createElement("h3")
    siteHeading.textContent = "Site"
    form.appendChild(siteHeading)

    const titleInput = adminFormField(form, "Site Title", "text", (config.site && config.site.title) || "")
    const baseUrlInput = adminFormField(form, "Base URL (e.g. https://wiki.example.com)", "text", (config.site && config.site.base_url) || "")
    const sidebarInput = adminFormField(form, "Sidebar Page", "text", (config.site && config.site.sidebar_page) || "")
    const footerInput = adminFormField(form, "Footer Page", "text", (config.site && config.site.footer_page) || "")
    const tocMaxLevelInput = adminFormField(form, "TOC max heading level (0 = disabled, 1-6)", "number", String((config.site && config.site.toc_max_level) ?? 3))
    tocMaxLevelInput.min = "0"
    tocMaxLevelInput.max = "6"
    const userDisplaySelect = adminFormSelect(form, "User display format", [
      { value: "", label: "Login (default)" },
      { value: "login", label: "Login" },
      { value: "fullname", label: "Full name" },
      { value: "email", label: "Email" },
    ], (config.site && config.site.user_display) || "")

    const codeThemeSelect = adminFormSelect(form, "Code theme", [
      { value: "github", label: "GitHub (light)" },
      { value: "atom-one-light", label: "Atom One Light" },
      { value: "vs", label: "Visual Studio (light)" },
      { value: "xcode", label: "Xcode (light)" },
      { value: "idea", label: "IntelliJ IDEA (light)" },
      { value: "github-dark", label: "GitHub Dark" },
      { value: "atom-one-dark", label: "Atom One Dark" },
      { value: "monokai", label: "Monokai (dark)" },
      { value: "nord", label: "Nord (dark)" },
      { value: "vs2015", label: "VS 2015 (dark)" },
      { value: "tokyo-night-dark", label: "Tokyo Night (dark)" },
    ], (config.site && config.site.code_theme) || "github")

    codeThemeSelect.addEventListener("change", () => {
      loadHighlightTheme(codeThemeSelect.value)
    })

    // Auth section
    const authHeading = document.createElement("h3")
    authHeading.textContent = "Authentication"
    form.appendChild(authHeading)

    const sessionTtlInput = adminFormField(form, "Session TTL (e.g. 24h, 168h)", "text", (config.auth && config.auth.session_ttl) || "")

    // OAuth section
    const oauthHeading = document.createElement("h3")
    oauthHeading.textContent = "OAuth / Microsoft 365"
    form.appendChild(oauthHeading)

    const oauth = (config.auth && config.auth.oauth) || {}
    const oauthProviderSelect = adminFormSelect(form, "Provider", [
      { value: "", label: "(disabled)" },
      { value: "azure", label: "Azure AD / Microsoft 365" },
    ], oauth.provider || "")
    const oauthTenantInput = adminFormField(form, "Tenant ID", "text", oauth.tenant_id || "")
    const oauthClientIdInput = adminFormField(form, "Client ID", "text", oauth.client_id || "")
    const oauthClientSecretInput = adminFormField(form, "Client Secret", "password", oauth.client_secret || "")
    const oauthAutoCreateCheckbox = document.createElement("input")
    oauthAutoCreateCheckbox.type = "checkbox"
    oauthAutoCreateCheckbox.checked = !!oauth.auto_create_users
    const oauthAutoCreateLabel = document.createElement("label")
    oauthAutoCreateLabel.style.display = "flex"
    oauthAutoCreateLabel.style.alignItems = "center"
    oauthAutoCreateLabel.style.gap = "8px"
    oauthAutoCreateLabel.style.margin = "8px 0"
    oauthAutoCreateLabel.appendChild(oauthAutoCreateCheckbox)
    oauthAutoCreateLabel.appendChild(document.createTextNode("Auto-create users on first OAuth login"))
    form.appendChild(oauthAutoCreateLabel)
    const oauthDefaultGroupsInput = adminFormField(form, "Default groups for auto-created users (comma-separated)", "text", (oauth.default_groups || []).join(", "))

    // Drafts section
    const draftsHeading = document.createElement("h3")
    draftsHeading.textContent = "Drafts"
    form.appendChild(draftsHeading)

    const autoSaveInput = adminFormField(form, "Auto Save Interval (e.g. 30s, 1m)", "text", (config.drafts && config.drafts.auto_save_interval) || "")
    const staleLockInput = adminFormField(form, "Stale Lock Timeout (e.g. 30m, 1h)", "text", (config.drafts && config.drafts.stale_lock_timeout) || "")

    // Todo section
    const todoHeading = document.createElement("h3")
    todoHeading.textContent = "Todo Plugin"
    form.appendChild(todoHeading)

    const todoConfig = config.todo || {}

    const todoDisabledCheckbox = document.createElement("input")
    todoDisabledCheckbox.type = "checkbox"
    todoDisabledCheckbox.checked = !!todoConfig.disabled
    const todoDisabledLabel = document.createElement("label")
    todoDisabledLabel.style.display = "flex"
    todoDisabledLabel.style.alignItems = "center"
    todoDisabledLabel.style.gap = "8px"
    todoDisabledLabel.style.margin = "8px 0"
    todoDisabledLabel.appendChild(todoDisabledCheckbox)
    todoDisabledLabel.appendChild(document.createTextNode("Disable todo plugin (requires restart)"))
    form.appendChild(todoDisabledLabel)

    const todoNote = document.createElement("div")
    todoNote.style.fontSize = "0.85em"
    todoNote.style.color = "#666"
    todoNote.style.margin = "0 0 8px 0"
    todoNote.textContent = "Todo is active when a database is connected and this box is unchecked."
    form.appendChild(todoNote)

    const reminderHoursInput = adminFormField(form, "Reminder hours before due date (comma-separated, e.g. 24, 48, 168)", "text",
      (todoConfig.reminder_hours || []).join(", "))

    // SMTP / Email Notifications section
    const smtpHeading = document.createElement("h3")
    smtpHeading.textContent = "Email Notifications (SMTP)"
    form.appendChild(smtpHeading)

    const emailConfig = (todoConfig.notify && todoConfig.notify.email) || {}

    const smtpEnabledCheckbox = document.createElement("input")
    smtpEnabledCheckbox.type = "checkbox"
    smtpEnabledCheckbox.checked = !!emailConfig.enabled
    const smtpEnabledLabel = document.createElement("label")
    smtpEnabledLabel.style.display = "flex"
    smtpEnabledLabel.style.alignItems = "center"
    smtpEnabledLabel.style.gap = "8px"
    smtpEnabledLabel.style.margin = "8px 0"
    smtpEnabledLabel.appendChild(smtpEnabledCheckbox)
    smtpEnabledLabel.appendChild(document.createTextNode("Enable email notifications"))
    form.appendChild(smtpEnabledLabel)

    const smtpFromInput = adminFormField(form, "From address", "text", emailConfig.from || "")
    const smtpHostInput = adminFormField(form, "SMTP Host", "text", emailConfig.smtp_host || "")
    const smtpPortInput = adminFormField(form, "SMTP Port", "number", String(emailConfig.smtp_port || 587))
    smtpPortInput.min = "1"
    smtpPortInput.max = "65535"
    const smtpUserInput = adminFormField(form, "SMTP Username", "text", emailConfig.smtp_user || "")
    const smtpPassInput = adminFormField(form, "SMTP Password", "password", emailConfig.smtp_pass || "")

    // Webhooks section
    const webhookHeading = document.createElement("h3")
    webhookHeading.textContent = "Webhooks"
    form.appendChild(webhookHeading)

    const webhooks = (todoConfig.notify && todoConfig.notify.webhook) || []
    const webhookContainer = document.createElement("div")
    webhookContainer.className = "gowiki-admin-webhooks"

    const webhookEntries = []

    function renderWebhookEntry(wh, index) {
      const entry = document.createElement("div")
      entry.className = "gowiki-admin-webhook-entry"
      entry.style.border = "1px solid #ddd"
      entry.style.borderRadius = "4px"
      entry.style.padding = "8px 12px"
      entry.style.marginBottom = "8px"
      entry.style.background = "#fafafa"

      const header = document.createElement("div")
      header.style.display = "flex"
      header.style.alignItems = "center"
      header.style.gap = "8px"
      header.style.marginBottom = "6px"

      const enabledCb = document.createElement("input")
      enabledCb.type = "checkbox"
      enabledCb.checked = !!wh.enabled

      const nameInput = document.createElement("input")
      nameInput.type = "text"
      nameInput.value = wh.name || ""
      nameInput.placeholder = "Hook name"
      nameInput.style.width = "10em"

      const removeBtn = document.createElement("button")
      removeBtn.className = "gowiki-admin-btn-small gowiki-admin-btn-danger"
      removeBtn.textContent = "Remove"
      removeBtn.addEventListener("click", () => {
        webhookEntries.splice(index, 1)
        rebuildWebhooks()
      })

      header.appendChild(enabledCb)
      header.appendChild(nameInput)
      header.appendChild(removeBtn)
      entry.appendChild(header)

      const urlInput = adminFormField(entry, "URL", "text", wh.url || "")
      urlInput.style.width = "100%"
      const secretInput = adminFormField(entry, "HMAC Secret (optional)", "password", wh.hmac_secret || "")

      webhookEntries[index] = { enabledCb, nameInput, urlInput, secretInput }
      return entry
    }

    function rebuildWebhooks() {
      webhookContainer.innerHTML = ""
      const current = webhookEntries.map(e => ({
        enabled: e.enabledCb.checked,
        name: e.nameInput.value,
        url: e.urlInput.value,
        hmac_secret: e.secretInput.value,
      }))
      webhookEntries.length = 0
      current.forEach((wh, i) => {
        webhookContainer.appendChild(renderWebhookEntry(wh, i))
      })
    }

    webhooks.forEach((wh, i) => {
      webhookContainer.appendChild(renderWebhookEntry(wh, i))
    })

    form.appendChild(webhookContainer)

    const addWebhookBtn = document.createElement("button")
    addWebhookBtn.className = "gowiki-admin-btn-small"
    addWebhookBtn.textContent = "Add Webhook"
    addWebhookBtn.style.marginBottom = "12px"
    addWebhookBtn.addEventListener("click", () => {
      const idx = webhookEntries.length
      webhookContainer.appendChild(renderWebhookEntry({ enabled: true, name: "", url: "", hmac_secret: "" }, idx))
    })
    form.appendChild(addWebhookBtn)

    // AI API section
    const aiHeading = document.createElement("h3")
    aiHeading.textContent = "AI Content API"
    form.appendChild(aiHeading)

    const aiConfig = config.ai_api || {}

    const aiEnabledCheckbox = document.createElement("input")
    aiEnabledCheckbox.type = "checkbox"
    aiEnabledCheckbox.checked = !!aiConfig.enabled
    const aiEnabledLabel = document.createElement("label")
    aiEnabledLabel.style.display = "flex"
    aiEnabledLabel.style.alignItems = "center"
    aiEnabledLabel.style.gap = "8px"
    aiEnabledLabel.style.margin = "8px 0"
    aiEnabledLabel.appendChild(aiEnabledCheckbox)
    aiEnabledLabel.appendChild(document.createTextNode("Enable AI Content API (token-based access for AI assistants)"))
    form.appendChild(aiEnabledLabel)

    const aiRateLimitReadInput = adminFormField(form, "Read rate limit (requests/minute per token)", "number", String(aiConfig.rate_limit_read ?? 120))
    aiRateLimitReadInput.min = "1"
    const aiRateLimitWriteInput = adminFormField(form, "Write rate limit (requests/minute per token)", "number", String(aiConfig.rate_limit_write ?? 30))
    aiRateLimitWriteInput.min = "1"
    const aiMaxTokensInput = adminFormField(form, "Max tokens per user", "number", String(aiConfig.max_tokens_per_user ?? 5))
    aiMaxTokensInput.min = "1"

    const aiRequireSummaryCheckbox = document.createElement("input")
    aiRequireSummaryCheckbox.type = "checkbox"
    aiRequireSummaryCheckbox.checked = aiConfig.require_summary !== false
    const aiRequireSummaryLabel = document.createElement("label")
    aiRequireSummaryLabel.style.display = "flex"
    aiRequireSummaryLabel.style.alignItems = "center"
    aiRequireSummaryLabel.style.gap = "8px"
    aiRequireSummaryLabel.style.margin = "8px 0"
    aiRequireSummaryLabel.appendChild(aiRequireSummaryCheckbox)
    aiRequireSummaryLabel.appendChild(document.createTextNode("Require summary for token-authenticated writes"))
    form.appendChild(aiRequireSummaryLabel)

    // Reviewflow section
    const rfHeading = document.createElement("h3")
    rfHeading.textContent = "Reviewflow Plugin"
    form.appendChild(rfHeading)

    const rfConfig = config.reviewflow || {}

    const rfEnabledCheckbox = document.createElement("input")
    rfEnabledCheckbox.type = "checkbox"
    rfEnabledCheckbox.checked = !!rfConfig.enabled
    const rfEnabledLabel = document.createElement("label")
    rfEnabledLabel.style.display = "flex"
    rfEnabledLabel.style.alignItems = "center"
    rfEnabledLabel.style.gap = "8px"
    rfEnabledLabel.style.margin = "8px 0"
    rfEnabledLabel.appendChild(rfEnabledCheckbox)
    rfEnabledLabel.appendChild(document.createTextNode("Enable reviewflow plugin"))
    form.appendChild(rfEnabledLabel)

    const rfNote = document.createElement("div")
    rfNote.style.fontSize = "0.85em"
    rfNote.style.color = "#666"
    rfNote.style.margin = "0 0 8px 0"
    rfNote.textContent = "Deadlines: one per line as role=duration (e.g. reviewer=72h, _default=168h). _default applies to roles without a specific deadline."
    form.appendChild(rfNote)

    const rfDeadlinesRaw = rfConfig.deadlines || {}
    const rfDeadlinesInput = document.createElement("textarea")
    rfDeadlinesInput.rows = 4
    rfDeadlinesInput.style.width = "100%"
    rfDeadlinesInput.style.fontFamily = "monospace"
    rfDeadlinesInput.style.fontSize = "13px"
    rfDeadlinesInput.value = Object.entries(rfDeadlinesRaw).map(([k, v]) => `${k}=${v}`).join("\n")
    const rfDeadlinesLabel = document.createElement("label")
    rfDeadlinesLabel.style.display = "block"
    rfDeadlinesLabel.style.margin = "8px 0 4px 0"
    rfDeadlinesLabel.style.fontWeight = "500"
    rfDeadlinesLabel.textContent = "Deadlines"
    form.appendChild(rfDeadlinesLabel)
    form.appendChild(rfDeadlinesInput)

    // Save button
    const actions = document.createElement("div")
    actions.className = "gowiki-admin-config-actions"

    const statusMsg = document.createElement("span")
    statusMsg.className = "gowiki-admin-status-msg"
    statusMsg.style.display = "none"

    const saveBtn = document.createElement("button")
    saveBtn.className = "gowiki-admin-btn gowiki-admin-btn-primary"
    saveBtn.textContent = "Save Configuration"
    saveBtn.addEventListener("click", async () => {
      const defaultGroupsRaw = oauthDefaultGroupsInput.value.trim()
      const defaultGroups = defaultGroupsRaw ? defaultGroupsRaw.split(",").map(s => s.trim()).filter(Boolean) : []
      const reminderRaw = reminderHoursInput.value.trim()
      const reminderHours = reminderRaw ? reminderRaw.split(",").map(s => parseInt(s.trim(), 10)).filter(n => n > 0) : []

      const savedWebhooks = webhookEntries.map(e => ({
        name: e.nameInput.value.trim(),
        enabled: e.enabledCb.checked,
        url: e.urlInput.value.trim(),
        hmac_secret: e.secretInput.value,
        content_type: "application/json",
        payload_tmpl: "",
      })).filter(w => w.url)

      const payload = {
        data_dir: config.data_dir || "",
        server: config.server || {},
        site: {
          title: titleInput.value.trim(),
          base_url: baseUrlInput.value.trim(),
          sidebar_page: sidebarInput.value.trim(),
          footer_page: footerInput.value.trim(),
          toc_max_level: parseInt(tocMaxLevelInput.value, 10) || 3,
          user_display: userDisplaySelect.value || "",
          code_theme: codeThemeSelect.value,
        },
        auth: {
          session_ttl: sessionTtlInput.value.trim(),
          oauth: {
            provider: oauthProviderSelect.value,
            tenant_id: oauthTenantInput.value.trim(),
            client_id: oauthClientIdInput.value.trim(),
            client_secret: oauthClientSecretInput.value.trim(),
            auto_create_users: oauthAutoCreateCheckbox.checked,
            default_groups: defaultGroups,
          },
        },
        drafts: {
          auto_save_interval: autoSaveInput.value.trim(),
          stale_lock_timeout: staleLockInput.value.trim(),
        },
        database: config.database || {},
        reviewflow: {
          enabled: rfEnabledCheckbox.checked,
          deadlines: (() => {
            const d = {}
            rfDeadlinesInput.value.trim().split("\n").filter(Boolean).forEach(line => {
              const eq = line.indexOf("=")
              if (eq > 0) d[line.slice(0, eq).trim()] = line.slice(eq + 1).trim()
            })
            return d
          })(),
        },
        ai_api: {
          enabled: aiEnabledCheckbox.checked,
          rate_limit_read: parseInt(aiRateLimitReadInput.value, 10) || 120,
          rate_limit_write: parseInt(aiRateLimitWriteInput.value, 10) || 30,
          max_tokens_per_user: parseInt(aiMaxTokensInput.value, 10) || 5,
          require_summary: aiRequireSummaryCheckbox.checked,
        },
        todo: {
          enabled: todoConfig.enabled || false,
          disabled: todoDisabledCheckbox.checked,
          reminder_hours: reminderHours,
          notify: {
            email: {
              enabled: smtpEnabledCheckbox.checked,
              from: smtpFromInput.value.trim(),
              smtp_host: smtpHostInput.value.trim(),
              smtp_port: parseInt(smtpPortInput.value, 10) || 587,
              smtp_user: smtpUserInput.value.trim(),
              smtp_pass: smtpPassInput.value,
            },
            webhook: savedWebhooks,
          },
        },
      }
      try {
        const r = await authFetch("/api/admin/config", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        })
        if (r.ok) {
          statusMsg.textContent = "Configuration saved."
          statusMsg.style.color = "#155724"
          statusMsg.style.display = "inline"
          setTimeout(() => { statusMsg.style.display = "none" }, 3000)
        } else {
          const err = await r.json().catch(() => ({}))
          statusMsg.textContent = err.error || "Failed to save configuration."
          statusMsg.style.color = "#c33"
          statusMsg.style.display = "inline"
        }
      } catch {
        statusMsg.textContent = "Network error."
        statusMsg.style.color = "#c33"
        statusMsg.style.display = "inline"
      }
    })

    actions.appendChild(saveBtn)
    actions.appendChild(statusMsg)
    form.appendChild(actions)
    container.appendChild(form)

  } catch {
    container.innerHTML = '<div class="gowiki-admin-error">Failed to load configuration.</div>'
  }
}

// ── Admin: Database Tab ──────────────────────────

async function renderAdminDatabaseTab(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading database status...</div>'

  try {
    const statusResp = await authFetch("/api/admin/database/status")
    if (!statusResp.ok) {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load database status.</div>'
      return
    }
    const status = await statusResp.json()

    // Also load current config to get DSN.
    const configResp = await authFetch("/api/admin/config")
    const config = configResp.ok ? await configResp.json() : {}
    const dbConfig = config.database || {}

    container.innerHTML = ""

    const form = document.createElement("div")
    form.className = "gowiki-admin-config-form"

    // ── Connection Status ──
    const connHeading = document.createElement("h3")
    connHeading.textContent = "Connection"
    form.appendChild(connHeading)

    const statusIndicator = document.createElement("div")
    statusIndicator.style.display = "flex"
    statusIndicator.style.alignItems = "center"
    statusIndicator.style.gap = "8px"
    statusIndicator.style.marginBottom = "12px"

    const dot = document.createElement("span")
    dot.style.width = "12px"
    dot.style.height = "12px"
    dot.style.borderRadius = "50%"
    dot.style.display = "inline-block"
    dot.style.background = status.connected ? "#40c057" : "#e03131"

    const statusText = document.createElement("span")
    statusText.textContent = status.connected ? "Connected" : "Not connected"

    statusIndicator.appendChild(dot)
    statusIndicator.appendChild(statusText)
    form.appendChild(statusIndicator)

    const dsnInput = adminFormField(form, "DSN (e.g. postgres://user:pass@host:5432/db?sslmode=disable)", "text", dbConfig.dsn || "")
    dsnInput.style.fontFamily = "monospace"
    dsnInput.style.fontSize = "12px"

    const connStatusMsg = document.createElement("span")
    connStatusMsg.className = "gowiki-admin-status-msg"
    connStatusMsg.style.display = "none"
    connStatusMsg.style.marginLeft = "8px"

    const btnRow = document.createElement("div")
    btnRow.style.display = "flex"
    btnRow.style.gap = "8px"
    btnRow.style.marginTop = "8px"

    const testBtn = document.createElement("button")
    testBtn.className = "gowiki-admin-btn"
    testBtn.textContent = "Test Connection"
    testBtn.addEventListener("click", async () => {
      connStatusMsg.style.display = "inline"
      connStatusMsg.style.color = "#636e72"
      connStatusMsg.textContent = "Testing..."
      try {
        const r = await authFetch("/api/admin/database/test", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ dsn: dsnInput.value.trim() }),
        })
        const data = await r.json()
        if (data.success) {
          connStatusMsg.textContent = "Connection successful!"
          connStatusMsg.style.color = "#155724"
        } else {
          connStatusMsg.textContent = data.error || "Connection failed"
          connStatusMsg.style.color = "#c33"
        }
      } catch {
        connStatusMsg.textContent = "Network error"
        connStatusMsg.style.color = "#c33"
      }
    })

    const saveConnBtn = document.createElement("button")
    saveConnBtn.className = "gowiki-admin-btn gowiki-admin-btn-primary"
    saveConnBtn.textContent = "Save & Connect"
    saveConnBtn.addEventListener("click", async () => {
      connStatusMsg.style.display = "inline"
      connStatusMsg.style.color = "#636e72"
      connStatusMsg.textContent = "Saving..."
      try {
        // Update config with new DSN.
        const currentConfig = configResp.ok ? await (await authFetch("/api/admin/config")).json() : {}
        currentConfig.database = {
          dsn: dsnInput.value.trim(),
          enabled: true,
        }
        const saveResp = await authFetch("/api/admin/config", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(currentConfig),
        })
        if (!saveResp.ok) {
          const err = await saveResp.json().catch(() => ({}))
          connStatusMsg.textContent = err.error || "Failed to save config"
          connStatusMsg.style.color = "#c33"
          return
        }

        // Connect.
        const connResp = await authFetch("/api/admin/database/connect", { method: "POST" })
        const connData = await connResp.json()
        if (connData.success) {
          connStatusMsg.textContent = "Connected!"
          connStatusMsg.style.color = "#155724"
          dot.style.background = "#40c057"
          statusText.textContent = "Connected"
          // Reload to show tables.
          setTimeout(() => renderAdminDatabaseTab(container), 1000)
        } else {
          connStatusMsg.textContent = connData.error || "Connection failed"
          connStatusMsg.style.color = "#c33"
        }
      } catch {
        connStatusMsg.textContent = "Network error"
        connStatusMsg.style.color = "#c33"
      }
    })

    btnRow.appendChild(testBtn)
    btnRow.appendChild(saveConnBtn)
    btnRow.appendChild(connStatusMsg)
    form.appendChild(btnRow)

    // ── Tables Section (only if connected) ──
    if (status.connected) {
      const tablesHeading = document.createElement("h3")
      tablesHeading.textContent = "Tables"
      tablesHeading.style.marginTop = "24px"
      form.appendChild(tablesHeading)

      const tablesContainer = document.createElement("div")
      form.appendChild(tablesContainer)

      await renderDatabaseTables(tablesContainer)
    }

    container.appendChild(form)
  } catch {
    container.innerHTML = '<div class="gowiki-admin-error">Failed to load database status.</div>'
  }
}

async function renderDatabaseTables(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading tables...</div>'

  try {
    const resp = await authFetch("/api/admin/database/tables")
    if (!resp.ok) {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load tables.</div>'
      return
    }
    const data = await resp.json()
    const tables = data.tables || []

    container.innerHTML = ""

    // New Table button.
    const newBtn = document.createElement("button")
    newBtn.className = "gowiki-admin-btn gowiki-admin-btn-primary"
    newBtn.textContent = "New Table"
    newBtn.style.marginBottom = "12px"
    newBtn.addEventListener("click", async () => {
      const result = await showDatabaseTableModal(null)
      if (result) await renderDatabaseTables(container)
    })
    container.appendChild(newBtn)

    if (tables.length === 0) {
      const empty = document.createElement("div")
      empty.style.color = "#636e72"
      empty.textContent = "No tables defined."
      container.appendChild(empty)
      return
    }

    // Table list.
    const tbl = document.createElement("table")
    tbl.className = "gowiki-admin-table"
    tbl.innerHTML = `<thead><tr><th>Name</th><th>Label</th><th>Scope</th><th>Actions</th></tr></thead>`
    const tbody = document.createElement("tbody")

    for (const t of tables) {
      const tr = document.createElement("tr")
      tr.innerHTML = `<td><code>${t.name}</code></td><td>${t.label || ""}</td><td>${t.scope_regexp || ".*"}</td><td></td>`
      const actionsTd = tr.querySelector("td:last-child")

      const editBtn = document.createElement("button")
      editBtn.className = "gowiki-admin-btn gowiki-admin-btn-sm"
      editBtn.textContent = "Edit"
      editBtn.addEventListener("click", async () => {
        const result = await showDatabaseTableModal(t)
        if (result) await renderDatabaseTables(container)
      })

      const fieldsBtn = document.createElement("button")
      fieldsBtn.className = "gowiki-admin-btn gowiki-admin-btn-sm"
      fieldsBtn.textContent = "Fields"
      fieldsBtn.addEventListener("click", async () => {
        await showDatabaseFieldsModal(t.id, t.name)
        await renderDatabaseTables(container)
      })

      const dataBtn = document.createElement("button")
      dataBtn.className = "gowiki-admin-btn gowiki-admin-btn-sm"
      dataBtn.textContent = "Data"
      dataBtn.addEventListener("click", async () => {
        await showDatabaseDataBrowser(t.name)
      })

      const deleteBtn = document.createElement("button")
      deleteBtn.className = "gowiki-admin-btn gowiki-admin-btn-sm gowiki-admin-btn-danger"
      deleteBtn.textContent = "Delete"
      deleteBtn.addEventListener("click", async () => {
        if (!confirm(`Delete table "${t.name}" and all its data? This is irreversible.`)) return
        const r = await authFetch(`/api/admin/database/tables/${t.id}`, { method: "DELETE" })
        if (r.ok) await renderDatabaseTables(container)
      })

      actionsTd.appendChild(editBtn)
      actionsTd.appendChild(fieldsBtn)
      actionsTd.appendChild(dataBtn)
      actionsTd.appendChild(deleteBtn)
      tbody.appendChild(tr)
    }
    tbl.appendChild(tbody)
    container.appendChild(tbl)
  } catch {
    container.innerHTML = '<div class="gowiki-admin-error">Failed to load tables.</div>'
  }
}

async function showDatabaseTableModal(existing) {
  return showAdminModal(existing ? "Edit Table" : "New Table", (body, close, showError) => {
    const nameInput = adminFormField(body, "Name (lowercase, underscores)", "text", existing ? existing.name : "")
    if (existing) nameInput.readOnly = true
    const labelInput = adminFormField(body, "Label", "text", existing ? existing.label : "")
    const scopeInput = adminFormField(body, "Scope Regexp", "text", existing ? existing.scope_regexp : ".*")
    const folderInput = adminFormField(body, "Page Folder", "text", existing ? existing.page_folder : "")
    const sortFieldInput = adminFormField(body, "Default Sort Field", "text", existing ? existing.default_sort_field : "")
    const sortOrderSelect = adminFormSelect(body, "Default Sort Order", [
      { value: "asc", label: "Ascending" },
      { value: "desc", label: "Descending" },
    ], existing ? existing.default_sort_order : "asc")
    const templateInput = adminFormField(body, "Page Template Path", "text", existing ? existing.page_template_path : "")

    adminModalActions(body, close, async () => {
      const payload = {
        name: nameInput.value.trim(),
        label: labelInput.value.trim(),
        scope_regexp: scopeInput.value.trim() || ".*",
        page_folder: folderInput.value.trim(),
        default_sort_field: sortFieldInput.value.trim(),
        default_sort_order: sortOrderSelect.value,
        page_template_path: templateInput.value.trim(),
      }
      if (!payload.name) { showError("Name is required"); return }

      const url = existing ? `/api/admin/database/tables/${existing.id}` : "/api/admin/database/tables"
      const method = existing ? "PUT" : "POST"
      const r = await authFetch(url, {
        method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      })
      if (r.ok) {
        close(true)
      } else {
        const err = await r.json().catch(() => ({}))
        showError(err.error || "Failed to save table")
      }
    }, existing ? "Save" : "Create")
  })
}

async function showDatabaseFieldsModal(tableId, tableName) {
  return showAdminModal(`Fields: ${tableName}`, async (body, close) => {
    body.closest(".gowiki-admin-modal").classList.add("gowiki-admin-modal-wide")
    const resp = await authFetch(`/api/admin/database/tables/${tableId}`)
    if (!resp.ok) {
      body.innerHTML = '<div class="gowiki-admin-error">Failed to load table.</div>'
      return
    }
    const table = await resp.json()
    const fields = (table.fields || []).filter(f => !f.archived_at)

    async function refreshFields() {
      body.innerHTML = ""
      const r = await authFetch(`/api/admin/database/tables/${tableId}`)
      if (!r.ok) return
      const t = await r.json()
      renderFieldList(t.fields || [])
    }

    function renderFieldList(allFields) {
      const active = allFields.filter(f => !f.archived_at)

      if (active.length === 0) {
        const empty = document.createElement("div")
        empty.style.color = "#636e72"
        empty.textContent = "No fields defined."
        body.appendChild(empty)
      } else {
        const tbl = document.createElement("table")
        tbl.className = "gowiki-admin-table"
        tbl.innerHTML = `<thead><tr><th>Order</th><th>Name</th><th>Label</th><th>Type</th><th>Required</th><th>Actions</th></tr></thead>`
        const tbody = document.createElement("tbody")

        for (const f of active) {
          const tr = document.createElement("tr")
          tr.innerHTML = `<td>${f.display_order}</td><td><code>${f.name}</code></td><td>${f.label || ""}</td><td>${f.type}</td><td>${f.required ? "Yes" : "No"}</td><td></td>`
          const actionsTd = tr.querySelector("td:last-child")

          const editBtn = document.createElement("button")
          editBtn.className = "gowiki-admin-btn gowiki-admin-btn-sm"
          editBtn.textContent = "Edit"
          editBtn.addEventListener("click", async () => {
            await showDatabaseFieldEditModal(tableId, f)
            await refreshFields()
          })

          const archiveBtn = document.createElement("button")
          archiveBtn.className = "gowiki-admin-btn gowiki-admin-btn-sm gowiki-admin-btn-danger"
          archiveBtn.textContent = "Archive"
          archiveBtn.addEventListener("click", async () => {
            if (!confirm(`Archive field "${f.name}"? The column will be preserved but hidden.`)) return
            await authFetch(`/api/admin/database/tables/${tableId}/fields/${f.id}`, { method: "DELETE" })
            await refreshFields()
          })

          actionsTd.appendChild(editBtn)
          actionsTd.appendChild(archiveBtn)
          tbody.appendChild(tr)
        }
        tbl.appendChild(tbody)
        body.appendChild(tbl)
      }

      // Add Field button.
      const addBtn = document.createElement("button")
      addBtn.className = "gowiki-admin-btn gowiki-admin-btn-primary"
      addBtn.textContent = "Add Field"
      addBtn.style.marginTop = "12px"
      addBtn.addEventListener("click", async () => {
        await showDatabaseFieldEditModal(tableId, null)
        await refreshFields()
      })
      body.appendChild(addBtn)

      // Close button.
      const closeBtn = document.createElement("button")
      closeBtn.className = "gowiki-admin-btn"
      closeBtn.textContent = "Close"
      closeBtn.style.marginTop = "12px"
      closeBtn.style.marginLeft = "8px"
      closeBtn.addEventListener("click", () => close())
      body.appendChild(closeBtn)
    }

    renderFieldList(table.fields || [])
  })
}

async function showDatabaseFieldEditModal(tableId, existing) {
  const fieldTypes = [
    { value: "text", label: "Text" },
    { value: "integer", label: "Integer" },
    { value: "float", label: "Float" },
    { value: "boolean", label: "Boolean" },
    { value: "date", label: "Date" },
    { value: "datetime", label: "DateTime" },
    { value: "page_link", label: "Page Link" },
    { value: "enum", label: "Enum" },
    { value: "multi_enum", label: "Multi Enum" },
    { value: "auto_increment", label: "Auto Increment" },
    { value: "image", label: "Image" },
    { value: "color", label: "Color" },
    { value: "tag", label: "Tag" },
  ]

  return showAdminModal(existing ? `Edit Field: ${existing.name}` : "Add Field", (body, close, showError) => {
    const nameInput = adminFormField(body, "Name (lowercase, underscores)", "text", existing ? existing.name : "")
    if (existing) nameInput.readOnly = true
    const labelInput = adminFormField(body, "Label", "text", existing ? existing.label : "")
    const typeSelect = adminFormSelect(body, "Type", fieldTypes, existing ? existing.type : "text")
    if (existing) typeSelect.disabled = true
    const requiredSelect = adminFormSelect(body, "Required", [
      { value: "false", label: "No" },
      { value: "true", label: "Yes" },
    ], existing ? String(existing.required) : "false")
    const defaultInput = adminFormField(body, "Default Value", "text", existing ? existing.default_value : "")
    const orderInput = adminFormField(body, "Display Order", "number", existing ? String(existing.display_order) : "0")
    const placeholderInput = adminFormField(body, "Placeholder", "text", existing ? existing.placeholder : "")

    // Enum values section (shown for enum/multi_enum).
    const enumSection = document.createElement("div")
    enumSection.style.display = "none"
    enumSection.style.marginTop = "8px"

    const enumLabel = document.createElement("label")
    enumLabel.textContent = "Enum Values (one per line)"
    enumLabel.style.display = "block"
    enumLabel.style.fontWeight = "500"
    enumLabel.style.marginBottom = "4px"
    enumSection.appendChild(enumLabel)

    const enumArea = document.createElement("textarea")
    enumArea.rows = 4
    enumArea.style.width = "100%"
    enumArea.style.fontFamily = "monospace"
    if (existing && existing.enum_values) enumArea.value = existing.enum_values.join("\n")
    enumSection.appendChild(enumArea)
    body.appendChild(enumSection)

    // Tag table section (shown for tag type).
    const tagSection = document.createElement("div")
    tagSection.style.display = "none"
    tagSection.style.marginTop = "8px"

    const tagLabel = document.createElement("label")
    tagLabel.textContent = "Tag Table (name of table with label/icon/color columns)"
    tagLabel.style.display = "block"
    tagLabel.style.fontWeight = "500"
    tagLabel.style.marginBottom = "4px"
    tagSection.appendChild(tagLabel)

    const tagInput = document.createElement("input")
    tagInput.type = "text"
    tagInput.style.width = "100%"
    tagInput.placeholder = "e.g. tags"
    if (existing && existing.foreign_key) tagInput.value = existing.foreign_key
    tagSection.appendChild(tagInput)
    body.appendChild(tagSection)

    function updateEnumVisibility() {
      const t = typeSelect.value
      enumSection.style.display = (t === "enum" || t === "multi_enum") ? "block" : "none"
      tagSection.style.display = t === "tag" ? "block" : "none"
    }
    typeSelect.addEventListener("change", updateEnumVisibility)
    updateEnumVisibility()

    adminModalActions(body, close, async () => {
      const payload = {
        name: nameInput.value.trim(),
        label: labelInput.value.trim(),
        type: typeSelect.value,
        required: requiredSelect.value === "true",
        default_value: defaultInput.value.trim(),
        display_order: parseInt(orderInput.value) || 0,
        placeholder: placeholderInput.value.trim(),
      }
      if (!payload.name) { showError("Name is required"); return }

      // Add enum values if applicable.
      if (payload.type === "enum" || payload.type === "multi_enum") {
        payload.enum_values = enumArea.value.split("\n").map(v => v.trim()).filter(Boolean)
      }

      // Add foreign_key for tag type.
      if (payload.type === "tag") {
        payload.foreign_key = tagInput.value.trim()
      }

      const url = existing
        ? `/api/admin/database/tables/${tableId}/fields/${existing.id}`
        : `/api/admin/database/tables/${tableId}/fields`
      const method = existing ? "PUT" : "POST"
      const r = await authFetch(url, {
        method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      })
      if (r.ok) {
        close(true)
      } else {
        const err = await r.json().catch(() => ({}))
        showError(err.error || "Failed to save field")
      }
    }, existing ? "Save" : "Add")
  })
}

async function showDatabaseDataBrowser(tableName) {
  return showAdminModal(`Data: ${tableName}`, async (body, close) => {
    // Make this modal wide for data tables.
    body.closest(".gowiki-admin-modal").classList.add("gowiki-admin-modal-wide")

    let currentOffset = 0
    const limit = 50

    async function loadData() {
      body.innerHTML = '<div class="gowiki-admin-loading">Loading data...</div>'

      try {
        const schemaResp = await authFetch(`/api/database/${encodeURIComponent(tableName)}/schema`)
        if (!schemaResp.ok) {
          body.innerHTML = '<div class="gowiki-admin-error">Failed to load schema.</div>'
          return
        }
        const schema = await schemaResp.json()
        const fields = (schema.fields || []).filter(f => !f.archived_at)

        const resp = await authFetch(`/api/database/${encodeURIComponent(tableName)}/rows?limit=${limit}&offset=${currentOffset}`)
        if (!resp.ok) {
          body.innerHTML = '<div class="gowiki-admin-error">Failed to load data.</div>'
          return
        }
        const data = await resp.json()
        const rows = data.rows || []
        const total = data.total || 0

        body.innerHTML = ""

        // Export CSV button.
        const exportBtn = document.createElement("button")
        exportBtn.className = "gowiki-admin-btn"
        exportBtn.textContent = "Export CSV"
        exportBtn.style.marginBottom = "12px"
        exportBtn.addEventListener("click", () => {
          window.open(`/api/database/${encodeURIComponent(tableName)}/export/csv`, "_blank")
        })
        body.appendChild(exportBtn)

        if (rows.length === 0) {
          const empty = document.createElement("div")
          empty.style.color = "#636e72"
          empty.textContent = "No data."
          body.appendChild(empty)
        } else {
          const tbl = document.createElement("table")
          tbl.className = "gowiki-admin-table"

          const thead = document.createElement("thead")
          const headerRow = document.createElement("tr")
          headerRow.innerHTML = "<th>ID</th><th>Page</th>"
          for (const f of fields) {
            const th = document.createElement("th")
            th.textContent = f.label || f.name
            headerRow.appendChild(th)
          }
          headerRow.innerHTML += "<th>Actions</th>"
          thead.appendChild(headerRow)
          tbl.appendChild(thead)

          const tbody = document.createElement("tbody")
          for (const row of rows) {
            const tr = document.createElement("tr")
            tr.innerHTML = `<td>${row.id}</td><td>${row.page_path || ""}</td>`
            for (const f of fields) {
              const td = document.createElement("td")
              const val = row.fields?.[f.name]
              if (Array.isArray(val)) {
                td.textContent = val.join(", ")
              } else if (val != null && (f.type === "date" || f.type === "datetime") && typeof val === "string") {
                // Format ISO dates: strip time/timezone for date fields, keep datetime readable.
                if (f.type === "date") {
                  td.textContent = val.slice(0, 10)
                } else {
                  td.textContent = val.replace("T", " ").replace(/Z$/, "").replace(/\.\d+$/, "")
                }
              } else if (val != null && f.type === "text" && String(val).length > 60) {
                td.textContent = String(val)
                td.classList.add("gowiki-cell-wrap")
              } else {
                td.textContent = val != null ? String(val) : ""
              }
              td.contentEditable = "true"
              td.addEventListener("blur", async () => {
                const newVal = td.textContent.trim()
                await authFetch(`/api/database/${encodeURIComponent(tableName)}/rows/${row.id}`, {
                  method: "PUT",
                  headers: { "Content-Type": "application/json" },
                  body: JSON.stringify({ fields: { [f.name]: newVal } }),
                })
              })
              tr.appendChild(td)
            }

            const actionsTd = document.createElement("td")
            const delBtn = document.createElement("button")
            delBtn.className = "gowiki-admin-btn gowiki-admin-btn-sm gowiki-admin-btn-danger"
            delBtn.textContent = "Delete"
            delBtn.addEventListener("click", async () => {
              if (!confirm("Delete this row?")) return
              await authFetch(`/api/database/${encodeURIComponent(tableName)}/rows/${row.id}`, { method: "DELETE" })
              await loadData()
            })
            actionsTd.appendChild(delBtn)
            tr.appendChild(actionsTd)
            tbody.appendChild(tr)
          }
          tbl.appendChild(tbody)
          body.appendChild(tbl)
        }

        // Pagination.
        if (total > limit) {
          const pag = document.createElement("div")
          pag.style.display = "flex"
          pag.style.gap = "8px"
          pag.style.alignItems = "center"
          pag.style.marginTop = "8px"

          const prevBtn = document.createElement("button")
          prevBtn.className = "gowiki-admin-btn"
          prevBtn.textContent = "Previous"
          prevBtn.disabled = currentOffset === 0
          prevBtn.addEventListener("click", () => { currentOffset = Math.max(0, currentOffset - limit); loadData() })
          pag.appendChild(prevBtn)

          const info = document.createElement("span")
          info.textContent = `${currentOffset + 1}-${Math.min(currentOffset + limit, total)} of ${total}`
          pag.appendChild(info)

          const nextBtn = document.createElement("button")
          nextBtn.className = "gowiki-admin-btn"
          nextBtn.textContent = "Next"
          nextBtn.disabled = currentOffset + limit >= total
          nextBtn.addEventListener("click", () => { currentOffset += limit; loadData() })
          pag.appendChild(nextBtn)

          body.appendChild(pag)
        }

        // Close button.
        const closeBtn = document.createElement("button")
        closeBtn.className = "gowiki-admin-btn"
        closeBtn.textContent = "Close"
        closeBtn.style.marginTop = "12px"
        closeBtn.addEventListener("click", () => close())
        body.appendChild(closeBtn)
      } catch {
        body.innerHTML = '<div class="gowiki-admin-error">Failed to load data.</div>'
      }
    }

    await loadData()
  })
}

// ── Admin: Todo Tab ──────────────────────────────────

async function renderAdminTodoTab(container) {
  container.innerHTML = '<div class="gowiki-admin-loading">Loading tasks...</div>'

  // Filter state
  let filterStatus = ""
  let filterAssignee = ""
  let filterPriority = ""
  let filterPage = ""
  let cursor = ""

  async function loadAndRender() {
    const params = new URLSearchParams()
    if (filterStatus) params.set("status", filterStatus)
    if (filterAssignee) params.set("assignee", filterAssignee)
    if (filterPriority) params.set("priority", filterPriority)
    if (filterPage) params.set("page", filterPage)
    if (cursor) params.set("cursor", cursor)
    params.set("limit", "50")

    try {
      const resp = await authFetch("/api/plugin/todo/v1/tasks?" + params.toString())
      if (!resp.ok) {
        container.innerHTML = '<div class="gowiki-admin-error">Failed to load tasks.</div>'
        return
      }
      const data = await resp.json()
      const tasks = data.tasks || []
      const nextCursor = data.cursor || ""

      container.innerHTML = ""

      // Filter bar
      const filterBar = document.createElement("div")
      filterBar.className = "gowiki-admin-toolbar"
      filterBar.style.gap = "8px"
      filterBar.style.flexWrap = "wrap"

      const statusSelect = document.createElement("select")
      statusSelect.className = "gowiki-admin-input"
      statusSelect.style.width = "auto"
      for (const [val, label] of [["", "All statuses"], ["open", "Open"], ["in_progress", "In progress"], ["done", "Done"], ["cancelled", "Cancelled"]]) {
        const opt = document.createElement("option")
        opt.value = val
        opt.textContent = label
        if (val === filterStatus) opt.selected = true
        statusSelect.appendChild(opt)
      }
      statusSelect.addEventListener("change", () => { filterStatus = statusSelect.value; cursor = ""; loadAndRender() })
      filterBar.appendChild(statusSelect)

      const prioritySelect = document.createElement("select")
      prioritySelect.className = "gowiki-admin-input"
      prioritySelect.style.width = "auto"
      for (const [val, label] of [["", "All priorities"], ["urgent", "Urgent"], ["high", "High"], ["normal", "Normal"], ["low", "Low"]]) {
        const opt = document.createElement("option")
        opt.value = val
        opt.textContent = label
        if (val === filterPriority) opt.selected = true
        prioritySelect.appendChild(opt)
      }
      prioritySelect.addEventListener("change", () => { filterPriority = prioritySelect.value; cursor = ""; loadAndRender() })
      filterBar.appendChild(prioritySelect)

      const assigneeInput = document.createElement("input")
      assigneeInput.className = "gowiki-admin-input"
      assigneeInput.style.width = "140px"
      assigneeInput.placeholder = "Assignee..."
      assigneeInput.value = filterAssignee
      let assigneeTimer = null
      assigneeInput.addEventListener("input", () => {
        clearTimeout(assigneeTimer)
        assigneeTimer = setTimeout(() => { filterAssignee = assigneeInput.value.trim(); cursor = ""; loadAndRender() }, 400)
      })
      filterBar.appendChild(assigneeInput)

      const pageInput = document.createElement("input")
      pageInput.className = "gowiki-admin-input"
      pageInput.style.width = "160px"
      pageInput.placeholder = "Source page..."
      pageInput.value = filterPage
      let pageTimer = null
      pageInput.addEventListener("input", () => {
        clearTimeout(pageTimer)
        pageTimer = setTimeout(() => { filterPage = pageInput.value.trim(); cursor = ""; loadAndRender() }, 400)
      })
      filterBar.appendChild(pageInput)

      const refreshBtn = document.createElement("button")
      refreshBtn.className = "gowiki-admin-btn"
      refreshBtn.textContent = "Refresh"
      refreshBtn.addEventListener("click", () => { cursor = ""; loadAndRender() })
      filterBar.appendChild(refreshBtn)

      container.appendChild(filterBar)

      // Empty state
      if (tasks.length === 0) {
        const empty = document.createElement("div")
        empty.className = "gowiki-admin-empty"
        empty.textContent = cursor ? "No more tasks." : "No tasks found."
        container.appendChild(empty)
        return
      }

      // Table
      const table = document.createElement("table")
      table.className = "gowiki-admin-table"

      const thead = document.createElement("thead")
      const headerRow = document.createElement("tr")
      for (const col of ["Title", "Assignee", "Status", "Priority", "Due", "Source", "Created by", "Created"]) {
        const th = document.createElement("th")
        th.textContent = col
        headerRow.appendChild(th)
      }
      thead.appendChild(headerRow)
      table.appendChild(thead)

      const tbody = document.createElement("tbody")
      const today = new Date().toISOString().slice(0, 10)

      for (const task of tasks) {
        const tr = document.createElement("tr")

        // Title
        const tdTitle = document.createElement("td")
        if (task.source_page) {
          const a = document.createElement("a")
          a.href = task.source_page
          a.textContent = task.title
          a.style.color = "var(--gowiki-link-color, #2563eb)"
          tdTitle.appendChild(a)
        } else {
          tdTitle.textContent = task.title
        }
        tr.appendChild(tdTitle)

        // Assignee
        const tdAssignee = document.createElement("td")
        if (task.assignee && task.assignee.target) {
          tdAssignee.textContent = (task.assignee.type === "group" ? "@" : "") + task.assignee.target
        }
        tr.appendChild(tdAssignee)

        // Status
        const tdStatus = document.createElement("td")
        const statusBadge = document.createElement("span")
        statusBadge.textContent = (task.status || "open").replace("_", " ")
        statusBadge.style.cssText = "padding:2px 6px;border-radius:3px;font-size:0.85em;"
        const statusColors = { open: "#dbeafe", in_progress: "#fef3c7", done: "#d1fae5", cancelled: "#f3f4f6" }
        statusBadge.style.background = statusColors[task.status] || "#f3f4f6"
        tdStatus.appendChild(statusBadge)
        tr.appendChild(tdStatus)

        // Priority
        const tdPriority = document.createElement("td")
        if (task.priority && task.priority !== "normal") {
          const priBadge = document.createElement("span")
          priBadge.textContent = task.priority
          priBadge.style.cssText = "padding:2px 6px;border-radius:3px;font-size:0.85em;"
          const priColors = { urgent: "#fecaca", high: "#fed7aa", low: "#e5e7eb" }
          priBadge.style.background = priColors[task.priority] || ""
          tdPriority.appendChild(priBadge)
        } else {
          tdPriority.textContent = task.priority || ""
        }
        tr.appendChild(tdPriority)

        // Due date
        const tdDue = document.createElement("td")
        if (task.due_date) {
          tdDue.textContent = task.due_date
          if (task.due_date < today && task.status !== "done" && task.status !== "cancelled") {
            tdDue.style.color = "#dc2626"
            tdDue.style.fontWeight = "600"
          }
        }
        tr.appendChild(tdDue)

        // Source page
        const tdSource = document.createElement("td")
        if (task.source_page) {
          const a = document.createElement("a")
          a.href = task.source_page
          a.textContent = task.source_page
          a.style.color = "var(--gowiki-link-color, #2563eb)"
          a.style.fontSize = "0.9em"
          tdSource.appendChild(a)
        }
        tr.appendChild(tdSource)

        // Created by
        const tdCreator = document.createElement("td")
        tdCreator.textContent = task.created_by || ""
        tr.appendChild(tdCreator)

        // Created at
        const tdCreated = document.createElement("td")
        tdCreated.textContent = task.created_at ? new Date(task.created_at).toLocaleDateString() : ""
        tdCreated.style.fontSize = "0.9em"
        tr.appendChild(tdCreated)

        tbody.appendChild(tr)
      }
      table.appendChild(tbody)
      container.appendChild(table)

      // Pagination
      if (nextCursor) {
        const pager = document.createElement("div")
        pager.style.cssText = "margin-top:12px;text-align:center;"
        const nextBtn = document.createElement("button")
        nextBtn.className = "gowiki-admin-btn"
        nextBtn.textContent = "Load more"
        nextBtn.addEventListener("click", () => { cursor = nextCursor; loadAndRender() })
        pager.appendChild(nextBtn)
        container.appendChild(pager)
      }
    } catch {
      container.innerHTML = '<div class="gowiki-admin-error">Failed to load tasks.</div>'
    }
  }

  await loadAndRender()
}

async function bootstrap() {
  applyStyles(registry.getStyles())

  // Export mode: render page cleanly for PDF generation.
  const isExportMode = new URLSearchParams(window.location.search).get("export") === "pdf"
  if (isExportMode) {
    // Hide chrome, render page in view mode.
    document.getElementById("banner")?.remove()
    document.getElementById("left")?.remove()
    document.getElementById("footer")?.remove()
    actionsRoot.style.display = "none"
    document.body.classList.add("gowiki-export")

    let page
    try {
      page = await fetchPage(pagePath)
    } catch {
      document.body.setAttribute("data-export-ready", "true")
      return
    }
    if (page) {
      currentMarkdown = page.markdown
      currentDoc = markdownToPM(currentMarkdown, registry)
      mountReadOnlyView(contentRoot, currentMarkdown, "gowiki-view")
    }
    document.body.setAttribute("data-export-ready", "true")
    return
  }

  renderActions()

  // Non-blocking banner setup
  resolveSiteInfo()
  resolveLogo()
  initSearch()

  // Global keyboard shortcuts that work even when focus is in a property panel input.
  document.addEventListener("keydown", e => {
    // F11: toggle fullscreen
    if (e.key === "F11") {
      e.preventDefault()
      toggleFullscreen()
      return
    }
    // Escape: exit fullscreen (only when no modal is open)
    if (e.key === "Escape" && isFullscreen && !document.querySelector(".gowiki-link-modal-overlay, .gowiki-media-modal-overlay, .gowiki-admin-modal-overlay, .gowiki-login-overlay")) {
      e.preventDefault()
      toggleFullscreen()
      return
    }
    const isMod = e.metaKey || e.ctrlKey
    if (!isMod) return
    // CMD+E: enter edit mode from view mode.
    if (e.key === "e" && !e.shiftKey && mode === "view" && currentUser) {
      e.preventDefault()
      void enterEditMode(true)
      return
    }
    if (mode !== "edit") return
    if (e.key === "s" && e.shiftKey) {
      e.preventDefault()
      void publishDraft()
    } else if (e.key === "s" && !e.shiftKey) {
      e.preventDefault()
      void saveDraftExplicit()
    } else if (e.key === "h" && !e.shiftKey && !e.altKey && editorView) {
      // CMD+H heading toggle — only when in visual mode and focus is NOT
      // inside ProseMirror (PM's own keymap already handles it there;
      // handling it again here would double-toggle).
      if (editMode === "visual" && !editorView.dom.contains(document.activeElement)) {
        e.preventDefault()
        const state = editorView.state
        const node = state.selection.$from.parent
        const schema = state.schema
        if (node.type === schema.nodes.heading) {
          setBlockType(schema.nodes.paragraph)(state, editorView.dispatch)
        } else {
          setBlockType(schema.nodes.heading, { level: 2 })(state, editorView.dispatch)
        }
        editorView.focus()
      }
    }
  })

  // Sitemap page: virtual route
  if (pagePath === "_sitemap") {
    await checkAuth()
    await renderSitemapPage()
    fetchAndMountZone("sidebar", sidebarRoot, "gowiki-sidebar").then(v => {
      sidebarView = v
    })
    fetchAndMountZone("footer", footerRoot, "gowiki-footer").then(v => {
      footerView = v
    })
    return
  }

  // Admin page: must await auth before rendering
  if (pagePath === "_admin") {
    await checkAuth()
    if (!currentUser || !currentUser.is_admin) {
      contentRoot.innerHTML = ""
      const msg = document.createElement("div")
      msg.style.padding = "40px 20px"
      msg.style.textAlign = "center"
      msg.style.color = "#666"
      msg.style.fontSize = "16px"
      msg.textContent = currentUser
        ? "Access denied. Admin privileges required."
        : "Please log in with an admin account to access this page."
      contentRoot.appendChild(msg)
      return
    }
    renderAdminPage()
    // Still mount sidebar and footer
    fetchAndMountZone("sidebar", sidebarRoot, "gowiki-sidebar").then(v => {
      sidebarView = v
    })
    fetchAndMountZone("footer", footerRoot, "gowiki-footer").then(v => {
      footerView = v
    })
    return
  }

  await checkAuth()

  // Check for search query in URL.
  const searchQuery = new URLSearchParams(window.location.search).get("q")
  if (searchQuery) {
    const input = document.getElementById("search-input")
    if (input) input.value = searchQuery
    await renderSearchResultsPage(searchQuery)
    // Still mount sidebar and footer.
    fetchAndMountZone("sidebar", sidebarRoot, "gowiki-sidebar").then(v => {
      sidebarView = v
    })
    fetchAndMountZone("footer", footerRoot, "gowiki-footer").then(v => {
      footerView = v
    })
    return
  }

  let page
  try {
    page = await fetchPage(pagePath)
  } catch (err) {
    if (err instanceof AccessDeniedError) {
      clearContent()
      actionsRoot.innerHTML = ""
      const banner = document.createElement("div")
      banner.className = "gowiki-access-denied"
      const msg = document.createElement("div")
      msg.textContent = `Access to ${pageDisplayPath} is forbidden.`
      banner.appendChild(msg)
      if (!currentUser) {
        const hint = document.createElement("div")
        hint.style.marginTop = "8px"
        const loginLink = document.createElement("a")
        loginLink.href = "#"
        loginLink.textContent = "Log in"
        loginLink.addEventListener("click", (e) => {
          e.preventDefault()
          showLoginDialog(() => {
            window.location.reload()
          })
        })
        hint.appendChild(loginLink)
        hint.appendChild(document.createTextNode(" to access this page."))
        banner.appendChild(hint)
      }
      contentRoot.appendChild(banner)
      // Still mount sidebar and footer.
      fetchAndMountZone("sidebar", sidebarRoot, "gowiki-sidebar").then(v => {
        sidebarView = v
      })
      fetchAndMountZone("footer", footerRoot, "gowiki-footer").then(v => {
        footerView = v
      })
      return
    }
    throw err
  }
  console.log("[gowiki] bootstrap: page =", page ? "exists" : "null", "pagePath =", pagePath)
  if (page) {
    currentMarkdown = page.markdown
    isNewPage = false
    hasTemplate = false
    currentPageVersion = page.meta?.version || 0
    currentPageMeta = page.meta || null
    // Capture lock/draft info from page response.
    pageLockInfo = null
    if (page.locked_by) {
      pageLockInfo = { locked_by: page.locked_by, is_draft: !!page.is_draft }
    }
    // Populate media version cache so the property panel dropdown knows the max version.
    if (page.media_versions) {
      for (const [absPath, ver] of Object.entries(page.media_versions)) {
        window.__gowikiUpdateMediaVersionCache(absPath, ver)
      }
    }
  } else {
    // Page doesn't exist — try to fetch a template.
    let templateMarkdown = null
    try {
      const tmplUrl = `/api/template/${encodePagePath(pagePath)}`
      console.log("[gowiki] fetching template:", tmplUrl)
      const tmplResp = await fetch(tmplUrl)
      console.log("[gowiki] template response:", tmplResp.status)
      if (tmplResp.ok) {
        const tmplData = await tmplResp.json()
        console.log("[gowiki] template data:", tmplData)
        templateMarkdown = tmplData.markdown
      }
    } catch (err) {
      console.error("[gowiki] template fetch error:", err)
    }
    currentMarkdown = templateMarkdown || defaultMarkdown
    hasTemplate = !!templateMarkdown
    isNewPage = true
    currentPageVersion = 0
    currentPageMeta = null
    pageLockInfo = null
  }

  // Normalize URL: strip /index suffix and ensure namespace index pages end with /
  const rawPathname = window.location.pathname
  if (rawPathname === "/index" || rawPathname === "/index/") {
    window.history.replaceState(null, "", "/" + window.location.search + window.location.hash)
  } else if (/\/index\/?$/.test(rawPathname)) {
    const canonical = rawPathname.replace(/\/index\/?$/, "/")
    window.history.replaceState(null, "", canonical + window.location.search + window.location.hash)
  }

  // If page is a namespace index, ensure URL ends with / so relative links resolve correctly.
  if (page && page.is_namespace_index) {
    const currentPathname = window.location.pathname
    if (!currentPathname.endsWith("/")) {
      window.history.replaceState(null, "", "/" + pagePath + "/" + window.location.search + window.location.hash)
    }
  }

  currentDoc = markdownToPM(currentMarkdown, registry)
  setMode("view")

  // Auto-view a specific version when ?v=N is present.
  const versionParam = new URLSearchParams(window.location.search).get("v")
  if (versionParam && !isNewPage) {
    const vNum = parseInt(versionParam, 10)
    if (vNum > 0) {
      // Clean the URL so a refresh doesn't re-trigger.
      window.history.replaceState(null, "", window.location.pathname)
      void viewVersion(vNum)
    }
  }

  // Auto-enter edit mode only when the user explicitly chose "Create new page".
  // Navigating to a non-existing page by accident should not auto-create it.
  const actionParam = new URLSearchParams(window.location.search).get("action")
  if (isNewPage && currentUser && actionParam === "create") {
    // Clean the URL so a refresh doesn't re-trigger auto-edit.
    window.history.replaceState(null, "", window.location.pathname)
    void enterEditMode(true)
  }

  // Fetch and mount sidebar and footer as read-only views (non-blocking)
  fetchAndMountZone("sidebar", sidebarRoot, "gowiki-sidebar").then(v => {
    sidebarView = v
  })
  fetchAndMountZone("footer", footerRoot, "gowiki-footer").then(v => {
    footerView = v
  })
}

bootstrap().catch(err => {
  console.error("Failed to start frontend", err)
  setStatus("Startup failed")
})
