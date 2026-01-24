import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { Schema } from "prosemirror-model"
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

const schema = new Schema({
  nodes: addListNodes(
    basicSchema.spec.nodes,
    "paragraph block*",
    "block"
  ),
  marks: basicSchema.spec.marks
})

const registry = buildRegistry(schema)

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

const menu = buildMenuItems(schema)
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
    menuBar({
      content: menu.fullMenu
    })
  ]
})

new EditorView(document.querySelector("#editor"), {
  state
})