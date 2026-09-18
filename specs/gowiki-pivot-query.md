# Feature request — pivot mode for `{database-query}`

Raised 2026-09-17 while modelling the IOScope/BiomScope **version matrix (VMA)**
in Gowiki tables.

## 1. Problem

Some registers are naturally read as a **cross-tabulation**: one axis is a
released version, the other is a software component, and the cell is the version
of that component embedded in that release.

Today this is kept in a spreadsheet, one column per component:

| Biomscope Pipeline | Biomscope Gate | image name | date | pipeline tag | docker | metagen tag | piper | magicparser | bowtie | fastp | samtools | seqtk | eskrim |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 0.4 | 0.91 | pipeline-v0.4 | 2022-01-13 | v0.4 | metagen-all-in-one:0.6.6.4 | v0.6.6 | v0.6.1 | v0.4.2 | 2.4.4 | 0.22.0 | 1.13 | 1.3 | 1.0.1.1 |

That shape does not survive the move to a relational table, for two reasons.

**Adding a component means adding a column.** The component list is data, not
schema. A wide table forces a schema change — `create_database_field` — every
time a dependency appears, and the column set drifts away from the repository
inventory that is supposed to be authoritative.

**The questions that matter cannot be asked.** When a CVE lands on
`bowtie 2.4.4`, the question is *which released versions embed it*. On the wide
sheet that is a manual read of one column. In a normalized table it is
`filter="component.name=bowtie&version=2.4.4"`.

So the storage must be normalized — **one row per (release, component)** — and
the matrix becomes a matter of *rendering*, which `database-query` cannot do
today. Ninety rows replace six, and the register stops being readable.

This request asks for the rendering, so that normalized storage costs nothing in
legibility.

## 2. Proposed syntax

Three new parameters on the existing directive, consistent with the current flat
parameter style:

```markdown
{database-query table=release_component pivot_rows=release pivot_cols=component pivot_cell=version}
```

| Parameter | Required | Description |
|---|---|---|
| `pivot_rows` | yes | Field whose distinct values become the rows |
| `pivot_cols` | yes | Field whose distinct values become the columns |
| `pivot_cell` | yes | Field rendered inside each cell |
| `pivot_agg` | no | Collision policy: `single` (default), `list`, `count`, `first`, `last` |
| `pivot_empty` | no | Text for a cell with no matching row. Default: empty |
| `pivot_cols_sort` | no | Field of the column axis used to order columns |
| `pivot_cols_max` | no | Refusal threshold on column count. Default 40 |

Passing any `pivot_*` parameter puts the directive in pivot mode. `fields` is
ignored in that mode — the column set comes from the data.

## 3. Semantics

1. Apply `filter` as today, on the underlying rows.
2. Group the surviving rows by the pair (`pivot_rows`, `pivot_cols`).
3. Render one table row per distinct `pivot_rows` value, one column per distinct
   `pivot_cols` value, and `pivot_cell` inside.
4. `sort` and `order` apply to the **row axis**, as they do today to rows.
5. Column order follows `pivot_cols_sort` when given; otherwise the column
   table's own default sort when the axis is a `lookup`/`tag`; otherwise
   alphabetical.

## 4. Worked example

With `component` and `release_component` modelled as:

```
component         : name · kind (Component|Subcomponent|SOUP) · parent (lookup)
release_component : release (lookup) · component (lookup) · version
```

the directive

```markdown
{database-query table=release_component pivot_rows=release pivot_cols=component
                pivot_cell=version filter="component.kind=SOUP" sort=release order=desc}
```

renders the SOUP section of the VMA — six rows, five columns — from rows that
remain individually queryable. The internal-component section is the same
directive with `filter="component.kind<>SOUP"`.

## 5. Behaviours we depend on

**Misconfiguration must be loud.** Filter expressions are currently dropped in
silence when they do not resolve, which makes a view that quietly shows
everything while claiming to filter. For pivot mode, an unknown field in
`pivot_rows`, `pivot_cols` or `pivot_cell` must render a visible error, not an
empty or unpivoted table. In a regulated register, a view that silently shows
the wrong thing is worse than one that refuses.

**Cell collisions must not be hidden.** If two rows fall in the same cell and
`pivot_agg` is left at `single`, render both values with a visible marker rather
than picking one. Silently keeping the first would make a duplicate look like a
clean entry.

**The column guard should refuse, not truncate.** Beyond `pivot_cols_max`,
return an error naming the count. A silently truncated matrix would be read as
complete.

**Links follow the existing convention.** If the row axis, column axis or cell
resolves to a page-bound row, render it as a link, consistent with the `%field%`
prefix in normal mode.

## 6. Out of scope

- Aggregation beyond the `pivot_agg` list — no sums, averages or computed
  measures. The cell is a value, not a metric.
- More than two axes.
- Editing from the pivot view. `{database-newrow}` stays row-oriented.
- CSV export, which stays normalized. The pivot is a view, not a storage shape.

## 7. Value beyond this case

Any register keyed by two dimensions gains from it, for instance training
records by person and by course, supplier qualification by supplier and by
criterion, or test coverage by requirement and by test campaign. Each is
currently either a spreadsheet outside the wiki or a list nobody reads.
