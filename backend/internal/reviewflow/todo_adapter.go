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
