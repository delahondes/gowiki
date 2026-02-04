import { Plugin } from "../compiler/registry"
import { tableNodes, tableEditing, goToNextCell } from "prosemirror-tables"
import { Node } from "prosemirror-model"
import { keymap } from "prosemirror-keymap"
import {
  addColumnAfter,
  addRowAfter,
  deleteColumn,
  deleteRow,
} from "prosemirror-tables"

const tableStyles = `
.ProseMirror table {
  border-collapse: collapse;
  margin: 0.5em 0;
}

.ProseMirror th,
.ProseMirror td {
  border: 1px solid #ccc;
  padding: 0.25em 0.5em;
  vertical-align: top;
}

.ProseMirror th {
  background: #f7f7f7;
  font-weight: 600;
  text-align: left;
}

.ProseMirror td > p,
.ProseMirror th > p {
  margin: 0;
}

.ProseMirror .selectedCell {
  background: #cce5ff;
}
`

const tableProperties = [
  {
    name: "width",
    label: "Width",
    default: null,
    parse: (raw: string) => raw,
    serialize: (value: string | null) => String(value ?? ""),
  },
]

function addWidthToDOM(spec: any, width: string | null) {
  if (!width) return spec
  if (!Array.isArray(spec)) return spec
  const [tag, maybeAttrs, ...rest] = spec
  const hasAttrs =
    maybeAttrs &&
    typeof maybeAttrs === "object" &&
    !Array.isArray(maybeAttrs)
  const attrs = hasAttrs ? maybeAttrs : {}
  const existing = attrs.style ? String(attrs.style) : ""
  const style = existing
    ? `${existing}${existing.trim().endsWith(";") ? " " : "; "}width: ${width};`
    : `width: ${width};`
  const newAttrs = { ...attrs, style }
  const children = hasAttrs ? rest : [maybeAttrs, ...rest]
  return [tag, newAttrs, ...children]
}


export function makeTable(
  schema: any,
  rows: number,
  cols: number,
  withHeader = true
): Node {
  const { table, table_row, table_cell, table_header } = schema.nodes

  const rowNodes: Node[] = []

  for (let r = 0; r < rows; r++) {
    const cells: Node[] = []
    for (let c = 0; c < cols; c++) {
      const cellType =
        withHeader && r === 0 ? table_header : table_cell
      cells.push(cellType.createAndFill()!)
    }
    rowNodes.push(table_row.create(null, cells))
  }

  return table.create(null, rowNodes)
}

/**
 * Table plugin (v1)
 * Scope:
 * - rectangular tables only
 * - no rowspan / colspan
 * - GitHub-style Markdown tables
 * - commands only (no menu wiring)
 */
export const tablePlugin: Plugin = {
  register(reg) {
    const nodes = tableNodes({
      tableGroup: "block",
      cellContent: "block+",
      cellAttributes: {},
    })

    const baseTable = nodes.table
    nodes.table = {
      ...baseTable,
      attrs: { ...baseTable.attrs, width: { default: null } },
      toDOM(node) {
        const domSpec = baseTable.toDOM
          ? baseTable.toDOM(node)
          : ["table", ["tbody", 0]]
        return addWidthToDOM(domSpec, node.attrs.width ?? null)
      },
    }

    reg.registerSchema({
      nodes,
    })

    reg.registerDirective("table", {
      nodeType: "table",
      appliesTo: ["table_open"],
      properties: tableProperties,
    })

    /* ----------------------------
     * Markdown -> PM
     * ---------------------------- */

    // markdown-it tokens: table_open, thead_open, tbody_open, tr_open, th_open, td_open, *_close

    reg.registerNode("table_open", {
      open(ctx) {
        const attrs =
          ctx.token?.meta?.directives?.table ?? null
        ctx.open(reg.schema.nodes.table.create(attrs))
      },
    })

    reg.registerNode("table_close", {
      close(ctx) {
        ctx.close()
      },
    })

    reg.registerNode("thead_open", {
      open(ctx) {
        // header rows are just rows with header cells
      },
    })

    reg.registerNode("tbody_open", {
      open(ctx) {},
    })

    reg.registerNode("tr_open", {
      open(ctx) {
        ctx.open(reg.schema.nodes.table_row.create())
      },
    })

    reg.registerNode("tr_close", {
      close(ctx) {
        ctx.close()
      },
    })

    reg.registerNode("th_open", {
      open(ctx) {
        ctx.open(reg.schema.nodes.table_header.create())
      },
    })

    reg.registerNode("th_close", {
      close(ctx) {
        ctx.close()
      },
    })

    reg.registerNode("td_open", {
      open(ctx) {
        ctx.open(reg.schema.nodes.table_cell.create())
      },
    })

    reg.registerNode("td_close", {
      close(ctx) {
        ctx.close()
      },
    })

    /* ----------------------------
     * PM -> Markdown
     * ---------------------------- */

    reg.registerPMNode("table", {
      print(node, ctx, recurse) {
        let rows: string[][] = []

        node.content.forEach(row => {
          let cells: string[] = []
          row.content.forEach(cell => {
            let txt = ""
            cell.content.forEach(p => {
              txt += recurse(p).trim()
            })
            cells.push(txt)
          })
          rows.push(cells)
        })

        if (rows.length === 0) return ""

        const header = rows[0]
        const body = rows.slice(1)

        let out = ""
        const width = node.attrs.width ?? null
        const widthProp = tableProperties.find(p => p.name === "width")
        if (widthProp && width && width !== widthProp.default) {
          const rendered = widthProp.serialize
            ? widthProp.serialize(width)
            : String(width)
          out += `{table width=${rendered}}\n`
        }
        out += "| " + header.join(" | ") + " |\n"
        out += "| " + header.map(() => "---").join(" | ") + " |\n"
        for (const row of body) {
          out += "| " + row.join(" | ") + " |\n"
        }
        return out + "\n"
      },
    })

    /* ----------------------------
     * Editor integration
     * ---------------------------- */

    reg.registerEditorPlugin(() =>
      keymap({
        Tab: goToNextCell(1),
        "Shift-Tab": goToNextCell(-1),
      })
    )

    reg.registerEditorPlugin(() => tableEditing())

    /* ----------------------------
     * Styles
     * ---------------------------- */

    reg.registerStyle("table", tableStyles)

    /* ----------------------------
     * Commands (exported via registry extras)
     * ---------------------------- */

    reg.registerCommand("table", "insert", (state, dispatch) => {
      const tableNode = makeTable(reg.schema, 3, 3, true)
      if (!tableNode) return false
      if (dispatch) {
        dispatch(state.tr.replaceSelectionWith(tableNode))
      }
      return true
    })

    reg.registerCommand("table", "row.addAfter", addRowAfter)
    reg.registerCommand("table", "column.addAfter", addColumnAfter)
    reg.registerCommand("table", "row.delete", deleteRow)
    reg.registerCommand("table", "column.delete", deleteColumn)
  },
}
