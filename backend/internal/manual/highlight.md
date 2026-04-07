# Highlight

Highlight text with a colored background.

## 1. Syntax

```
==highlighted text==
=={red}highlighted text==
=={#ff9900}highlighted text==
```

- `==text==` — default yellow highlight
- `=={color}text==` — custom color (CSS color name or hex code)

## 1. Examples

| Syntax | Result |
| --- | --- |
| `==important==` | Yellow highlight |
| `=={#ccffcc}approved==` | Green highlight |
| `=={#cce5ff}info==` | Blue highlight |
| `=={#ffcccc}warning==` | Red highlight |

## 1. Using in the editor

- Select text and click the highlight button (crayon icon with yellow underline) in the toolbar
- In raw mode, wrap text with `==` delimiters
- To change color, edit the `{color}` prefix in raw mode

## 1. Nesting

Highlight can be combined with other formatting:

```
==**bold highlight**==
==*italic highlight*==
```
