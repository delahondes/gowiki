.PHONY: frontend-install dev dev-backend dev-frontend build-frontend run-prod

frontend-install:
	npm --prefix frontend install

dev:
	./scripts/dev.sh

dev-backend:
	cd backend && go run ./cmd/server -addr :8080 -data-dir ./data/pages

dev-frontend:
	npm --prefix frontend run dev

build-frontend:
	npm --prefix frontend run build

run-prod:
	cd backend && go run ./cmd/server -addr :8080 -data-dir ./data/pages -serve-web -web-dir ../frontend/dist
