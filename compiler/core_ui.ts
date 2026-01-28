import { Plugin, PluginKey } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"
import type { Node as PMNode } from "prosemirror-model"
import type { Registry, NodePropertySpec } from "./registry"

type PanelState = {
  enabled: boolean
}

const panelKey = new PluginKey<PanelState>("nodePropertiesPanel")

const panelStyles = `
.wikidown-props-panel {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  margin: 0 0 6px 0;
  padding: 4px 6px;
  border: 1px solid #ddd;
  background: #f6f6f6;
  border-radius: 4px;
  font-size: 0.85em;
}

.wikidown-props-panel input {
  width: 8em;
  font-size: 0.95em;
}

.wikidown-props-label {
  color: #444;
}
`

function buildPanel(
  view: any,
  node: PMNode,
  pos: number,
  properties: NodePropertySpec[]
) {
  const wrap = document.createElement("div")
  wrap.className = "wikidown-props-panel"

  for (const prop of properties) {
    const label = document.createElement("span")
    label.className = "wikidown-props-label"
    label.textContent = prop.label

    const input = document.createElement("input")
    input.type = "text"
    const current = node.attrs[prop.name]
    input.value = current ?? prop.default ?? ""

    input.addEventListener("input", () => {
      const raw = input.value
      const parsed =
        raw === ""
          ? prop.default ?? null
          : prop.parse
          ? prop.parse(raw)
          : raw
      const live = view.state.doc.nodeAt(pos)
      if (!live) return
      const attrs = { ...live.attrs, [prop.name]: parsed }
      view.dispatch(
        view.state.tr.setNodeMarkup(pos, live.type, attrs)
      )
    })

    wrap.appendChild(label)
    wrap.appendChild(input)
  }

  return wrap
}

function findPropertyNode(state: any, registry: Registry) {
  const $from = state.selection.$from
  for (let depth = $from.depth; depth > 0; depth--) {
    const node = $from.node(depth)
    const props = registry.getNodeProperties(node.type.name)
    if (props.length > 0) {
      return {
        node,
        pos: $from.before(depth),
        props,
      }
    }
  }
  return null
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
          target.pos,
          view => buildPanel(view, target.node, target.pos, target.props),
          {
            side: -1,
            key: "wikidown-props-panel",
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
