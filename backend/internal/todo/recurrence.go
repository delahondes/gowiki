package todo

import (
	"time"
)

// NextDueDate computes the next due date based on recurrence rules.
// For delay type: completedAt + N days.
// For calendar type: advance current due date by N units.
func NextDueDate(currentDue string, r Recurrence, completedAt time.Time) string {
	if r.IsZero() {
		return ""
	}

	switch r.Type {
	case "delay":
		next := completedAt.AddDate(0, 0, r.Days)
		return next.Format("2006-01-02")
	case "calendar":
		base, err := time.Parse("2006-01-02", currentDue)
		if err != nil {
			// If no valid current due date, use completedAt as base.
			base = completedAt
		}
		return advanceDate(base, r.Every, r.Unit)
	}
	return ""
}

// advanceDate moves a date forward by count units.
func advanceDate(base time.Time, count int, unit string) string {
	switch unit {
	case "day":
		return base.AddDate(0, 0, count).Format("2006-01-02")
	case "week":
		return base.AddDate(0, 0, count*7).Format("2006-01-02")
	case "month":
		return base.AddDate(0, count, 0).Format("2006-01-02")
	case "year":
		return base.AddDate(count, 0, 0).Format("2006-01-02")
	}
	return base.Format("2006-01-02")
}

// SpawnNext creates a new task from a completed recurring task.
// The new task is linked via recurrence_group_id.
func SpawnNext(original *Task, completedAt time.Time) *CreateRequest {
	nextDue := NextDueDate(original.DueDate, original.Recurrence, completedAt)

	groupID := original.RecurrenceGroupID
	if groupID == "" {
		groupID = original.ID
	}

	return &CreateRequest{
		Title:             original.Title,
		Description:       original.Description,
		Source:            original.Source,
		SourcePage:        original.SourcePage,
		NodeKey:           original.NodeKey,
		Assignee:          original.Assignee,
		DueDate:           nextDue,
		Recurrence:        original.Recurrence,
		WikiAction:        original.WikiAction,
		Tags:              original.Tags,
		Priority:          original.Priority,
		CreatedBy:         original.CreatedBy,
		RecurrenceGroupID: groupID,
	}
}
