# Canonical Page Paths

## The rule

Every page in Gowiki has exactly one canonical path. The canonical path is what appears in URLs, API responses, WebSocket messages, link targets, and all internal references.

| Storage file | Canonical path | Type |
|---|---|---|
| `content/page.md` | `/page` | Leaf page |
| `content/docs/guide.md` | `/docs/guide` | Leaf page |
| `content/index.md` | `/` | Root namespace index |
| `content/docs/index.md` | `/docs/` | Namespace index |
| `content/a/b/index.md` | `/a/b/` | Namespace index |

**Rules:**
1. All canonical paths start with `/`
2. Leaf pages have no trailing slash: `/page`, `/docs/guide`
3. Namespace index pages have a trailing slash: `/docs/`, `/a/b/`
4. The root page is `/` (not `/index`)
5. The word `index` never appears in a canonical path
6. `content/path/ns.md` must not exist if `content/path/ns/` exists (namespace constraint)

## Forbidden forms

These are never valid as canonical paths:

- `page` (no leading slash)
- `docs/guide` (no leading slash)
- `/index` (use `/`)
- `/docs/index` (use `/docs/`)
- `/docs/index/` (nonsensical)

## Conversion functions

### Go: `storage.CanonicalPath(storagePath string) string`

Converts an internal storage path (as used by the file store) to a canonical path.

```
"index"          → "/"
"docs/guide"     → "/docs/guide"
"docs/index"     → "/docs/"
"a/b/index"      → "/a/b/"
"a/b/page"       → "/a/b/page"
```

### JS: `canonicalPagePath(storagePath: string): string`

Same conversion on the frontend.

## Where to enforce

- **API responses**: all `path` fields must be canonical
- **WebSocket messages**: all `page` fields must be canonical
- **Link resolution**: internal links are canonical paths
- **ACL patterns**: match against canonical paths
- **Search results**: return canonical paths
- **Changelog entries**: use canonical paths

## Where storage paths are used internally

The file store uses storage paths (without leading slash, with `index` suffix) for filesystem operations only. These should never leak to the API, the frontend, or any user-facing output.
