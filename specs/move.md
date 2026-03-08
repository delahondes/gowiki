# Move Page

## Summary

Move (rename/relocate) a wiki page, automatically rewriting all inbound and outbound links. Supports namespace index conversion and optional co-moving of exclusively-referenced media files.

## API

```
POST /api/move/{path}   (requireAuth + "edit" permission)
```

### Request body

```json
{
  "to": "/new/path",
  "move_media": false,
  "to_namespace_index": false,
  "to_regular_page": false
}
```

Exactly one of `to`, `to_namespace_index`, or `to_regular_page` must be set.

### Response (move)

```json
{
  "page": { "path": "/new/path", "markdown": "...", "meta": { ... } },
  "old_path": "/old/path",
  "new_path": "/new/path",
  "updated_pages": ["/other", "/another"],
  "moved_media": ["/ns/img.png"]
}
```

### Response (namespace conversion)

```json
{
  "page": { "path": "/same/path", ... },
  "old_path": "/same/path",
  "new_path": "/same/path",
  "updated_pages": []
}
```

### Error codes

| Code | Condition |
|------|-----------|
| 400  | Missing path, invalid body, conflicting flags |
| 404  | Source page not found |
| 409  | Destination exists, namespace conflict, draft lock, namespace not empty |
| 500  | Internal error |

## Operations

### Move page

1. Validate: normalize paths, source exists, destination doesn't, namespace constraints OK, no draft lock.
2. Read source page content.
3. Gather backlinkers: `LinkIndex.GetBacklinks(oldPath)` + `IncludeIndex.GetIncluders(oldPath)`, deduplicate, exclude self.
4. If `move_media`: identify exclusively-referenced media via `RefIndex`. Physically move files, rename `MediaVersionStore` keys. Build old→new media path mapping.
5. Rebase moved page's own relative refs: `RebaseRelativeRefs()`. If media moved, also `RewriteMediaRef()`.
6. Rewrite each backlinker/includer: `Get()` → `RewritePageRef()` (+ `RewriteMediaRef()`) → `Put()`.
7. Write moved page at new path via `Put()`.
8. Delete old page via `Delete()`.
9. Log to changelog with type "move".

### Convert to namespace index

Move `content/{path}.md` → `content/{path}/index.md` (and meta). Page path unchanged → no link rewriting needed.

### Convert to regular page

Reverse of above. Only allowed if namespace dir contains only `index.md`.

## Link rewriting

Three pure functions in `markdown/rewrite.go`:

- `RewritePageRef(content, oldPagePath, newPagePath, contextPagePath)` — rewrites links/includes targeting oldPagePath.
- `RewriteMediaRef(content, oldMediaPath, newMediaPath, contextPagePath)` — rewrites media refs targeting oldMediaPath.
- `RebaseRelativeRefs(content, oldResolvePath, newResolvePath)` — converts relative refs in the moved page to absolute so they still resolve correctly.

## Frontend

- "Move" button in view mode actions (before Delete).
- `prompt()` for new path, `confirm()` for media co-move option.
- On success, navigate to new path.
- Conditional "Convert to namespace" / "Convert to regular page" buttons based on `is_namespace_index`.
