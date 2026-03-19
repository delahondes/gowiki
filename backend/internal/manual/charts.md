# Charts

Gowiki can render data charts directly in wiki pages using [Chart.js](https://www.chartjs.org/). Charts are authored as simple `name = value` pairs inside a fenced block.

## 1. Basic syntax

A chart is written as a fenced code block with the `chart` keyword:

`````markdown
````chart pie "Fruit Distribution"
Apples = 30
Peaches = 23
Strawberries = 25
Peanuts = 7
````
`````

This renders as:

````chart pie "Fruit Distribution"
Apples = 30
Peaches = 23
Strawberries = 25
Peanuts = 7
````

## 1. Chart types

| Type | Description |
| --- | --- |
| `pie` | Pie chart (default) |
| `doughnut` | Doughnut (ring) chart |
| `bar` | Vertical bar chart |
| `hbar` | Horizontal bar chart |
| `line` | Line chart |
| `radar` | Radar/spider chart |
| `polar` | Polar area chart |

If no type is specified, `pie` is used.

## 1. Options

Options appear after the chart type on the opening fence line, in any order:

| Option | Syntax | Default | Description |
| --- | --- | --- | --- |
| Size | `400x250` | `400x250` | Canvas size in pixels (width x height) |
| Title | `"My Title"` | (none) | Chart title displayed above the chart |
| Legend | `legend` / `nolegend` | `legend` | Show or hide the legend |
| Values | `values` | (hidden) | Display data values on the chart |
| Align | `left` / `center` / `right` | `center` | Horizontal alignment |
| Colors | `#rrggbb` | (auto) | Custom color palette — multiple colors can be listed |

## 1. Examples

**Horizontal bar chart with values:**

````chart hbar 500x300 values "Sales by Region"
Europe = 45
Americas = 38
Asia = 52
Africa = 12
````

**Line chart with custom colors:**

````chart line 600x300 #2563eb #10b981
Q1 = 100
Q2 = 150
Q3 = 130
Q4 = 200
````

**Doughnut chart, right-aligned, compact:**

````chart doughnut 250x250 right nolegend values
Yes = 75
No = 25
````

## 1. Data format

- One entry per line: `Label = Value`
- Value must be a number (integer or decimal, negative allowed)
- Blank lines and lines starting with `#` are ignored (comments)
- Minimum 1 data entry, maximum 50

```
# This is a comment
Apples = 30
Bananas = 25

# Blank lines are ignored
Oranges = 15
```

## 1. Color palette

When no custom colors are specified, charts use a built-in palette of 12 distinct, colorblind-friendly colors (Tableau 12). If there are more data points than colors, the palette cycles.

Custom colors override the palette:

`````markdown
````chart pie #e63946 #457b9d #1d3557
A = 40
B = 35
C = 25
````
`````

## 1. Editing charts

In visual mode:
- Use the **Chart** toolbar button to insert a chart with sample data
- Click a chart to select it and open the **property panel** (type, size, title, legend, etc.)
- Click **Edit data** in the property panel to edit the `name = value` lines in a text area

In raw mode, edit the fenced block directly.

## 1. Fallback

If the chart plugin is disabled, the fenced block renders as a plain code block with the data visible as text — no data is lost.
