package todo

import (
	"strconv"
	"strings"
	"time"
)

// NextDueDate computes the next due date for a recurring task.
//
// Delay (`+Nd`, bare `N`): completedAt + N days. Mode and Tolerance
// are ignored — delay recurrences are, by construction, anchored to
// completion.
//
// Calendar (`Nd`/`Nw`/`Nm`/`Ny` + `daily`/`weekly`/…): the behaviour
// depends on `Mode` + the tolerance window around currentDue.
//
//   - Let tol = Tolerance (default: 10% of period, floor 1 day).
//
//   - Let early = completedAt is strictly before (currentDue - tol).
//
//   - Mode "at-least" (default): guarantees max gap ≤ N units.
//     Within tolerance / on-time / late: next = currentDue + N.
//     Early beyond tolerance: next = completedAt + N (re-anchor).
//     The tolerance stops drift when someone consistently finishes
//     one day early — a 1-day-early completion still fires one
//     period after the planned date, not one period after the
//     completion.
//
//   - Mode "fixed": calendar schedule is sacred.
//     Within tolerance / on-time / late: next = currentDue + N (the
//     completion satisfies the current cycle).
//     Early beyond tolerance: next = currentDue (unchanged — the
//     completion is recorded but does NOT satisfy this cycle; the
//     next instance keeps the original due date, so the user still
//     gets the alert when the scheduled date comes).
//
// For a currentDue that fails to parse, falls back to completedAt as
// the base (same as the pre-mode behaviour).
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
			return advanceDate(completedAt, r.Every, r.Unit)
		}
		// Compare at day resolution (completedAt may carry a time).
		completionDay := time.Date(completedAt.Year(), completedAt.Month(), completedAt.Day(), 0, 0, 0, 0, time.UTC)
		tolerance := r.ToleranceDays()
		earliestAcceptable := base.AddDate(0, 0, -tolerance)
		early := completionDay.Before(earliestAcceptable)

		mode := r.Mode
		if mode == "" {
			mode = "at-least"
		}
		switch mode {
		case "fixed":
			if early {
				// Cycle not satisfied — next instance keeps the
				// original due date so the user still gets the alert
				// on the scheduled day.
				return base.Format("2006-01-02")
			}
			return advanceDate(base, r.Every, r.Unit)
		default: // at-least and any unknown value
			if early {
				// Re-anchor from the completion day so the next gap
				// stays within N units.
				return advanceDate(completionDay, r.Every, r.Unit)
			}
			return advanceDate(base, r.Every, r.Unit)
		}
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

// PeriodDays returns the approximate day count for a calendar
// recurrence's period. Months and years are approximated at 30 and 365
// days respectively — good enough for the tolerance window; the actual
// due-date arithmetic in advanceDate still uses calendar-correct
// AddDate.
func (r Recurrence) PeriodDays() int {
	if r.Type != "calendar" {
		return 0
	}
	switch r.Unit {
	case "day":
		return r.Every
	case "week":
		return r.Every * 7
	case "month":
		return r.Every * 30
	case "year":
		return r.Every * 365
	}
	return 0
}

// ToleranceDays returns the tolerance window (in days) around a
// calendar recurrence's due date. Returns 0 for non-calendar types
// (tolerance has no meaning for delay).
//
// Parsing of r.Tolerance:
//   - ""        → default: 10% of period, floor 1 day
//   - "0d"/"0%" → strict: no tolerance (every day of early is "early")
//   - "N%"      → N percent of period, floor 1 day when N > 0
//   - "Nd"      → N days (absolute); negative treated as 0
//
// Unparseable values fall back to the default. A tolerance that would
// exceed the period itself is clamped to the period (a 200% tolerance
// on a yearly task would make every completion "within tolerance",
// which collapses the mode semantic — clamping keeps "within" and
// "outside" meaningful).
func (r Recurrence) ToleranceDays() int {
	if r.Type != "calendar" {
		return 0
	}
	period := r.PeriodDays()
	raw := strings.TrimSpace(r.Tolerance)
	if raw == "" {
		// Default: 10% of period, floor 1 day.
		tol := period / 10
		if tol < 1 {
			tol = 1
		}
		return clampToPeriod(tol, period)
	}
	if strings.HasSuffix(raw, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err != nil || n < 0 {
			return defaultTolerance(period)
		}
		return clampToPeriod(n, period)
	}
	if strings.HasSuffix(raw, "%") {
		pct, err := strconv.Atoi(strings.TrimSuffix(raw, "%"))
		if err != nil || pct < 0 {
			return defaultTolerance(period)
		}
		tol := period * pct / 100
		if tol < 1 && pct > 0 {
			tol = 1
		}
		return clampToPeriod(tol, period)
	}
	// Unrecognised shape — fall back to default rather than silently
	// disabling tolerance.
	return defaultTolerance(period)
}

func defaultTolerance(period int) int {
	tol := period / 10
	if tol < 1 {
		tol = 1
	}
	return clampToPeriod(tol, period)
}

func clampToPeriod(tol, period int) int {
	if period > 0 && tol > period {
		return period
	}
	return tol
}

// SpawnNext creates a new task from a completed recurring task.
// The new task is linked via recurrence_group_id.
//
// When NextDueDate returns the SAME date as the completed task's
// current due date (the "fixed-mode early beyond tolerance" case),
// the spawned instance keeps the original schedule — the user gets a
// fresh task with the same deadline, which is exactly what "fixed"
// promises: the cycle isn't closed by an early completion.
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
