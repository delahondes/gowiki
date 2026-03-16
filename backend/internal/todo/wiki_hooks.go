package todo

import (
	"context"
	"log"
	"time"
)

// TodoSyncer implements storage.DatabaseSyncer for syncing todo directives
// from page content to the todo_tasks table.
type TodoSyncer struct {
	store      *TodoStore
	hub        *Hub
	dispatcher *Dispatcher
	createdBy  string // default author for wiki-sourced tasks
}

// NewTodoSyncer creates a new syncer.
func NewTodoSyncer(store *TodoStore, hub *Hub, dispatcher *Dispatcher) *TodoSyncer {
	return &TodoSyncer{
		store:      store,
		hub:        hub,
		dispatcher: dispatcher,
	}
}

// SyncPageRows extracts {todo ...} directives from markdown and upserts tasks.
// Implements storage.DatabaseSyncer.
func (ts *TodoSyncer) SyncPageRows(pagePath, markdown string) {
	directives := ExtractTodoDirectives(markdown)
	if len(directives) == 0 && !ts.hasExistingTasks(pagePath) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Snapshot existing task IDs before upsert to detect new tasks.
	existingTasks, _ := ts.store.ListForPage(ctx, pagePath)
	existingIDs := make(map[string]bool, len(existingTasks))
	for _, t := range existingTasks {
		existingIDs[t.ID] = true
	}

	if err := ts.store.UpsertForPage(ctx, pagePath, directives, ts.createdBy); err != nil {
		log.Printf("todo sync: upsert for page %s failed: %v", pagePath, err)
		return
	}

	// Notify via SSE about page task changes, and send email for new tasks.
	tasks, err := ts.store.ListForPage(ctx, pagePath)
	if err != nil {
		return
	}
	for _, task := range tasks {
		if task.Status == StatusOpen {
			for _, t := range SplitTargets(task.Assignee.Target) {
				ts.hub.Publish(CleanTargetName(t), Event{
					Type: "task.updated",
					Task: task,
				})
			}
		}
		// Send email/webhook notification for newly created tasks.
		if !existingIDs[task.ID] && ts.dispatcher != nil && task.Assignee.Target != "" {
			for _, t := range SplitTargets(task.Assignee.Target) {
				name := CleanTargetName(t)
				ts.dispatcher.Notify(NotifyEvent{
					Type:      "assigned",
					Task:      task,
					Recipient: name,
					UserID:    name,
				})
			}
		}
	}
}

// RemovePageRows cancels all tasks for a deleted page.
// Implements storage.DatabaseSyncer.
func (ts *TodoSyncer) RemovePageRows(pagePath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get tasks before cancelling for SSE notification.
	tasks, _ := ts.store.ListForPage(ctx, pagePath)

	if err := ts.store.CancelAllForPage(ctx, pagePath); err != nil {
		log.Printf("todo sync: cancel for page %s failed: %v", pagePath, err)
	}

	for _, task := range tasks {
		for _, t := range SplitTargets(task.Assignee.Target) {
			ts.hub.Publish(CleanTargetName(t), Event{
				Type: "task.updated",
				Task: task,
			})
		}
	}
}

func (ts *TodoSyncer) hasExistingTasks(pagePath string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tasks, err := ts.store.ListForPage(ctx, pagePath)
	if err != nil {
		return false
	}
	return len(tasks) > 0
}
