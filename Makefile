.PHONY: frontend-install dev dev-backend dev-frontend build-frontend build-backend run-prod

BACKEND_BIN := backend/server

frontend-install:
	npm --prefix frontend install

dev:
	./scripts/dev.sh

dev-backend: build-backend
	$(BACKEND_BIN) -addr :8080 -data-dir ./backend/data

dev-frontend:
	npm --prefix frontend run dev

build-backend:
	cd backend && go build -o server ./cmd/server

build-frontend:
	npm --prefix frontend run build

run-prod: build-backend
	$(BACKEND_BIN) -addr :8080 -data-dir ./backend/data -serve-web -web-dir ./frontend/dist
