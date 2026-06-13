# MCP (Model Context Protocol) Server

Gowiki exposes its AI-oriented API as a full MCP server at `/api/mcp/v1`. An MCP client (Claude Desktop, Claude.ai, Cursor, any MCP-compatible agent) can connect to the wiki as a tool provider, giving the LLM structured access to your pages, todos, structured-data tables, and reviewflow state.

The MCP server is a thin wrapper around the existing AI Content API (`/api/ai/v1/…`). Everything it does is subject to the same ACL model: the authenticated user AND the `@ai` pseudo-subject must both have permission to view (or edit) a page for the operation to succeed.

## Connection

- **URL:** `https://{{SERVER}}/api/mcp/v1`
- **Transport:** Streamable HTTP (MCP 2025-03-26 spec)
- **Auth:** `Authorization: Bearer gwk_<your_token>` — the same API tokens you use with `/api/ai/v1/…`

Create a token under **Admin → Tokens** (per-user) and paste the URL + bearer header into your MCP client.

### Example — Claude Desktop

`~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "gowiki": {
      "url": "https://wiki.example.com/api/mcp/v1",
      "headers": {
        "Authorization": "Bearer gwk_your_token_here"
      }
    }
  }
}
```

### Example — mcp-inspector

```
npx @modelcontextprotocol/inspector \
  --transport streamable-http \
  --url https://wiki.example.com/api/mcp/v1 \
  --header "Authorization=Bearer gwk_..."
```

## Tools

| Tool | Purpose |
|---|---|
| `get_conventions` | Markdown dialect rules and content guidelines. **Call first.** |
| `list_namespace` | Enumerate pages and sub-namespaces under a path |
| `read_pages_batch` | Read up to 20 pages in one call |
| `get_page_meta` | Page title, version, tags, backlinks, reviewflow status |
| `search_pages` | Full-text, typo-tolerant search — pass `tag` to filter by tag instead (combine with `query` to narrow by substring) |
| `get_reviewflow_status` | Reviewflow roles, confirmations, validation state |
| `preview_page_diff` | Dry-run edit — returns diff without saving |
| `write_page` | Create/update a page — requires a summary |
| `list_todos` | Todo tasks, filterable by status/assignee/namespace/due |
| `complete_todo` | Mark a todo as done |
| `list_database_tables` | Structured-data tables with field definitions |
| `query_database_rows` | Query rows from a structured-data table |

## Searching by tag

`search_pages` accepts an optional `tag` parameter alongside `query`. At least one of the two must be set:

- `query` alone — full-text, typo-tolerant FTS over page bodies. Returns snippets.
- `tag` alone — list every page bearing that tag (no snippets, no ranking).
- `tag` + `query` — pages bearing the tag, narrowed to those whose path or title contains `query` (case-insensitive substring).

All results are filtered by the caller's ACL.

Examples:

```json
{ "tag": "sop" }
{ "tag": "sop", "query": "biomscope" }
```

This is the same syntax the wiki search bar exposes as `tag:NAME [substring]`. See [Tags](/wiki/manual/tags) and [Search](/wiki/manual/search) for the user-facing equivalent.

## Resources

Pages are exposed as `wiki:///path/to/page` URIs via a resource template. Clients can:

- List `wiki:///` as the starting entry point
- Fetch any page path directly through the template

`read_pages_batch` is generally faster for the LLM than issuing multiple `resources/read` calls, so resources are most useful when the client pins a URI as conversation context.

## Prompts

| Prompt | Arguments | Purpose |
|---|---|---|
| `summarize_page` | `path`, `max_bullets` | Bullet summary of a page |
| `draft_review_checklist` | `path` | Reviewer checklist — includes reviewflow state |
| `suggest_tags` | `path` | Propose tags, preferring existing ones |

## Conventions the client must follow

The server advertises these in its instructions and re-exposes them through `get_conventions`. In short:

1. Call `get_conventions` once at session start.
2. Always `preview_page_diff` before `write_page`, and show the diff to the user.
3. Every write requires a summary formatted `[AI: <tool>] <description>`.
4. Optimistic locking: read → preview → write with `expected_version` set to the read version.

## Relationship to the HTTP AI API

The MCP server reuses the same handlers conceptually but is a separate entry point. If you already integrate via `/api/ai/v1/…`, nothing changes — both paths stay available. New agent integrations should prefer MCP because:

- The tool contract is explicit (JSON Schema inputs, typed outputs).
- Resources give clients a native way to include pages as conversation context.
- Prompts centralize ready-made workflows instead of duplicating prompt templates across clients.
