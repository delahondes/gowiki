# Highlight

Highlight text with a colored background.

## 1. Syntax

```
==highlighted text==
=={color=red}highlighted text==
=={color=#ff9900}highlighted text==
```

- `==text==` — default yellow highlight
- `=={color=VALUE}text==` — custom color (CSS color name or hex code)

## 1. Examples

| Syntax | Result |
| --- | --- |
| `==important==` | Yellow highlight |
| `=={color=#ccffcc}approved==` | Green highlight |
| `=={color=#cce5ff}info==` | Blue highlight |
| `=={color=#ffcccc}warning==` | Red highlight |

## 1. Using in the editor

- Select text and click the highlight button (crayon icon with yellow underline) in the toolbar
- In raw mode, wrap text with `==` delimiters
- To change color, edit the `{color=VALUE}` prefix in raw mode, or use the color dropdown next to the highlight button

## 1. Nesting

Highlight can be combined with other formatting:

```
==**bold highlight**==
==*italic highlight*==
```
