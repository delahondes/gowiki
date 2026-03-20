# Gowiki

Gowiki is a modern wiki engine inspired by [DokuWiki](https://www.dokuwiki.org/), one of the best wikis in the world.

## What it borrows from DokuWiki

DokuWiki got many things right, and Gowiki inherits these ideas:

- **Plain text storage** — content is stored as human-readable text files, not in a database
- **Version history** — every change is tracked, with diffs and full traceability
- **Full-text search** — incremental, immediate, typo-tolerant
- **Extensibility** — plugins for tables, structured data, workflows, and more (DokuWiki's Struct plugin was a particular inspiration)

## What it does differently

Gowiki makes several architectural changes that couldn't be done within DokuWiki:

- **Markdown dialect** — a bijective (one canonical syntax per element) Markdown dialect replaces DokuWiki's custom syntax. Round-trips between raw and visual editing are lossless by construction.
- **ProseMirror from the start** — the visual editor is deeply integrated, not bolted on. Everything you can do in raw mode, you can do in visual mode, and vice versa.
- **Go backend** — the server is written in Go instead of PHP, with a clean REST API boundary between frontend and backend.
- **Frontend authority for rendering** — ProseMirror handles all document rendering. The backend stores Markdown and never encodes editor semantics.
- **Fewer but more powerful plugins** — each plugin (tables, reviewflow, todo, database) consolidates functionality that required multiple DokuWiki plugins.
- **AI Content API** — a built-in REST API with personal token authentication lets AI assistants (Claude, ChatGPT, etc.) read and manage wiki content on behalf of users.

These changes — particularly the switch to Markdown and the ProseMirror-first architecture — are too fundamental to be contributed back to DokuWiki. Gowiki is a separate project that stands on DokuWiki's shoulders.

## License

Gowiki is licensed under the **GNU General Public License v3.0** (GPL-3.0). In tribute to DokuWiki's open-source tradition, we chose the GPL family to ensure this project remains open.

## Acknowledgments

This software was designed and built with the help of AI agents: ChatGPT (OpenAI) for the initial architecture, design discussions and audit of security and trust aspects, and Claude (Anthropic) for implementation and coding. The author directed all design decisions and takes full responsibility for the result.

Many thanks to the DokuWiki team for creating the wiki that made this one possible.

## Repository layout

- `frontend/`: Vite + ProseMirror document/editor engine (TypeScript/ESM)
- `backend/`: Go API server (chi), authoritative page storage
- `scripts/`: local orchestration helpers

## Development

Run both backend and frontend:

```sh
make dev
```

Or run separately:

```sh
make dev-backend
make dev-frontend
```

Frontend dev server proxies `/api/*` to `http://localhost:8080`.

## Production-style run

Build frontend assets:

```sh
make build-frontend
```

Serve API + built frontend from Go:

```sh
make run-prod
```

## Initial backend API (v0)

- `GET /api/health`
- `GET /api/pages/{path}` → returns raw Markdown + page metadata
- `PUT /api/pages/{path}` with JSON body `{ "markdown": "..." }` → persists Markdown atomically and returns page payload
