# AI Content API — Specification

## 1. Overview

The AI Content API allows external AI assistants (ChatGPT, Claude, Copilot, local LLMs, etc.) to interact with Gowiki on behalf of a user. The AI authenticates as the user via a personal API token and is bound by the same ACL rules, reviewflow constraints, and audit trail as any browser session.

### Design Goals

- Any user can delegate content tasks to their AI of choice — the wiki doesn't mandate a specific provider
- The AI operates under the user's identity and permissions — no privilege escalation
- All changes made via the API are attributed to the user and appear in page history
- The API surface covers content management only — no admin, no ACL, no auth management
- Batch-friendly: AI workflows are chatty, so key operations support multi-page access
- Safe by default: dry-run/preview before committing changes

### Non-Goals

- The AI API does not bypass reviewflow validation
- No admin operations (user management, ACL, config, database schema)
- No direct media binary upload (use the existing `POST /api/media` endpoint with the same token)
- No real-time collaboration (SSE/WebSocket) — the AI works in request/response mode
- No AI provider integration on the server side — Gowiki is the content layer, not the AI layer

---

## 2. Authentication

### Personal API Tokens

Each user can generate one or more long-lived API tokens. Tokens authenticate API requests as that user with the same effective permissions (ACL, group membership).

#### Token format

```
gwk_<base64url(32 random bytes)>
```

Prefix `gwk_` makes tokens grep-able in logs and distinguishable from session IDs. The random payload is 32 bytes (43 characters in base64url), giving 256 bits of entropy.

#### Storage

Tokens are stored in `data/meta/api_tokens.json`:

```json
[
  {
    "id": "tok_abc123",
    "user": "alice",
    "name": "Claude assistant",
    "token_hash": "$2a$10$...",
    "created_at": "2026-03-17T10:00:00Z",
    "last_used_at": "2026-03-17T14:30:00Z",
    "expires_at": null
  }
]
```

- `token_hash`: bcrypt hash of the full token. The plaintext is shown once at creation and never stored.
- `expires_at`: optional expiry. `null` = no expiry (revoke manually).
- `name`: user-chosen label to identify the token's purpose.

#### Request authentication

The preferred method is a Bearer header:

```
Authorization: Bearer gwk_<token>
```

As a fallback for AI platforms that cannot set custom HTTP headers (Claude App, ChatGPT, etc.), the token can be passed as a query parameter:

```
https://wiki.example.com/api/ai/v1/conventions?token=gwk_<token>
```

The query parameter method is less secure — the token may appear in server access logs and reverse proxy logs. It is acceptable for self-hosted instances where the admin controls the log pipeline. The Bearer header should be used whenever the client supports it.

The middleware checks both methods (header first, then query param):
1. Looks for `Authorization: Bearer gwk_*` header, or `?token=gwk_*` query parameter
2. Verifies token against stored hashes
3. If matched: sets the username in context (same as session auth), updates `last_used_at`
4. If the user is disabled: rejects with `403`
5. If the token is expired: rejects with `401`

Token auth and session auth are mutually exclusive per request. If both are present, the token takes precedence.

#### Rate limiting

Token-authenticated requests are rate-limited per token:
- **Read endpoints**: 120 requests/minute
- **Write endpoints**: 30 requests/minute
- Exceeded: `429 Too Many Requests` with `Retry-After` header

#### Token management endpoints

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/tokens` | session | List current user's tokens (no hashes) |
| `POST` | `/api/tokens` | session | Create token — returns plaintext once |
| `DELETE` | `/api/tokens/{id}` | session | Revoke a token |

Tokens can only be created and revoked via browser session (not via another token). This prevents a leaked token from generating more tokens.

Admin users can list and revoke any user's tokens:

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/admin/tokens` | admin | List all tokens across users |
| `DELETE` | `/api/admin/tokens/{id}` | admin | Revoke any token |

---

## 3. Content API Endpoints

