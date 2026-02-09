import { EditorState } from "prosemirror-state"
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

const pagePath =
  new URLSearchParams(window.location.search).get("page") ?? "home"
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

function findLinkRangeAtCursor(state) {
  const linkType = state.schema.marks.link
  if (!linkType) return null
  if (!state.selection.empty) return null

  const $from = state.selection.$from
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
  return (state, dispatch) => {
    const linkType = state.schema.marks.link
    if (!linkType) return false

    let target = null
    let currentHref = ""

    const linkAtCursor = findLinkRangeAtCursor(state)
    if (linkAtCursor) {
      target = { from: linkAtCursor.from, to: linkAtCursor.to }
      currentHref = linkAtCursor.mark.attrs.href ?? ""
    } else if (!state.selection.empty) {
      target = { from: state.selection.from, to: state.selection.to }
    } else {
      const wordRange = findWordRangeAtCursor(state)
      if (wordRange) {
        target = wordRange
      }
    }

    const promptSeed = currentHref || "https://"
    const href = window.prompt("External link (http/https)", promptSeed)
    if (href === null) return false

    const normalized = href.trim()
    if (!/^https?:\/\//i.test(normalized)) {
      setStatus("Invalid link: use http:// or https://")
      return false
    }

    if (!dispatch) return true
    const tr = state.tr
    if (target) {
      tr.removeMark(target.from, target.to, linkType)
      tr.addMark(target.from, target.to, linkType.create({ href: normalized }))
    } else {
      const linkMark = linkType.create({ href: normalized })
      const linkText = state.schema.text(normalized, [linkMark])
      tr.replaceSelectionWith(linkText)
    }
    setStatus("Link updated")
    dispatch(tr.scrollIntoView())
    return true
  }
}

const linkMenuItem = new MenuItem({
  icon: icons.link,
  title: "Set or edit external link",
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

  if (editMode === "visual" && editorView) {
    currentMarkdown = pmToMarkdown(editorView.state.doc, registry)
  } else if (editMode === "raw" && rawEditor) {
    currentMarkdown = rawEditor.value
  }

  if (nextEditMode === "visual") {
    try {
      currentDoc = markdownToPM(currentMarkdown, registry)
    } catch (err) {
      console.error("Switch to visual failed", err)
      setStatus("Invalid Markdown for visual mode")
      return
    }
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

  editorView = new EditorView(editorEl, { state })
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
  pageLabel.textContent = `Page: /${pagePath}`
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

    await savePage(pagePath, markdown)
    currentMarkdown = markdown
    currentDoc = markdownToPM(markdown, registry)
    editBaselineMarkdown = markdown
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
