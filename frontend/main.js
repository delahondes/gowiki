import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { DOMSerializer } from "prosemirror-model"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { keymap } from "prosemirror-keymap"
import { baseKeymap } from "prosemirror-commands"
import { history } from "prosemirror-history"
import { menuBar, MenuItem } from "prosemirror-menu"
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
let currentMarkdown = defaultMarkdown
let currentDoc = markdownToPM(defaultMarkdown, registry)
let editorView = null
let statusText = ""

const menu = buildMenuItems(schema)
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
  mode = nextMode
  if (mode === "edit") {
    renderEdit()
  } else {
    renderView()
  }
  renderActions()
}

function clearContent() {
  if (editorView) {
    editorView.destroy()
    editorView = null
  }
  contentRoot.innerHTML = ""
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

function renderEdit() {
  clearContent()
  const editorEl = document.createElement("div")
  editorEl.id = "gowiki-editor"
  contentRoot.appendChild(editorEl)

  const listKeymap = keymap({
    Enter: splitListItem(schema.nodes.list_item),
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

async function saveAndMaybeSwitch(toView) {
  if (mode !== "edit" || !editorView) return
  try {
    const markdown = pmToMarkdown(editorView.state.doc, registry)
    await savePage(pagePath, markdown)
    currentMarkdown = markdown
    currentDoc = markdownToPM(markdown, registry)
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
