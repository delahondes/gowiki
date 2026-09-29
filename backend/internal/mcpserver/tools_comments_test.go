package mcpserver

import (
	"testing"
	"time"

	"gowiki/backend/internal/comment"
)

// buildCommentThreadView is the shaping helper the list_page_comments
// tool calls after reading from the comment store. Its behaviour is the
// contract the LLM sees at the wire: replies are nested under their
// parent, resolved threads drop out by default, the returned counts
// reflect the true state.

func mkComment(id, parentID, text string, resolved bool, created time.Time) comment.Comment {
	return comment.Comment{
		ID:        id,
		ParentID:  parentID,
		Text:      text,
		Author:    "alice",
		CreatedAt: created,
		UpdatedAt: created,
		Resolved:  resolved,
		Anchor:    comment.Anchor{Selected: "text"},
	}
}

func TestBuildCommentThreadView_EmptyInput(t *testing.T) {
	t.Parallel()
	threads, open, total := buildCommentThreadView(nil, false)
	if open != 0 || total != 0 || len(threads) != 0 {
		t.Errorf("empty: got open=%d total=%d threads=%d, want 0 0 0", open, total, len(threads))
	}
}

func TestBuildCommentThreadView_UnresolvedOnly(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	comments := []comment.Comment{
		mkComment("t1", "", "still open", false, now),
		mkComment("t2", "", "also open", false, now),
	}
	threads, open, total := buildCommentThreadView(comments, false)
	if open != 2 || total != 2 || len(threads) != 2 {
		t.Errorf("2 open: got open=%d total=%d shown=%d, want 2 2 2", open, total, len(threads))
	}
}

func TestBuildCommentThreadView_MixedResolution_DefaultExcludesResolved(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	comments := []comment.Comment{
		mkComment("t1", "", "open", false, now),
		mkComment("t2", "", "closed", true, now),
	}
	threads, open, total := buildCommentThreadView(comments, false)
	// open = 1, total = 2, shown = 1 (resolved dropped by default).
	if open != 1 {
		t.Errorf("open = %d, want 1", open)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2 (counted before filter)", total)
	}
	if len(threads) != 1 {
		t.Fatalf("shown threads = %d, want 1", len(threads))
	}
	if threads[0].ID != "t1" || threads[0].Resolved {
		t.Errorf("returned thread should be the open one, got %+v", threads[0])
	}
}

func TestBuildCommentThreadView_IncludeResolvedReturnsBoth(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	comments := []comment.Comment{
		mkComment("t1", "", "open", false, now),
		mkComment("t2", "", "closed", true, now),
	}
	threads, open, total := buildCommentThreadView(comments, true)
	if open != 1 || total != 2 || len(threads) != 2 {
		t.Errorf("include_resolved: got open=%d total=%d shown=%d, want 1 2 2", open, total, len(threads))
	}
}

func TestBuildCommentThreadView_RepliesGroupedUnderParent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	comments := []comment.Comment{
		mkComment("root", "", "top-level question", false, now),
		mkComment("r1", "root", "first answer", false, now.Add(1*time.Minute)),
		mkComment("r2", "root", "second answer", false, now.Add(2*time.Minute)),
		mkComment("other", "", "separate thread", false, now),
	}
	threads, open, total := buildCommentThreadView(comments, false)
	if open != 2 || total != 2 {
		t.Errorf("open=%d total=%d, want 2 2 (replies do not count as their own threads)", open, total)
	}
	if len(threads) != 2 {
		t.Fatalf("expected 2 top-level threads, got %d", len(threads))
	}
	var root *threadView
	for i, tr := range threads {
		if tr.ID == "root" {
			root = &threads[i]
		}
	}
	if root == nil {
		t.Fatalf("root thread missing")
	}
	if len(root.Replies) != 2 {
		t.Errorf("root replies = %d, want 2", len(root.Replies))
	}
	// Replies keep the order they arrived in — the store already sorts
	// by CreatedAt when it serialises.
	if root.Replies[0].ID != "r1" || root.Replies[1].ID != "r2" {
		t.Errorf("reply order lost: got %+v", root.Replies)
	}
}

func TestBuildCommentThreadView_UpdatedAtEmittedOnlyWhenChanged(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	// One thread never edited (UpdatedAt == CreatedAt) — UpdatedAt must
	// be omitted from the output shape to keep the JSON tidy. Another
	// with a real edit must emit the field.
	untouched := mkComment("t1", "", "stable", false, created)
	edited := mkComment("t2", "", "was rephrased", false, created)
	edited.UpdatedAt = created.Add(3 * time.Hour)

	threads, _, _ := buildCommentThreadView([]comment.Comment{untouched, edited}, false)
	if len(threads) != 2 {
		t.Fatalf("got %d threads, want 2", len(threads))
	}
	for _, tr := range threads {
		switch tr.ID {
		case "t1":
			if tr.UpdatedAt != "" {
				t.Errorf("t1 UpdatedAt should be empty (never edited), got %q", tr.UpdatedAt)
			}
		case "t2":
			if tr.UpdatedAt == "" {
				t.Errorf("t2 UpdatedAt should be populated (edit is 3h after create)")
			}
		}
	}
}

func TestBuildCommentThreadView_ReplyToResolvedParentStaysHidden(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	comments := []comment.Comment{
		mkComment("root", "", "closed conversation", true, now),
		mkComment("r1", "root", "afterthought", false, now),
	}
	// With includeResolved=false the resolved root drops, so the
	// afterthought reply is not surfaced either (it belongs to a hidden
	// thread). The count semantics unchanged.
	threads, open, total := buildCommentThreadView(comments, false)
	if open != 0 || total != 1 || len(threads) != 0 {
		t.Errorf("resolved thread with reply: got open=%d total=%d shown=%d, want 0 1 0",
			open, total, len(threads))
	}
}
