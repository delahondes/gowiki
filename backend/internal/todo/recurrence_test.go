package todo

import (
	"testing"
	"time"
)

// NextDueDate + advanceDate + SpawnNext form the recurrence engine. These
// are all pure and time-parameterised — they take a completedAt and a
// currentDue as inputs so tests can pin exact dates.

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("bad date %q: %v", s, err)
	}
	return d
}

func TestNextDueDate_Delay(t *testing.T) {
	t.Parallel()
	completedAt := mustDate(t, "2026-03-10")
	// Delay is measured from completedAt, not currentDue.
	got := NextDueDate("2026-01-01", Recurrence{Type: "delay", Days: 5}, completedAt)
	if got != "2026-03-15" {
		t.Errorf("delay 5 from 2026-03-10 = %q, want 2026-03-15", got)
	}
}

func TestNextDueDate_CalendarDay(t *testing.T) {
	t.Parallel()
	completedAt := mustDate(t, "2026-05-20")
	// Calendar is measured from currentDue, not completedAt.
	got := NextDueDate("2026-05-01", Recurrence{Type: "calendar", Every: 3, Unit: "day"}, completedAt)
	if got != "2026-05-04" {
		t.Errorf("got %q, want 2026-05-04 (currentDue+3d)", got)
	}
}

func TestNextDueDate_CalendarWeek(t *testing.T) {
	t.Parallel()
	got := NextDueDate("2026-01-05", Recurrence{Type: "calendar", Every: 2, Unit: "week"}, mustDate(t, "2026-01-10"))
	if got != "2026-01-19" {
		t.Errorf("got %q, want 2026-01-19 (5th + 14d)", got)
	}
}

func TestNextDueDate_CalendarMonth(t *testing.T) {
	t.Parallel()
	got := NextDueDate("2026-01-15", Recurrence{Type: "calendar", Every: 1, Unit: "month"}, mustDate(t, "2026-01-16"))
	if got != "2026-02-15" {
		t.Errorf("got %q, want 2026-02-15", got)
	}
}

func TestNextDueDate_CalendarYear(t *testing.T) {
	t.Parallel()
	got := NextDueDate("2026-03-01", Recurrence{Type: "calendar", Every: 1, Unit: "year"}, mustDate(t, "2026-03-05"))
	if got != "2027-03-01" {
		t.Errorf("got %q, want 2027-03-01", got)
	}
}

func TestNextDueDate_MonthEdgeCase_Jan31PlusOneMonth(t *testing.T) {
	t.Parallel()
	// Go's AddDate normalises overflow: Jan 31 + 1 month = Mar 3 in a
	// non-leap year (Feb has 28 days → 31−28 = 3 overflow into March).
	// Pin the observed behaviour; if it ever changes, this test alerts.
	got := NextDueDate("2026-01-31", Recurrence{Type: "calendar", Every: 1, Unit: "month"}, mustDate(t, "2026-02-01"))
	if got != "2026-03-03" {
		t.Errorf("Jan 31 + 1 month = %q, want 2026-03-03 (AddDate normalization)", got)
	}
}

func TestNextDueDate_LeapDayPlusYear(t *testing.T) {
	t.Parallel()
	// Feb 29 2024 + 1 year → Mar 1 2025 (AddDate normalization: Feb 29 in
	// non-leap year → March 1).
	got := NextDueDate("2024-02-29", Recurrence{Type: "calendar", Every: 1, Unit: "year"}, mustDate(t, "2024-03-01"))
	if got != "2025-03-01" {
		t.Errorf("Feb 29 leap + 1yr = %q, want 2025-03-01", got)
	}
}

func TestNextDueDate_ZeroRecurrence(t *testing.T) {
	t.Parallel()
	if got := NextDueDate("2026-01-01", Recurrence{}, mustDate(t, "2026-01-02")); got != "" {
		t.Errorf("zero recurrence should return empty, got %q", got)
	}
}

func TestNextDueDate_CalendarWithInvalidCurrentDue(t *testing.T) {
	t.Parallel()
	// When currentDue is malformed, the function falls back to completedAt.
	got := NextDueDate("not-a-date", Recurrence{Type: "calendar", Every: 3, Unit: "day"}, mustDate(t, "2026-06-10"))
	if got != "2026-06-13" {
		t.Errorf("invalid currentDue fallback = %q, want 2026-06-13", got)
	}
}

func TestNextDueDate_UnknownUnit(t *testing.T) {
	t.Parallel()
	// advanceDate falls through when unit is unrecognised, returning the base date unchanged.
	got := NextDueDate("2026-01-01", Recurrence{Type: "calendar", Every: 5, Unit: "fortnight"}, mustDate(t, "2026-01-02"))
	if got != "2026-01-01" {
		t.Errorf("unknown unit should return base unchanged, got %q", got)
	}
}

func TestNextDueDate_UnknownType(t *testing.T) {
	t.Parallel()
	// Neither "delay" nor "calendar" → empty.
	if got := NextDueDate("2026-01-01", Recurrence{Type: "mystery", Days: 3}, mustDate(t, "2026-01-02")); got != "" {
		t.Errorf("unknown type should return empty, got %q", got)
	}
}

func TestSpawnNext_UsesOriginalGroupID(t *testing.T) {
	t.Parallel()
	orig := &Task{
		ID:                "task-1",
		Title:             "Weekly review",
		Description:       "Review the queue",
		Source:            SourceWikiNode,
		SourcePage:        "/team/reviews",
		NodeKey:           "abc",
		Assignee:          Assignee{Type: "user", Target: "alice", Resolution: "any"},
		DueDate:           "2026-01-15",
		Recurrence:        Recurrence{Type: "calendar", Every: 1, Unit: "week"},
		WikiAction:        WikiAction{Type: "read", Page: "/team/reviews"},
		Tags:              "recurring",
		Priority:          PriorityHigh,
		CreatedBy:         "system",
		RecurrenceGroupID: "group-xyz",
	}
	req := SpawnNext(orig, mustDate(t, "2026-01-20"))
	if req.RecurrenceGroupID != "group-xyz" {
		t.Errorf("group id should be preserved, got %q", req.RecurrenceGroupID)
	}
	if req.DueDate != "2026-01-22" {
		t.Errorf("next due = %q, want 2026-01-22 (currentDue+7d)", req.DueDate)
	}
	// Every other field is copied from the original — verify a few.
	if req.Title != orig.Title || req.Assignee != orig.Assignee ||
		req.WikiAction != orig.WikiAction || req.Priority != orig.Priority {
		t.Errorf("spawned request should copy fields verbatim: %+v", req)
	}
}

func TestSpawnNext_UsesOriginalIDAsFallbackGroup(t *testing.T) {
	t.Parallel()
	orig := &Task{
		ID:         "task-2",
		Title:      "Daily",
		Recurrence: Recurrence{Type: "delay", Days: 1},
	}
	req := SpawnNext(orig, mustDate(t, "2026-06-10"))
	if req.RecurrenceGroupID != "task-2" {
		t.Errorf("fallback group id should be original ID, got %q", req.RecurrenceGroupID)
	}
	if req.DueDate != "2026-06-11" {
		t.Errorf("delay-1 from 2026-06-10 = %q, want 2026-06-11", req.DueDate)
	}
}
