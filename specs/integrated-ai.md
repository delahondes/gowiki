# Integrated AI Assistant — Specification

## 1. Overview

The Integrated AI Assistant embeds an AI agent directly into the wiki UI. Users interact with it from a chat panel in the editor — no external tool, no API token, no setup. The admin configures a provider API key once; every authenticated user gets AI assistance immediately.

This spec builds on top of the existing AI Content API (`ai_api.md`). The external token-based API remains unchanged. The integrated assistant is a new, parallel access path where the **server is the AI client**, not the user.

### Motivation

The external AI API requires users to:
1. Generate a personal token
2. Set up an external AI client (Claude Code, ChatGPT custom action, Python script)
3. Understand the API conventions
4. Copy-paste results back into the wiki

This works for power users but creates an adoption barrier. Most wiki users will never set up an external AI workflow. An integrated assistant removes all friction: the user clicks a button, types a request, and the AI operates directly on the page they're editing.

### Design Goals

- **Zero setup for users** — if the admin has configured an AI provider, every user sees the assistant
- **Same rules** — the assistant follows the same conventions, ACL, and reviewflow constraints as an external AI agent
- **Transparent** — the user sees what the AI proposes before it's applied; no silent rewrites
- **Provider-agnostic backend** — the server-side proxy abstracts the LLM provider; switching providers doesn't change the frontend
- **Cost-controlled** — per-user rate limits and usage tracking prevent runaway costs

### Non-Goals

- The assistant does not replace the external AI API — both coexist
- No autonomous background agents (the assistant acts only in response to user requests)
- No training or fine-tuning — the assistant uses a general-purpose LLM with wiki conventions as system prompt
- No voice input, no image generation
- No cross-page batch operations from the chat panel (use the external API for that)

### Relationship to `ai_api.md`

| | External AI API (`ai_api.md`) | Integrated Assistant (this spec) |
|---|---|---|
| **Who is the AI client?** | User's external tool | Gowiki server |
| **Authentication** | Personal API token | User's browser session |
| **Setup** | User generates token, configures tool | None (admin configures provider key) |
| **Scope** | Multi-page, batch, automation | Single-page, conversational |
| **Content access** | Via REST endpoints | Server reads/writes directly (no HTTP round-trip) |
| **Provider** | User's choice | Admin's choice (configured server-side) |

---

## 2. Architecture

```
┌─────────────┐     chat messages      ┌─────────────────┐     LLM API calls     ┌──────────┐
│  Frontend    │ ◄──────────────────► │  Go backend       │ ◄──────────────────► │ Claude   │
│  Chat panel  │     (WebSocket)       │  /api/ai/chat     │     (HTTPS)           │ API      │
└─────────────┘                        └─────────────────┘                        └──────────┘
                                              │
                                              │ direct function calls
                                              ▼
                                       ┌─────────────┐
                                       │ Page store   │
                                       │ ACL store    │
                                       │ Conventions  │
                                       └─────────────┘
```

### Draft context

The AI always operates on the **current draft**, not the published version. Since the AI panel is only available in edit mode, a draft always exists. The backend reads the draft content (via the existing draft system) when assembling the LLM context. After the AI applies changes, the modified content is saved back as a draft — this allows the AI to read back its own changes in follow-up requests without the user having to publish first.

### Request flow (action mode)

1. User opens the AI panel and types: "Add a mermaid diagram showing the approval workflow"
2. Frontend sends the message via POST to `/api/ai/chat` with `mode: "action"`
3. Backend builds the LLM request:
   - System prompt: wiki conventions (same content as `GET /api/ai/v1/conventions`)
   - Context: current draft markdown (including any previous AI modifications), page metadata, user identity
   - User instruction
4. Backend streams the LLM response back to the frontend
5. The AI edits are applied directly to the editor buffer and marked with inline comments explaining each change
6. The modified content is saved as a draft (enabling the AI to read it back on follow-up)
7. The user reviews the changes naturally in the editor — comments pinpoint what was modified and why
8. The user may ask the AI for further adjustments (the AI reads the current draft, including its previous changes and comments)
9. The user publishes when satisfied (normal save flow)

