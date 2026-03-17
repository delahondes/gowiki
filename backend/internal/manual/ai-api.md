# AI Content API

The AI Content API lets AI assistants (Claude, ChatGPT, Copilot, local LLMs, etc.) interact with the wiki on your behalf.

## 1. Getting started

1. Ask your admin to enable the AI API in Admin > Configuration
2. Click **API Tokens** in the top-right banner and create a token
3. Give the token to your AI assistant

## 1. What the AI can do

- **Read pages** — fetch content, metadata, history
- **Search** — full-text search across the wiki
- **Browse** — list pages and namespaces
- **Preview changes** — see a diff before saving
- **Write pages** — update content (with required summary)

## 1. Endpoints

The full API is described at `/api/openapi.json` (OpenAPI 3.1 schema).

| Endpoint | Description |
| --- | --- |
| GET /api/ai/v1/conventions | Wiki rules the AI must follow |
| GET /api/ai/v1/namespace/{path} | List pages and sub-namespaces |
| POST /api/ai/v1/batch/read | Read up to 20 pages at once |
| GET /api/ai/v1/meta/{path} | Page metadata (tags, reviewflow, backlinks) |
| POST /api/ai/v1/preview/{path} | Dry-run diff without saving |
| GET /api/pages/{path} | Read a single page |
| PUT /api/pages/{path} | Write a page |
| GET /api/search?q=... | Full-text search |

## 1. Authentication

Pass the token as a header or query parameter:

```
Authorization: Bearer gwk_<token>
```

Or:

```
https://wiki.example.com/api/ai/v1/meta/page?token=gwk_<token>
```

## 1. How it works with Claude Code

Claude Code can use the API directly via `curl` or `fetch`. Example workflow:

1. Fetch conventions: `GET /api/ai/v1/conventions`
2. Read pages: `POST /api/ai/v1/batch/read` with paths
3. Make changes to the markdown
4. Preview: `POST /api/ai/v1/preview/{path}`
5. If the diff looks right, write: `PUT /api/pages/{path}`

## 1. Safety

- All changes are attributed to your user account
- Summaries are required (convention: `[AI: Claude] description`)
- ACL is enforced — the AI has exactly your permissions
- Rate limits prevent runaway loops
- Tokens can be revoked instantly
