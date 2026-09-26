// {database-row ...} + companion Field/Value table
//
// The row directive is a block-level marker followed by an optional
// 2-column table (Field | Value). The parser reads the table into a
// `_fields` map on the node; the serializer writes both the directive
// line and the field table back out. Round-trip must preserve every
// field verbatim across the second pass.
import { describe, it, expect } from "vitest"
import type { Node as PMNode } from "prosemirror-model"
import { roundTrip, countNodes } from "../helpers"

// Find the first database_row node and return its attrs.
function firstRow(doc: PMNode): { table: string; fields: Record<string, string> } | null {
  let found: { table: string; fields: Record<string, string> } | null = null
  doc.descendants(n => {
    if (found === null && n.type.name === "database_row") {
      found = { table: n.attrs.table || "", fields: { ...(n.attrs._fields || {}) } }
      return false
    }
    return true
  })
  return found
}

describe("database-row: placeholder", () => {
  it("bare {database-row} carries no table and no fields", () => {
    const rt = roundTrip("{database-row}\n")
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "database_row")).toBe(1)
    const row = firstRow(rt.doc)
    expect(row?.table).toBe("")
    expect(Object.keys(row?.fields ?? {})).toHaveLength(0)
  })
})

describe("database-row: bound rows across every field type", () => {
  it("text field", () => {
    const rt = roundTrip(
      "{database-row table=customers}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| name | Alice |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    const row = firstRow(rt.doc)
    expect(row?.table).toBe("customers")
    expect(row?.fields.name).toBe("Alice")
  })

  it("number field", () => {
    const rt = roundTrip(
      "{database-row table=orders}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| amount | 42 |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.amount).toBe("42")
  })

  it("enum field", () => {
    const rt = roundTrip(
      "{database-row table=orders}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| status | shipped |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.status).toBe("shipped")
  })

  it("multi_enum field (comma-separated)", () => {
    const rt = roundTrip(
      "{database-row table=customers}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| countries | France, Belgium, Switzerland |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.countries).toBe("France, Belgium, Switzerland")
  })

  it("tag field", () => {
    const rt = roundTrip(
      "{database-row table=projects}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| tags | urgent |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.tags).toBe("urgent")
  })

  it("user field", () => {
    const rt = roundTrip(
      "{database-row table=projects}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| owner | alice |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.owner).toBe("alice")
  })

  it("lookup field (bare row id)", () => {
    const rt = roundTrip(
      "{database-row table=orders}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| customer | 17 |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.customer).toBe("17")
  })

  it("image field (attachment path)", () => {
    const rt = roundTrip(
      "{database-row table=products}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| photo | /media/products/sku42.jpg |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.photo).toBe("/media/products/sku42.jpg")
  })

  it("date field (ISO)", () => {
    const rt = roundTrip(
      "{database-row table=orders}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| placed_on | 2026-09-26 |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.placed_on).toBe("2026-09-26")
  })
})

describe("database-row: multi-field rows", () => {
  it("keeps every field in its original position", () => {
    const rt = roundTrip(
      "{database-row table=customers}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| name | Alice |\n" +
      "| email | alice@example.com |\n" +
      "| country | France |\n" +
      "| active | yes |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    const row = firstRow(rt.doc)
    expect(row?.fields).toEqual({
      name: "Alice",
      email: "alice@example.com",
      country: "France",
      active: "yes",
    })
  })
})

describe("database-row: value quoting and special characters", () => {
  it("empty-string value survives round-trip", () => {
    const rt = roundTrip(
      "{database-row table=customers}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| name | Alice |\n" +
      "| middle |  |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    const row = firstRow(rt.doc)
    expect(row?.fields.middle).toBe("")
  })

  it("value with unicode (French letters)", () => {
    const rt = roundTrip(
      "{database-row table=customers}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| name | Éléonore |\n" +
      "| city | Aix-en-Provence |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.fields.name).toBe("Éléonore")
  })
})

describe("database-row: table-name quoting variants", () => {
  it("bare identifier", () => {
    const rt = roundTrip(
      "{database-row table=orders}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| id | 1 |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.table).toBe("orders")
  })

  // The parser accepts double-quoted table names; the serializer
  // canonicalises to bare form when the name has no whitespace. Assert
  // the parse recognises both, and the second pass is stable.
  it("double-quoted table name parses (canonicalises on serialize)", () => {
    const rt = roundTrip(
      "{database-row table=\"orders\"}\n" +
      "| Field | Value |\n" +
      "| --- | --- |\n" +
      "| id | 1 |\n" +
      "\n",
    )
    expect(rt.isStable).toBe(true)
    expect(firstRow(rt.doc)?.table).toBe("orders")
  })
})

describe("database-row: missing companion table (no Field|Value block)", () => {
  it("row without a following field table stays a bare row", () => {
    const rt = roundTrip("{database-row table=orders}\n\nParagraph after.\n")
    expect(rt.isStable).toBe(true)
    const row = firstRow(rt.doc)
    expect(row?.table).toBe("orders")
    expect(Object.keys(row?.fields ?? {})).toHaveLength(0)
  })
})
