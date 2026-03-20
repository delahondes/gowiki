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
| GET /api/render/{path} | Render a page as HTML (headless browser) |
| GET /api/search?q=... | Full-text search |

## 1. Authentication

Pass the token as a header or query parameter:

```
Authorization: Bearer gwk_<token>
```

Or:

```
https://{{SERVER}}/api/ai/v1/meta/page?token=gwk_<token>
```

## 1. How it works with Claude Code

Claude Code can use the API directly via `curl` or `fetch`. Example workflow:

1. Fetch conventions: `GET /api/ai/v1/conventions`
2. Read pages: `POST /api/ai/v1/batch/read` with paths
3. Make changes to the markdown
4. Preview: `POST /api/ai/v1/preview/{path}`
5. If the diff looks right, write: `PUT /api/pages/{path}`

## 1. Quick start prompt

Copy and paste this prompt to any AI assistant (Claude, ChatGPT, Copilot, etc.) to get started. Replace the URL and token with your own:

{blockquote class=tip}
> I want you to be able to read and manage content in my wiki. Here is how to get started:
>
> 1. Fetch the API conventions by reading this URL: @`https://{{SERVER}}/api/ai/v1/conventions?token=gwk_YOUR_TOKEN_HERE` — read the response carefully, it contains the markdown dialect rules and content guidelines you MUST follow.
> 2. Fetch the OpenAPI schema at @`https://{{SERVER}}/api/openapi.json` to discover all available endpoints.
> 3. For authentication, append `?token=gwk_YOUR_TOKEN_HERE` to every API URL.
>    I will tell you what I need right after this.

Adapt the prompt to your needs — for example, you can add:

> Browse the namespace `/docs/` and give me a summary of all pages there.

Or:

> Read the page `/projects/alpha` and translate section 3 to English. Show me a preview diff before saving.

Or:

> Search for all pages mentioning "deployment" and list them with their last modified date.

## 1. Additional API access

Beyond page content, the AI can also access:

- **Database tables** — query rows (`GET /api/database/{table}/rows`), insert rows (`POST`), update rows (`PUT`), get schema (`GET /api/database/{table}/schema`)
- **Comments** — read and create page comments (`/api/plugin/comment/v1/comments/{path}`)
- **Todo tasks** — list and manage tasks (`/api/plugin/todo/v1/tasks`)
- **Reviewflow** — check validation status (`/api/plugin/reviewflow/v1/status/{path}`)
- **Page operations** — move pages (`POST /api/move/{path}`), view history (`GET /api/history/{path}`), search (`GET /api/search?q=...`)

All endpoints respect your ACL permissions.

## 1. Safety

- All changes are attributed to your user account
- Summaries are required (convention: `[AI: Claude] description`)
- ACL is enforced — the AI has exactly your permissions
- Rate limits prevent runaway loops
- Tokens can be revoked instantly
