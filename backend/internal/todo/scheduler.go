package todo

import (
	"context"
	"log"
	"time"
)

// RunScheduler starts a background goroutine that checks for
// upcoming and overdue tasks every hour and sends notifications.
// It reads reminder_hours from the config store dynamically.
//
// Notification rules:
//   - "assigned": sent once at creation time (by CreateTask), never re-sent by scheduler.
//   - "due_reminder": sent once when the task enters the reminder window (N hours before due).
//   - "overdue": sent once per day starting from the deadline.
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
				reminderHours = []int{24}
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
			// Only send due_reminder once per task.
			already, err := store.HasNotificationBeenSent(ctx, task.ID, "due_reminder")
			if err != nil {
				log.Printf("todo scheduler: check notification failed for %s: %v", task.ID, err)
				continue
			}
			if already {
				continue
			}

			dispatcher.Notify(NotifyEvent{
				Type:      "due_reminder",
				Task:      task,
				Recipient: task.Assignee.Target,
				UserID:    task.Assignee.Target,
			})
			_ = store.RecordNotificationSent(ctx, task.ID, "due_reminder")
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
		// Send overdue notification at most once per day.
		lastSent, err := store.GetNotificationSentAt(ctx, task.ID, "overdue")
		if err != nil {
			log.Printf("todo scheduler: check overdue notification failed for %s: %v", task.ID, err)
			continue
		}
		if !lastSent.IsZero() && now.Sub(lastSent) < 24*time.Hour {
			continue
		}

		dispatcher.Notify(NotifyEvent{
			Type:      "overdue",
			Task:      task,
			Recipient: task.Assignee.Target,
			UserID:    task.Assignee.Target,
		})
		_ = store.RecordNotificationSent(ctx, task.ID, "overdue")
	}
}
