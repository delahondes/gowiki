# Round-trip Testing

## Principle

The Gowiki markdown dialect is **bijective**: one canonical syntax per construct. This means `markdown → PM → markdown` must be an identity after normalization, and `markdown → PM → markdown → PM → markdown` must be stable (pass1 = pass2).

Any change to the parser, serializer, or plugin inline rules must pass round-trip validation before deployment.

## Test runner

```
cd frontend

# Run all built-in cases
npx tsx test-roundtrip.ts

# Test a specific string
npx tsx test-roundtrip.ts "==*hello*=="

# Test from file (e.g. a full page)
npx tsx test-roundtrip.ts --file /tmp/page.md
```

The test runner performs two round-trips and reports:
- **source → pass1**: whether the first normalize changes the input
- **pass1 → pass2**: whether the second normalize matches the first (stability)

A green `✓` means pass1 = pass2 (stable). A red `✗` means the round-trip is not idempotent — this is a bug.

## Regression trap pages

We should maintain a few wiki pages whose sole purpose is to exercise tricky syntax combinations. These pages should be loaded, edited (visual + raw), and saved periodically to catch regressions.

Recommended trap content:

### Highlight traps
- `==plain highlight==`
- `=={#ccffcc}colored highlight==`
- `==[*italic inside brackets in highlight*]==`
- `==\{\}*escaped braces then italic*==`
- `=={#ccffcc}\{\}**bold colored**==`
- `==text with = signs inside==`

### Highlight in table cells
- `| ==[*placeholder*]== |` — highlight with brackets in cell
- `| ==\*literal stars\*== |` — escaped stars in highlight in cell
- `| =={#cce5ff}colored cell== |` — colored highlight in cell

### Mark nesting
- `==**bold highlight**==`
- `==*italic* and **bold** in one highlight==`
- `[==highlighted link text==](https://example.com)` — link containing highlight
- `==text with [link](url) inside==` — highlight containing link

### Escaped characters in highlights
- `==\{\}==` — escaped curly braces
- `==\*not italic\*==` — escaped stars
- `==\\backslash\\==` — escaped backslashes
- `==\{not a color\}text==` — escaped braces that look like color syntax

### Ordered/unordered lists with highlights
- `1. =={#ccffcc}\{\}**item**==`
- `- ==highlighted list item==`

### Formulas in table cells (not highlights)
- `| =SUM(A1:A5) |` — formula must NOT be treated as highlight
- `| =A1+B1 |` — simple formula

## What to check

1. **Raw mode blur**: type the trap content in raw mode, click away. Content must not change.
2. **Visual round-trip**: switch raw → visual → raw. Content must not change.
3. **Save + reload**: save the page, reload, enter edit. Content must match.
4. **Table cells**: highlights inside table cells are the most fragile — always test these.

## Known past issues

| Symptom | Root cause | Fix |
| --- | --- | --- |
| `\\\*` → `\\\\*` each save (backslash doubling in table cells) | `formula_protect` escaped `*` in cells starting with `=`, including `==` highlights | Skip formula_protect when cell starts with `==` |
| `==[*text*]==` disappears on blur | `[color]` extraction consumed entire content as color attribute | Changed to `{color}` syntax; validate color with `/^#?[a-zA-Z0-9]+$/` |
| `==text1====text2==` (`====` duplication) | Per-node mark wrapping closed/reopened `==` at every mark boundary | Mark-tracking serializer in paragraph/heading printers |
| Color lost on serialize | Mark-tracking function imported cross-module, registry mismatch | Moved mark-tracking to local function in core_nodes.ts |
