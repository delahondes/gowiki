# Namespaces

Namespaces are folders that organize pages hierarchically.

## 1. Structure

Pages live under `data/content/`. A namespace is simply a directory:

- `/regulatory/qms/dir/mq01` is a page at `content/regulatory/qms/dir/mq01/index.md`
- `/regulatory/qms/dir/sop01` could be a leaf page (`sop01.md`) or a namespace (`sop01/index.md`)

## 1. Namespace index

When a directory exists, the page is served from `index.md` inside it. This allows a page to have both content and sub-pages.

## 1. Constraint

A page and a namespace cannot share the same path. If `content/path/to/ns/` exists as a directory, `content/path/to/ns.md` must not exist.

## 1. Media files

Attachments (images, PDFs, etc.) live alongside pages in the same `content/` tree. They are distinguished by having a file extension.

## 1. Moving pages

Use the **Move** action in the action bar to rename or relocate a page. The move operation:
- Updates all internal links pointing to the old path
- Optionally moves associated media files
- Preserves page history
