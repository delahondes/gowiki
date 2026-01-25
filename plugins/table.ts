import { Plugin } from "../compiler/registry"
import { tableNodes } from "prosemirror-tables"
import { Node } from "prosemirror-model"
import {
  addColumnAfter,
  addRowAfter,
  deleteColumn,
  deleteRow,
} from "prosemirror-tables"


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
    reg.registerSchema({
      nodes: tableNodes({
        tableGroup: "block",
        cellContent: "block+",
        cellAttributes: {},
      }),
    })

    /* ----------------------------
     * Markdown -> PM
     * ---------------------------- */

    // markdown-it tokens: table_open, thead_open, tbody_open, tr_open, th_open, td_open, *_close

    reg.registerNode("table_open", {
      open(ctx) {
        ctx.open(reg.schema.nodes.table.create())
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
        out += "| " + header.join(" | ") + " |\n"
        out += "| " + header.map(() => "---").join(" | ") + " |\n"
        for (const row of body) {
          out += "| " + row.join(" | ") + " |\n"
        }
        return out + "\n"
      },
    })

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