All endpoints below require authentication (session or token). ACL is enforced per page — a `403` is returned for pages the user cannot access.

### 3.1 Read Operations

#### Get page

```
GET /api/pages/{path}
```

Already exists. Returns markdown, metadata, title. No changes needed — token auth grants access.

#### List namespace

```
GET /api/ai/v1/namespace/{path}
```

Lists pages and sub-namespaces under a given path. Essential for AI navigation.

**Response:**

```json
{
  "namespace": "/regulatory/qms",
  "pages": [
    { "path": "/regulatory/qms/kpi", "title": "KPI Dashboard", "version": 42 },
    { "path": "/regulatory/qms/cpm", "title": "CPM Overview", "version": 18 }
  ],
  "namespaces": [
    { "path": "/regulatory/qms/dir", "page_count": 12 },
    { "path": "/regulatory/qms/soft", "page_count": 45 }
  ]
}
```

Query parameters:
- `depth=1` (default) — immediate children only. `depth=0` — recursive.
- `include_meta=true` — include last modified date and author per page.

#### Batch read

```
POST /api/ai/v1/batch/read
```

Read multiple pages in a single request. Reduces round-trips for AI workflows that need context from several pages.

**Request:**

```json
{
  "paths": [
    "/regulatory/qms/dir/mq01",
    "/regulatory/qms/dir/sop01"
  ]
}
```

**Response:**

```json
{
  "pages": [
    {
      "path": "/regulatory/qms/dir/mq01",
      "title": "Quality Manual",
      "markdown": "# DIR/MQ01: Quality Manual\n...",
      "version": 70,
      "ok": true
    },
    {
      "path": "/regulatory/qms/dir/sop01",
      "title": "Document Control",
      "markdown": "# DIR/SOP01: ...\n...",
      "version": 15,
      "ok": true
    }
  ]
}
```

If a page doesn't exist or the user lacks permission, the entry has `"ok": false` and an `"error"` field. The batch request does not fail entirely — each page is independent.

Limit: 20 pages per batch.

#### Search

```
GET /api/search?q=...
```

Already exists. No changes needed.

### 3.2 Write Operations

#### Preview (dry-run)

```
POST /api/ai/v1/preview/{path}
```

