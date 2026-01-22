import { EditorState, Transaction, Command } from "prosemirror-state";
import { EditorView } from "prosemirror-view";

import { buildSchema } from "./registry"
import { docModelToEditorState as docmodelToPM } from "./editor/docmodel_to_pm"
import { DocNode } from "./docmodel"

const schema = buildSchema()

import { keymap } from "prosemirror-keymap";
import { baseKeymap, exitCode, chainCommands } from "prosemirror-commands";
import { history } from "prosemirror-history";
import { undo, redo } from "prosemirror-history";
import { menuBar, MenuItem, blockTypeItem, wrapItem } from "prosemirror-menu";
import { MarkType } from "prosemirror-model";

// import core plugins
import "../core/core"   // side-effect import, once
import "./kernel/kernel"

function insertHardBreakCommand(
  state: EditorState,
  dispatch?: (tr: Transaction) => void
): boolean {
  const br = state.schema.nodes.hard_break;
  if (!br) return false;

  if (dispatch) {
    dispatch(
      state.tr
        .replaceSelectionWith(br.create())
        .scrollIntoView()
    );
  }
  return true;
}

function hardBreakWithExitCode(
  state: EditorState,
  dispatch?: (tr: Transaction) => void,
  view?: EditorView
): boolean {
  return chainCommands(
    exitCode,
    insertHardBreakCommand
  )(state, dispatch, view);
}

const customKeymap = {
  "Shift-Enter": hardBreakWithExitCode,
  "Mod-Enter": hardBreakWithExitCode,
  "Ctrl-Enter": hardBreakWithExitCode,
  "Mod-z": undo,
  "Mod-y": redo,
  "Mod-Shift-z": redo,
};



function markActive(
  state: EditorState,
  type: MarkType
): boolean {
  const { from, $from, to, empty } = state.selection;

  if (empty) {
    return !!type.isInSet(state.storedMarks || $from.marks());
  }
  return state.doc.rangeHasMark(from, to, type);
}

function toggleMarkProperly(markType: MarkType): Command {
  return function (
    state: EditorState,
    dispatch?: (tr: Transaction) => void
  ): boolean {
    const { from, to, empty } = state.selection;
    const isActive = markActive(state, markType);

    // "Can this run?" probe
    if (!dispatch) return true;

    const tr = state.tr;

    if (empty) {
      // Cursor-only toggle → stored marks
      if (isActive) {
        tr.removeStoredMark(markType);
      } else {
        tr.addStoredMark(markType.create());
      }
      dispatch(tr);
      return true;
    }

    // Range toggle
    if (isActive) {
      dispatch(tr.removeMark(from, to, markType));
    } else {
      dispatch(tr.addMark(from, to, markType.create()));
    }
    return true;
  };
}

function buildToolbar(schema: import("prosemirror-model").Schema) {
  const items = [];

  if (schema.marks.strong) {
    const type = schema.marks.strong;
    items.push(new MenuItem({
      label: "B",
      title: "Bold",
      active: (state: EditorState) => markActive(state, type),
      run: toggleMarkProperly(type)
    }));
  }

  if (schema.marks.em) {
    const type = schema.marks.em;
    items.push(new MenuItem({
      label: "I",
      title: "Italic",
      active: (state: EditorState) => markActive(state, type),
      run: toggleMarkProperly(type)
    }));
  }

  if (schema.nodes.heading) {
    items.push(blockTypeItem(schema.nodes.heading, {
      label: "H1",
      title: "Heading 1",
      attrs: { level: 1 }
    }));
  }

  if (schema.nodes.blockquote) {
    items.push(wrapItem(schema.nodes.blockquote, {
      label: "❝",
      title: "Block quote"
    }));
  }

  return menuBar({
    content: [items],
    floating: false
  });
}

let view: EditorView | null = null;

// Expects a DocModel JSON object from backend
window.initProseMirrorEditor = function (
  container: HTMLElement,
  doc: DocNode
) {
  if (!container) {
    console.error("ProseMirror: missing container");
    return;
  }
  if (!doc) {
    console.error("ProseMirror: missing DocModel");
    return;
  }

  // Clear container (defensive, avoids double init)
  container.innerHTML = "";

  const state = docmodelToPM(
    schema,
    doc,
    [
      history(),
      buildToolbar(schema),
      keymap(customKeymap),
      keymap(baseKeymap),
    ]
  );

  view = new EditorView(container, {
    state,
    dispatchTransaction(transaction) {
      if (!view) return;
      const newState = view.state.apply(transaction);
      view.updateState(newState);
    }
  });
};

// TEMP: roundtrip back to DocModel (later sent to backend)
// Removed as per instructions

document.addEventListener("DOMContentLoaded", () => {
  const form = document.querySelector("form.edit-form") as HTMLFormElement | null;
  const hiddenInput = document.getElementById("docmodel-json") as HTMLInputElement | null;

  if (!form || !hiddenInput) {
    return;
  }

  form.addEventListener("submit", (event) => {
    if (!view) {
      console.error("ProseMirror: editor not initialized");
      return;
    }
    const docModel = window.pmToDocModel(view.state.doc);
    hiddenInput.value = JSON.stringify(docModel);
  });
});

declare global {
  interface Window {
    initProseMirrorEditor: (
      container: HTMLElement,
      doc: DocNode
    ) => void

    pmToDocModel: (doc: import("prosemirror-model").Node) => DocNode
  }
}

export {}