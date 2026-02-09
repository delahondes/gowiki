import { EditorState, TextSelection } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { DOMSerializer } from "prosemirror-model"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { keymap } from "prosemirror-keymap"
import { baseKeymap } from "prosemirror-commands"
import { history } from "prosemirror-history"
import { menuBar, MenuItem, icons } from "prosemirror-menu"
import { buildMenuItems } from "prosemirror-example-setup"
import { splitListItem } from "prosemirror-schema-list"
import { markdownToPM } from "./compiler/markdown_to_pm.ts"
import { pmToMarkdown } from "./compiler/pm_to_markdown.ts"
import { buildRegistry } from "./compiler/build_registry.ts"

const registry = buildRegistry(basicSchema)
const schema = registry.buildSchema()
registry.bindSchema(schema)

function resolvePagePathFromLocation(loc) {
  const path = decodeURIComponent(loc.pathname || "/")
  if (path === "/") return "index"
  const trimmed = path.replace(/^\/+|\/+$/g, "")
  if (!trimmed) return "index"
  return trimmed
}

const pagePath = resolvePagePathFromLocation(window.location)
const pageDisplayPath = pagePath === "index" ? "/" : `/${pagePath}`
const defaultMarkdown = `
## Gowiki

This is editable text.

- One
- Two
`

const contentRoot = document.querySelector("#content")
const actionsRoot = document.querySelector("#actions")

let mode = "view"
let editMode = "visual"
let currentMarkdown = defaultMarkdown
let currentDoc = markdownToPM(defaultMarkdown, registry)
let editBaselineMarkdown = defaultMarkdown
let editorView = null
let rawEditor = null
let statusText = ""

const menu = buildMenuItems(schema)
menu.fullMenu = menu.fullMenu
  .map(group =>
    group.filter(item => item.spec?.title !== "Add or remove link")
  )
  .filter(group => group.length > 0)

function insertMenuItemAfterTitle(menuGroups, title, item) {
  for (const group of menuGroups) {
    const idx = group.findIndex(menuItem => menuItem.spec?.title === title)
    if (idx >= 0) {
      group.splice(idx + 1, 0, item)
      return
    }
  }
  if (menuGroups.length === 0) {
    menuGroups.push([item])
    return
  }
  menuGroups[0].push(item)
}

