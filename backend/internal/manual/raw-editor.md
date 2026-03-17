# Raw Editor

The raw editor lets you edit the markdown source directly. Toggle to it using the **Raw** button in the toolbar.

## 1. When to use raw mode

- Precise control over markdown formatting
- Editing directives and properties
- Pasting pre-formatted markdown content
- Debugging rendering issues

## 1. Features

- Monospace font for clear character alignment
- Tab key inserts indentation (useful for lists and code blocks)
- Syntax is validated on publish — invalid constructs are rejected
- Line numbers displayed for reference

## 1. Round-trip guarantee

Switching between raw and visual mode is lossless. The markdown you write in raw mode will render identically in visual mode, and vice versa. This is a core guarantee of the bijective dialect.
