# Highlight Mark — Specification

## Syntax

```
==highlighted text==
=={color=red}highlighted text==
=={color=#ff9900}highlighted text==
```

- `==text==` — default highlight (yellow background)
- `=={color=VALUE}text==` — colored highlight, where VALUE is a named CSS color or hex code
- The `{color=VALUE}` prefix is optional; when absent, defaults to yellow

## Mark schema

- **Name**: `highlight`
- **Attrs**: `color` (string, default: `"yellow"`)
- **Inclusive**: true (typing at the edge extends the mark)

## Parsing

The mark is parsed as a markdown-it inline rule:

1. Detect `==` opening delimiter
2. If followed by `{color=`, parse color value until `}`, then parse content until closing `==`
3. If not followed by `{color=`, parse content until closing `==` with default color
4. Nesting of other inline marks inside is allowed (`==*bold highlight*==`)
5. The `==` delimiter must not be preceded/followed by whitespace (same as `*italic*` — no `== spaced ==`)
6. Color value must match `/^#?[a-zA-Z0-9]+$/` — only color names or hex codes

Regex pattern for the full match: `==(?:\{color=([^}]+)\})?(.*?)==`

## Serialization

- Default color: `==text==`
- Non-default color: `=={color=VALUE}text==`

Bijectivity: the serializer always omits `{color=yellow}` since it's the default. Any other color is always written.

## Rendering

```html
<mark style="background-color: yellow">text</mark>
```

Or for custom colors:

```html
<mark style="background-color: #ff9900">text</mark>
```

In ProseMirror `toDOM`:

```typescript
["mark", { style: `background-color: ${color}` }, 0]
```

## Toolbar

A toolbar button with a dropdown for color selection:
- Yellow (default)
- Red, green, blue, orange, pink, cyan
- Custom hex input

Toggling: clicking the button applies/removes the default yellow highlight. The dropdown allows changing color.

## Editor behavior

- `==` typed in raw mode triggers the mark
- In visual mode, selecting text and clicking the highlight button wraps it
- The mark is toggleable: applying highlight to already-highlighted text removes it

## Interaction with other marks

Highlight can be combined with any other inline mark:
- `==**bold highlight**==` — bold + highlight
- `==*italic highlight*==` — italic + highlight
- `==` `code highlight` `==` — not supported (code marks protect content from further parsing)

## Implementation

This is a simple inline mark plugin, similar to `strikethrough` (`~~text~~`). The only addition is the optional `{color=VALUE}` prefix.

### Files to modify

1. **New plugin**: `frontend/plugins/highlight.ts` — schema, parser rule, serializer, toolbar command, CSS
2. **Plugin index**: `frontend/plugins/index.ts` — register the plugin
3. **CLAUDE.md dialect table**: add `=={color=VALUE}highlight==` as implemented
