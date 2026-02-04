.PHONY: frontend-install dev dev-backend dev-frontend build-frontend run-prod

frontend-install:
	npm --prefix frontend install

dev:
	./scripts/dev.sh

dev-backend:
	go run ./backend/cmd/server -addr :8080 -data-dir ./backend/data/pages

dev-frontend:
	npm --prefix frontend run dev

build-frontend:
	npm --prefix frontend run build

run-prod:
	go run ./backend/cmd/server -addr :8080 -data-dir ./backend/data/pages -serve-web -web-dir ./frontend/dist
