# Current TODO

Issues raised 2026-03-20, from the broken page rendering discussion.

## Done

- [x] **Resilient directive rendering** — Mismatched directives show inline error blocks instead of crashing the page
- [x] **Resilient link rendering** — Bare relative links (from DokuWiki import) auto-fixed with `./` prefix instead of throwing
- [x] **Global error recovery** — If `markdownToPM` throws for any reason, view mode shows error + raw markdown; edit mode falls back to raw
- [x] **Force raw edit** — Shift+click Edit or Cmd+Shift+E forces raw mode (recovery shortcut)
- [x] **Admin draft management** — View, Reclaim, Discard any user's draft (including orphaned drafts without locks)
- [x] **Render endpoint** — `GET /api/render/{path}` for systematic quality checks

## TODO

- [ ] **Persistent error messages** — Save/publish errors are transient toasts that disappear after 3s. Errors should be persistent (manually dismissable). "Draft saved" can remain transient; "publish failed" or "save failed" must stay visible.

- [ ] **Erroneous links audit** — Many imported links may lack the `./` prefix. The auto-fix (prepend `./`) handles rendering, but the markdown source should be corrected. Needs: scan all pages for bare relative links, generate a batch fix report. Treat with caution — some may be intentional.

- [ ] **Internal link creation helper** — Search-based link picker triggered from the link button. Type a query, see matching pages, click to insert link. Similar to DokuWiki's link wizard. Scope: visual and raw mode, namespace browsing, search integration.
