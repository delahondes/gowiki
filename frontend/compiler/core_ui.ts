import { NodeSelection, Plugin, PluginKey } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"
import type { Node as PMNode } from "prosemirror-model"
import type { Registry, NodePropertySpec } from "./registry"

type PanelState = {
  enabled: boolean
}

const panelKey = new PluginKey<PanelState>("gowiki.nodePropertiesPanel")

const panelStyles = `
.gowiki-props-panel {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  margin: 0 0 6px 0;
  padding: 4px 6px;
  border: 1px solid #ddd;
  background: #fff6cf;
  border-radius: 4px;
  font-size: 0.85em;
}

.gowiki-props-panel input {
  width: 8em;
  font-size: 0.95em;
}

.gowiki-props-label {
  color: #444;
}

.gowiki-props-error {
  color: #a03a00;
  font-size: 0.9em;
}

.gowiki-props-panel--block {
  display: flex;
  width: max-content;
}
`

function buildPanel(
  view: any,
  node: PMNode,
  pos: number,
  properties: NodePropertySpec[]
) {
  const wrap = document.createElement("div")
  wrap.className = "gowiki-props-panel"

  for (const prop of properties) {
    const label = document.createElement("span")
    label.className = "gowiki-props-label"
    label.textContent = prop.label

    const input = document.createElement("input")
    input.type = "text"
    const current = node.attrs[prop.name]
    input.value = current ?? prop.default ?? ""

    const error = document.createElement("span")
    error.className = "gowiki-props-error"

    input.addEventListener("input", () => {
      const raw = input.value
      let parsed: string | null
      try {
        parsed =
          raw === ""
            ? prop.default ?? null
            : prop.parse
            ? prop.parse(raw)
            : raw
      } catch (err) {
        const msg = err instanceof Error ? err.message : "Invalid value"
        error.textContent = msg
        return
      }
      error.textContent = ""
      const live = view.state.doc.nodeAt(pos)
      if (!live) return
      const attrs = { ...live.attrs, [prop.name]: parsed }

      const state = view.state
      const wasNodeSelection =
        state.selection instanceof NodeSelection && state.selection.from === pos

      let tr = state.tr.setNodeMarkup(pos, live.type, attrs)
      if (wasNodeSelection) {
        tr = tr.setSelection(NodeSelection.create(tr.doc, pos))
      }
      view.dispatch(tr)
    })

    wrap.appendChild(label)
    wrap.appendChild(input)
    wrap.appendChild(error)
  }

  return wrap
}

function findPropertyNode(state: any, registry: Registry) {
  if (state.selection instanceof NodeSelection) {
    const node = state.selection.node
    const props = registry.getNodeProperties(node.type.name)
    if (props.length > 0) {
      const nodePos = state.selection.from
      const $from = state.selection.$from
      const isStandaloneImageParagraph =
        node.type.name === "image" &&
        $from.parent?.type?.name === "paragraph" &&
        $from.parent.childCount === 1

      const anchorPos =
        isStandaloneImageParagraph && $from.depth > 0
          ? $from.before($from.depth)
          : nodePos

      return {
        node,
        pos: nodePos,
        anchorPos,
        props,
      }
    }
  }

  const $from = state.selection.$from
  for (let depth = $from.depth; depth > 0; depth--) {
    const node = $from.node(depth)
    const props = registry.getNodeProperties(node.type.name)
    if (props.length > 0) {
      const pos = $from.before(depth)
      return {
        node,
        pos,
        anchorPos: pos,
        props,
      }
    }
  }
  return null
}

function panelDecorationKey(target: any): string {
  const values = target.props
    .map((prop: NodePropertySpec) => `${prop.name}:${String(target.node.attrs[prop.name] ?? "")}`)
    .join("|")
  return `gowiki-props-panel-${target.anchorPos}-${values}`
}

function propertiesPlugin(registry: Registry) {
  return new Plugin<PanelState>({
    key: panelKey,
    state: {
      init() {
        return { enabled: false }
      },
      apply(tr, prev) {
        const meta = tr.getMeta(panelKey)
        if (meta && typeof meta.enabled === "boolean") {
          return { enabled: meta.enabled }
        }
        return prev
      },
    },
    props: {
      decorations(state) {
        const pluginState = panelKey.getState(state)
        if (!pluginState?.enabled) return null
        const target = findPropertyNode(state, registry)
        if (!target) return null
        const deco = Decoration.widget(
          target.anchorPos,
          view => {
            const panel = buildPanel(view, target.node, target.pos, target.props)
            if (target.anchorPos !== target.pos) {
              panel.classList.add("gowiki-props-panel--block")
            }
            return panel
          },
          {
            side: -1,
            key: panelDecorationKey(target),
            stopEvent: () => true,
          }
        )
        return DecorationSet.create(state.doc, [deco])
      },
    },
  })
}

function togglePropertiesCommand() {
  return (state: any, dispatch: any) => {
    const current = panelKey.getState(state)
    const enabled = !current?.enabled
    if (dispatch) {
      dispatch(state.tr.setMeta(panelKey, { enabled }))
    }
    return true
  }
}

export function registerCoreUI(registry: Registry) {
  registry.registerEditorPlugin(() => propertiesPlugin(registry))
  registry.registerCommand("ui", "properties.toggle", togglePropertiesCommand())
  registry.registerStyle("properties-panel", panelStyles)
}
