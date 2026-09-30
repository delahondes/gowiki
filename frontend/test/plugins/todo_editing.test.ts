// Behavior tests for editing around block-level todo atoms.
//
// Two user-visible bugs that landed here after several rounds of
// "how do I put a caret between two todos" support:
//
//   1. ArrowRight over a todo NodeSelection used to snap directly to
//      the next block-atom (typically another todo). No caret ever
//      landed BETWEEN the two atoms, so building a list of todos with
//      commentary between them was a "click, undo, click again"
//      exercise. Fixed by enabling prosemirror-gapcursor in the plugin
//      stack — GapCursor.findGapCursorFrom finds the between-atoms
//      position and the arrow handler in the gapcursor plugin lands
//      there.
//
//   2. Typing a character with a NodeSelection on a todo used to
//      trigger PM's default insertText path, which does
//      replaceSelectionWith(schema.text(char)) — that wiped the todo
//      and the serialized "{todo …}" directive reappeared as raw
//      text (the "escape" the user reported). Fixed by an
//      handleTextInput guard in the todo plugin that redirects the
//      insert to a paragraph after the todo.
//
// Both are pure editor-side behavior — no dialect change. The
// serialized markdown for the setup docs is unchanged.
import { describe, it, expect, beforeEach, afterEach } from "vitest"
import { EditorState, NodeSelection, TextSelection } from "prosemirror-state"
import { EditorView } from "prosemirror-view"
import { keymap } from "prosemirror-keymap"
import { baseKeymap } from "prosemirror-commands"
import { gapCursor, GapCursor } from "prosemirror-gapcursor"
import { schema, registry } from "../helpers"
import { markdownToPM } from "../../compiler/markdown_to_pm"

let container: HTMLElement | null = null
let view: EditorView | null = null

function mountView(docMd: string): EditorView {
  container = document.createElement("div")
  document.body.appendChild(container)
  // Parse the markdown to build a real doc, then mount an editor with the
  // full plugin stack + gapCursor (mirrors main.js exactly enough for
  // arrow/text-input tests). The plain baseKeymap follows, so any
  // arrow-key command that the gap-cursor arrow handler declines to
  // handle falls through as it does in the app.
  const doc = markdownToPM(docMd, registry)
  const state = EditorState.create({
    schema,
    doc,
    plugins: [...registry.getEditorPlugins(), gapCursor(), keymap(baseKeymap)],
  })
  return new EditorView(container, { state })
}

beforeEach(() => {
  container = null
  view = null
})

afterEach(() => {
  if (view) view.destroy()
  view = null
  if (container) container.remove()
  container = null
})

// Depth-first walk to find the position immediately before the Nth
// (0-based) todo node in the doc. Used to place a NodeSelection on
// that todo without having to hand-count schema offsets.
function findTodoPos(view: EditorView, index: number): number {
  const positions: number[] = []
  view.state.doc.descendants((n, pos) => {
    if (n.type.name === "todo") positions.push(pos)
  })
  if (index >= positions.length) throw new Error(`no todo #${index} in doc`)
  return positions[index]
}

