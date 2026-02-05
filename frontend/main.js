import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { keymap } from "prosemirror-keymap"
import { baseKeymap } from "prosemirror-commands"
import { history } from "prosemirror-history"
import { menuBar,MenuItem } from "prosemirror-menu"
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

function applyStyles(styles) {
  for (const { id, css } of styles) {
    const styleId = `wikidown-style-${id}`
    if (document.getElementById(styleId)) continue
    const style = document.createElement("style")
    style.id = styleId
    style.textContent = css
    document.head.appendChild(style)
  }
}

applyStyles(registry.getStyles())

const menu = buildMenuItems(schema)
const defaultMarkdown = `
## ProseMirror

This is editable text.

- One
- Two
`

registry.onCommand((namespace, name, cmd) => {
  const label = `${namespace}:${name}`

  const item = new MenuItem({
    label,
    title: label,
    run: cmd,
    enable: state => cmd(state)
  })

  menu.fullMenu.push([item])
})

function dumpDocCommand() {
  return (state) => {
    console.log("=== ProseMirror doc ===")
    console.log(state.doc)
    console.log("=== as JSON ===")
    console.log(JSON.stringify(state.doc.toJSON(), null, 2))
    return true
  }
}

const dumpDocMenuItem = new MenuItem({
  label: "Dump",
  title: "Dump ProseMirror document to console",
  run: dumpDocCommand()
})

function dumpMDCommand() {
  return (state) => {
    const md = pmToMarkdown(state.doc, registry)
    console.log("=== Markdown ===")
    console.log(md)
    return true
  }
}

const dumpMDMenuItem = new MenuItem({
  label: "DumpMD",
  title: "Dump document as Markdown",
  run: dumpMDCommand()
})

let view = null

function saveCommand() {
  return state => {
    const markdown = pmToMarkdown(state.doc, registry)
    void savePage(pagePath, markdown)
      .then(() => {
        console.log(`Saved /${pagePath}`)
      })
      .catch(err => {
        console.error("Save failed", err)
      })
    return true
  }
}

function reloadCommand() {
  return () => {
    void fetchPage(pagePath)
      .then(page => {
        const markdown = page?.markdown ?? defaultMarkdown
        const nextState = EditorState.create({
          doc: markdownToPM(markdown, registry),
          schema,
          plugins: view.state.plugins,
        })
        view.updateState(nextState)
        console.log(`Reloaded /${pagePath}`)
      })
      .catch(err => {
        console.error("Reload failed", err)
      })
    return true
  }
}

const saveMenuItem = new MenuItem({
  label: "Save",
  title: `Save /${pagePath}`,
  run: saveCommand(),
})

const reloadMenuItem = new MenuItem({
  label: "Reload",
  title: `Reload /${pagePath}`,
  run: reloadCommand(),
})

menu.fullMenu.push([dumpDocMenuItem, dumpMDMenuItem, saveMenuItem, reloadMenuItem])

const listKeymap = keymap({
  Enter: splitListItem(schema.nodes.list_item)
})

async function bootstrap() {
  const page = await fetchPage(pagePath)
  const markdown = page?.markdown ?? defaultMarkdown
  const state = EditorState.create({
    doc: markdownToPM(markdown, registry),
    schema,
    plugins: [
      listKeymap,
      history(),
      keymap(baseKeymap),
      ...registry.getEditorPlugins(),
      menuBar({
        content: menu.fullMenu
      })
    ]
  })

  view = new EditorView(document.querySelector("#editor"), {
    state
  })
}

bootstrap().catch(err => {
  console.error("Failed to start editor", err)
})