registry.onCommand((namespace, name, cmd) => {
  const label = `${namespace}:${name}`
  const item = new MenuItem({
    label,
    title: label,
    run: cmd,
    enable: state => cmd(state),
  })
  menu.fullMenu.push([item])
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
  if (/^(\/(?!\/)|\.\/|\.\.\/)\S*$/.test(target)) {
    return { ok: true, normalized: target, kind: "internal" }
  }
  return {
    ok: false,
    error:
      "Use http://, https://, or an internal path starting with '/', './', or '../'.",
  }
}

function defaultLinkTextForTarget(target) {
  if (/^https?:\/\//i.test(target)) return target
  const pathOnly = target.split(/[?#]/)[0]
  const clean = pathOnly.replace(/\/+$/, "")
  const parts = clean.split("/").filter(Boolean).filter(p => p !== "." && p !== "..")
  return parts[parts.length - 1] ?? "index"
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

function isSelectionInLink(state) {
  const linkType = state.schema.marks.link
  if (!linkType) return false
  const { from, to, empty } = state.selection
  if (empty) {
    return Boolean(findLinkRangeAtCursor(state))
  }
  return state.doc.rangeHasMark(from, to, linkType)
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
      tr.setSelection(TextSelection.create(tr.doc, markFrom, markTo))
      setStatus("Link updated")
      activeDispatch(tr.scrollIntoView())
    })
    return true
  }
}

const linkMenuItem = new MenuItem({
  icon: icons.link,
  title: "Set or edit link",
  run: setExternalLinkCommand(),
  enable: state => Boolean(state.schema.marks.link),
  active: state => isSelectionInLink(state),
})

insertMenuItemAfterTitle(menu.fullMenu, "Toggle code font", linkMenuItem)

function applyStyles(styles) {
  for (const { id, css } of styles) {
    const styleId = `gowiki-style-${id}`
    if (document.getElementById(styleId)) continue
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

async function fetchPage(path) {
  const resp = await fetch(`/api/pages/${encodePagePath(path)}`)
  if (resp.status === 404) return null
  if (!resp.ok) {
    throw new Error(`Failed to load page ${path}: ${resp.status}`)
  }
  return await resp.json()
}

async function savePage(path, markdown) {
  const resp = await fetch(`/api/pages/${encodePagePath(path)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ markdown }),
  })
  if (!resp.ok) {
    throw new Error(`Failed to save page ${path}: ${resp.status}`)
  }
  return await resp.json()
}

function normalizeMarkdownForStorage(markdown) {
  const doc = markdownToPM(markdown, registry)
  const normalizedMarkdown = pmToMarkdown(doc, registry)
  return {
    markdown: normalizedMarkdown,
    doc,
    changed: normalizedMarkdown !== markdown,
  }
}

function applyNormalizedEditState(normalized, refreshVisual = false) {
  currentMarkdown = normalized.markdown
  currentDoc = normalized.doc

  if (mode !== "edit") return

  if (editMode === "raw" && rawEditor) {
    if (rawEditor.value !== normalized.markdown) {
      rawEditor.value = normalized.markdown
    }
    return
  }

  if (editMode === "visual" && editorView && refreshVisual && normalized.changed) {
    renderEdit("visual")
  }
}

function setStatus(text) {
  statusText = text
  renderActions()
}

function setMode(nextMode) {
  if (mode !== "edit" && nextMode === "edit") {
    editBaselineMarkdown = currentMarkdown
  }

  if (mode === "edit" && nextMode !== "edit") {
    editBaselineMarkdown = currentMarkdown
  }

  mode = nextMode
  if (mode === "edit") {
    renderEdit(editMode)
  } else {
    renderView()
  }
  renderActions()
}

function setEditMode(nextEditMode) {
  if (mode !== "edit") return
  if (nextEditMode === editMode) return

  let markdown = currentMarkdown
  if (editMode === "visual" && editorView) {
    markdown = pmToMarkdown(editorView.state.doc, registry)
  } else if (editMode === "raw" && rawEditor) {
    markdown = rawEditor.value
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
  rawEditor = null
  contentRoot.innerHTML = ""
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

function renderView() {
  clearContent()
  const wrapper = document.createElement("div")
  wrapper.className = "gowiki-view"
  const root = document.createElement("div")
  root.className = "ProseMirror"
  const serializer = DOMSerializer.fromSchema(schema)
  root.appendChild(serializer.serializeFragment(currentDoc.content))
  wrapper.appendChild(root)
  contentRoot.appendChild(wrapper)
}

function renderRawEdit() {
  const editorEl = document.createElement("textarea")
  editorEl.id = "gowiki-raw-editor"
  editorEl.className = "gowiki-raw-editor"
  editorEl.value = currentMarkdown
  editorEl.addEventListener("blur", () => {
    if (mode !== "edit" || editMode !== "raw") return
    try {
      const normalized = normalizeMarkdownForStorage(editorEl.value)
      applyNormalizedEditState(normalized)
    } catch {
      // Keep invalid in-progress raw text unchanged.
    }
  })
  contentRoot.appendChild(editorEl)
  rawEditor = editorEl
}

function renderEdit(nextEditMode) {
  clearContent()
  if (nextEditMode === "raw") {
    renderRawEdit()
    return
  }

  const editorEl = document.createElement("div")
  editorEl.id = "gowiki-editor"
  contentRoot.appendChild(editorEl)

  const listKeymap = keymap({
    Enter: splitListItem(schema.nodes.list_item),
    "Alt-Enter": insertHardBreakCommand(),
  })

  const state = EditorState.create({
    doc: currentDoc,
    schema,
    plugins: [
      listKeymap,
      history(),
      keymap(baseKeymap),
      ...registry.getEditorPlugins(),
      menuBar({
        content: menu.fullMenu,
      }),
    ],
  })

  editorView = new EditorView(editorEl, {
    state,
    handleDOMEvents: {
      blur(view) {
        if (mode !== "edit" || editMode !== "visual") return false
        try {
          const serialized = pmToMarkdown(view.state.doc, registry)
          const normalized = normalizeMarkdownForStorage(serialized)
          applyNormalizedEditState(normalized, true)
        } catch {
          // Keep edit session live while user content is in progress.
        }
        return false
      },
    },
  })
}

function makeActionButton(label, onClick) {
  const btn = document.createElement("button")
  btn.type = "button"
  btn.className = "gowiki-action-btn"
  btn.textContent = label
  btn.addEventListener("click", onClick)
  return btn
}

function renderActions() {
  actionsRoot.innerHTML = ""

  const pageLabel = document.createElement("div")
  pageLabel.className = "gowiki-status"
  pageLabel.textContent = `Page: ${pageDisplayPath}`
  actionsRoot.appendChild(pageLabel)

  if (mode === "edit") {
    const editModeLabel = document.createElement("div")
    editModeLabel.className = "gowiki-status"
    editModeLabel.textContent = `Edit mode: ${editMode}`
    actionsRoot.appendChild(editModeLabel)

    if (editMode === "visual") {
      actionsRoot.appendChild(
        makeActionButton("Switch to raw", () => {
          setEditMode("raw")
        })
      )
    } else {
      actionsRoot.appendChild(
        makeActionButton("Switch to visual", () => {
          setEditMode("visual")
        })
      )
    }

    actionsRoot.appendChild(
      makeActionButton("Save & continue", () => {
        void saveAndMaybeSwitch(false)
      })
    )
    actionsRoot.appendChild(
      makeActionButton("Save", () => {
        void saveAndMaybeSwitch(true)
      })
    )
    actionsRoot.appendChild(
      makeActionButton("Cancel", () => {
        cancelEdit()
      })
    )
  } else {
    actionsRoot.appendChild(
      makeActionButton("Edit", () => {
        setMode("edit")
      })
    )
  }

  const status = document.createElement("div")
  status.className = "gowiki-status"
  status.textContent = statusText
  actionsRoot.appendChild(status)
}

function cancelEdit() {
  if (mode !== "edit") return
  currentMarkdown = editBaselineMarkdown
  try {
    currentDoc = markdownToPM(currentMarkdown, registry)
  } catch (err) {
    console.error("Cancel failed while rebuilding document", err)
    setStatus("Cancel failed")
    return
  }
  setStatus("Edit cancelled")
  setMode("view")
}

async function saveAndMaybeSwitch(toView) {
  if (mode !== "edit") return
  try {
    let markdown = currentMarkdown

    if (editMode === "visual") {
      if (!editorView) return
      markdown = pmToMarkdown(editorView.state.doc, registry)
    } else {
      if (!rawEditor) return
      markdown = rawEditor.value
    }

    const normalized = normalizeMarkdownForStorage(markdown)
    await savePage(pagePath, normalized.markdown)
    applyNormalizedEditState(
      normalized,
      !toView && mode === "edit" && editMode === "visual"
    )
    editBaselineMarkdown = normalized.markdown
    setStatus(`Saved ${new Date().toLocaleTimeString()}`)
    if (toView) {
      setMode("view")
    }
  } catch (err) {
    console.error("Save failed", err)
    setStatus("Save failed")
  }
}

async function bootstrap() {
  applyStyles(registry.getStyles())
  renderActions()
  const page = await fetchPage(pagePath)
  currentMarkdown = page?.markdown ?? defaultMarkdown
  currentDoc = markdownToPM(currentMarkdown, registry)
  setMode("view")
}

bootstrap().catch(err => {
  console.error("Failed to start frontend", err)
  setStatus("Startup failed")
})
