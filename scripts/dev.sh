#!/usr/bin/env bash
set -euo pipefail

cleanup() {
  if [[ -n "${BACKEND_PID:-}" ]]; then
    kill "${BACKEND_PID}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT INT TERM

go run ./backend/cmd/server -addr :8080 -data-dir ./backend/data/pages &
BACKEND_PID=$!

npm --prefix frontend run dev
