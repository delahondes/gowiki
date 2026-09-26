package mcpserver

import (
	"context"
	"errors"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpsrv "github.com/mark3labs/mcp-go/server"

	"gowiki/backend/internal/storage"
)

// presencePageKey returns the canonical page path used by the presence hub
// (leading slash; trailing slash preserved for namespace indexes). The
// tools receive `pagePath` with the leading slash already stripped, so we
// reintroduce it here for the presence lookup.
func presencePageKey(pagePath string) string {
	return "/" + pagePath
}

// The draft-session tools let an agent act as a member of Gowiki's
// collaborative edit system. The flow mirrors what a human editor does in
// the browser:
//
//   1. enter_edit_session → acquire or resume a lock, get the current draft
//      markdown and an edit_token. force=true supersedes the caller's OWN
//      earlier session (a second tab); it never steals another user's lock,
//      which is admin territory.
//   2. save_edit_draft → checkpoint work-in-progress. Non-publishing.
//   3. publish_edit_draft → run the full server-side publish pipeline
//      (inline-row-edit guard, flow markers, database validation, page store
//      write, todo auto-complete). Clears the lock and draft on success.
//   4. discard_edit_draft → drop the draft + lock.
//
// read_edit_draft is separate: it peeks at any user's draft (admin-style
// read) without touching the lock. Handy for review or hand-off.

// ── enter_edit_session ──────────────────────────────────────────────────

func registerEnterEditSessionTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("enter_edit_session",
		mcpgo.WithDescription(
			"Take part in a collaborative edit: acquire (or resume) the edit lock and return the current draft markdown "+
				"and a fresh edit_token. Requires edit permission for the caller AND the @ai subject.\n\n"+
				"Behaviour:\n"+
				"- No lock and no draft: creates a new draft seeded from the current published page (or from initial_markdown, if provided).\n"+
				"- Caller already holds the lock: returns ErrEditSuperseded unless force=true (use force to reclaim a session left open in another tab).\n"+
				"- Another user holds the lock: refused with locked_by set. This tool never steals another user's session — that is admin territory.\n"+
				"- force=true refused with 'owner_active' if the same account is currently editing this page in a live browser tab. The aim of force is to recover stale sessions, never to interrupt someone who is actively typing.\n\n"+
				"After entering, save with save_edit_draft, then either publish_edit_draft or discard_edit_draft. Every write uses the returned edit_token.",
		),
		mcpgo.WithString("path", mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional. Namespace indexes end with '/'."),
		),
		mcpgo.WithBoolean("force",
			mcpgo.Description("Reclaim a stale edit session you already own (a tab you closed or a crashed session). Never overrides another user's lock, and refuses when the same account is presently editing the page in a live browser tab."),
		),
		mcpgo.WithString("initial_markdown",
			mcpgo.Description("Only used for a brand-new page (no published version and no existing draft). Ignored otherwise."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.DraftEditor == nil {
			return errorResult("draft editor not available"), nil
		}
		pagePath := strings.TrimPrefix(strings.TrimSpace(req.GetString("path", "")), "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		if !deps.canEdit(ctx, pagePath) {
			return errorResult("edit permission denied"), nil
		}
		force := req.GetBool("force", false)

		// force=true is meant for recovering the caller's OWN stale
		// session (a tab they closed, a crash, an SDK that dropped its
		// edit_token). If the same account is presently connected as a
		// live editor on this page, treat force as a mistake and refuse
		// — otherwise the AI would silently invalidate the human's
		// edit_token and the next save from the browser would lose
		// whatever the human typed since their last checkpoint.
		username := deps.ExtractUsername(ctx)
		if force && deps.Presence != nil && deps.Presence.HasLiveEditor(presencePageKey(pagePath), username) {
			return jsonResult(map[string]any{
				"error":     "owner_active",
				"message":   "your account is actively editing this page in a live browser session — reclaiming with force=true would cut off that session. Ask the human to save and close the tab, or wait for it to disconnect, then retry.",
				"locked_by": username,
			}), nil
		}

		// Seed content: published if any, else the caller-supplied initial
		// markdown for a brand-new page. We never overwrite an existing
		// draft with initial_markdown — DraftStore.EnterEditMode reads the
		// draft file directly when one exists.
		var published string
		var version int64
		if deps.Store != nil {
			if page, err := deps.Store.Get(pagePath); err == nil {
				published = page.Markdown
				version = page.Meta.Version
			}
		}
		if published == "" {
			if seed := req.GetString("initial_markdown", ""); seed != "" {
				published = seed
			}
		}

		markdown, editToken, err := deps.DraftEditor.EnterEditMode(pagePath, username, force, published)
		if errors.Is(err, storage.ErrPageLocked) {
			resp := map[string]any{
				"error":   "page_locked",
				"message": err.Error(),
			}
			if deps.DraftState != nil {
				lock := deps.DraftState.GetLock(pagePath)
				if lock.Owner != "" {
					resp["locked_by"] = lock.Owner
					resp["since"] = lock.Since
				}
			}
			return jsonResult(resp), nil
		}
		if errors.Is(err, storage.ErrEditSuperseded) {
			return jsonResult(map[string]any{
				"error":   "edit_superseded",
				"message": "you already have this page open in another session — retry with force=true to reclaim it",
			}), nil
		}
		if err != nil {
			return errorResult("enter_edit_session: " + err.Error()), nil
		}

		return jsonResult(map[string]any{
			"path":         "/" + pagePath,
			"edit_token":   editToken,
			"markdown":     markdown,
			"page_version": version,
		}), nil
	})
}

