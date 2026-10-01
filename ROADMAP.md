# Roadmap

Living document. Items land here when they're decided but not yet
scheduled; the CHANGELOG records what has actually shipped.

## Deferred to v1.1 or later

### Fenced grouping block (a real "div") — separate from `>`

The blockquote (`>` prefix per line) is currently doing double duty as
both quotation and grouping (figure + caption bundles, todo groups,
callouts). That's beyond the CommonMark intent and leaks into editing
pain:

- Every content line inside a `>` block needs its own `>` prefix, so
  editing continuation lines around block-atom nodes (todos, images,
  includes) is fragile — a missing prefix reappears as raw
  `{todo …}` / `![]()` source in the visual editor.
- The prefix also complicates cursor movement into and out of the
  block.

The plan is to split the two use cases at the syntax level. Keep `>`
as **quotation** (the semantic markdown meaning), and add a fenced
grouping block for the div-like usage.

Candidate syntax: **Zulip's `` ```quote … ``` ``** — a triple-backtick
fence with a language-like tag. Rationale:

- Composes with our existing "info-string opens a fence" language and
  the fenced code block already used for code, mermaid, and slides.
  No new punctuation family to teach.
- Extends cleanly: `` ```figure ``, `` ```aside ``, `` ```callout `` — each
  is a named variant of the same fenced-div machinery.
- Reads as a first-class Gowiki construct rather than a repurposed
  CommonMark idiom.

**Bijectivity**: parse both `>` and the new fenced form on input, but
canonicalize to one form on save. If we ship the new form as canonical
for grouping and keep `>` canonical for quotation, we still need a
rewrite pass (or an author-driven migration flag) for pages currently
using `>` as a grouping div — otherwise the same source parses one way
and re-serializes as the other and every historical page churns on
next save. **Do not** ship the canonical switch as a silent flag on an
existing page's next save — stage it with an explicit migration.

**Also on the same topic**: today's `remark` node is more like a `div`
than a quote too. Whether it becomes a named variant of the new
fenced-div or stays its own construct is decided once the fenced-div
mechanics land.

**Not v1.0.0**. The RC window is for stabilisation. Interim editing
fixes (gap cursor between block-atoms, todo NodeSelection no longer
escapes when typed next to) shipped separately and don't touch the
dialect.

### Accept alternate table alignment on paste, canonicalize on save

Same pattern as the fenced-div work: we already reject CommonMark's
`| :---: |` / `| ---: |` alignment syntax in favour of column
directives, for bijectivity. Pasting Markdown from outside (Excel
exports, GitHub tables, etc.) still fails on that mismatch. Extend the
paste-side importer to accept the CommonMark alignment forms and
rewrite them to our column-directive canonical form before the
serializer sees them. Same rule as above: parse permissively, emit
canonically. No dialect change.

### Fragment-anchor scroll on pages with slow-rendering blocks

A direct hit on `/page#heading` scrolls to the heading's current
layout position at load time. On pages with mermaid diagrams (and
charts, images with no cached size, iframe-ish blocks) the diagram
renders a few hundred ms later, shifts every subsequent heading
down, and the scroll ends up above the actual anchor. Clicking the
same anchor from the in-page index after render works fine — the
layout is stable by then.

Candidate fix: cache the rendered dimensions of each mermaid/chart
block in the page meta sidecar at save time. The reader reserves
that height for the block before mermaid runs, so the layout is
stable at load even though the picture hasn't painted yet. Rough
shape:

- Save path: after the editor's mermaid NodeView has rendered, read
  `offsetHeight` and `offsetWidth`, write them to a per-block
  `rendered_size` field in the attic entry's `plugin_meta.mermaid`.
- Read path: the mermaid NodeView reserves that size via a CSS
  `min-height` / `min-width` before the diagram paints, and only
  releases it when the real render disagrees.
- Invalidation: the content hash of the mermaid source gates the
  cache — a source change wipes the cached size, so the diagram
  can grow or shrink without being clipped.

Not for rc.3. Out of scope for v1.0.0 unless a user hits it hard;
worth revisiting if a QMS page with many diagrams keeps mis-targeting
anchors. For now, the workaround is to scroll manually or follow
the link from the in-page index after the diagrams have painted.
