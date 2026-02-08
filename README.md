# Gowiki

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
