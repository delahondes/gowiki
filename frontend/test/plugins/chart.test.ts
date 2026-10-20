// chart plugin — ```chart <type> [options]\n<data lines>\n``` fenced block.
// Registers a `chart` PM node whose attrs carry the type, size, title,
// legend, values, align, colors, series (multi-series label), and the
// raw `name=value` body (or `name = v1 | v2 | …` for multi-series).
import { describe, it, expect } from "vitest"
import { roundTrip, countNodes } from "../helpers"

function firstChart(doc: any): Record<string, any> | null {
  let found: Record<string, any> | null = null
  doc.descendants((n: any) => {
    if (found === null && n.type.name === "chart") found = { ...n.attrs }
  })
  return found
}

describe("chart: basic fenced block", () => {
  it("minimal single-series pie round-trips", () => {
    const src = "```chart pie\nApples = 30\nPeaches = 23\n```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    expect(countNodes(rt.doc, "chart")).toBe(1)
    const attrs = firstChart(rt.doc)
    expect(attrs?.type).toBe("pie")
    expect(attrs?.data).toContain("Apples = 30")
    expect(attrs?.data).toContain("Peaches = 23")
  })

  it("size + title round-trip", () => {
    const src = '```chart bar 500x300 "Sales by Region"\nEurope = 45\nAsia = 52\n```\n'
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const a = firstChart(rt.doc)
    expect(a?.type).toBe("bar")
    expect(a?.width).toBe(500)
    expect(a?.height).toBe(300)
    expect(a?.title).toBe("Sales by Region")
  })
})

describe("chart: multi-series", () => {
  it('series="A,B" + pipe-separated values round-trip', () => {
    const src =
      '```chart bar "Mice vs Men" series="Mice,Men"\n' +
      "Height = 2.5 | 180\n" +
      "Weight = 25 | 75\n" +
      "```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const a = firstChart(rt.doc)
    expect(a?.type).toBe("bar")
    expect(a?.title).toBe("Mice vs Men")
    expect(a?.series).toBe("Mice,Men")
    expect(a?.data).toContain("Height = 2.5 | 180")
    expect(a?.data).toContain("Weight = 25 | 75")
  })

  it("pipe-values WITHOUT series= still parses — series names default", () => {
    const src = "```chart bar\n" + "Q1 = 10 | 20\n" + "Q2 = 15 | 25\n" + "```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const a = firstChart(rt.doc)
    expect(a?.series).toBe("")
    expect(a?.data).toContain("Q1 = 10 | 20")
  })

  it("series= WITHOUT pipe-values is tolerated (collapses to single-series)", () => {
    const src = '```chart bar series="Only"\nA = 10\nB = 20\n```\n'
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const a = firstChart(rt.doc)
    expect(a?.series).toBe("Only")
    expect(a?.data).toContain("A = 10")
  })

  it("title and series= coexist without colliding", () => {
    const src = '```chart line "Before vs After" series="Before,After"\n' + "Day 1 = 10 | 15\n" + "```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const a = firstChart(rt.doc)
    expect(a?.title).toBe("Before vs After")
    expect(a?.series).toBe("Before,After")
  })
})

describe("chart: options", () => {
  it("nolegend + values + align right round-trip", () => {
    const src = "```chart hbar nolegend values right\n" + "Europe = 45\n" + "Asia = 52\n" + "```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const a = firstChart(rt.doc)
    expect(a?.legend).toBe("false")
    expect(a?.values).toBe("true")
    expect(a?.align).toBe("right")
  })

  it("custom colors round-trip", () => {
    const src = "```chart line #2563eb #10b981\n" + "Q1 = 100\n" + "Q2 = 150\n" + "```\n"
    const rt = roundTrip(src)
    expect(rt.isStable).toBe(true)
    const a = firstChart(rt.doc)
    expect(a?.colors).toContain("#2563eb")
    expect(a?.colors).toContain("#10b981")
  })
})