### Request flow (review mode)

1. User triggers "AI Review" or types "Review English quality"
2. The AI analyzes the current draft and returns a numbered list of proposals
3. Proposals appear in a dedicated review panel (not in the editor)
4. The user marks each proposal Accept/Reject/Clarify
5. Accepted proposals are applied as a batch edit to the editor buffer and saved as draft

### Why server-side proxy (not client-side)

- **API key security**: the provider API key never reaches the browser
- **Convention injection**: the server controls the system prompt — users can't skip or alter the conventions
- **ACL enforcement**: the server reads only pages the user has access to
- **Usage tracking**: the server meters all LLM calls per user
- **Provider flexibility**: switching from Claude to another provider is a config change, not a frontend deployment

---

## 3. Backend

### 3.1 Configuration

```yaml
# config.yaml
ai_assistant:
  enabled: false                    # Master switch
  provider: "anthropic"             # "anthropic" | "openai" | "ollama" (future)
  api_key: ""                       # Provider API key (or use AI_ASSISTANT_API_KEY env var)
  model: "claude-sonnet-4-20250514"   # Model identifier
  max_tokens: 4096                  # Max response tokens per request
  allowed_groups: []                # Groups that can use the AI panel (empty = all authenticated users)

  # Cost control
  costs:
    rate_limit_per_user: 30         # Max requests per hour per user (0 = unlimited)
    max_tokens_per_request: 4096    # Max output tokens the model may generate per request
    max_context_tokens: 16000       # Max input tokens sent to the model (page + system prompt)
    daily_limit_per_user: 100       # Max requests per day per user (0 = unlimited)
    monthly_budget: 50.00           # Monthly spending cap in USD across all users (0 = unlimited)
    warn_at_percentage: 80          # Show admin warning when budget reaches this % (0 = no warning)
```

The API key can also be provided via environment variable `AI_ASSISTANT_API_KEY` (takes precedence over config file) to avoid storing secrets in `config.yaml`.

**`allowed_groups`**: controls which users see the AI panel and can call the AI endpoints. When empty, all authenticated users have access. When set (e.g. `["admin"]`), only members of those groups can use the assistant. The frontend fetches this flag at page load (via the existing bootstrap/config endpoint) to show or hide the AI button. The backend enforces it on `/api/ai/chat` regardless — a user outside the allowed groups gets `403`.

### Cost control behavior

