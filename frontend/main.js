import { EditorState, TextSelection } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { DOMSerializer } from "prosemirror-model"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { keymap } from "prosemirror-keymap"
import { baseKeymap, setBlockType } from "prosemirror-commands"
import { history } from "prosemirror-history"
import { menuBar, MenuItem, Dropdown, icons } from "prosemirror-menu"
import { buildMenuItems } from "prosemirror-example-setup"
import { splitListItem, sinkListItem, liftListItem } from "prosemirror-schema-list"
import { markdownToPM } from "./compiler/markdown_to_pm.ts"
import { pmToMarkdown } from "./compiler/pm_to_markdown.ts"
import { buildRegistry } from "./compiler/build_registry.ts"
import { isPropertiesPanelEnabled } from "./compiler/core_ui.ts"
import { openMediaManager } from "./media_manager.js"

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
const pageNamespace = pagePath.includes("/")
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

let mode = "view"
let editMode = "visual"
let currentMarkdown = defaultMarkdown
let currentDoc = markdownToPM(defaultMarkdown, registry)
let editBaselineMarkdown = defaultMarkdown
let editorView = null
let rawEditor = null
let statusText = ""
let isNewPage = false
let viewView = null
let sidebarView = null
let footerView = null

const menu = buildMenuItems(schema)
menu.fullMenu = menu.fullMenu
  .map(group =>
    group.filter(item =>
      item.spec?.title !== "Add or remove link" &&
      item.options?.label !== "Type..." &&
      item.options?.label !== "Insert"
    )
  )
  .filter(group => group.length > 0)

const headingItems = schema.nodes.heading
  ? [1, 2, 3, 4, 5].map(level => {
      const cmd = setBlockType(schema.nodes.heading, { level })
      return new MenuItem({
        label: `H${level}`,
        title: `Change to heading ${level}`,
        run: cmd,
        enable: state => cmd(state),
        active: state => {
          const { $from, to, node } = state.selection
          if (node) {
            return node.type === schema.nodes.heading && node.attrs.level === level
          }
          return to <= $from.end() &&
            $from.parent.type === schema.nodes.heading &&
            $from.parent.attrs.level === level
        },
      })
    })
  : []

if (headingItems.length > 0) {
  const headingMenu = new Dropdown(headingItems, {
    label: "H#",
    title: "Headings",
    class: "gowiki-menu-headings",
  })
  if (menu.fullMenu.length === 0) {
    menu.fullMenu.push([headingMenu])
  } else {
    menu.fullMenu[0].unshift(headingMenu)
  }
}

const tableCommands = new Map()
const extraCommandGroups = []
let togglePropertiesCommand = null

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

function svgIcon(path, alt) {
  const img = document.createElement("img")
  img.src = path
  img.alt = alt
  img.width = 14
  img.height = 14
  img.className = "gowiki-menu-icon"
  return { dom: img }
}

function canInsertNode(state, nodeType) {
  const $from = state.selection.$from
  for (let d = $from.depth; d >= 0; d--) {
    const index = $from.index(d)
    if ($from.node(d).canReplaceWith(index, index, nodeType)) return true
  }
  return false
}


