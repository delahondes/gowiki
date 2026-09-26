.PHONY: frontend-install dev dev-backend dev-frontend build-frontend build-backend build-backend-release test test-backend test-frontend run-prod

BACKEND_BIN := backend/server

# Release-mode build metadata for -ldflags injection. Reading git here means
# a `make build-backend-release` always stamps the current HEAD; CI passes
# explicit values so tag builds record the tag commit even if HEAD has moved.
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X gowiki/backend/internal/api.BuildCommit=$(GIT_COMMIT) -X gowiki/backend/internal/api.BuildDate=$(BUILD_DATE)

frontend-install:
	npm --prefix frontend install

dev:
	./scripts/dev.sh

dev-backend: build-backend
	$(BACKEND_BIN) -addr :8080 -data-dir ./backend/data

dev-frontend:
	npm --prefix frontend run dev

# Plain dev build — leaves BuildCommit / BuildDate as "unknown". Fine for
# local work; production must go through build-backend-release.
build-backend:
	cd backend && go build -o server ./cmd/server

# Release build — stamps commit + date via -ldflags so /api/version reports
# what is actually running. This is what the deploy procedure and CI use.
build-backend-release:
	cd backend && go build -ldflags "$(LDFLAGS)" -o server ./cmd/server

build-frontend:
	npm --prefix frontend run build

test: test-backend test-frontend

test-backend:
	cd backend && go test ./...

test-frontend:
	npm --prefix frontend test

run-prod: build-backend
	$(BACKEND_BIN) -addr :8080 -data-dir ./backend/data -serve-web -web-dir ./frontend/dist
