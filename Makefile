.PHONY: frontend-install dev dev-backend dev-frontend build-frontend build-backend build-backend-release test test-backend test-frontend test-integration lint lint-backend lint-frontend run-prod

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

# Integration tests need a live Postgres. Point DATABASE_URL at your
# local instance (or use the default gowiki:gowiki@localhost:5432/gowiki_test)
# and run `make test-integration`. CI does this automatically via a
# postgres service container.
test-integration:
	cd backend && go test -tags=integration ./...

test-frontend:
	npm --prefix frontend test

lint: lint-backend lint-frontend

# Requires golangci-lint (`brew install golangci-lint`). CI installs it
# automatically via the golangci-lint-action.
lint-backend:
	cd backend && golangci-lint run

lint-frontend:
	npm --prefix frontend run lint
	npm --prefix frontend run format:check

run-prod: build-backend
	$(BACKEND_BIN) -addr :8080 -data-dir ./backend/data -serve-web -web-dir ./frontend/dist
