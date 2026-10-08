package reviewflow

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
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

// CreateReviewTasks creates one todo task per role that needs confirmation.
func (a *TodoAdapter) CreateReviewTasks(pagePath string, roles map[string]string, versionTag string, dueDate string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tagLabel := ""
	if versionTag != "" {
		tagLabel = fmt.Sprintf(" (%s)", versionTag)
	}

	for role, user := range roles {
		title := fmt.Sprintf("Review%s: %s as %s on %s", tagLabel, user, role, pagePath)
		req := todo.CreateRequest{
			Title:      title,
			Source:     todo.SourceAPI,
			SourcePage: pagePath,
			NodeKey:    reviewTaskNodeKey(pagePath, role, user),
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
