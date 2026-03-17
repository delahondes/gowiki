# AI Conventions

This page describes the rules that AI agents must follow when working with wiki content. These rules are also available programmatically at `GET /api/ai/v1/conventions`.

## 1. Markdown dialect

Gowiki uses a bijective markdown dialect. Key rules:

- `*italic*` only — `_text_` means underline, NOT italic
- `**bold**` only — `__bold__` is rejected
- `-` for unordered lists — `*` as list marker is rejected
- ATX headings only (`#`) — setext headings rejected
- Raw HTML is forbidden
- HTML entities are not interpreted — use UTF-8 directly
- Single newline = hard line break in paragraphs
- `\n` literal = line break in lists and tables only

## 1. Content rules

- Do not introduce alternative markdown syntaxes
- Do not silently change document structure
- Do not remove or reformat content you were not asked to change
- Preserve existing formatting conventions in the document

## 1. Write conventions

- Always include a summary: `[AI: <tool>] <description>`
- Use optimistic locking: read the version, write with `expected_version`
- Do not create extension-less files under `data/content/`
- Do not create `path/ns.md` if `path/ns/` directory exists

## 1. Link conventions

- `/path/to/page` — rendered page link
- `./page` — relative page link
- `./file.ext` — attachment/media link
- Links with extensions are always treated as media files
