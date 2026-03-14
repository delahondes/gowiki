#!/usr/bin/env bash
set -euo pipefail

BINARY=$(mktemp -t gowiki-server.XXXXXX)

cleanup() {
  if [[ -n "${BACKEND_PID:-}" ]]; then
    kill "${BACKEND_PID}" >/dev/null 2>&1 || true
    wait "${BACKEND_PID}" 2>/dev/null || true
  fi
  rm -f "${BINARY}"
}

trap cleanup EXIT INT TERM

# Build first, then run the binary directly (no go run wrapper).
# go run spawns a child process that survives Ctrl-C, leaving orphan
# servers that hold the bolt database lock.
(cd backend && go build -o "${BINARY}" ./cmd/server)

"${BINARY}" -addr :8080 -data-dir ./backend/data &
BACKEND_PID=$!

npm --prefix frontend run dev
