package reviewflow

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"gowiki/backend/internal/todo"
)

// TodoAdapter implements TodoIntegrator using the todo plugin's service.
type TodoAdapter struct {
	todoService *todo.TodoService
}

// NewTodoAdapter creates a new adapter that bridges reviewflow to the todo plugin.
func NewTodoAdapter(svc *todo.TodoService) *TodoAdapter {
	return &TodoAdapter{todoService: svc}
}

// reviewTaskNodeKey returns a stable key for a reviewflow todo task.
func reviewTaskNodeKey(pagePath, role, user string) string {
	h := sha1.Sum([]byte("reviewflow:" + pagePath + ":" + role + ":" + user))
	return hex.EncodeToString(h[:])
}

// CreateReviewTasks creates one todo task per role that needs
// confirmation. Idempotent: if an open or in-progress task already
// exists for this (page, role, user) under the same version tag,
// the call is a no-op — no new task row, no fresh "assigned"
// notification. This is the invariant that keeps a server restart
// (which re-runs ReconcileOrphanTasks → CancelReviewTasks →
// CreateReviewTasks) from blasting a user with another round of
// emails for documents they already have open review tasks on.
//
// The dedup key is node_key = sha1("reviewflow:page:role:user") PLUS
// the version tag embedded in the title. A tag change (1.0 → 1.1)
// does spawn a fresh task because the user has to re-review the new
// content — that's the whole point of the version bump.
func (a *TodoAdapter) CreateReviewTasks(pagePath string, roles map[string]string, versionTag string, dueDate string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tagLabel := ""
	if versionTag != "" {
		tagLabel = fmt.Sprintf(" (%s)", versionTag)
	}

	store := a.todoService.Store()
	// Fetch the existing tasks for this page ONCE and build the
	// (node_key → surviving task) map so each role lookup is O(1).
	// Covers both slash forms of the page path, same as the other
	// adapter paths that have been bitten by that spelling drift.
	existing, _ := store.ListForPage(ctx, pagePath)
	byNodeKey := make(map[string]*todo.Task, len(existing))
	for _, t := range existing {
		if t.Tags != "reviewflow" {
			continue
		}
		if t.Status != todo.StatusOpen && t.Status != todo.StatusInProgress {
			continue
		}
		byNodeKey[t.NodeKey] = t
	}

	for role, user := range roles {
		nodeKey := reviewTaskNodeKey(pagePath, role, user)
		// Idempotent short-circuit: an open task for this exact
		// (page, role, user) at the SAME version tag already exists,
		// so creating another would duplicate the row and fire a
		// redundant "assigned" email. The version tag check uses the
		// title's embedded "(tag)" fragment so a tag bump is still
		// treated as a fresh review cycle.
		if t, ok := byNodeKey[nodeKey]; ok {
			if versionTag == "" || strings.Contains(t.Title, tagLabel) {
				continue
			}
		}
		title := fmt.Sprintf("Review%s: %s as %s on %s", tagLabel, user, role, pagePath)
		req := todo.CreateRequest{
			Title:      title,
			Source:     todo.SourceAPI,
			SourcePage: pagePath,
			NodeKey:    nodeKey,
			Assignee: todo.Assignee{
				Type:       "user",
				Target:     user,
				Resolution: "any",
			},
			DueDate:   dueDate,
			Tags:      "reviewflow",
			Priority:  todo.PriorityNormal,
			CreatedBy: "reviewflow",
		}
		if _, err := a.todoService.CreateTask(ctx, req); err != nil {
			log.Printf("reviewflow: failed to create todo for %s/%s: %v", pagePath, role, err)
		}
	}
	return nil
}

