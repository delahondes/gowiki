# Inline Directives — Specification

## Motivation

Directives (`{name key=value}`) currently must appear on their own line before the target block. This works well for block-level targets (tables, blockquotes, includes) but creates problems for inline targets — particularly images inside paragraphs and list items.

When an image inside a paragraph is resized (gaining `{image size=70%}`), the current rules force it to become block-level because the directive requires its own line. This triggers an inline-to-block conversion that:
- Breaks the paragraph structure
- Is especially destructive in list items (splits the list)
- Requires a block-to-inline conversion to undo (lossy)
- Is the source of recurring bugs

## Proposal

Allow a directive to appear on the same line as its target, immediately before it, with no whitespace-only separation required.

### Syntax

**Block form** (existing, unchanged):
```
{image size=70%}
![Alt text](./photo.png)
```

**Inline form** (new):
```
{image size=70%}![Alt text](./photo.png)
```

The directive is attached to the immediately following content on the same line. The `}` closing brace marks the end of the directive; the target begins at the next non-space character.

### Rule

A directive is parsed as inline when:
1. It appears at the start of a line (or after other inline content)
2. It is followed by its target on the **same line**

A directive is parsed as block when:
1. It occupies a line by itself
2. Its target is the **next line**

Both forms produce the same ProseMirror node structure — the difference is purely syntactic. The form is determined by context, not by author choice.

## Scope

This applies to all directives generically. The parser does not need to know which directive names exist — it recognizes `{...}` and attaches the properties to whatever follows.

In practice, the inline form will primarily be used for:
- `{image size=...}![](...)` — resized images in paragraphs and lists
- `{image size=... caption="..."}![](...)` — captioned inline images

Block-level targets (tables, blockquotes, includes) will continue to use the block form because their targets are inherently multi-line.

## Serialization

The serializer chooses the form based on context:

| Context | Form | Example |
| --- | --- | --- |
| Image is the sole child of a paragraph (block-level) | Block | `{image size=70%}\n![](./photo.png)` |
| Image is inline (inside a paragraph with other content, or inside a list item) | Inline | `{image size=70%}![](./photo.png)` |
| Table, blockquote, include (always block) | Block | `{table headers=1c}\n\| ... \|` |

This preserves bijectivity: given a ProseMirror document, the serializer deterministically produces one canonical form.

## Parser changes

The markdown-it parser currently recognizes directives only as standalone block-level tokens. The change:

1. **Block directive rule** (existing): a line matching `^{name ...}$` followed by a blank or block start on the next line → block directive token.
2. **Inline directive rule** (new): a line matching `^{name ...}` followed by non-directive content on the same line → the directive properties are attached to the following inline content. The `{...}` prefix is stripped and the rest of the line is parsed normally, with the directive properties carried forward.

Edge cases:
- `{image size=70%}` alone on a line: remains a block directive (existing behavior)
- `{image size=70%}![](./photo.png)` on a line: inline directive + image
- `{image size=70%} some text` on a line: inline directive + text (directive is carried but has no valid target — the properties are silently dropped, same as current behavior for mismatched directives)
- `text {image size=70%}![](./photo.png)` mid-line: **not supported** in this spec — directives must start at the beginning of the line (or at the beginning of inline content within a list item/cell). This avoids ambiguity with `{...}` patterns in prose.

## Serializer changes

The `printNode` function for images currently always emits the directive on a separate line. The change:

1. Check whether the image node is inline (has siblings in its parent paragraph, or parent is a list item with other content).
2. If inline: emit `{image ...}![](...)` on one line.
3. If block (sole child of paragraph): emit `{image ...}\n![](...)` on two lines (existing behavior).

## Impact on existing documents

- All existing documents remain valid — the block form is unchanged.
- No migration needed.
- Documents with inline images that are resized will naturally serialize to the inline form on next save.

## Round-trip validation

The following must hold:
- `parse(serialize(doc)) == doc` for documents containing inline directives
- `serialize(parse(markdown)) == markdown` for both inline and block directive forms
- Switching an image between inline and block (by adding/removing surrounding text) changes the directive form but preserves all properties

## Implementation plan

### Phase 1 — Parser
- Modify the markdown-it directive rule to detect inline directives
- Attach directive properties to the following inline content
- Test with `{image size=70%}![](./photo.png)` in paragraphs, list items, and table cells

### Phase 2 — Serializer
- Detect inline vs block context for image nodes
- Emit the appropriate form
- Test round-trip: parse → serialize → parse must be stable

### Phase 3 — Editor behavior
- Remove the inline-to-block conversion on resize — resizing an inline image now just adds/updates the `{image}` directive inline
- The "Convert to inline" / "Convert to block" button in the properties panel becomes unnecessary for the size property (may still be useful for other reasons)

### Phase 4 — Validation
- Full round-trip test suite for inline directives in all contexts (paragraph, list, table cell, nested list)
- Verify that disabling the image plugin doesn't corrupt documents with inline directives
