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

## 1. Quality checks

After writing a page, you can verify it renders correctly:

```
GET /api/render/{path}
```

This returns the rendered HTML via headless browser. Check:
- The `errors` array for JavaScript exceptions
- The HTML content for `⚠ Directive` error blocks (broken formatting)
- The `rendering` field: `"ok"` or `"failed"`

To scan all pages for rendering errors, combine `GET /api/sitemap` with `GET /api/render/{path}` for each page.

## 1. Document review

When asked to review a document for language, clarity, or consistency, follow this batch process:

1. **Fetch** the page content
2. **Generate a change document** — a numbered list of proposed changes, each with:
   - The original text
   - The suggested replacement
   - A brief reason for the change
   - Context notes for punctuation changes (e.g. "in table cell", "end of list item")
   - A `Decision:` field left empty for the reviewer
3. **Present** the change document for review — the reviewer annotates each proposal with accept, reject, or a clarification request
4. **Clarify** if needed — update proposals based on feedback, then present again
5. **Apply** all accepted changes at once and deploy

This avoids slow one-by-one back-and-forth. A typical review of a large document should take 2-3 exchanges, not dozens.

When reviewing English, pay attention to:
- False friends from French (planification, resume, follower, etc.)
- Passive voice in responsibility statements — prefer active voice
- Regulatory terms that should stay capitalized (Customer, Design Output, etc.)
- Missing articles, comma splices, dangling prepositions
- Formatting issues (missing periods, broken table cell line breaks)