// ── save_edit_draft ─────────────────────────────────────────────────────

func registerSaveEditDraftTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("save_edit_draft",
		mcpgo.WithDescription(
			"Checkpoint work-in-progress on a page draft you own. Requires the edit_token returned by enter_edit_session; "+
				"if that session was superseded (another tab reclaimed it), the save is rejected. Non-publishing: the change stays "+
				"in the draft file until publish_edit_draft.",
		),
		mcpgo.WithString("path", mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional."),
		),
		mcpgo.WithString("edit_token", mcpgo.Required(),
			mcpgo.Description("Token returned by enter_edit_session."),
		),
		mcpgo.WithString("markdown", mcpgo.Required(),
			mcpgo.Description("Draft markdown in the Gowiki dialect."),
		),
		mcpgo.WithString("summary",
			mcpgo.Description("Change summary for audit. Format: '[AI: <tool-name>] <description>'. Required when the server enforces summaries."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.DraftEditor == nil {
			return errorResult("draft editor not available"), nil
		}
		pagePath := strings.TrimPrefix(strings.TrimSpace(req.GetString("path", "")), "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		if !deps.canEdit(ctx, pagePath) {
			return errorResult("edit permission denied"), nil
		}
		editToken, err := req.RequireString("edit_token")
		if err != nil {
			return errorResult("edit_token is required"), nil
		}
		markdown, err := req.RequireString("markdown")
		if err != nil {
			return errorResult("markdown is required"), nil
		}
		summary := strings.TrimSpace(req.GetString("summary", ""))
		if deps.RequireSummary && summary == "" {
			return errorResult("summary is required — format '[AI: <tool>] <description>'"), nil
		}

		username := deps.ExtractUsername(ctx)
		if err := deps.DraftEditor.SaveDraft(pagePath, username, editToken, markdown); err != nil {
			if errors.Is(err, storage.ErrEditSuperseded) || errors.Is(err, storage.ErrNoDraft) {
				return jsonResult(map[string]any{
					"error":   "edit_superseded",
					"message": "your edit session is no longer active — re-enter with enter_edit_session",
				}), nil
			}
			if errors.Is(err, storage.ErrPageLocked) {
				return jsonResult(map[string]any{
					"error":   "page_locked",
					"message": err.Error(),
				}), nil
			}
			return errorResult("save_edit_draft: " + err.Error()), nil
		}
		return jsonResult(map[string]any{
			"path":   "/" + pagePath,
			"status": "draft_saved",
		}), nil
	})
}

// ── read_edit_draft ─────────────────────────────────────────────────────

func registerReadEditDraftTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("read_edit_draft",
		mcpgo.WithDescription(
			"Peek at the current draft for a page, regardless of who owns it — the same view the collaborative-edit UI shows to "+
				"observers. Requires view permission. Returns the raw draft markdown, the owner, and whether the caller is that owner. "+
				"When no draft exists but a lock does, the response says so. When neither exists, returns error 'no_draft'.",
		),
		mcpgo.WithString("path", mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.DraftEditor == nil {
			return errorResult("draft editor not available"), nil
		}
		pagePath := strings.TrimPrefix(strings.TrimSpace(req.GetString("path", "")), "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		if !deps.canView(ctx, pagePath) {
			return errorResult("access denied"), nil
		}

		username := deps.ExtractUsername(ctx)

		var owner string
		var since string
		if deps.DraftState != nil {
			if info, ok := deps.DraftState.FindAnyDraft(pagePath); ok {
				owner = info.Owner
				since = info.Since
			}
		}
		if owner == "" {
			// No draft file — check for a bare lock (transient, but worth
			// surfacing to the caller so they know why writes will fail).
			var lockOwner string
			var lockSince string
			if deps.DraftState != nil {
				if lock := deps.DraftState.GetLock(pagePath); lock.Owner != "" {
					lockOwner = lock.Owner
					lockSince = lock.Since
				}
			}
			resp := map[string]any{
				"error":   "no_draft",
				"message": "no draft exists for this page",
			}
			if lockOwner != "" {
				resp["lock"] = map[string]any{
					"owner": lockOwner,
					"since": lockSince,
				}
			}
			return jsonResult(resp), nil
		}

		markdown, err := deps.DraftEditor.AdminReadDraft(pagePath, owner)
		if err != nil {
			if errors.Is(err, storage.ErrNoDraft) {
				return jsonResult(map[string]any{
					"error":   "no_draft",
					"message": "draft file disappeared between lookup and read",
				}), nil
			}
			return errorResult("read_edit_draft: " + err.Error()), nil
		}
		return jsonResult(map[string]any{
			"path":         "/" + pagePath,
			"markdown":     markdown,
			"owner":        owner,
			"since":        since,
			"is_own_draft": owner == username,
		}), nil
	})
}

// ── publish_edit_draft ──────────────────────────────────────────────────

func registerPublishEditDraftTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("publish_edit_draft",
		mcpgo.WithDescription(
			"Publish the draft you own: run the full server-side pipeline (inline-row-edit conflict guard, flow-marker strip, "+
				"database validation, page store write, todo auto-complete), then clear the draft and lock. Requires edit permission "+
				"and the edit_token from enter_edit_session.\n\n"+
				"Failure modes:\n"+
				"- database_row_conflict: a database row bound to this page was edited inline while your draft was open. Set "+
				"force_publish=true to overwrite the inline edit; otherwise re-enter the session to pick up the row edit.\n"+
				"- edit_superseded / no_draft: your session was reclaimed or never existed.\n"+
				"- validation: the draft failed database system-column validation (fix the markdown, save_edit_draft again, retry).\n\n"+
				"Always call preview_page_diff or show the draft to the user before publishing.",
		),
		mcpgo.WithString("path", mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional."),
		),
		mcpgo.WithString("edit_token", mcpgo.Required(),
			mcpgo.Description("Token from enter_edit_session."),
		),
		mcpgo.WithString("summary",
			mcpgo.Description("Change summary for audit. Format: '[AI: <tool-name>] <description>'. Required when the server enforces summaries."),
		),
		mcpgo.WithBoolean("force_publish",
			mcpgo.Description("Override a database_row_conflict warning and publish anyway. Use only after confirming with the user."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.DraftPublisher == nil {
			return errorResult("draft publisher not available"), nil
		}
		pagePath := strings.TrimPrefix(strings.TrimSpace(req.GetString("path", "")), "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		if !deps.canEdit(ctx, pagePath) {
			return errorResult("edit permission denied"), nil
		}
		editToken, err := req.RequireString("edit_token")
		if err != nil {
			return errorResult("edit_token is required"), nil
		}
		summary := strings.TrimSpace(req.GetString("summary", ""))
		if deps.RequireSummary && summary == "" {
			return errorResult("summary is required — format '[AI: <tool>] <description>'"), nil
		}
		force := req.GetBool("force_publish", false)

		username := deps.ExtractUsername(ctx)
		result, perr := deps.DraftPublisher.PublishDraft(ctx, pagePath, username, editToken, force)
		if perr != nil {
			switch perr.PublishErrorKind() {
			case PublishKindDatabaseRowConflict:
				return jsonResult(map[string]any{
					"error":   "database_row_conflict",
					"message": perr.Error(),
					"table":   perr.ConflictTable(),
				}), nil
			case PublishKindEditSuperseded:
				return jsonResult(map[string]any{
					"error":   "edit_superseded",
					"message": "your edit session is no longer active — re-enter with enter_edit_session",
				}), nil
			case PublishKindNoDraft:
				return jsonResult(map[string]any{
					"error":   "no_draft",
					"message": "no draft to publish for this page",
				}), nil
			case PublishKindValidation:
				return errorResult("validation failed: " + perr.Error()), nil
			default:
				return errorResult("publish_edit_draft: " + perr.Error()), nil
			}
		}
		return jsonResult(map[string]any{
			"path":               result.Path,
			"version":            result.Version,
			"edit_token_cleared": true,
			"status":             "published",
		}), nil
	})
}

