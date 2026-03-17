# Markdown Syntax

Gowiki uses a custom Markdown dialect. It is **bijective**: each formatting has exactly one canonical syntax. This ensures lossless round-trips between the visual editor, raw editor, and stored content.

## 1. Inline formatting

| Syntax | Result | Notes |
| --- | --- | --- |
| `*italic*` | *italic* | `_text_` is NOT italic — it is underline |
| `**bold**` | **bold** | `__text__` is rejected |
| `_underline_` | _underline_ | Different from CommonMark |
| `~~strikethrough~~` | ~~strikethrough~~ | |
| `` `code` `` | `code` | Inline code |
| `~subscript~` | ~subscript~ | |
| `^superscript^` | ^superscript^ | |
| `^[footnote text]` | ^[footnote text] | Inline footnote |

## 1. Headings

ATX headings only (setext headings are rejected):

```
# Heading 1
## Heading 2
### Heading 3
```

Numbered headings use a `1.` prefix:

```
## 1. First Section
## 1. Second Section
```

## 1. Lists

Unordered lists use `-` only (not `*`):

```
- First item
- Second item
  - Nested item
```

Ordered lists use `1.`:

```
1. First item
2. Second item
```

Use Tab/Shift+Tab to increase/decrease nesting.

## 1. Links

**Internal page links:**

```
[Display text](/path/to/page)
[](/path/to/page)
```

An empty label `[]` automatically displays the page name.

**External links:**

```
[Example](https://example.com)
```

External links display with a distinct icon and open in a new tab.

**Media/attachment links:**

```
[Download](./document.pdf)
```

Links to files with extensions are treated as attachment downloads.

## 1. Images

```
![Alt text](./image.png)
```

Images support drag-resize in the visual editor. The size is stored as a property:

```
{image size=400px}
![Alt text](./image.png)
```

## 1. Tables

```
| Header 1 | Header 2 | Header 3 |
| --- | --- | --- |
| Cell 1 | Cell 2 | Cell 3 |
| Cell 4 | Cell 5 | Cell 6 |
```

Use Tab to navigate between cells. Column alignment syntax is not supported.

## 1. Code blocks

````
```python
def hello():
    print("Hello, world!")
```
````

Language-specific syntax highlighting is applied automatically.

## 1. Line breaks

- A single newline in a paragraph produces a hard line break (`<br>`)
- Trailing spaces have no meaning (unlike CommonMark)
- In lists and tables, use `\n` for an explicit line break

## 1. Properties / Directives

Properties are written on their own line before the target block:

```
{pluginname key=value key2="value with spaces"}
```

Self-contained directives stand alone:

```
{reviewflow version=1.0 author=alice reviewer=bob}
{tag sop}
{include path=/wiki/sidebar}
```

## 1. What is NOT supported

- Raw HTML is forbidden — `<` and `>` are plain characters
- HTML entities are not interpreted — use UTF-8 directly
- `_text_` is underline, not italic
- `*` as a list marker is rejected
- Setext headings (underline-style) are rejected
- Column alignment in tables is not supported
- Multi-body tables are not supported
