import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { Schema } from "prosemirror-model"
import { schema as basicSchema } from "prosemirror-schema-basic"
import { addListNodes } from "prosemirror-schema-list"
import { keymap } from "prosemirror-keymap"
import { baseKeymap } from "prosemirror-commands"
import { history } from "prosemirror-history"
import { DOMParser } from "prosemirror-model"
import { menuBar } from "prosemirror-menu"
import { buildMenuItems } from "prosemirror-example-setup"

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

const state = EditorState.create({
  doc: DOMParser.fromSchema(schema).parse(content),
  schema,
  plugins: [
    history(),
    keymap(baseKeymap),
    menuBar({
      content: buildMenuItems(schema).fullMenu
    })
  ]
})

new EditorView(document.querySelector("#editor"), {
  state
})