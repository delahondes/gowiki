# Development notes

This document describes how the project is developed and run locally, and how this differs from production deployment.

## Development mode

In development mode, the frontend and backend are run separately to maximize iteration speed and debuggability.

- The frontend is served by Vite using a Node.js development server.
- ES modules are loaded directly without bundling.
- Hot module reload is enabled.
- Plugins are loaded dynamically as separate modules.
- The frontend communicates with the backend over HTTP (REST).

In this mode, the Go backend:
- exposes only API endpoints (documents, media, search, export, etc.),
- does not serve frontend assets.

This mode is intended for:
- frontend and plugin development,
- rapid iteration,
- debugging document semantics and editor behavior.

## Production mode

In production mode, the frontend is built ahead of time and served as static assets.

- The frontend is bundled into standalone JavaScript and CSS files.
- The core frontend and each plugin are built as separate bundles.
- No Node.js runtime or build tools are required at runtime.

In this mode, the Go backend:
- serves the bundled frontend assets (either from disk or embedded resources),
- exposes the same REST API as in development mode.

This mode is intended for:
- deployment,
- stability,
- minimal runtime dependencies.

## Bundling strategy

The frontend build produces:
- a core bundle containing the editor, registry, and core document semantics,
- one bundle per plugin.

Plugins are loaded dynamically by the frontend at runtime. Enabling or disabling a plugin does not require rebuilding the core bundle.

## Invariants

The following invariants apply in all modes:

- The Go backend never depends on Node.js, Vite, or the TypeScript toolchain.
- The backend is authoritative for storage, identity, access control, and export.
- The frontend is responsible for document semantics, editing, and rendering.
- The communication boundary between frontend and backend is HTTP (REST).