// EnsureReviewTasks reconciles the open-task set for one page against
// the desired (role → user) map. A task matching (page, role, user)
// AT THE SAME VERSION TAG is left alone; one that no longer belongs
// is cancelled; a missing one is created.
//
// This replaces the "CancelReviewTasks + CreateReviewTasks" pair for
// callers that want idempotent sync behaviour. The old pair is still
// available for callers that explicitly want "wipe everything and
// start over" (e.g. a page delete). The key difference: Ensure NEVER
// cancels a task that is still correct, so the "assigned" email that
// CreateTask would otherwise emit after a restart stays silent.
//
// Returns the counts (kept, cancelled, created) for logging.
func (a *TodoAdapter) EnsureReviewTasks(pagePath string, want map[string]string, versionTag string, dueDate string) (kept, cancelled, created int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := a.todoService.Store()

	tagLabel := ""
	if versionTag != "" {
		tagLabel = fmt.Sprintf(" (%s)", versionTag)
	}

	// Index the live set by node_key for O(1) lookup. Only open and
	// in-progress tasks are considered "live"; cancelled/done ones
	// don't block a fresh create.
	existing, err := store.ListForPage(ctx, pagePath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list tasks for page: %w", err)
	}
	live := make(map[string]*todo.Task, len(existing))
	for _, t := range existing {
		if t.Tags != "reviewflow" {
			continue
		}
		if t.Status != todo.StatusOpen && t.Status != todo.StatusInProgress {
			continue
		}
		live[t.NodeKey] = t
	}

	// Compute the desired node_key → user set.
	desired := make(map[string]string, len(want))
	for role, user := range want {
		desired[reviewTaskNodeKey(pagePath, role, user)] = user
	}

	// Phase 1: cancel live tasks that are not in the desired set OR
	// whose version tag has changed. A changed tag means a new review
	// cycle, so the old task is stale and the new one needs to fire.
	for key, task := range live {
		wantUser, inDesired := desired[key]
		if !inDesired {
			if _, cancelErr := store.Cancel(ctx, task.ID); cancelErr != nil {
				log.Printf("reviewflow: cancel stale todo %s failed: %v", task.ID, cancelErr)
				continue
			}
			cancelled++
			continue
		}
		if versionTag != "" && !strings.Contains(task.Title, tagLabel) {
			if _, cancelErr := store.Cancel(ctx, task.ID); cancelErr != nil {
				log.Printf("reviewflow: cancel version-mismatched todo %s failed: %v", task.ID, cancelErr)
				continue
			}
			cancelled++
			delete(live, key)
			_ = wantUser // user is re-looked-up below
		}
	}

	// Phase 2: create tasks for desired entries with no surviving
	// live counterpart.
	for role, user := range want {
		nodeKey := reviewTaskNodeKey(pagePath, role, user)
		if t, ok := live[nodeKey]; ok && (versionTag == "" || strings.Contains(t.Title, tagLabel)) {
			kept++
			continue
		}
		title := fmt.Sprintf("Review%s: %s as %s on %s", tagLabel, user, role, pagePath)
		req := todo.CreateRequest{
			Title:      title,
			Source:     todo.SourceAPI,
			SourcePage: pagePath,
			NodeKey:    nodeKey,
			Assignee: todo.Assignee{
				Type:       "user",
				Target:     user,
				Resolution: "any",
			},
			DueDate:   dueDate,
			Tags:      "reviewflow",
			Priority:  todo.PriorityNormal,
			CreatedBy: "reviewflow",
		}
		if _, createErr := a.todoService.CreateTask(ctx, req); createErr != nil {
			log.Printf("reviewflow: failed to create todo for %s/%s: %v", pagePath, role, createErr)
			continue
		}
		created++
	}
	return kept, cancelled, created, nil
}

// CancelReviewTasks cancels any open reviewflow todo tasks for a page.
func (a *TodoAdapter) CancelReviewTasks(pagePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := a.todoService.Store()
	tasks, err := store.ListForPage(ctx, pagePath)
	if err != nil {
		return fmt.Errorf("list tasks for page: %w", err)
	}
	for _, t := range tasks {
		if t.Tags == "reviewflow" && (t.Status == todo.StatusOpen || t.Status == todo.StatusInProgress) {
			if _, err := store.Cancel(ctx, t.ID); err != nil {
				log.Printf("reviewflow: failed to cancel todo %s: %v", t.ID, err)
			}
		}
	}
	return nil
}

// CompleteReviewTasks marks the reviewflow task for each confirmed
// (role, user) pair as done. Looks up tasks by node_key (deterministic
// from page+role+user) and marks every matching open/in-progress task
// as done — not just the most recent. The multi-match pass is needed
// because namespace-index pages have been created with two different
// path spellings (/foo and /foo/) over the project's history, so a
// single (role, user) can have multiple surviving open tasks under
// different node_keys that all deserve to close when the confirmation
// lands.
//
// Tasks already in done or cancelled state are skipped. Returns the
// total number of tasks transitioned.
func (a *TodoAdapter) CompleteReviewTasks(pagePath string, confirmedByRole map[string]string) (int, error) {
	if len(confirmedByRole) == 0 {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := a.todoService.Store()
	tasks, err := store.ListForPage(ctx, pagePath)
	if err != nil {
		return 0, fmt.Errorf("list tasks for page: %w", err)
	}

	n := 0
	for role, user := range confirmedByRole {
		// Build the set of node_keys that could identify this
		// confirmation. Both path forms are checked so legacy tasks
		// created under a different spelling of the namespace index
		// are still matched.
		keys := map[string]bool{
			reviewTaskNodeKey(pagePath, role, user): true,
		}
		if alt := altPagePath(pagePath); alt != pagePath {
			keys[reviewTaskNodeKey(alt, role, user)] = true
		}
		for _, t := range tasks {
			if t.Tags != "reviewflow" || !keys[t.NodeKey] {
				continue
			}
			if t.Status == todo.StatusDone || t.Status == todo.StatusCancelled {
				continue
			}
			if _, err := store.MarkDone(ctx, t.ID); err != nil {
				log.Printf("reviewflow: failed to mark todo %s done: %v", t.ID, err)
				continue
			}
			n++
		}
	}
	return n, nil
}

// ListOpenPagesWithReviewTasks returns the distinct source_page values
// of every open or in-progress reviewflow task.
func (a *TodoAdapter) ListOpenPagesWithReviewTasks() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tasks, err := a.todoService.Store().ListOpenReviewflowTasks(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(tasks))
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if t.SourcePage == "" || seen[t.SourcePage] {
			continue
		}
		seen[t.SourcePage] = true
		out = append(out, t.SourcePage)
	}
	return out, nil
}

// altPagePath returns the alternate slash form of a namespace page
// path — mirrors store.pagePathAltForm, kept local to avoid an
// exported path helper leaking cross-package.
func altPagePath(pagePath string) string {
	if pagePath == "" || pagePath == "/" {
		return pagePath
	}
	if len(pagePath) > 0 && pagePath[len(pagePath)-1] == '/' {
		return pagePath[:len(pagePath)-1]
	}
	return pagePath + "/"
}
