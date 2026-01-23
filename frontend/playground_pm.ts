import { Schema, NodeSpec, MarkSpec, Node as PMNode } from "prosemirror-model"
import { EditorState } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { keymap } from "prosemirror-keymap"
import { baseKeymap } from "prosemirror-commands"


// --- Minimal nodes

const nodes: Record<string, NodeSpec> = {
  doc: {
    content: "block+",
  },

  paragraph: {
    content: "inline*",
    group: "block",
    toDOM() { return ["p", 0] },
    parseDOM: [{ tag: "p" }],
  },

  text: {
    group: "inline",
  },

  bullet_list: {
    content: "list_item+",
    group: "block",
    toDOM() { return ["ul", 0] },
    parseDOM: [{ tag: "ul" }],
  },

  list_item: {
    // This is the canonical ProseMirror list-item shape:
    // at least one paragraph, then optional other blocks
    content: "paragraph block*",
    toDOM() { return ["li", 0] },
    parseDOM: [{ tag: "li" }],
  },
}

// --- Minimal marks

const marks: Record<string, MarkSpec> = {
  emph: {
    toDOM() { return ["em", 0] },
    parseDOM: [{ tag: "em" }, { tag: "i" }, { style: "font-style=italic" }],
  },
}

export const schema = new Schema({ nodes, marks })

// Build a PM doc by hand
function buildExampleDoc(schema: Schema): PMNode {
  const em = schema.marks.emph.create()

  const p1 = schema.nodes.paragraph.create(
    null,
    [
      schema.text("Hello "),
      schema.text("world", [em]),
      schema.text("!"),
    ],
  )

  const li1 = schema.nodes.list_item.create(null, [
    schema.nodes.paragraph.create(null, schema.text("one")),
  ])

  const li2 = schema.nodes.list_item.create(null, [
    schema.nodes.paragraph.create(null, schema.text("two")),
  ])

  const ul = schema.nodes.bullet_list.create(null, [li1, li2])

  const doc = schema.nodes.doc.create(null, [p1, ul])

  // This is the “truth test”
  doc.check()
  return doc
}

// Mount an editor + log the doc JSON
export function initPMPlayground(container: HTMLElement) {
  const doc = buildExampleDoc(schema)
  console.log("PM DOC JSON:", doc.toJSON())

  const state = EditorState.create({
    schema,
    doc,
    plugins: [keymap(baseKeymap)],
  })

  new EditorView(container, { state })
}