package todo

import (
	"context"
	"log"
	"regexp"
	"time"
)

// TodoService is the public API for the todo plugin, used by other packages.
type TodoService struct {
	store      *TodoStore
	hub        *Hub
	dispatcher *Dispatcher
}

// NewService creates a new todo service.
func NewService(store *TodoStore, hub *Hub, dispatcher *Dispatcher) *TodoService {
	return &TodoService{
		store:      store,
		hub:        hub,
		dispatcher: dispatcher,
	}
}

// Store returns the underlying store for direct access.
func (svc *TodoService) Store() *TodoStore {
	return svc.store
}

// Hub returns the SSE hub.
func (svc *TodoService) Hub() *Hub {
	return svc.hub
}

// CreateTask creates a task and publishes SSE events.
func (svc *TodoService) CreateTask(ctx context.Context, req CreateRequest) (*Task, error) {
	task, err := svc.store.Create(ctx, req)
	if err != nil {
		return nil, err
	}

	svc.hub.Publish(task.Assignee.Target, Event{Type: "task.created", Task: task})

	if svc.dispatcher != nil && task.Assignee.Target != "" {
		svc.dispatcher.Notify(NotifyEvent{
			Type:      "assigned",
			Task:      task,
			Recipient: task.Assignee.Target,
			UserID:    task.Assignee.Target,
		})
	}

	return task, nil
}

// CompleteTask completes a task, handles recurrence, and publishes events.
func (svc *TodoService) CompleteTask(ctx context.Context, taskID, userID string, resolver GroupResolver) (*Task, error) {
	task, promoted, err := svc.store.Complete(ctx, taskID, userID, resolver)
	if err != nil {
		return nil, err
	}

	svc.hub.Publish(task.Assignee.Target, Event{Type: "task.completed", Task: task})

	// Handle recurrence if task was fully promoted to done.
	if promoted && !task.Recurrence.IsZero() {
		spawnReq := SpawnNext(task, time.Now().UTC())
		newTask, err := svc.store.Create(ctx, *spawnReq)
		if err != nil {
			log.Printf("todo: spawn recurrence failed for %s: %v", task.ID, err)
		} else {
			svc.hub.Publish(newTask.Assignee.Target, Event{Type: "task.created", Task: newTask})
			if svc.dispatcher != nil {
				svc.dispatcher.Notify(NotifyEvent{
					Type:      "recurrence_spawned",
					Task:      newTask,
					Recipient: newTask.Assignee.Target,
					UserID:    newTask.Assignee.Target,
				})
			}
		}
	}

	return task, nil
}

// ReopenTask reopens a task and publishes an event.
func (svc *TodoService) ReopenTask(ctx context.Context, taskID string) (*Task, error) {
	task, err := svc.store.Reopen(ctx, taskID)
	if err != nil {
		return nil, err
	}
	svc.hub.Publish(task.Assignee.Target, Event{Type: "task.reopened", Task: task})
	return task, nil
}

// ReopenReadTasks reopens all completed read-action tasks for a page.
// This is called when a page is saved, so users must re-acknowledge.
// Completions are preserved so the previous ack version is available for diff links.
func (svc *TodoService) ReopenReadTasks(ctx context.Context, pagePath string) {
	tasks, err := svc.store.ListDoneReadTasks(ctx, pagePath)
	if err != nil {
		log.Printf("todo: list done read tasks for %s failed: %v", pagePath, err)
		return
	}

	for _, task := range tasks {
		reopened, err := svc.store.ReopenKeepCompletions(ctx, task.ID)
		if err != nil {
			log.Printf("todo: reopen read task %s failed: %v", task.ID, err)
			continue
		}
		svc.hub.Publish(reopened.Assignee.Target, Event{Type: "task.reopened", Task: reopened})
	}
}

// AcknowledgeTask records a read acknowledgement and promotes the task if appropriate.
func (svc *TodoService) AcknowledgeTask(ctx context.Context, taskID, userID string, version int64, resolver GroupResolver) (*Task, error) {
	task, promoted, err := svc.store.Acknowledge(ctx, taskID, userID, version, resolver)
	if err != nil {
		return nil, err
	}

	svc.hub.Publish(task.Assignee.Target, Event{Type: "task.completed", Task: task})

	// Handle recurrence if task was fully promoted to done.
	if promoted && !task.Recurrence.IsZero() {
		spawnReq := SpawnNext(task, time.Now().UTC())
		newTask, err := svc.store.Create(ctx, *spawnReq)
		if err != nil {
			log.Printf("todo: spawn recurrence failed for %s: %v", task.ID, err)
		} else {
			svc.hub.Publish(newTask.Assignee.Target, Event{Type: "task.created", Task: newTask})
		}
	}

	return task, nil
}

// AutoCompleteWikiAction finds and completes tasks triggered by wiki actions.
func (svc *TodoService) AutoCompleteWikiAction(ctx context.Context, actionType, pagePath, userID string) {
	tasks, err := svc.store.ListWikiActionTasks(ctx, actionType, pagePath)
	if err != nil {
		log.Printf("todo: wiki action query failed: %v", err)
		return
	}

	for _, task := range tasks {
		if _, err := svc.CompleteTask(ctx, task.ID, userID, nil); err != nil {
			log.Printf("todo: auto-complete wiki action %s for task %s failed: %v", actionType, task.ID, err)
		}
	}
}

// AutoCompleteCreateAction checks all open "create" action tasks and completes
// those whose pattern matches the newly created/saved page path, provided the
// user performing the save is the task's assignee.
func (svc *TodoService) AutoCompleteCreateAction(ctx context.Context, pagePath, userID string) {
	tasks, err := svc.store.ListCreateActionTasks(ctx)
	if err != nil {
		log.Printf("todo: create action query failed: %v", err)
		return
	}

	for _, task := range tasks {
		// Check the assignee matches the user who created the page.
		if task.Assignee.Target != userID {
			continue
		}

		// Match page path against the task's regex pattern.
		re, err := regexp.Compile("^" + task.WikiAction.Pattern + "$")
		if err != nil {
			log.Printf("todo: invalid create action pattern %q for task %s: %v", task.WikiAction.Pattern, task.ID, err)
			continue
		}
		if !re.MatchString(pagePath) {
			continue
		}

		if _, err := svc.CompleteTask(ctx, task.ID, userID, nil); err != nil {
			log.Printf("todo: auto-complete create action for task %s failed: %v", task.ID, err)
		}
	}
}