registry.onCommand((namespace, name, cmd) => {
  if (namespace === "table") {
    tableCommands.set(name, cmd)
    return
  }

  if (namespace === "ui" && name === "properties.toggle") {
    togglePropertiesCommand = cmd
    return
  }

  const label = `${namespace}:${name}`
  extraCommandGroups.push([
    new MenuItem({
      label,
      title: label,
      run: cmd,
      enable: state => cmd(state),
    }),
  ])
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

const hrMenuItem = new MenuItem({
  icon: {
    text: "—",
    css: "display:inline-block; transform:scaleX(2.4); font-weight:700; line-height:1;",
  },
  title: "Insert horizontal rule",
  run: (state, dispatch) => {
    const hr = state.schema.nodes.horizontal_rule
    if (!hr) return false
    if (!dispatch) return true
    dispatch(state.tr.replaceSelectionWith(hr.create()).scrollIntoView())
    return true
  },
  enable: state => {
    const hr = state.schema.nodes.horizontal_rule
    return Boolean(hr && canInsertNode(state, hr))
  },
})

const linkMenuItem = new MenuItem({
  icon: icons.link,
  title: "Set or edit link",
  run: setExternalLinkCommand(),
  enable: state => Boolean(state.schema.marks.link),
  active: state => isSelectionInLink(state),
})

insertMenuItemAfterTitle(menu.fullMenu, "Toggle code font", linkMenuItem)

if (menu.fullMenu.length === 0) {
  menu.fullMenu.push([hrMenuItem])
} else {
  menu.fullMenu[0].splice(1, 0, hrMenuItem)
}

if (togglePropertiesCommand) {
  const propertiesMenuItem = new MenuItem({
    icon: svgIcon("/icons/tools.svg", "Properties"),
    title: "Toggle properties",
    run: togglePropertiesCommand,
    enable: state => Boolean(togglePropertiesCommand?.(state)),
    active: state => isPropertiesPanelEnabled(state),
    class: "gowiki-menu-properties-toggle",
  })
  if (menu.fullMenu.length === 0) {
    menu.fullMenu.push([propertiesMenuItem])
  } else {
    menu.fullMenu[0].unshift(propertiesMenuItem)
  }
}

if (tableCommands.size > 0) {
  const order = [
    "insert",
    "row.addAfter",
    "column.addAfter",
    "row.delete",
    "column.delete",
  ]
  const labels = {
    insert: "Insert table",
    "row.addAfter": "Add row",
    "column.addAfter": "Add column",
    "row.delete": "Delete row",
    "column.delete": "Delete column",
  }

  const items = order
    .filter(name => tableCommands.has(name))
    .map(name =>
      new MenuItem({
        label: labels[name] ?? name,
        title: labels[name] ?? name,
        run: tableCommands.get(name),
        enable: state => Boolean(tableCommands.get(name)?.(state)),
      })
    )

  if (items.length > 0) {
    const tableMenu = new Dropdown(items, {
      label: " ",
      title: "Table",
      class: "gowiki-menu-table",
    })
    menu.fullMenu.splice(2, 0, [tableMenu])
  }
}

for (const group of extraCommandGroups) {
  menu.fullMenu.push(group)
}

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

function splitPathParts(raw) {
  return String(raw ?? "")
    .split("/")
    .filter(Boolean)
}

function buildMediaReferencePath(currentNamespace, mediaPath) {
  const from = splitPathParts(currentNamespace)
  const to = splitPathParts(mediaPath)

  let idx = 0
  while (idx < from.length && idx < to.length && from[idx] === to[idx]) {
    idx += 1
  }

  const upCount = from.length - idx
  const down = to.slice(idx).join("/")

  if (upCount <= 2) {
    if (upCount === 0) return "./" + down
    return "../".repeat(upCount) + down
  }

  return "/" + to.join("/")
}

function mediaLabelFromPath(mediaPath) {
  const parts = splitPathParts(mediaPath)
  return parts[parts.length - 1] ?? "file"
}

function insertMediaReference(kind, mediaEntry) {
  if (!editorView || mode !== "edit" || editMode !== "visual") return

  const target = buildMediaReferencePath(pageNamespace, mediaEntry.path)
  const label = mediaLabelFromPath(mediaEntry.path)
  const state = editorView.state
  const tr = state.tr

  if (kind === "image" && state.schema.nodes.image) {
    const imageNode = state.schema.nodes.image.create({
      src: target,
      alt: label,
      title: null,
    })
    tr.replaceSelectionWith(imageNode, false)
    editorView.dispatch(tr.scrollIntoView())
    editorView.focus()
    setStatus("Inserted image " + target)
    return
  }

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
    autoResizeRawEditor(rawEditor)
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
    appRoot.classList.add("gowiki-editing")
    renderEdit(editMode)
  } else {
    appRoot.classList.remove("gowiki-editing")
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
  if (viewView) {
    viewView.destroy()
    viewView = null
  }
  rawEditor = null
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
  const scroller = document.querySelector("#main") || contentRoot
  if (!(scroller instanceof HTMLElement)) return false

  const menu = view.dom.parentElement?.querySelector(".ProseMirror-menubar")
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
  return new EditorView(wrapper, {
    state,
    editable: () => false,
  })
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
}

function autoResizeRawEditor(editorEl) {
  editorEl.style.height = "auto"
  editorEl.style.height = `${Math.max(editorEl.scrollHeight, 360)}px`
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

function rawSetHeadingLevel(textarea, level) {
  const { lineStart, lineEnd } = rawGetCurrentLineRange(textarea)
  const val = textarea.value
  const line = val.substring(lineStart, lineEnd)

  // Strip any existing heading prefix
  const stripped = line.replace(/^#{1,6}\s*/, "")
  const newPrefix = "#".repeat(level) + " "

  // If line already has this exact heading level, toggle it off
  const existingMatch = line.match(/^(#{1,6})\s/)
  const alreadyThisLevel = existingMatch && existingMatch[1].length === level

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

function buildRawMenubar(textarea) {
  const bar = document.createElement("div")
  bar.className = "gowiki-raw-menubar"

  function addButton(label, title, onClick) {
    const btn = document.createElement("span")
    btn.className = "gowiki-raw-menuitem"
    btn.textContent = label
    btn.title = title
    btn.addEventListener("mousedown", e => {
      e.preventDefault() // prevent textarea blur
      onClick()
    })
    bar.appendChild(btn)
    return btn
  }

  function addSeparator() {
    const sep = document.createElement("span")
    sep.className = "gowiki-raw-menusep"
    bar.appendChild(sep)
  }

  // Heading dropdown
  const headingWrap = document.createElement("span")
  headingWrap.className = "gowiki-raw-menuitem gowiki-raw-menu-dropdown-wrap"
  headingWrap.title = "Headings"
  headingWrap.textContent = "H#"
  const headingMenu = document.createElement("div")
  headingMenu.className = "gowiki-raw-dropdown-menu"
  for (let level = 1; level <= 5; level++) {
    const item = document.createElement("div")
    item.className = "gowiki-raw-dropdown-item"
    item.textContent = `H${level}`
    item.addEventListener("mousedown", e => {
      e.preventDefault()
      headingMenu.style.display = "none"
      rawSetHeadingLevel(textarea, level)
    })
    headingMenu.appendChild(item)
  }
  headingWrap.appendChild(headingMenu)
  headingWrap.addEventListener("mousedown", e => {
    e.preventDefault()
    const isOpen = headingMenu.style.display === "block"
    headingMenu.style.display = isOpen ? "none" : "block"
  })
  bar.appendChild(headingWrap)

  // Close heading dropdown when clicking elsewhere
  document.addEventListener("mousedown", e => {
    if (!headingWrap.contains(e.target)) {
      headingMenu.style.display = "none"
    }
  })

  addSeparator()

  // HR
  const hrBtn = addButton("---", "Insert horizontal rule", () => {
    rawInsertHorizontalRule(textarea)
  })
  hrBtn.style.fontWeight = "700"
  hrBtn.style.letterSpacing = "-1px"

  addSeparator()

  // Inline formatting
  addButton("B", "Bold", () => rawWrapSelection(textarea, "**", "**"))
    .style.fontWeight = "700"
  addButton("I", "Italic", () => rawWrapSelection(textarea, "*", "*"))
    .style.fontStyle = "italic"
  addButton("</>", "Inline code", () => rawWrapSelection(textarea, "`", "`"))

  addSeparator()

  // Link
  addButton("Link", "Insert or edit link", () => {
    void rawInsertLink(textarea)
  })

  addSeparator()

  // Block actions
  addButton("UL", "Unordered list", () => rawToggleLinePrefix(textarea, "- "))
  addButton("OL", "Ordered list", () => rawToggleLinePrefix(textarea, "1. "))
  addButton("CB", "Code block", () => rawInsertCodeBlock(textarea))

  addSeparator()

  // Save
  addButton("Save", "Save page", () => {
    void saveAndMaybeSwitch(false)
  })

  return bar
}

function renderRawEdit() {
  const wrapper = document.createElement("div")
  wrapper.className = "gowiki-raw-wrapper"

  const editorEl = document.createElement("textarea")
  editorEl.id = "gowiki-raw-editor"
  editorEl.className = "gowiki-raw-editor"
  editorEl.value = currentMarkdown

  const menubar = buildRawMenubar(editorEl)
  wrapper.appendChild(menubar)
  wrapper.appendChild(editorEl)

  editorEl.addEventListener("input", () => {
    autoResizeRawEditor(editorEl)
  })

  editorEl.addEventListener("blur", () => {
    if (mode !== "edit" || editMode !== "raw") return
    try {
      const normalized = normalizeMarkdownForStorage(editorEl.value)
      applyNormalizedEditState(normalized)
    } catch {
      // Keep invalid in-progress raw text unchanged.
    }
  })

  contentRoot.appendChild(wrapper)
  autoResizeRawEditor(editorEl)
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
    Backspace: backspaceEmptyListItemCommand(),
    Tab: tabKeyCommand("in"),
    "Shift-Tab": tabKeyCommand("out"),
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
        floating: false,
      }),
    ],
  })

  editorView = new EditorView(editorEl, {
    state,
    handleScrollToSelection(view) {
      return scrollSelectionIntoContentView(view)
    },
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
    pathInput.placeholder = "namespace/page-name"

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
      window.location.href = "/" + pagePath
    }
  })
}

function renderActions() {
  actionsRoot.innerHTML = ""

  const pageLabel = document.createElement("div")
  pageLabel.className = "gowiki-status"
  pageLabel.textContent = isNewPage
    ? `Page: ${pageDisplayPath} (new)`
    : `Page: ${pageDisplayPath}`
  actionsRoot.appendChild(pageLabel)

  if (mode === "edit") {
    const editModeLabel = document.createElement("div")
    editModeLabel.className = "gowiki-status"
    editModeLabel.textContent = `Edit mode: ${editMode}`
    actionsRoot.appendChild(editModeLabel)

    if (editMode === "visual") {
      actionsRoot.appendChild(
        makeActionButton("Media manager", () => {
          openMediaManager(
            pageNamespace,
            text => setStatus(text),
            (kind, entry) => insertMediaReference(kind, entry)
          )
        })
      )
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
    actionsRoot.appendChild(
      makeActionButton("New page", () => {
        void promptNewPage()
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
    isNewPage = false
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
  if (page) {
    currentMarkdown = page.markdown
    isNewPage = false
  } else {
    currentMarkdown = defaultMarkdown
    isNewPage = true
  }
  currentDoc = markdownToPM(currentMarkdown, registry)
  setMode("view")

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
