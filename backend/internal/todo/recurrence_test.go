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

// ────────────────────────────────────────────────────────────
// Mode + Tolerance semantics (ships with the mode= / tolerance=
// directive attributes). These lock in the agreed behaviour: default
// mode is "at-least", default tolerance is 10% of the period (floor
// 1 day).
// ────────────────────────────────────────────────────────────

// Default (at-least) behaviour. 1-year cadence; completion 1 day early
// is "within tolerance" (default tol = 36 days for 1y), so the next
// due stays anchored to the original schedule — no drift when the user
// consistently finishes a day or two early.
func TestNextDueDate_AtLeast_WithinTolerance_NoDrift(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2026-12-31"))
	if got != "2028-01-01" {
		t.Errorf("at-least within tolerance = %q, want 2028-01-01 (no drift)", got)
	}
}

// at-least + 10%-of-1y (= 36 days). Completion 7 months early is
// clearly outside tolerance → next re-anchors to completedAt + N.
func TestNextDueDate_AtLeast_OutsideTolerance_Reanchors(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2026-06-01"))
	if got != "2027-06-01" {
		t.Errorf("at-least outside tolerance = %q, want 2027-06-01 (re-anchor to completedAt+1y)", got)
	}
}

// at-least late: lateness never shifts the cadence (that's the
// cadence promise). currentDue + N regardless of how late.
func TestNextDueDate_AtLeast_Late_StaysAnchored(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2027-04-01"))
	if got != "2028-01-01" {
		t.Errorf("at-least late = %q, want 2028-01-01", got)
	}
}

// Fixed within tolerance behaves exactly like at-least within
// tolerance: the completion satisfies the cycle, next = currentDue + N.
func TestNextDueDate_Fixed_WithinTolerance_AdvancesLikeNormal(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year", Mode: "fixed"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2026-12-31"))
	if got != "2028-01-01" {
		t.Errorf("fixed within tolerance = %q, want 2028-01-01", got)
	}
}

// Fixed outside tolerance is the semantic that triggered this feature:
// the completion is recorded, but the schedule is NOT satisfied — the
// next instance keeps the original due date, so the user still gets
// the alert on 2027-01-01.
func TestNextDueDate_Fixed_OutsideTolerance_KeepsOriginalDue(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year", Mode: "fixed"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2026-06-01"))
	if got != "2027-01-01" {
		t.Errorf("fixed outside tolerance = %q, want 2027-01-01 (schedule unchanged)", got)
	}
}

// Fixed + late: cycle is satisfied, advance as usual.
func TestNextDueDate_Fixed_Late_Advances(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year", Mode: "fixed"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2027-04-01"))
	if got != "2028-01-01" {
		t.Errorf("fixed late = %q, want 2028-01-01", got)
	}
}

// Custom tolerance as absolute days overrides the default.
// 1y cadence with tolerance=60d: 2026-11-15 is 47 days early, within
// tolerance → cycle satisfied, next = currentDue + N.
func TestNextDueDate_Fixed_CustomToleranceDays(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year", Mode: "fixed", Tolerance: "60d"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2026-11-15"))
	if got != "2028-01-01" {
		t.Errorf("tolerance=60d / 47 days early = %q, want 2028-01-01", got)
	}
}

// Same as above but completion is just outside the 60-day window
// (62 days early → outside). Fixed → schedule unchanged.
func TestNextDueDate_Fixed_CustomTolerance_JustOutside(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year", Mode: "fixed", Tolerance: "60d"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2026-10-31"))
	if got != "2027-01-01" {
		t.Errorf("tolerance=60d / 62 days early = %q, want 2027-01-01", got)
	}
}

// Strict fixed (tolerance=0d): any day early is outside tolerance.
func TestNextDueDate_Fixed_StrictZeroTolerance(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "year", Mode: "fixed", Tolerance: "0d"}
	got := NextDueDate("2027-01-01", r, mustDate(t, "2026-12-31"))
	if got != "2027-01-01" {
		t.Errorf("strict fixed 1 day early = %q, want 2027-01-01 (not satisfied)", got)
	}
}

// Custom tolerance as percentage.
// 1-month cadence (30 days). tolerance=50% → 15 days. Completion
// 10 days early is within tolerance → advance.
func TestNextDueDate_AtLeast_PercentTolerance(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "month", Tolerance: "50%"}
	got := NextDueDate("2027-02-15", r, mustDate(t, "2027-02-05"))
	if got != "2027-03-15" {
		t.Errorf("tolerance=50%% / 10 days early = %q, want 2027-03-15", got)
	}
}

// Tolerance floor: a 1-day cadence × default 10% = 0.1 days, but the
// floor is 1 day. A 1-day-early completion must count as "within
// tolerance".
func TestNextDueDate_AtLeast_DailyTolerance_FloorsAtOneDay(t *testing.T) {
	t.Parallel()
	r := Recurrence{Type: "calendar", Every: 1, Unit: "day"}
	got := NextDueDate("2027-02-15", r, mustDate(t, "2027-02-14"))
	if got != "2027-02-16" {
		t.Errorf("daily cadence 1 day early = %q, want 2027-02-16 (floor tolerance = 1d)", got)
	}
}

// ToleranceDays helper: direct unit test for the parsing table.
func TestToleranceDays(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		r    Recurrence
		want int
	}{
		{"default 10% of 1y", Recurrence{Type: "calendar", Every: 1, Unit: "year"}, 36},
		{"default 10% of 3m", Recurrence{Type: "calendar", Every: 3, Unit: "month"}, 9},
		{"default 10% of 1w → floor 1d", Recurrence{Type: "calendar", Every: 1, Unit: "week"}, 1},
		{"explicit 30d", Recurrence{Type: "calendar", Every: 1, Unit: "year", Tolerance: "30d"}, 30},
		{"explicit 0d strict", Recurrence{Type: "calendar", Every: 1, Unit: "year", Tolerance: "0d"}, 0},
		{"explicit 25%", Recurrence{Type: "calendar", Every: 1, Unit: "year", Tolerance: "25%"}, 91},
		{"explicit 0% strict", Recurrence{Type: "calendar", Every: 1, Unit: "year", Tolerance: "0%"}, 0},
		{"clamped: 500% of 1y → 365 (= period)", Recurrence{Type: "calendar", Every: 1, Unit: "year", Tolerance: "500%"}, 365},
		{"unparseable → default", Recurrence{Type: "calendar", Every: 1, Unit: "year", Tolerance: "garbage"}, 36},
		{"delay type → 0", Recurrence{Type: "delay", Days: 7, Tolerance: "30d"}, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.r.ToleranceDays(); got != tc.want {
				t.Errorf("ToleranceDays() = %d, want %d", got, tc.want)
			}
		})
	}
}
