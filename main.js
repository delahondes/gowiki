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

const schema = new Schema({
  nodes: addListNodes(
    basicSchema.spec.nodes,
    "paragraph block*",
    "block"
  ),
  marks: basicSchema.spec.marks
})


const content = document.createElement("div")
content.innerHTML = `
  <h2>ProseMirror</h2>
  <p>This is editable text.</p>
  <ul>
    <li>One</li>
    <li>Two</li>
  </ul>
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

const menu = buildMenuItems(schema)
menu.fullMenu.push([dumpDocMenuItem])

const listKeymap = keymap({
  Enter: splitListItem(schema.nodes.list_item)
})

const state = EditorState.create({
  doc: DOMParser.fromSchema(schema).parse(content),
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