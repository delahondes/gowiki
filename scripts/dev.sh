#!/usr/bin/env bash
set -euo pipefail

cleanup() {
  if [[ -n "${BACKEND_PID:-}" ]]; then
    kill "${BACKEND_PID}" >/dev/null 2>&1 || true
  fi
}

trap cleanup EXIT INT TERM

(
  cd backend
  go run ./cmd/server -addr :8080 -data-dir ./data/content
) &
BACKEND_PID=$!

npm --prefix frontend run dev
