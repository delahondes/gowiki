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

- [x] **Persistent error messages** — Error toasts (red) are now persistent with a dismiss button. Info toasts remain transient (3s).

- [x] **Erroneous links audit** — Only 5 bare links on 1 page (`dataset/biomscope-artefacts.md`). Fixed in source. Auto-fix in compiler remains as safety net.

- [x] **Internal link creation helper** — Search-based link picker integrated into the link modal. Typing in the target field searches wiki pages live (debounced 200ms). Results show title + path, click to fill target + text. Arrow keys navigate results, Enter selects, Escape dismisses. Works in both visual and raw mode.
