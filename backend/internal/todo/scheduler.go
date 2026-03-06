package todo

import (
	"context"
	"log"
	"time"
)

// RunScheduler starts a background goroutine that checks for
// upcoming and overdue tasks every hour and sends notifications.
// It reads reminder_hours from the config store dynamically.
func RunScheduler(ctx context.Context, store *TodoStore, dispatcher *Dispatcher) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	log.Printf("todo scheduler: started")

	for {
		select {
		case <-ctx.Done():
			log.Printf("todo scheduler: stopped")
			return
		case <-ticker.C:
			var reminderHours []int
			if dispatcher.configReader != nil {
				reminderHours = dispatcher.configReader.Get().Todo.ReminderHours
			}
			if len(reminderHours) == 0 {
				reminderHours = []int{24, 2}
			}
			checkReminders(ctx, store, dispatcher, reminderHours)
			checkOverdue(ctx, store, dispatcher)
		}
	}
}

func checkReminders(ctx context.Context, store *TodoStore, dispatcher *Dispatcher, hours []int) {
	now := time.Now().UTC()
	for _, h := range hours {
		from := now
		to := now.Add(time.Duration(h) * time.Hour)

		tasks, err := store.ListDueBetween(ctx, from, to)
		if err != nil {
			log.Printf("todo scheduler: reminder query failed: %v", err)
			continue
		}

		for _, task := range tasks {
			dispatcher.Notify(NotifyEvent{
				Type:      "due_reminder",
				Task:      task,
				Recipient: task.Assignee.Target,
				UserID:    task.Assignee.Target,
			})
		}
	}
}

func checkOverdue(ctx context.Context, store *TodoStore, dispatcher *Dispatcher) {
	now := time.Now().UTC()
	tasks, err := store.ListOverdue(ctx, now)
	if err != nil {
		log.Printf("todo scheduler: overdue query failed: %v", err)
		return
	}

	for _, task := range tasks {
		dispatcher.Notify(NotifyEvent{
			Type:      "overdue",
			Task:      task,
			Recipient: task.Assignee.Target,
			UserID:    task.Assignee.Target,
		})
	}
}
