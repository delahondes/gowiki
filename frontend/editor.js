import "prosemirror-view/style/prosemirror.css";
import { EditorState } from "prosemirror-state";
import { EditorView } from "prosemirror-view";
import { schema } from "prosemirror-schema-basic";
import { defaultMarkdownParser, defaultMarkdownSerializer } from "prosemirror-markdown";

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