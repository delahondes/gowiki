# Build instructions

This document describes how to build **optional frontend assets** used by gowiki.

⚠️ **Important**  
Node.js is **not required** to run gowiki.  
It is only needed if you want to **rebuild the WYSIWYG editor assets** (ProseMirror).

Prebuilt assets are committed to the repository.

## History

Here are the steps that were added to create the assets:

```sh
cd frontend
npm init -y
npm install prosemirror-state prosemirror-view prosemirror-model \
            prosemirror-schema-basic prosemirror-schema-list \
            prosemirror-history prosemirror-keymap prosemirror-commands \
            prosemirror-markdown prosemirror-menu
npm install --save-dev esbuild
```

## Frontend assets (ProseMirror)

The ProseMirror editor is bundled using Node.js.
Node is required **only** to rebuild frontend assets.

### Requirements
- Node.js ≥ 18

### Build
cd frontend
npm install
npm run build

The generated bundle is committed under web/static/.
---

## Backend (Go)

The Go backend can be built normally and does **not** require Node.js.

```sh
go build ./...
```