- **Per-user rate limiting**: enforced at both hourly and daily granularity. When a user hits a limit, the AI panel shows a clear message with the time until reset.
- **Monthly budget**: the backend tracks cumulative token usage and estimated cost (based on provider pricing). When the budget is reached, the assistant is disabled for all users until the next month. The admin is warned when `warn_at_percentage` is reached.
- **Context truncation**: if a page exceeds `max_context_tokens`, the backend truncates the page content (keeping the beginning and the section around the user's cursor if available) rather than rejecting the request.
- **Usage tracking**: stored in `data/meta/ai_usage.json` with daily aggregates per user (request count, input tokens, output tokens, estimated cost). Visible in the admin dashboard.
- **Admin override**: admins are exempt from per-user rate limits but still count toward the monthly budget.

### 3.2 Endpoint

```
POST /api/ai/chat
```

**Authentication**: session cookie (same as other browser endpoints). Token auth is not supported — this endpoint is for interactive browser use only.

**Request:**

```json
{
  "page_path": "/regulatory/qms/dir/sop01",
  "message": "Translate section 3 to English",
  "mode": "action"
}
```

- `page_path`: the page the user is currently editing (server fetches its content as context)
- `message`: the user's natural-language instruction
- `mode`: `"action"` (single instruction → diff) or `"review"` (analysis → numbered proposals)

**Response**: Server-Sent Events (SSE) stream.

```
event: token
data: {"text": "Here's the translation"}

event: token
data: {"text": " of section 3:\n\n"}

event: diff
data: {"old_start": 45, "old_end": 52, "new_text": "## 3. Document Control\n\nThis procedure..."}

event: done
data: {"usage": {"input_tokens": 2341, "output_tokens": 856}}
```

Event types:
- `token`: streaming text response (for conversational replies)
- `diff`: a proposed edit to the page (frontend renders as an inline diff)
- `error`: an error occurred (rate limit, provider error, ACL violation)
- `done`: stream complete, includes token usage

### 3.3 Context assembly

For each request, the backend assembles the LLM context:

1. **System prompt**: conventions document + mode-specific instructions
2. **Page content**: current markdown of the page (fetched server-side, ACL-checked against both user and `@ai` permissions)
3. **Page metadata**: path, version, reviewflow status, last author
4. **User instruction**: the single action or review request

The system prompt includes:
- All rules from `GET /api/ai/v1/conventions`
- Mode-specific instructions: action mode is told to produce a diff; review mode is told to produce numbered proposals with rationale
- The user's display name

### 3.4 Diff format

When the AI proposes an edit, the backend parses the AI's output to extract the proposed change and formats it as a structured diff. The frontend does not receive raw "here's the new markdown" — it receives a precise region replacement:

```json
{
  "old_start": 45,
  "old_end": 52,
  "old_text": "## 3. Contrôle des documents\n\nCette procédure...",
  "new_text": "## 3. Document Control\n\nThis procedure..."
}
```

This allows the frontend to show an inline diff without parsing the AI's prose.

### 3.5 Provider abstraction

The backend defines a provider interface:

```go
type AIProvider interface {
    Chat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
}
```

Initial implementation: Anthropic (Claude API). The interface allows adding OpenAI or local models (Ollama) later without changing the chat endpoint or frontend.

---

## 4. Frontend

### 4.1 AI panel

A collapsible side panel (right side of the editor), toggled by a toolbar button. The panel has two modes matching the interaction model (§5):

**Action mode** (default):
- A text input field with a "Run" button
- While the AI is working: a progress indicator
- When done: changes are applied directly in the editor, with inline comments marking modified regions
- Lightweight conversation history for follow-ups ("now do the same for section 4")

**Review mode**:
- Triggered by a toolbar button ("AI Review") or an action like "Review English quality"
- A structured list of numbered proposals, each showing: original text, proposed text, rationale
- Per-proposal buttons: Accept (green), Reject (red), Clarify (with text input)
- A "Apply accepted" button at the bottom that applies all accepted changes at once
- If any proposals are marked Clarify, a "Refine" button sends them back for one revision round

The panel is only visible in edit mode (visual or raw). In view mode, the toolbar button is hidden.

### 4.2 Applying changes

**Action mode**: the AI applies changes directly to the editor buffer and creates inline comments (using the comment plugin) to mark each modified region. The comments include a short explanation of what was changed. The modified content is saved as a draft — this lets the AI read back its changes on follow-up requests. The user can:
- Undo with `Ctrl+Z`
- Manually adjust the AI's changes
- Dismiss comments once reviewed
- Ask the AI for further changes (follow-up in the panel — the AI reads the current draft)
- Publish when satisfied (normal save flow)

**Review mode**: accepted proposals are applied as a single batch edit to the editor buffer, also with comment markers, and saved as draft.

Changes remain in draft state until the user explicitly publishes. Comments provide traceability — the user always knows what the AI touched.

### 4.3 Keyboard shortcut

`Ctrl+L` (or `Cmd+L` on macOS) focuses the AI panel input field.

### 4.4 Context indicators

The AI panel shows:
- Current page path
- Whether `@ai` has read access to this page (if not, the panel shows a notice and disables input)

---

## 5. Interaction Model

The assistant favors direct actions and structured UI over open-ended conversation. Conversational follow-ups are supported but the design should steer users toward giving clear instructions or using the review interface.

### 5.1 Actions (direct edit)

The primary interaction: the user types an instruction, the AI directly modifies the editor content and adds inline comments to mark what it changed. The page is not saved — the user is still in their editing session with a modified draft.

Examples:
- "Add a mermaid diagram showing the approval workflow"
- "Translate section 3 to English"
- "Generate a template for an equipment calibration SOP"

The AI applies changes directly and attaches comments to the modified regions. The user reviews the changes in the editor like any other edit — they can undo (`Ctrl+Z`), manually adjust, or ask the AI for further modifications. The AI can recover its own comment markers to understand what it previously changed, enabling follow-up instructions like "actually keep the original heading" or "make the diagram bigger".

The AI agent chooses between action and review mode based on the scope of the request. Narrow-scope changes (add a diagram, translate a section, insert a table) are best handled as direct edits. Wide-scope changes (grammar review across the whole page, style normalization, terminology consistency) are better handled through the review interface.

### 5.2 Reviews (structured batch)

For editorial work involving many localized changes (translation corrections, style fixes, terminology normalization), the action model is too coarse — a single diff replacing the whole document is hard to review, and individual actions per sentence are too slow.

The review mode follows the workflow defined in `ai-conventions.md` §6:

1. The user triggers a review (toolbar button or command like "Review English quality")
2. The AI analyzes the page and produces a **numbered list of proposals**, each with: location (line or section), original text, proposed text, and rationale
3. The proposals are presented in a **dedicated review panel** (not free-form chat)
4. The user marks each proposal: **Accept**, **Reject**, or **Clarify** (with a comment)
5. If any proposals are marked Clarify, a refinement round produces revised proposals for those items only
6. The user clicks "Apply accepted" — all accepted proposals are applied as a single edit to the editor buffer

The review panel is a structured interface, not a conversation. It is the integrated equivalent of the external batch review workflow, replacing the need for VS Code + Claude Code.

### 5.3 Comments as AI markers

When the AI modifies the editor content (action or review mode), it attaches inline comments to each modified region. These comments:
- Use the standard `comment` node with an `ai: true` attribute — no separate node type
- Are visually distinct (different color or icon) based on the `ai` attribute
- Contain a short explanation of what was changed and why
- Are recoverable by the AI in follow-up requests — the backend includes existing AI comments in the LLM context so the AI understands what it already modified
- Can be dismissed individually by the user, or all at once ("Clear AI comments")
- Have a toggle in the comment UI to flip the `ai` attribute — if the user wants to keep an AI comment after publish, they toggle it to a regular comment
- Are persisted in the draft (so the AI can read them back on follow-up) but **comments with `ai: true` are stripped on publish** — regular comments (including former AI comments the user has toggled) are preserved

This requires a dedicated endpoint to submit AI edits + comments back to the frontend:

```
POST /api/ai/apply
```

**Request:** the frontend calls this after receiving the AI's response. The backend returns structured edit operations:

```json
{
  "edits": [
    {
      "old_start": 45,
      "old_end": 52,
      "new_text": "## 3. Document Control\n\nThis procedure...",
      "comment": "Translated section heading and first paragraph from French to English"
    }
  ]
}
```

The frontend applies each edit to the editor buffer and creates a comment at the corresponding location.

### 5.4 What the AI can do

The assistant operates on the current page only:

- **Direct edit**: modify the editor content and mark changes with comments (action mode)
- **Propose localized edits**: suggest targeted changes with rationale (review mode)
- **Answer questions**: about page content, formatting rules, wiki conventions

The assistant cannot:
- Read other pages (unless a future enhancement adds explicit page references)
- Save or publish pages (the user does this)
- Modify ACL, users, or configuration
- Access external URLs

---

## 6. Security Considerations

- **API key isolation**: the provider API key is server-side only, never sent to the browser
- **ACL enforcement**: the server only includes page content the user has permission to read
- **`@ai` ACL subject**: a special `@ai` subject recognized by the ACL engine, alongside `@all` and group names. The integrated assistant must have `@ai` read permission on a page for it to be included in LLM context. Since ACL defaults to deny, namespaces without an explicit `@ai` rule are automatically excluded. Admins grant AI access where appropriate (e.g. `/regulatory/**  @ai  read`) and leave sensitive namespaces unmentioned. The `@ai` check is evaluated server-side before context assembly — content without `@ai` access never reaches the LLM provider.
- **No privilege escalation**: the AI operates under the intersection of the user's permissions and `@ai` permissions — it can only access content that both the user and `@ai` are allowed to read
- **Rate limiting**: prevents individual users from generating excessive API costs
- **No data exfiltration**: the AI's output is streamed to the requesting user only; page content is sent to the configured LLM provider (admin should be aware of this)
- **Prompt injection**: the backend should sanitize page content included in the LLM context to reduce prompt injection risk (e.g., strip any text that looks like system prompt overrides)

### Data privacy notice

When the integrated assistant is enabled, page content is sent to the configured LLM provider's API. The admin should:
- Inform users that AI features transmit page content to a third-party API
- Evaluate the provider's data retention policy
- Only grant `@ai` read access to namespaces that should be available to the assistant — everything else is excluded by default

---

## 7. Open Questions

### Review panel scope

The review mode (§5.2) is designed for single-page editorial work. Should it also support cross-page reviews (e.g. "review terminology consistency across all SOPs in /regulatory/")? This would require the assistant to read multiple pages and produce a multi-page proposal set — significantly more complex, and potentially expensive. The external AI API may remain the better tool for cross-page batch work.

### Clarification round limits

The review workflow allows one clarification round (§5.2 step 5). Is one round sufficient, or should users be able to iterate multiple times on individual proposals before applying? More rounds improve precision but increase cost and complexity.

---

## 8. Implementation Plan

Deployed incrementally on production, gated behind `allowed_groups: ["admin"]`. Regular users see no change until the feature is opened up.

### Phase 1 — Backend: config + provider proxy
- `ai_assistant` config section in `config.yaml` (parsed, validated, defaults)
- `allowed_groups` enforcement (middleware: check group membership, return 403)
- Provider interface (`AIProvider`) and Anthropic implementation (HTTP client, streaming)
- Environment variable support for API key (`AI_ASSISTANT_API_KEY`)
- **Test**: curl with admin session cookie → streamed response from Claude API

### Phase 2 — Backend: chat endpoint + context assembly
- `POST /api/ai/chat` endpoint with SSE streaming
- System prompt assembly: conventions + draft markdown + page metadata + mode instructions
- Draft reading: fetch current draft content as LLM context
- `@ai` ACL subject: check `@ai` permission before including page content in context
- Per-user rate limiting (hourly + daily)
- **Test**: curl action request → structured edits returned, rate limit enforced

### Phase 3 — Frontend: action mode
- AI button in editor toolbar (visible only if `ai_assistant.enabled` and user in `allowed_groups`)
- AI panel: text input, progress indicator, streaming display
- Direct edit application: apply structured edits to ProseMirror buffer
- AI comments: `ai: true` attribute on comment node, distinct visual style
- Draft save after AI edits
- Follow-up support: lightweight conversation history in panel
- **Test**: admin edits a page, asks AI to add a diagram, reviews AI comments, publishes

### Phase 4 — Frontend: AI comment lifecycle
- Toggle in comment UI: flip `ai` attribute (keep as regular comment / mark as AI)
- Strip `ai: true` comments on publish
- "Clear AI comments" bulk action
- **Test**: toggle an AI comment to regular, publish, verify it survives

### Phase 5 — Review mode
- Review endpoint behavior: LLM returns numbered proposals with rationale
- `POST /api/ai/apply` endpoint for structured batch edits
- Frontend: review panel with proposal list, Accept/Reject/Clarify per item
- Clarification round (Refine button → re-submit clarified items to LLM)
- Batch apply of accepted proposals + AI comments
- **Test**: admin triggers review on a French page, accepts some, clarifies one, applies

### Phase 6 — Cost control + admin UI
- Usage tracking: `data/meta/ai_usage.json` (daily per-user aggregates)
- Monthly budget cap with `warn_at_percentage` alert
- Context truncation for large pages
- Admin UI: AI settings panel (enable/disable, allowed groups, cost limits, usage dashboard)
- **Test**: verify budget cap disables assistant, verify usage dashboard shows correct data

### Phase 7 — Enhancements (future)
- Cross-page context: "also read /path/to/other/page"
- Suggested quick actions in the AI panel ("Translate to English", "Fix grammar")
- Ollama / local model support