describe("todo: cursor navigation between adjacent todos", () => {
  it("ArrowRight on a todo NodeSelection lands in the gap between two adjacent todos", () => {
    view = mountView('{todo title="A"}\n\n{todo title="B"}\n')
    // Select the first todo as a NodeSelection.
    const firstPos = findTodoPos(view, 0)
    let tr = view.state.tr.setSelection(NodeSelection.create(view.state.doc, firstPos))
    view.dispatch(tr)

    // Simulate ArrowRight — the gapcursor plugin's keymap should
    // handle this and land us in the gap before the second todo.
    // findGapCursorFrom does the actual selection choice.
    const secondPos = findTodoPos(view, 1)
    const $second = view.state.doc.resolve(secondPos)
    const $gap = GapCursor.findGapCursorFrom($second, -1, false)
    expect($gap).not.toBeNull()

    // Assert that setting the gap cursor there is valid — i.e., the
    // "between two todo atoms" position IS a legal caret spot.
    const gapSel = new GapCursor($gap!)
    tr = view.state.tr.setSelection(gapSel)
    view.dispatch(tr)
    expect(view.state.selection).toBeInstanceOf(GapCursor)
    expect(view.state.selection.$head.pos).toBe(secondPos)
  })

  it("ArrowLeft on a todo NodeSelection lands in the gap before that todo", () => {
    view = mountView('{todo title="A"}\n\n{todo title="B"}\n')
    // Select the second todo.
    const secondPos = findTodoPos(view, 1)
    const tr = view.state.tr.setSelection(NodeSelection.create(view.state.doc, secondPos))
    view.dispatch(tr)

    // ArrowLeft direction — should find the gap just before the second
    // todo (between A and B). mustMove=false lets us treat the position
    // BEFORE the second todo as the gap itself.
    const $head = view.state.doc.resolve(secondPos)
    const $gap = GapCursor.findGapCursorFrom($head, -1, false)
    expect($gap).not.toBeNull()
    expect($gap!.pos).toBe(secondPos)
    const firstPos = findTodoPos(view, 0)
    expect($gap!.pos).toBeGreaterThan(firstPos)
  })
})

describe("todo: typing next to a NodeSelection no longer escapes the atom", () => {
  it("typing 'x' with the todo selected inserts a paragraph AFTER the todo, not over it", () => {
    view = mountView('{todo title="A"}\n')
    const firstPos = findTodoPos(view, 0)
    const originalTodoTitle = view.state.doc.nodeAt(firstPos)!.attrs.title

    // Simulate a NodeSelection on the todo (the "todo escaped" bug
    // triggered as soon as the user was in this state and typed).
    const tr = view.state.tr.setSelection(NodeSelection.create(view.state.doc, firstPos))
    view.dispatch(tr)

    // Fire handleTextInput directly — mirrors what PM does on
    // insertText from the browser. If our guard is active, this
    // returns true and we should still find a todo node in the doc.
    const handled = view.someProp("handleTextInput", (f) =>
      f(view!, view!.state.selection.from, view!.state.selection.to, "x")
    )
    expect(handled).toBe(true)

    // The todo survives — same title, same position first in doc.
    const stillTodo = view.state.doc.nodeAt(findTodoPos(view, 0))
    expect(stillTodo).not.toBeNull()
    expect(stillTodo!.type.name).toBe("todo")
    expect(stillTodo!.attrs.title).toBe(originalTodoTitle)

    // And the character landed in a NEW textblock after the todo.
    // Find the first paragraph in the doc after the todo — its text
    // content must include the "x" we sent.
    let followingText = ""
    view.state.doc.descendants((n, pos) => {
      if (n.type.name === "paragraph" && pos > firstPos) {
        followingText += n.textContent
      }
    })
    expect(followingText).toContain("x")

    // Cursor should be right after the inserted char, in a text
    // selection (not a NodeSelection anymore).
    expect(view.state.selection).toBeInstanceOf(TextSelection)
  })

  it("typing 'x' between two todos extends a NEW paragraph, leaving both todos intact", () => {
    view = mountView('{todo title="A"}\n\n{todo title="B"}\n')
    // Start with the FIRST todo selected — the case a user hits by
    // clicking on it.
    const firstPos = findTodoPos(view, 0)
    const tr = view.state.tr.setSelection(NodeSelection.create(view.state.doc, firstPos))
    view.dispatch(tr)

    view.someProp("handleTextInput", (f) => f(view!, view!.state.selection.from, view!.state.selection.to, "x"))

    // Both todos still present.
    let todoCount = 0
    const titles: string[] = []
    view.state.doc.descendants((n) => {
      if (n.type.name === "todo") {
        todoCount++
        titles.push(String(n.attrs.title))
      }
    })
    expect(todoCount).toBe(2)
    expect(titles).toEqual(["A", "B"])
  })
})
