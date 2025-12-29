# Tools
GO      := go
NPM     := npm

# Paths
FRONTEND_DIR := frontend
EDITOR_BUNDLE := web/static/editor/prosemirror.bundle.js

# Default target
.PHONY: all
all: build

# Build everything
.PHONY: build
build: build-go build-frontend

# Go backend
.PHONY: build-go
build-go:
	$(GO) build ./...

# Frontend (ProseMirror)
.PHONY: build-frontend
build-frontend:
	cd $(FRONTEND_DIR) && $(NPM) install
	cd $(FRONTEND_DIR) && $(NPM) run build

# Clean (optional, conservative)
.PHONY: clean
clean:
	rm -rf $(FRONTEND_DIR)/node_modules