// ── discard_edit_draft ──────────────────────────────────────────────────

func registerDiscardEditDraftTool(srv *mcpsrv.MCPServer, deps Deps) {
	tool := mcpgo.NewTool("discard_edit_draft",
		mcpgo.WithDescription(
			"Throw away the caller's own draft and clear the lock. Requires edit permission. When edit_token is provided it must "+
				"match the current lock; when omitted, the call is refused if a token-bearing session still exists (another tab). "+
				"Never touches another user's draft.\n\n"+
				"Refused with 'owner_active' if the same account is currently editing this page in a live browser tab — discarding "+
				"would silently wipe the human's in-progress work.",
		),
		mcpgo.WithString("path", mcpgo.Required(),
			mcpgo.Description("Page path, leading slash optional."),
		),
		mcpgo.WithString("edit_token",
			mcpgo.Description("Token from enter_edit_session. Optional — omit to discard a session left open in this tab without a token."),
		),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if deps.DraftEditor == nil {
			return errorResult("draft editor not available"), nil
		}
		pagePath := strings.TrimPrefix(strings.TrimSpace(req.GetString("path", "")), "/")
		if pagePath == "" {
			return errorResult("path is required"), nil
		}
		if !deps.canEdit(ctx, pagePath) {
			return errorResult("edit permission denied"), nil
		}
		editToken := strings.TrimSpace(req.GetString("edit_token", ""))

		username := deps.ExtractUsername(ctx)
		// Same-account live editor guard: if the human is on the page in
		// edit mode right now, refuse the discard — otherwise the AI
		// would silently blank the draft under them. A closed tab clears
		// presence, so genuinely stale sessions stay discardable.
		if deps.Presence != nil && deps.Presence.HasLiveEditor(presencePageKey(pagePath), username) {
			return jsonResult(map[string]any{
				"error":   "owner_active",
				"message": "your account is actively editing this page in a live browser session — discarding would wipe unsaved work. Close the tab first, or publish/save from the browser, then retry.",
			}), nil
		}
		if err := deps.DraftEditor.DiscardDraft(pagePath, username, editToken); err != nil {
			switch {
			case errors.Is(err, storage.ErrNotDraftOwner):
				return errorResult("not the draft owner — this session was created by another user"), nil
			case errors.Is(err, storage.ErrEditSuperseded):
				return jsonResult(map[string]any{
					"error":   "edit_superseded",
					"message": "another editing session is active — provide its edit_token or wait for it to end",
				}), nil
			default:
				return errorResult("discard_edit_draft: " + err.Error()), nil
			}
		}
		return jsonResult(map[string]any{
			"path":   "/" + pagePath,
			"status": "draft_discarded",
		}), nil
	})
}

