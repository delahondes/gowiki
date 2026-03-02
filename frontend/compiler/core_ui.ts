import { NodeSelection, Selection, Plugin, PluginKey } from "prosemirror-state"
import { Decoration, DecorationSet } from "prosemirror-view"
import type { Node as PMNode } from "prosemirror-model"
import type { Registry, NodePropertySpec } from "./registry"

type PanelState = {
  enabled: boolean
}

const panelKey = new PluginKey<PanelState>("gowiki.nodePropertiesPanel")

let pendingInputRefocus: {
  propName: string
  start: number | null
  end: number | null
} | null = null

const panelStyles = `
.gowiki-props-panel {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  margin: 0 0 4px 10px;
  padding: 3px 8px;
  background: #fff6cf;
  border: 1px solid #ddd;
  border-radius: 6px;
  font-size: 0.85em;
  position: relative;
  z-index: 1;
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

  wrap.addEventListener("keydown", (e: KeyboardEvent) => {
    if (e.key !== "Tab") return
    e.preventDefault()
    e.stopPropagation()

    const focusable = Array.from(wrap.querySelectorAll<HTMLElement>("input, select"))
    const current = document.activeElement as HTMLElement
    const idx = focusable.indexOf(current)

    if (!e.shiftKey) {
      // Forward: next field, or leave node
      if (idx >= 0 && idx < focusable.length - 1) {
        focusable[idx + 1].focus()
      } else {
        // Move cursor to just after the node
        const after = pos + node.nodeSize
        const $pos = view.state.doc.resolve(Math.min(after, view.state.doc.content.size))
        const sel = Selection.near($pos)
        view.dispatch(view.state.tr.setSelection(sel))
        view.focus()
      }
    } else {
      // Backward: previous field, or move before node
      if (idx > 0) {
        focusable[idx - 1].focus()
      } else {
        const $pos = view.state.doc.resolve(Math.max(0, pos))
        const sel = Selection.near($pos, -1)
        view.dispatch(view.state.tr.setSelection(sel))
        view.focus()
      }
    }
  })

  for (const prop of properties) {
    if (prop.visible && !prop.visible(node.attrs)) continue

    const label = document.createElement("span")
    label.className = "gowiki-props-label"
    label.textContent = prop.label

    const current = node.attrs[prop.name]
    const error = document.createElement("span")
    error.className = "gowiki-props-error"

    const dispatchChange = (raw: string) => {
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
    }

    let control: HTMLElement
    const resolvedOptions = typeof prop.options === "function"
      ? prop.options(node.attrs)
      : prop.options
    if (resolvedOptions && resolvedOptions.length > 0) {
      const select = document.createElement("select")
      const populateOptions = (opts: typeof resolvedOptions, currentVal: string) => {
        select.innerHTML = ""
        for (const opt of opts) {
          const option = document.createElement("option")
          option.value = opt.value
          option.textContent = opt.label
          select.appendChild(option)
        }
        select.value = currentVal
      }
      populateOptions(resolvedOptions, current ?? prop.default ?? "")
      select.addEventListener("change", () => dispatchChange(select.value))
      // Re-populate options on focus to pick up cache changes (e.g. new media versions).
      if (typeof prop.options === "function") {
        const optionsFn = prop.options
        select.addEventListener("focus", () => {
          const liveNode = view.state.doc.nodeAt(pos)
          if (!liveNode) return
          const freshOptions = optionsFn(liveNode.attrs)
          populateOptions(freshOptions, select.value)
        })
      }
      control = select
    } else {
      const input = document.createElement("input")
      input.type = "text"
      input.value = current ?? prop.default ?? ""

      if (pendingInputRefocus && pendingInputRefocus.propName === prop.name) {
        const focus = pendingInputRefocus
        pendingInputRefocus = null
        requestAnimationFrame(() => {
          input.focus()
          if (focus.start !== null && focus.end !== null) {
            try {
              input.setSelectionRange(focus.start, focus.end)
            } catch {
              // Ignore browsers that reject range restoration.
            }
          }
        })
      }

      input.addEventListener("input", () => {
        pendingInputRefocus = {
          propName: prop.name,
          start: input.selectionStart,
          end: input.selectionEnd,
        }
        dispatchChange(input.value)
      })
      control = input
    }

    wrap.appendChild(label)
    wrap.appendChild(control)
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
    const allProps = registry.getNodeProperties(node.type.name)
    const props = allProps.filter(p => !p.visible || p.visible(node.attrs))
    if (props.length > 0) {
      const pos = $from.before(depth)
      // For table cells, place the panel inside the cell (pos+1) rather than
      // before it in the row (pos), so it renders as inline content.
      const isTableCell =
        node.type.name === "table_cell" || node.type.name === "table_header"
      const anchorPos = isTableCell ? pos + 1 : pos
      return {
        node,
        pos,
        anchorPos,
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

function shiftTabToPanel(view: any, event: KeyboardEvent): boolean {
  if (event.key !== "Tab" || !event.shiftKey) return false
  const pluginState = panelKey.getState(view.state)
  if (!pluginState?.enabled) return false
  const target = findPropertyNode(view.state, registry)
  if (!target) return false
  const panel = view.dom.parentElement?.querySelector(".gowiki-props-panel")
  if (!panel) return false
  const first = panel.querySelector<HTMLElement>("input, select")
  if (!first) return false
  event.preventDefault()
  first.focus()
  return true
}

let registry: Registry

function propertiesPlugin(reg: Registry) {
  registry = reg
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
      handleDOMEvents: {
        keydown(view: any, event: KeyboardEvent) {
          return shiftTabToPanel(view, event)
        },
      },
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

export function isPropertiesPanelEnabled(state: any): boolean {
  return Boolean(panelKey.getState(state)?.enabled)
}

export function enablePropertiesPanel(tr: any): any {
  return tr.setMeta(panelKey, { enabled: true })
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
