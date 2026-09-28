package lifecycle

import (
	"testing"
	"time"
)

func TestEvaluate_Stale_FreshPageDoesNotFire(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fresh := now.Add(-10 * 24 * time.Hour) // 10 days ago
	r := Rule{Condition: Condition{Kind: "stale", Duration: 30 * 24 * time.Hour}}
	v := Evaluate(r, "/x", now, Probes{}, func(string) time.Time { return fresh })
	if v.Fires {
		t.Error("page edited 10 days ago should not fire a stale:30d rule")
	}
	if !v.LastAttested.Equal(fresh) {
		t.Errorf("LastAttested lost: %v", v.LastAttested)
	}
}

func TestEvaluate_Stale_OldPageFires(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := now.Add(-60 * 24 * time.Hour) // 60 days ago
	r := Rule{Condition: Condition{Kind: "stale", Duration: 30 * 24 * time.Hour}}
	v := Evaluate(r, "/x", now, Probes{}, func(string) time.Time { return old })
	if !v.Fires {
		t.Error("page edited 60 days ago should fire a stale:30d rule")
	}
}

func TestEvaluate_Stale_UsesMaxAcrossSources(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Page's content was edited long ago (would be stale on its own)
	// but reviewflow validated it recently — max wins, no fire.
	pageEdit := func(string) time.Time { return now.Add(-1000 * 24 * time.Hour) }
	reviewflow := func(string) time.Time { return now.Add(-5 * 24 * time.Hour) }
	r := Rule{Condition: Condition{Kind: "stale", Duration: 30 * 24 * time.Hour}}
	v := Evaluate(r, "/x", now, Probes{}, pageEdit, reviewflow)
	if v.Fires {
		t.Error("recent reviewflow validation should refresh the page — must not fire")
	}
	if !v.LastAttested.Equal(now.Add(-5 * 24 * time.Hour)) {
		t.Errorf("LastAttested should be the newer of the two, got %v", v.LastAttested)
	}
}

func TestEvaluate_Stale_ZeroAttestationFires(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Page has no attestation from any source — the fully-unattested
	// case. Should fire so the rule surfaces the gap.
	r := Rule{Condition: Condition{Kind: "stale", Duration: 30 * 24 * time.Hour}}
	v := Evaluate(r, "/x", now, Probes{}, func(string) time.Time { return time.Time{} })
	if !v.Fires {
		t.Error("zero-attestation page should fire immediately")
	}
	if !v.LastAttested.IsZero() {
		t.Errorf("LastAttested should be zero: %v", v.LastAttested)
	}
}

func TestEvaluate_UnknownConditionIsSafe(t *testing.T) {
	t.Parallel()
	// A corrupt-on-disk rule with an unknown condition kind must NOT
	// panic the scanner — refuse silently.
	r := Rule{Condition: Condition{Kind: "does-not-exist"}}
	v := Evaluate(r, "/x", time.Now(), Probes{}, func(string) time.Time { return time.Now() })
	if v.Fires {
		t.Error("unknown condition kind must not fire")
	}
}

func TestEvaluate_ReviewflowOverdue_FiresWhenAnyRoleOverdue(t *testing.T) {
	t.Parallel()
	overdue := func(pagePath string) []string {
		if pagePath == "/late" {
			return []string{"reviewer"}
		}
		return nil
	}
	r := Rule{Condition: Condition{Kind: "reviewflow_overdue"}}
	probes := Probes{Overdue: overdue}
	if !Evaluate(r, "/late", time.Now(), probes).Fires {
		t.Error("expected fire on page with overdue reviewer")
	}
	if Evaluate(r, "/ok", time.Now(), probes).Fires {
		t.Error("expected no fire on page with no overdue roles")
	}
}

func TestEvaluate_ReviewflowOverdue_NilProbeIsSafe(t *testing.T) {
	t.Parallel()
	// A wiki without reviewflow wired should just not fire the rule —
	// never panic. Same defence-in-depth as the unknown-kind case.
	r := Rule{Condition: Condition{Kind: "reviewflow_overdue"}}
	if Evaluate(r, "/x", time.Now(), Probes{}).Fires {
		t.Error("nil overdue probe must yield no fire, not a panic")
	}
}

func TestEvaluate_CommentsOpen_FiresWhenThreadsOpen(t *testing.T) {
	t.Parallel()
	openComments := func(pagePath string) int {
		if pagePath == "/noisy" {
			return 3
		}
		return 0
	}
	r := Rule{Condition: Condition{Kind: "comments_open"}}
	probes := Probes{OpenComments: openComments}
	if !Evaluate(r, "/noisy", time.Now(), probes).Fires {
		t.Error("expected fire on page with 3 open comments")
	}
	if Evaluate(r, "/quiet", time.Now(), probes).Fires {
		t.Error("expected no fire on page with zero open comments")
	}
}

func TestEvaluate_CommentsOpen_NilProbeIsSafe(t *testing.T) {
	t.Parallel()
	r := Rule{Condition: Condition{Kind: "comments_open"}}
	if Evaluate(r, "/x", time.Now(), Probes{}).Fires {
		t.Error("nil OpenComments probe must yield no fire")
	}
}

func TestLastAttestation_MaxNonZero(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got := LastAttestation("/x",
		func(string) time.Time { return t1 },
		func(string) time.Time { return t2 },
		nil,
		func(string) time.Time { return time.Time{} },
	)
	if !got.Equal(t2) {
		t.Errorf("expected %v, got %v", t2, got)
	}
}
