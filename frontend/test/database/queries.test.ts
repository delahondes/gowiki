// {database-query ...} — every attr the plugin knows about, including
// the full pivot surface.
//
// database-query is a self-contained block directive. All params live on
// the node's attrs; round-trip must preserve every one across the
// second pass. The pivot label maps (pivot_rows_labels /
// pivot_cols_labels) serialise as JSON strings — the shape of the JSON
// matters, so tests assert on the parsed attr, not just stability.
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

function firstQuery(doc: PMNode): Record<string, any> | null {
  let found: Record<string, any> | null = null
  doc.descendants(n => {
    if (found === null && n.type.name === "database_query") {
      found = { ...n.attrs }
      return false
    }
    return true
  })
  return found
}

describe("database-query: minimum shape", () => {
  it("{database-query table=orders} produces one node", () => {
    const rt = roundTrip("{database-query table=orders}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "database_query")).toBe(1)
    expect(firstQuery(rt.doc)?.table).toBe("orders")
  })
})

describe("database-query: individual params", () => {
  it("fields=", () => {
    const rt = roundTrip("{database-query table=orders fields=\"id,name,amount\"}\n")
    expect(rt.isStable).toBe(true)
    expect(firstQuery(rt.doc)?.fields).toBe("id,name,amount")
  })

  it("filter= with operator", () => {
    const rt = roundTrip("{database-query table=orders filter=\"amount>100\"}\n")
    expect(rt.isStable).toBe(true)
    expect(firstQuery(rt.doc)?.filter).toBe("amount>100")
  })

  it("sort= + order=desc", () => {
    const rt = roundTrip("{database-query table=orders sort=amount order=desc}\n")
    expect(rt.isStable).toBe(true)
    const q = firstQuery(rt.doc)
    expect(q?.sort).toBe("amount")
    expect(q?.order).toBe("desc")
  })

  it("order=asc is dropped on serialize (default)", () => {
    const rt = roundTrip("{database-query table=orders order=asc}\n")
    expect(rt.isStable).toBe(true)
    // Default omitted → cleaner canonical form.
    expect(rt.first).not.toMatch(/order=asc/)
  })

  it("limit=50 (non-default)", () => {
    const rt = roundTrip("{database-query table=orders limit=50}\n")
    expect(rt.isStable).toBe(true)
    expect(firstQuery(rt.doc)?.limit).toBe("50")
  })

  it("limit=20 (default) is dropped on serialize", () => {
    const rt = roundTrip("{database-query table=orders limit=20}\n")
    expect(rt.isStable).toBe(true)
    expect(rt.first).not.toMatch(/limit=20/)
  })
})

describe("database-query: pivot params", () => {
  it("pivot_rows + pivot_cols + pivot_cell + pivot_agg", () => {
    const rt = roundTrip(
      "{database-query table=orders pivot_rows=customer pivot_cols=status pivot_cell=amount pivot_agg=sum}\n",
    )
    expect(rt.isStable).toBe(true)
    const q = firstQuery(rt.doc)
    expect(q?.pivot_rows).toBe("customer")
    expect(q?.pivot_cols).toBe("status")
    expect(q?.pivot_cell).toBe("amount")
    expect(q?.pivot_agg).toBe("sum")
  })

  it("pivot_empty= sentinel", () => {
    const rt = roundTrip(
      "{database-query table=orders pivot_rows=customer pivot_cols=status pivot_empty=\"(none)\"}\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstQuery(rt.doc)?.pivot_empty).toBe("(none)")
  })

  it("pivot_cols_sort=alpha", () => {
    const rt = roundTrip(
      "{database-query table=orders pivot_rows=customer pivot_cols=status pivot_cols_sort=alpha}\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstQuery(rt.doc)?.pivot_cols_sort).toBe("alpha")
  })

  it("pivot_cols_max=5", () => {
    const rt = roundTrip(
      "{database-query table=orders pivot_rows=customer pivot_cols=status pivot_cols_max=5}\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstQuery(rt.doc)?.pivot_cols_max).toBe("5")
  })
})

// KNOWN DEFECT — the directive attribute parser in
// `frontend/compiler/markdown_to_pm.ts` uses a regex `"([^"]*)"` that
// Directive attribute values carry JSON blobs (pivot label maps) that
// need backslash-escaped inner quotes on the write side and a matching
// unescape on the read side. `parseDirective` in `compiler/markdown_to_pm.ts`
// accepts `\.` escape sequences inside quoted values and unescapes on
// extraction. `plugins/database.ts` writes the corresponding `\"`.
// These tests pin the invariant end-to-end.
describe("database-query: pivot label maps (JSON blobs)", () => {
  it("pivot_rows_labels round-trips a simple map", () => {
    const src = `{database-query table=orders pivot_rows=status pivot_rows_labels="{\\"open\\":\\"Open\\",\\"done\\":\\"Done\\"}"}\n`
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const q = firstQuery(rt.doc)
    expect(JSON.parse(q?.pivot_rows_labels)).toEqual({ open: "Open", done: "Done" })
  })

  it("pivot_cols_labels with @null sentinel", () => {
    const src = `{database-query table=orders pivot_cols=country pivot_cols_labels="{\\"@null\\":\\"(none)\\",\\"FR\\":\\"France\\"}"}\n`
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const q = firstQuery(rt.doc)
    expect(JSON.parse(q?.pivot_cols_labels)).toEqual({ "@null": "(none)", FR: "France" })
  })

  it("empty pivot map (`{}`) round-trips", () => {
    const src = `{database-query table=orders pivot_rows=status pivot_rows_labels="{}"}\n`
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const q = firstQuery(rt.doc)
    if (q?.pivot_rows_labels) {
      expect(JSON.parse(q.pivot_rows_labels)).toEqual({})
    }
  })

  it("label value with unicode key/value survives", () => {
    const src = `{database-query table=customers pivot_rows=country pivot_rows_labels="{\\"FR\\":\\"France 🇫🇷\\",\\"BE\\":\\"Belgïe\\"}"}\n`
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const q = firstQuery(rt.doc)
    const parsed = JSON.parse(q?.pivot_rows_labels)
    expect(parsed.FR).toBe("France 🇫🇷")
    expect(parsed.BE).toBe("Belgïe")
  })
})

describe("database-query: interaction with tables and code", () => {
  it("directive inside a code fence stays literal (no parse)", () => {
    const src =
      "```\n" +
      "{database-query table=orders}\n" +
      "```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "database_query")).toBe(0)
  })

  it("directive in a table cell wrapped in backticks stays literal", () => {
    // Backtick-protected cell content must not spawn a directive node.
    // This mirrors the invariant tested across the tables corpus.
    const src =
      "| head |\n" +
      "| --- |\n" +
      "| `{database-query table=orders}` |\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "database_query")).toBe(0)
  })
})

describe("database-newrow", () => {
  it("{database-newrow table=orders}", () => {
    const rt = roundTrip("{database-newrow table=orders}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "database_newrow")).toBe(1)
  })
})
