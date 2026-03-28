# Namespaces

Namespaces are folders that organize pages hierarchically.

## 1. Structure

Pages live under `data/content/`. A namespace is simply a directory:

- `/regulatory/qms/dir/mq01` is a page at `content/regulatory/qms/dir/mq01/index.md`
- `/regulatory/qms/dir/sop01` could be a leaf page (`sop01.md`) or a namespace (`sop01/index.md`)

## 1. Namespace index

When a directory exists, the page is served from `index.md` inside it. This allows a page to have both content and sub-pages.

## 1. Page and namespace constraint

A leaf page and a namespace cannot share the same name. For example, `/docs/guide` can be either:
- A leaf page (`content/docs/guide.md`)
- A namespace index (`content/docs/guide/index.md`)

But not both at the same time.

### Converting between page types

If you need to add sub-pages under an existing leaf page, you must first convert it to a namespace index. Use the **Convert to namespace** button in the action bar.

![Convert to namespace button](./screenshots/41.png)

This:
- Moves `guide.md` to `guide/index.md`
- Moves associated media files into the new directory
- Preserves all content, history, and reviewflow validation

The reverse is also possible: **Convert to regular page** moves a namespace index back to a leaf page. This only works if the namespace contains no sub-pages.

### Creating a namespace index directly

When creating a new page, add a trailing slash to the path (e.g. `/docs/guide/`). This creates the page as a namespace index from the start, allowing sub-pages to be added immediately.

### Automatic conversion on conflict

If you try to create a sub-page and a leaf page blocks the namespace, the wiki will offer to convert the blocking page to a namespace index automatically.

## 1. Media files

Attachments (images, PDFs, etc.) live alongside pages in the same `content/` tree. They are distinguished by having a file extension.

## 1. Moving pages

Use the **Move** action in the action bar to rename or relocate a page. The move operation:
- Updates all internal links pointing to the old path
- Optionally moves associated media files
- Preserves page history
