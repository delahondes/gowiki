# Blockquotes & Callouts

## 1. Basic blockquotes

Standard markdown blockquotes use `>` at the start of each line:

```markdown
> This is a blockquote.
> It can span multiple lines.
```

> In Gowiki, blockquotes are extended with classes, icons, colors, and layout options — making them useful for callouts, admonitions, and page layout.

## 1. Callout classes

Add a `{blockquote class=...}` directive before the blockquote to apply a predefined style:

```markdown
{blockquote class=tip}
> This is a helpful tip for the reader.
```

Available classes:

{blockquote class=tip}
> **Tip** — Green border. Use for helpful suggestions and best practices.

{blockquote class=note}
> **Note** — Blue border. Use for additional information or context.

{blockquote class=important}
> **Important** — Amber border. Use for critical information the reader must not miss.

{blockquote class=warning}
> **Warning** — Red border. Use for dangerous actions or potential data loss.

Each class includes a colored border, background, icon, and label — all automatic.

## 1. Raw syntax

In raw mode, the directive goes on the line before the blockquote:

```markdown
{blockquote class=important}
> Before using any database directive, an administrator must
> first create the table in Admin > Database.
```

All content inside the blockquote (bold, links, lists, etc.) works normally.

## 1. Custom blockquotes

The `custom` class gives you full control over appearance:

```markdown
{blockquote class=custom color=#e8f5e9 icon=lightbulb}
> A custom callout with a green background and lightbulb icon.
```

Custom properties (only available when class is `custom`):

| Property | Description | Values |
| --- | --- | --- |
| color | Background color | Any CSS color: `#e8f5e9`, `lightblue` |
| icon | Icon displayed before content | `lightbulb`, `info`, `warning`, `important` |
| width | Box width | `80%`, `400px` |
| align | Text alignment | `left`, `center`, `right` |

## 1. Layout: wrap

Blockquotes can float alongside text using the `wrap` property:

```markdown
{blockquote wrap=right width=40%}
> This box floats to the right, and text wraps around it on the left.

The surrounding text flows naturally around the floated blockquote, creating a column-like layout.
```

| Wrap value | Behavior |
| --- | --- |
| `left` | Float left, text wraps on the right |
| `right` | Float right, text wraps on the left |

This is useful for sidebar notes, pull quotes, or placing supplementary information alongside the main content.

## 1. Image width

The `image-width` property controls the maximum width of images inside the blockquote:

```markdown
{blockquote image-width=200px}
> ![Photo](./photo.jpg)
> Caption text below the image.
```

This is useful for constraining large images within a blockquote layout.

## 1. Property panel

In visual mode, click a blockquote to select it and open its property panel. The panel shows:
- **Class** dropdown — select the callout type
- **Wrap** — float direction
- **Image width** — constrain images

When `custom` is selected, additional properties appear: color, icon, width, and alignment.
