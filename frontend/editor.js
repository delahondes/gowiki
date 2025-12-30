import "prosemirror-view/style/prosemirror.css";
import "prosemirror-menu/style/menu.css";
import { EditorState } from "prosemirror-state";
import { EditorView } from "prosemirror-view";
import { schema, defaultMarkdownParser, defaultMarkdownSerializer } from "prosemirror-markdown";
import { keymap } from "prosemirror-keymap";
import { baseKeymap, exitCode, chainCommands } from "prosemirror-commands";
import { history } from "prosemirror-history";
import { undo, redo } from "prosemirror-history";
import { menuBar, MenuItem, blockTypeItem, wrapItem } from "prosemirror-menu";
import { toggleMark } from "prosemirror-commands";

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


function markActive(state, type) {
  const { from, $from, to, empty } = state.selection;
  if (empty) return !!type.isInSet(state.storedMarks || $from.marks());
  return state.doc.rangeHasMark(from, to, type);
}

function toggleMarkProperly(markType) {
  return function (state, dispatch) {
    const { from, to, empty } = state.selection;
    const isActive = markActive(state, markType);

    // "Can this run?" probe
    if (!dispatch) return true;

    const tr = state.tr;

    if (empty) {
      // Toggle the stored mark (affects next input). This is the expected
      // "Word/Docs-like" behavior when the cursor is inside existing markup.
      if (isActive) {
        tr.removeStoredMark(markType);
      } else {
        tr.addStoredMark(markType.create());
      }
      dispatch(tr);
      return true;
    }

    // Non-empty selection: toggle the mark on the selected range.
    if (isActive) {
      dispatch(tr.removeMark(from, to, markType));
    } else {
      dispatch(tr.addMark(from, to, markType.create()));
    }
    return true;
  };
}

function buildToolbar(schema) {
  const items = [];

  if (schema.marks.strong) {
    const type = schema.marks.strong;
    items.push(new MenuItem({
      label: "B",
      title: "Bold",
      active: state => markActive(state, type),
      select: state => toggleMarkProperly(type)(state),
      run: toggleMarkProperly(type)
    }));
  }

  if (schema.marks.em) {
    const type = schema.marks.em;
    items.push(new MenuItem({
      label: "I",
      title: "Italic",
      active: state => markActive(state, type),
      select: state => toggleMarkProperly(type)(state),
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
      buildToolbar(schema),
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