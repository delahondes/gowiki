import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { addListNodes } from "prosemirror-schema-list"
import { keymap } from "prosemirror-keymap"
import { baseKeymap } from "prosemirror-commands"
import { history } from "prosemirror-history"
import { DOMParser } from "prosemirror-model"
import { menuBar,MenuItem } from "prosemirror-menu"
import { buildMenuItems } from "prosemirror-example-setup"
import { splitListItem } from "prosemirror-schema-list"
import { markdownToPM } from "./compiler/markdown_to_pm.ts"
import { pmToMarkdown } from "./compiler/pm_to_markdown.ts"
import { buildRegistry } from "./compiler/build_registry.ts"

const registry = buildRegistry(basicSchema)
const schema = registry.buildSchema()
registry.bindSchema(schema)

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

const markdown = `
## ProseMirror

This is editable text.

- One
- Two
`


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

menu.fullMenu.push([dumpDocMenuItem, dumpMDMenuItem])

const listKeymap = keymap({
  Enter: splitListItem(schema.nodes.list_item)
})

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

new EditorView(document.querySelector("#editor"), {
  state
})
