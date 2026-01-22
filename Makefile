# Tools
GO      := go
NPM     := npm

# Paths
FRONTEND_DIR := frontend
EDITOR_BUNDLE := web/static/editor/prosemirror.bundle.js
NODE_SPEC_GEN := frontend/nodespec.gen.ts

# Default target
.PHONY: all
all: build

# Build everything
.PHONY: build
build: build-go build-frontend

# Go backend
.PHONY: build-go
build-go:
	$(GO) build -o gowiki ./cmd/gowiki

.PHONY: gen-nodespec
gen-nodespec:
	$(GO) run ./cmd/gen-nodespec > $(NODE_SPEC_GEN)

# Frontend (ProseMirror)
.PHONY: build-frontend
build-frontend: gen-nodespec
	cd $(FRONTEND_DIR) && $(NPM) install
	cd $(FRONTEND_DIR) && $(NPM) run build

# Clean (optional, conservative)
.PHONY: clean
clean:
	rm -rf $(FRONTEND_DIR)/node_modules