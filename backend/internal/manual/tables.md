# Tables

## 1. Basic syntax

```
| Header 1 | Header 2 | Header 3 |
| --- | --- | --- |
| Cell 1 | Cell 2 | Cell 3 |
| Cell 4 | Cell 5 | Cell 6 |
```

## 1. Editing tables

![Table editing in visual mode](./screenshots/09.png)

In visual mode:
- **Tab** moves to the next cell
- **Shift+Tab** moves to the previous cell
- Use the toolbar or right-click menu to add/remove rows and columns

In raw mode, edit the pipe-delimited text directly.

## 1. Cell content

Cells support inline formatting: bold, italic, code, links, and inline directives like `{reviewflow-link version=2.0}`.

For multi-line content within a cell, use `\n` for line breaks:

```
| Version | Description |
| --- | --- |
| 1.0 | - First change,\n- Second change |
```

## 1. Table properties

Select a table to open its property panel. Available properties include size settings.

## 1. Limitations

- Column alignment syntax (`|:---|`, `|:---:|`, `|---:|`) is not supported
- Multi-body tables (multiple `<tbody>` sections) are not supported
