import { NodeSelection, TextSelection, Selection, Plugin, PluginKey } from "prosemirror-state"
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

export function requestInputFocus(propName: string) {
  pendingInputRefocus = { propName, start: null, end: null }
}

const panelStyles = `
.gowiki-props-panel {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  align-items: baseline;
  margin: 0 0 4px 10px;
  padding: 4px 8px;
  background: #fff6cf;
  border: 1px solid #ddd;
  border-radius: 6px;
  font-size: 0.85em;
  position: relative;
  z-index: 1;
  max-width: 720px;
}

.gowiki-props-group {
  display: inline-flex;
  align-items: baseline;
  gap: 3px;
  white-space: nowrap;
}

.gowiki-props-group--wide {
  flex-basis: 100%;
}

.gowiki-props-group--wide input {
  width: 100%;
  flex: 1;
}

.gowiki-props-panel input {
  width: 8em;
  font-size: 0.95em;
}

.gowiki-props-panel textarea {
  font-family: monospace;
  font-size: 0.9em;
  width: 25em;
  min-height: 4em;
  resize: vertical;
  padding: 2px 4px;
}

.gowiki-props-label {
  color: #444;
}

.gowiki-props-error {
  color: #a03a00;
  font-size: 0.9em;
}

.gowiki-props-help {
  color: #888;
  font-size: 0.8em;
  white-space: pre-line;
}

.gowiki-props-panel.gowiki-props-panel--block {
  display: flex;
  width: max-content;
  margin: 0 0 4px 0;
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

    const focusable = Array.from(wrap.querySelectorAll<HTMLElement>("input, select, textarea"))
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

    const displayValue = prop.serialize
      ? prop.serialize(current)
      : String(current ?? prop.default ?? "")

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
    } else if (prop.multiline) {
      const textarea = document.createElement("textarea")
      textarea.value = displayValue

      if (pendingInputRefocus && pendingInputRefocus.propName === prop.name) {
        const focus = pendingInputRefocus
        pendingInputRefocus = null
        requestAnimationFrame(() => {
          textarea.focus()
          if (focus.start !== null && focus.end !== null) {
            try {
              textarea.setSelectionRange(focus.start, focus.end)
            } catch {
              // Ignore browsers that reject range restoration.
            }
          }
        })
      }

      textarea.addEventListener("input", () => {
        pendingInputRefocus = {
          propName: prop.name,
          start: textarea.selectionStart,
          end: textarea.selectionEnd,
        }
        dispatchChange(textarea.value)
      })

      // Prevent Tab from leaving textarea — allow normal tab behavior
      textarea.addEventListener("keydown", (e: KeyboardEvent) => {
        if (e.key === "Tab") {
          // Let the panel-level handler deal with it
        }
        if (e.key === "Backspace" && textarea.value === "" && prop.backspaceEmpty !== undefined) {
          e.preventDefault()
          e.stopPropagation()
          const live = view.state.doc.nodeAt(pos)
          if (!live) return
          const attrs = { ...live.attrs, [prop.name]: prop.backspaceEmpty }
          let tr = view.state.tr.setNodeMarkup(pos, live.type, attrs)
          tr = tr.setSelection(TextSelection.near(tr.doc.resolve(pos + 1)))
          view.dispatch(tr)
          view.focus()
        }
      })

      control = textarea
    } else {
      const input = document.createElement("input")
      input.type = "text"
      input.value = displayValue

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
      input.addEventListener("keydown", (e: KeyboardEvent) => {
        if (e.key === "Backspace" && input.value === "" && prop.backspaceEmpty !== undefined) {
          e.preventDefault()
          e.stopPropagation()
          const live = view.state.doc.nodeAt(pos)
          if (!live) return
          const attrs = { ...live.attrs, [prop.name]: prop.backspaceEmpty }
          let tr = view.state.tr.setNodeMarkup(pos, live.type, attrs)
          tr = tr.setSelection(TextSelection.near(tr.doc.resolve(pos + 1)))
          view.dispatch(tr)
          view.focus()
        }
      })
      control = input
    }

    const group = document.createElement("span")
    group.className = prop.wide ? "gowiki-props-group gowiki-props-group--wide" : "gowiki-props-group"
    group.appendChild(label)
    group.appendChild(control)
    group.appendChild(error)
    if (prop.helpText) {
      const help = document.createElement("span")
      help.className = "gowiki-props-help"
      help.textContent = prop.helpText
      group.appendChild(help)
    }
    wrap.appendChild(group)
  }

  return wrap
}

type PropertyTarget = {
  node: PMNode
  pos: number
  anchorPos: number
  props: NodePropertySpec[]
  autoShow: boolean
}

function findPropertyNodes(state: any, registry: Registry): PropertyTarget[] {
  const targets: PropertyTarget[] = []

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

      const isTableCell =
        node.type.name === "table_cell" || node.type.name === "table_header"

      const anchorPos =
        isStandaloneImageParagraph && $from.depth > 0
          ? $from.before($from.depth)
          : isTableCell
          ? nodePos + 1
          : nodePos

      targets.push({ node, pos: nodePos, anchorPos, props, autoShow: false })
    }
  }

  const $from = state.selection.$from
  for (let depth = $from.depth; depth > 0; depth--) {
    const node = $from.node(depth)
    const allProps = registry.getNodeProperties(node.type.name)
    if (allProps.length === 0) continue

    const pos = $from.before(depth)

    // Skip if we already have a target at this position (from NodeSelection above)
    if (targets.some(t => t.pos === pos)) continue

    const isTableCell =
      node.type.name === "table_cell" || node.type.name === "table_header"
    const anchorPos = isTableCell ? pos + 1 : pos

    const isAutoShow = false

    // For auto-show targets, filter to only visible props
    // For toggled-on targets, include all props (buildPanel filters by visible)
    targets.push({ node, pos, anchorPos, props: allProps, autoShow: isAutoShow })
  }

  return targets
}

function panelDecorationKey(target: PropertyTarget): string {
  const values = target.props
    .map((prop: NodePropertySpec) => `${prop.name}:${String(target.node.attrs[prop.name] ?? "")}`)
    .join("|")
  return `gowiki-props-panel-${target.pos}-${values}`
}

function shiftTabToPanel(view: any, event: KeyboardEvent): boolean {
  if (event.key !== "Tab" || !event.shiftKey) return false
  const pluginState = panelKey.getState(view.state)
  const targets = findPropertyNodes(view.state, registry)
  if (targets.length === 0) return false

  // Only respond if at least one panel is actually showing
  const hasShowing = targets.some(t => pluginState?.enabled || t.autoShow)
  if (!hasShowing) return false

  // In a table: only intercept Shift-Tab on the first cell (A1).
  // Other cells should use normal Shift-Tab navigation (previous cell).
  const $from = view.state.selection.$from
  for (let d = $from.depth; d > 0; d--) {
    const name = $from.node(d).type.name
    if (name === "table_cell" || name === "table_header") {
      if ($from.index(d - 1) !== 0 || $from.index(d - 2) !== 0) {
        return false
      }
      break
    }
  }

  const panel = view.dom.parentElement?.querySelector(".gowiki-props-panel")
  if (!panel) return false
  const first = panel.querySelector<HTMLElement>("input, select, textarea")
  if (!first) return false
  event.preventDefault()
  first.focus()
  return true
}

/** Active body-mounted vtext panel overlays, keyed by doc position. */
const vtextOverlays = new Map<number, { overlay: HTMLElement; cleanup: () => void }>()

function cleanupVtextOverlay(pos: number) {
  const entry = vtextOverlays.get(pos)
  if (entry) {
    entry.cleanup()
    vtextOverlays.delete(pos)
  }
}

/** Build a body-mounted property panel for vertical-text cells.
 *  Returns a tiny invisible placeholder that stays in the PM DOM tree.
 *  The real panel is a separate element on document.body, positioned via
 *  getBoundingClientRect. */
function buildVtextPanelOverlay(
  view: any, node: PMNode, pos: number, props: NodePropertySpec[]
): HTMLElement {
  // Clean up any previous overlay at this position.
  cleanupVtextOverlay(pos)

  const placeholder = document.createElement("span")
  placeholder.style.display = "none"
  placeholder.className = "gowiki-props-panel-vtext-placeholder"

  const overlay = buildPanel(view, node, pos, props)
  overlay.style.position = "fixed"
  overlay.style.zIndex = "10000"
  overlay.style.maxWidth = "none"
  overlay.style.width = "max-content"
  overlay.style.writingMode = "horizontal-tb"
  overlay.style.transform = "none"

  let scrollHandler: (() => void) | null = null

  const cleanup = () => {
    overlay.remove()
    if (scrollHandler) {
      window.removeEventListener("scroll", scrollHandler, true)
      window.removeEventListener("resize", scrollHandler)
    }
  }

  // Store for external cleanup.
  vtextOverlays.set(pos, { overlay, cleanup })

  // Append after a microtask so the placeholder is in the DOM.
  setTimeout(() => {
    if (!placeholder.isConnected) { cleanup(); vtextOverlays.delete(pos); return }
    document.body.appendChild(overlay)

    const cell = placeholder.closest("td, th") as HTMLElement | null
    const reposition = () => {
      const anchor = cell && cell.isConnected ? cell : placeholder
      if (!anchor.isConnected) { cleanupVtextOverlay(pos); return }
      const r = anchor.getBoundingClientRect()
      overlay.style.left = r.left + "px"
      overlay.style.top = (r.top - overlay.offsetHeight - 2) + "px"
    }
    reposition()

    scrollHandler = reposition
    window.addEventListener("scroll", scrollHandler, true)
    window.addEventListener("resize", scrollHandler)

    // Watch cell for vtext attribute changes (user switching to horizontal)
    // and placeholder removal (PM rebuilding decorations).
    if (cell) {
      const observer = new MutationObserver(() => {
        if (!placeholder.isConnected || !cell.isConnected) {
          observer.disconnect()
          cleanupVtextOverlay(pos)
          return
        }
        const vtext = cell.getAttribute("data-cell-vtext")
        if (vtext !== "upward" && vtext !== "downward") {
          observer.disconnect()
          cleanupVtextOverlay(pos)
        }
      })
      observer.observe(cell, { attributes: true, attributeFilter: ["data-cell-vtext"] })
      // Also watch for placeholder removal.
      if (placeholder.parentNode) {
        observer.observe(placeholder.parentNode, { childList: true })
      }
    }
  }, 0)

  return placeholder
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
        const targets = findPropertyNodes(state, registry)
        if (targets.length === 0) return null

        const decos: Decoration[] = []

        for (const target of targets) {
          // Auto-show: formula cells and colored cells show panel without toggle
          const isAutoShow = target.autoShow
          if (!pluginState?.enabled && !isAutoShow) continue

          // For auto-show targets, filter to visible props only
          const visibleProps = target.props.filter(p => !p.visible || p.visible(target.node.attrs))
          if (visibleProps.length === 0) continue

          const deco = Decoration.widget(
            target.anchorPos,
            view => {
              // For vertical-text cells, render the panel as a body overlay
              // to escape the cell's writing-mode/transform context.
              const vtext = target.node.attrs.cellVtext
              if (vtext === "upward" || vtext === "downward") {
                return buildVtextPanelOverlay(view, target.node, target.pos, target.props)
              }
              // Clean up any stale vtext overlay at this position (e.g. switched to horizontal).
              cleanupVtextOverlay(target.pos)
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
          decos.push(deco)
        }

        if (decos.length === 0) return null
        return DecorationSet.create(state.doc, decos)
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
