# Blockquotes — Specification

## 1. Overview

Gowiki supports two blockquote forms:

1. **Prefix form** (today, default) — CommonMark-style `>` at the start of
   each line:

   ```markdown
   > A quote.
   > More of the quote.
   ```

2. **Fence form** (planned, see §3) — triple-angle-bracket fence around
   the quoted content, with the same attribute directive as any other
   block:

   ```markdown
   {blockquote class=important image-bg=invert}
   >>>
   ![](diagrams/flow.png)

   This paragraph quotes a rich block — image with its own `{image …}`
   directive, a table, an include, whatever.
   >>>
   ```

Both forms produce the same PM `blockquote` node and the same stored
HTML; they differ only in what's expressible inside them.

---

## 2. Prefix form (status: shipped)

The prefix form is how every blockquote is written today. Every line is
prefixed with `> ` (or nested with `> > `). Attribute directives are
supported on the outer line:

```markdown
{blockquote class=tip image-width=60%}
> Text here.
> More text.
```

### Known limitation

Inline directives placed on a line inside the blockquote fail in some
cases. Specifically, an image directive placed on a line that starts
with `> ` either loses its attributes entirely or produces broken
round-trip serialization, because the markdown-it parser treats the `> `
prefix as blockquote-opening syntax **before** the inline directive
parser sees the content. Examples of things that do *not* work reliably
inside a prefix blockquote today:

- Per-image sizing — the `{image size=…}` directive on an image line
  inside a `> ` block is stripped.
- Table directives — `{column=…}` cell directives inside a table
  embedded in a prefix blockquote.
- Any multi-line block (slide, chart, mermaid, include) — the inner
  block cannot open a nested fenced structure because `>` interferes
  with the fence detection.

The project's workaround today is to set block-level directives on the
blockquote itself (`image-width`, `image-bg`, etc.) so authors don't
need per-item customisation inside. That's a partial fix — see §3.

---

## 3. Fence form (status: planned)

### 3.1 Motivation

The `>` prefix is a universal line decoration in Markdown, which is
convenient for short quotes but breaks down the moment the block's
content wants to be itself complex — nested fences, multi-line inline
directives, tables with cell directives, other blocks. Every other
rich-content construct in Gowiki (code blocks, mermaid, chart, slide,
include) uses a triple-backtick-style fence; blockquote is the last
holdout.

A fenced blockquote would parse its body as **ordinary wiki content**
(paragraphs, images, tables, includes, everything) so that authors can
put whatever they want inside without worrying about `>` interference.

### 3.2 Syntax

Open with a line containing exactly three greater-than signs, close with
the same token:

```markdown
>>>
Anything goes here.

- lists work
- tables work
- images with full directive support work:

{image size=400px bg=invert}
![A diagram](/media/flow.png)
>>>
```

Attribute directives go on a preceding line, same pattern as every
other block:

```markdown
{blockquote class=important}
>>>
Body content — rich markdown, no `>` anywhere.
>>>
```

**Rationale for `>>>` rather than `` ```blockquote ``:**

- Visually echoes the prefix form (`>` → `>>>`).
- Keeps triple-backtick exclusive to "code-ish" content (code blocks,
  mermaid, chart, etc.), which matches reader expectation.
- Doesn't collide with any existing Gowiki or CommonMark construct.

### 3.3 Semantics

The fence form produces the **same PM node** as the prefix form
(`blockquote`). Nothing in the document model, the stored HTML, or the
rendered output distinguishes them — they're two syntaxes for the same
thing.

The serializer is allowed to choose either form at save time. A
reasonable policy: preserve the input form on round-trip; when the
content can't be expressed in prefix form (contains a nested fence, an
image with inline directive, etc.), upgrade to fence form. Authors can
always manually pick either.

### 3.4 Bijectivity

Gowiki's dialect is bijective: one canonical syntax per construct. The
two-form design here doesn't break that, because the **canonical** form
is determined by content, not author preference:

- Content that fits prefix form → prefix form.
- Content that *requires* fence form → fence form.

The serializer's job is to pick the minimum form that works. Round-trip
is defined as: parse → serialize produces the same markdown **only when
both inputs are in canonical form**. Non-canonical input (e.g. fence
form used for content that would fit prefix) is normalized on save.

### 3.5 Parser implementation notes

A fenced blockquote parses its body with the **block** state of
markdown-it, pushing the body onto the token stream as if it were a
top-level sequence. The wrapper becomes a single `blockquote_open` /
`blockquote_close` pair surrounding whatever the body produced.

Concretely:

1. Detect `^>>>\s*$` in block ruler.
2. Scan forward for the matching `>>>` close fence (depth counting for
   nested `>>>` inside, though this is an unusual case).
3. Extract the body, feed it back into the block tokenizer with a
   fresh env, collect the resulting tokens.
4. Wrap with `blockquote_open` / `blockquote_close`, carrying any
   preceding `{blockquote …}` directive attrs.

### 3.6 PM ↔ Markdown round-trip

Serializer decision tree:

```
blockquote node →
  does any descendant require fence form?
    * nested blockquote, fenced code block, mermaid, chart, slide, include
    * image with inline directive that would be lost under `> `
    * table with cell-level directives
  ├─ yes → emit `>>>\n<body>\n>>>\n`
  └─ no  → emit prefix form, one `> ` per line
```

Every blockquote's `{blockquote ...}` directive (class, image-width,
image-bg, etc.) is emitted on the line above, regardless of form.

### 3.7 Editor UX

Insertion via the toolbar / command palette produces the prefix form by
default (it's what most users expect). Upgrading to fence form is
automatic on save when the content requires it. The visual editor shows
a blockquote identically either way — the form distinction is purely at
the serialization layer.

### 3.8 Non-goals

- **Different visual styling by form.** Both render identically.
- **Nesting `>>>` inside `>>>`.** Allowed in principle (depth-counted
  parse), but unusual; no special editor support.
- **Backward-incompatible removal of prefix form.** Prefix form stays
  forever — it's universal and simple. Fence form is an opt-in for
  rich-content cases.

---

## 4. Implementation cost

Tier-1 (prefix form + block-level image directives): already shipped.

Tier-2 (fence form): roughly one day of focused work — block ruler,
body re-tokenisation, serializer upgrade-to-fence logic, round-trip
tests. The main risk is edge cases around nested fences of other kinds
(a fenced blockquote containing a fenced code block containing a fenced
blockquote). Conservative implementation: support only one level of
re-tokenisation in v1; nested fences count as plain text until a later
iteration.

---

## 5. Decisions

1. **Two syntaxes, one model** — the fence form doesn't create a new
   node type. `blockquote` stays the same PM node; only the source
   syntax differs.

2. **Prefix form as default** — authors typing a quote naturally reach
   for `> `. The fence form exists to unblock complex content, not to
   replace the simple case.

3. **Serializer decides** — not the author. Authors can manually switch
   forms, but on save the canonical form is picked by content
   requirements.

4. **`>>>` fence delimiter** — mirrors `>` for readability, avoids
   overlap with code-fence syntax, doesn't collide with anything
   existing.

5. **No styling difference** — rendering is identical, so switching
   forms is purely an encoding choice with no visible consequence.