Submit markdown and see what would change — without actually saving. Lets the AI (or the user reviewing the AI's work) validate before committing.

**Request:**

```json
{
  "markdown": "# Updated content\n...",
  "summary": "Fix typo in section 3"
}
```

**Response:**

```json
{
  "path": "/regulatory/qms/dir/mq01",
  "current_version": 70,
  "diff": {
    "added": 3,
    "removed": 1,
    "hunks": [
      {
        "old_start": 15,
        "old_count": 3,
        "new_start": 15,
        "new_count": 5,
        "lines": [
          { "op": " ", "text": "existing line" },
          { "op": "-", "text": "old text" },
          { "op": "+", "text": "new text" },
          { "op": "+", "text": "added line" }
        ]
      }
    ]
  },
  "warnings": []
}
```

Warnings may include: circular include detected, broken internal links, reviewflow constraint violations.

#### Write page

```
PUT /api/pages/{path}
```

Already exists. The only addition: when authenticated via token, the `summary` field is required (not optional as it is for browser edits). This ensures the audit trail is meaningful for AI-generated changes.

The endpoint enforces:
- ACL write permission
- Reviewflow constraints (cannot overwrite a page under active review by someone else)
- Circular include detection
- Optimistic concurrency: if `"expected_version"` is provided and doesn't match, the write is rejected with `409 Conflict`

#### Batch write

```
POST /api/ai/v1/batch/write
```

Write multiple pages atomically. If any page fails validation, none are written.

**Request:**

```json
{
  "pages": [
    {
      "path": "/regulatory/qms/dir/mq01",
      "markdown": "# ...",
      "summary": "Update quality manual",
      "expected_version": 70
    },
    {
      "path": "/regulatory/qms/dir/sop01",
      "markdown": "# ...",
      "summary": "Update document control procedure"
    }
  ]
}
```

**Response (success):**

```json
{
  "ok": true,
  "results": [
    { "path": "/regulatory/qms/dir/mq01", "new_version": 71 },
    { "path": "/regulatory/qms/dir/sop01", "new_version": 16 }
  ]
}
```

**Response (partial failure):**

```json
{
  "ok": false,
  "error": "validation failed for 1 page",
  "results": [
    { "path": "/regulatory/qms/dir/mq01", "ok": true },
    { "path": "/regulatory/qms/dir/sop01", "ok": false, "error": "version conflict: expected 14, current 15" }
  ]
}
```

Limit: 10 pages per batch.

### 3.3 Metadata Operations

#### Get page metadata

```
GET /api/ai/v1/meta/{path}
```

Returns structured metadata for a page without the full markdown content:

```json
{
  "path": "/regulatory/qms/dir/mq01",
  "title": "DIR/MQ01: Quality Manual",
  "version": 70,
  "last_modified": "2026-03-16T18:00:00Z",
  "author": "raynald.delahondes",
  "tags": ["sop"],
  "reviewflow": {
    "version_tag": "2.1",
    "roles": {
      "author": "raynald.delahondes",
      "reviewer": "michel.laborde",
      "validation": "etienne.formstecher"
    },
    "is_fully_validated": false,
    "validated_page_version": 67
  },
  "backlinks": ["/regulatory/qms/dir"],
  "includes": ["/wiki/sidebar", "/wiki/footer"]
}
```

#### Get reviewflow status

```
GET /api/plugin/reviewflow/v1/status/{path}
```

Already exists. No changes needed.

#### Get todos for page

```
GET /api/plugin/todo/v1/tasks/page/{path}
```

Already exists. No changes needed.

---

## 4. OpenAPI Schema

The API is described by an OpenAPI 3.1 schema, served at:

```
GET /api/openapi.json
```

This endpoint is public (no authentication required) and includes CORS headers (`Access-Control-Allow-Origin: *`) so that external tools can fetch it directly.

The schema covers:
- All AI Content API endpoints (`/api/ai/v1/*`)
- Core page endpoints (`/api/pages/*`, `/api/search`, `/api/history/*`)
- Token management endpoints (`/api/tokens`)

AI platforms that support OpenAPI auto-discovery (e.g. ChatGPT custom actions, OpenAI Assistants, LangChain tools) can ingest this schema to auto-generate client bindings.

The schema is embedded in the Go binary at compile time — it always matches the running server's API surface.

---

## 5. Conventions for AI Clients

### 5.1 User-Agent

AI clients should set a descriptive `User-Agent` header:

```
User-Agent: claude-code/1.0 (gowiki-ai-api; user=raynald.delahondes)
```

This aids debugging and audit. Not enforced, but recommended.

### 5.2 Optimistic Concurrency

For write operations, AI clients should:
1. Read the page to get the current `version`
2. Perform edits on the markdown
3. Write with `expected_version` set to the version they read

If another user edited the page in between, the write returns `409 Conflict` and the AI must re-read and rebase its changes.

### 5.3 Summary Convention

When an AI writes a page, the `summary` should indicate the AI tool and describe the change:

```
[AI: Claude] Translate section 3 to English
```

The `[AI: <tool>]` prefix is a convention, not enforced by the server. It helps users scanning page history distinguish AI-assisted edits from manual ones.

### 5.4 Rate Limit Handling

On `429`, wait for the `Retry-After` header value (in seconds) before retrying. Do not retry in a tight loop.

---

## 6. Admin UI

### Token management in user profile

The user profile page (or a dedicated "API Tokens" section) allows users to:
- View their active tokens (name, creation date, last used, truncated prefix)
- Create a new token (prompted to name it, plaintext shown once in a copy-able dialog)
- Revoke a token (with confirmation)

### Admin token overview

The admin panel "Locks" tab (or a new "Tokens" tab) shows:
- All active tokens across users
- Filter by user
- Revoke any token

---

## 7. Configuration

```yaml
# data/config.yaml
ai_api:
  enabled: true                   # Master switch — false disables token auth entirely
  rate_limit_read: 120            # Requests per minute per token
  rate_limit_write: 30            # Requests per minute per token
  max_tokens_per_user: 5          # Maximum tokens a user can create
  require_summary: true           # Require non-empty summary for token-authenticated writes
```

Default: disabled. Must be explicitly enabled by admin.

---

## 8. Security Considerations

- **Token storage**: Only bcrypt hashes are stored. Plaintext is shown once at creation.
- **No privilege escalation**: Token grants exactly the user's current permissions. If the user is removed from a group, the token immediately loses those permissions.
- **No token self-generation**: Tokens can only be created via browser session, not via another token.
- **Disabled users**: Token auth is rejected for disabled users.
- **Rate limiting**: Prevents runaway AI loops from overwhelming the server.
- **Audit trail**: All writes include author attribution. The changelog records the operation.
- **Revocation**: Tokens can be revoked instantly by the user or admin. No grace period.

---

## 9. Relationship to RAG

This API and Retrieval-Augmented Generation (RAG) are complementary but distinct:

| | AI Content API (this spec) | RAG |
|---|---|---|
| **Pattern** | Agent — AI acts on behalf of user | Retrieval — wiki feeds context to AI |
| **Direction** | AI **reads and writes** wiki content | AI **reads** wiki content to answer questions |
| **Use cases** | Translate pages, fix content, manage metadata, batch updates | "What does our QMS say about X?", summarize procedures, onboarding Q&A |
| **Infrastructure** | REST endpoints, token auth, ACL | Token auth (shared), search/embeddings, chunk retrieval |

### Why they're separate

The agent API is immediately useful and self-contained: a user gives their AI a token and says "translate these 20 pages to English." This requires no embedding store, no chunking strategy, no vector database.

RAG requires additional infrastructure (embedding generation, vector or full-text index tuned for chunk retrieval, context window management) that is orthogonal to content management.

### What RAG would add (future)

A RAG extension would reuse the token infrastructure from this spec and add:

- **`POST /api/ai/v1/context`** — given a natural-language query, return the most relevant page chunks with source attribution, pre-formatted for an LLM context window
- **Chunk-level retrieval** — return relevant sections of pages rather than full pages, respecting ACL per page
- **Embedding store** — optional; the existing full-text search with typo tolerance may be sufficient for many deployments

RAG is planned as a separate spec and implementation phase. It layers on top without breaking anything — purely additive read-only endpoints sharing the same auth model.

---

## 10. Implementation Plan

### Phase 1 — Token infrastructure
- Token model, storage, CRUD endpoints
- Bearer token middleware (integrate with existing auth middleware)
- Rate limiter (per-token, in-memory with sliding window)
- Admin UI: token tab
- User profile: token management

### Phase 2 — AI-specific endpoints
- `GET /api/ai/v1/namespace/{path}` — namespace listing
- `POST /api/ai/v1/batch/read` — multi-page read
- `POST /api/ai/v1/preview/{path}` — dry-run diff
- `GET /api/ai/v1/meta/{path}` — page metadata

### Phase 3 — Batch writes
- `POST /api/ai/v1/batch/write` — atomic multi-page write
- Optimistic concurrency enforcement
- Validation pipeline (ACL + reviewflow + includes)

### Phase 4 — Documentation & client examples
- API documentation page (served by the wiki itself)
- Example scripts: Claude Code hook, ChatGPT custom action schema, Python client

### Phase 5 — RAG (separate spec)
- Context retrieval endpoint
- Chunk-level search with ACL filtering
- Optional embedding store integration
