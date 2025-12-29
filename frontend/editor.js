import "prosemirror-view/style/prosemirror.css";
import { EditorState } from "prosemirror-state";
import { EditorView } from "prosemirror-view";
import { schema } from "prosemirror-schema-basic";
import { defaultMarkdownParser, defaultMarkdownSerializer } from "prosemirror-markdown";
import { keymap } from "prosemirror-keymap";
import { baseKeymap, exitCode, chainCommands } from "prosemirror-commands";
import { history } from "prosemirror-history";
import { undo, redo } from "prosemirror-history";

function insertHardBreakCommand(state, dispatch) {
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

function hardBreakWithExitCode(state, dispatch, view) {
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

let view = null;

window.initProseMirrorEditor = function (container, sourceTextarea) {
  if (!container || !sourceTextarea) {
    console.error("ProseMirror: missing container or source textarea");
    return;
  }

  // Clear container (defensive, avoids double init)
  container.innerHTML = "";

  const content = sourceTextarea.value || "";

  const state = EditorState.create({
    doc: defaultMarkdownParser.parse(content),
    schema: schema,
    plugins: [
      history(),
      keymap(customKeymap),
      keymap(baseKeymap),
    ],
  });

  view = new EditorView(container, {
    state,
  });
};

window.getMarkdown = function () {
  if (!view) {
    console.error("ProseMirror: editor not initialized");
    return "";
  }
  return defaultMarkdownSerializer.serialize(view.state.doc);
};