package reviewflow

import (
	"testing"
)

// ── OnPageDelete ─────────────────────────────────────────────────────

// Deleting a page must cancel its open review tasks AND remove the
// state file — the two surfaces where reviewflow outlives the page.
func TestOnPageDelete_CancelsTasksAndRemovesState(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	if err := svc.SyncFromMarkdown("/doc", 1, directive2); err != nil {
		t.Fatalf("sync: %v", err)
	}
	spy.cancels = nil // ignore the setup's cancel

	if err := svc.OnPageDelete("/doc"); err != nil {
		t.Fatalf("OnPageDelete: %v", err)
	}

	if len(spy.cancels) != 1 || spy.cancels[0] != "/doc" {
		t.Errorf("expected one Cancel for /doc, got %+v", spy.cancels)
	}
	if st, _ := svc.store.Load("/doc"); st != nil {
		t.Errorf("state file should be gone after delete, got %+v", st)
	}
}

// ── Version-bump: stale-role cancel ──────────────────────────────────

// Tag bump that swaps a reviewer (bob → alice) must cancel bob's open
// task so it doesn't survive forever. This is the user-reported case
// (sop03 kept a reviewer task on the old tag).
func TestSyncFromMarkdown_RoleReassignment_CancelsStaleTasks(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, "{reviewflow version=2.1 reviewer=bob}\n")
	spy.cancels = nil
	spy.creates = nil

	// Content changes AND the directive now assigns a different user.
	if err := svc.SyncFromMarkdown("/doc", 2, "{reviewflow version=3.0 reviewer=alice}\n\nbody\n"); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(spy.cancels) != 1 || spy.cancels[0] != "/doc" {
		t.Errorf("expected one Cancel to kill bob's task; got %+v", spy.cancels)
	}
	if len(spy.creates) != 1 || spy.creates[0].roles["reviewer"] != "alice" {
		t.Errorf("expected one Create for reviewer=alice; got %+v", spy.creates)
	}
}

// Same-tag content edit with no re-attach must also cancel + recreate
// (today's behavior; verifies the new code path still handles it).
func TestSyncFromMarkdown_ContentEditNoReattach_CancelsAndRecreates(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, "{reviewflow reviewer=bob}\n")
	spy.cancels = nil
	spy.creates = nil

	if err := svc.SyncFromMarkdown("/doc", 2, "{reviewflow reviewer=bob}\n\nbody\n"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(spy.cancels) != 1 {
		t.Errorf("expected one Cancel on content edit; got %+v", spy.cancels)
	}
	if len(spy.creates) != 1 || spy.creates[0].roles["reviewer"] != "bob" {
		t.Errorf("expected recreate for bob; got %+v", spy.creates)
	}
}

// ── finalize belt+suspenders cancel ──────────────────────────────────

// When a page becomes fully validated, every open review task must go.
// CompleteReviewTasks handles the ones whose (role, user) matches the
// confirmations; CancelReviewTasks catches any leftover (role reassigned
// mid-review, pre-cleanup drift). Belt + suspenders.
func TestFinalize_CancelsLeftoverOpenTasks(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, directive2)
	digestV1 := ComputeDigest([]byte(directive2))
	spy.cancels = nil
	spy.creates = nil
	spy.completes = nil

	// Both roles confirm → finalizeValidatedVersion fires.
	_, _ = svc.Confirm("/doc", "author", "alice", &ConfirmOpts{Signature: "sig-a", Digest: digestV1, CertificatePEM: "PEM-a"})
	_, _ = svc.Confirm("/doc", "reviewer", "bob", &ConfirmOpts{Signature: "sig-b", Digest: digestV1, CertificatePEM: "PEM-b"})

	// Complete fires for both roles (one per Confirm call that completes
	// the set — the second one triggers finalize).
	if len(spy.completes) == 0 {
		t.Fatal("expected CompleteReviewTasks to fire")
	}
	// AND a Cancel from the belt+suspenders path inside finalize.
	foundCancel := false
	for _, c := range spy.cancels {
		if c == "/doc" {
			foundCancel = true
		}
	}
	if !foundCancel {
		t.Errorf("expected finalize to issue Cancel for leftover tasks; got cancels=%v", spy.cancels)
	}
}

// ── ReconcileOrphanTasks ─────────────────────────────────────────────

// A state file for a page that no longer exists must have its tasks
// cancelled AND its state file removed.
func TestReconcileOrphanTasks_DeletedPage(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/gone", 1, directive2)
	spy.cancels = nil

	exists := func(p string) bool { return false } // every page absent
	n, err := svc.ReconcileOrphanTasks(exists)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("touched = %d, want 1", n)
	}
	if len(spy.cancels) != 1 || spy.cancels[0] != "/gone" {
		t.Errorf("expected Cancel for /gone; got %+v", spy.cancels)
	}
	if st, _ := svc.store.Load("/gone"); st != nil {
		t.Errorf("state should be removed; got %+v", st)
	}
}

// A state whose ValidatedVersion matches CurrentPageVersion must have
// all open review tasks cancelled (page is done — no legitimate open
// task remains).
func TestReconcileOrphanTasks_FullyValidatedPage(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	// Seed a validated state directly.
	st := &State{
		Roles:              map[string]string{"author": "alice"},
		VersionTag:         "1.0",
		CurrentPageVersion: 7,
		ValidatedVersion:   7,
	}
	_ = svc.store.Save("/done", st)
	spy.cancels = nil
	spy.ensures = nil
	// One stray open task on the validated page gets cancelled.
	spy.ensureResult = &ensureResult{cancelled: 1}

	exists := func(p string) bool { return true }
	n, err := svc.ReconcileOrphanTasks(exists)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("touched = %d, want 1", n)
	}
	if len(spy.ensures) != 1 || spy.ensures[0].pagePath != "/done" || len(spy.ensures[0].want) != 0 {
		t.Errorf("expected Ensure(/done, no tasks wanted); got %+v", spy.ensures)
	}

	// Nothing left open: the page is not counted again.
	spy.ensureResult = &ensureResult{}
	if n2, _ := svc.ReconcileOrphanTasks(exists); n2 != 0 {
		t.Errorf("second run touched = %d, want 0 (no stray task left)", n2)
	}
	// State file stays (the page is still alive) — only tasks go.
	if st2, _ := svc.store.Load("/done"); st2 == nil {
		t.Errorf("validated-page state should survive reconcile")
	}
}

// A live state (page exists, not fully validated) must get its task set
// rebuilt: cancel all open, create one task per missing role for
// parallel reviews. Sequential reviews are covered by the regular sync
// tests — the reconciler delegates to the same code shape.
func TestReconcileOrphanTasks_LivePageRebuildsTaskSet(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	// Parallel directive — every missing role gets a task via the
	// normal SyncFromMarkdown flow before the reconciler runs.
	_ = svc.SyncFromMarkdown("/live", 1, "{reviewflow parallel=true author=alice reviewer=bob}\n")
	spy.cancels = nil
	spy.creates = nil
	spy.ensures = nil

	exists := func(p string) bool { return true }
	n, err := svc.ReconcileOrphanTasks(exists)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Reconciler now routes through EnsureReviewTasks, which diffs
	// the live task set against the desired set and returns
	// kept=2/cancelled=0/created=0 for an already-correct page. The
	// touched counter counts PAGES WHERE A CHANGE LANDED, so a
	// no-drift page does not increment it — the whole point is
	// stopping an email-emitting rebuild on every restart.
	if n != 0 {
		t.Errorf("touched = %d, want 0 (page is already in the desired shape)", n)
	}
	if len(spy.cancels) != 0 {
		t.Errorf("no Cancel should fire on a no-drift reconcile; got %+v", spy.cancels)
	}
	if len(spy.creates) != 0 {
		t.Errorf("no Create should fire on a no-drift reconcile; got %+v", spy.creates)
	}
	if len(spy.ensures) != 1 || spy.ensures[0].pagePath != "/live" {
		t.Fatalf("expected one Ensure for /live; got %+v", spy.ensures)
	}
	if spy.ensures[0].want["author"] != "alice" || spy.ensures[0].want["reviewer"] != "bob" {
		t.Errorf("Ensure want = %+v, want author=alice reviewer=bob", spy.ensures[0].want)
	}
}

// Drift case: when EnsureReviewTasks reports a change (cancelled >0
// or created >0), touched counts the page. Exercised via the spy's
// canned ensureResult.
func TestReconcileOrphanTasks_DriftTouched(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/drifted", 1, "{reviewflow parallel=true author=alice reviewer=bob}\n")
	spy.cancels = nil
	spy.creates = nil
	spy.ensures = nil
	spy.ensureResult = &ensureResult{kept: 1, cancelled: 0, created: 1}

	exists := func(p string) bool { return true }
	n, _ := svc.ReconcileOrphanTasks(exists)
	if n != 1 {
		t.Errorf("touched = %d, want 1 (page was in drift, Ensure created one task)", n)
	}
}

// Dormant states (directive was removed earlier, roles cleared) are no-ops.
func TestReconcileOrphanTasks_DormantStateUntouched(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	st := &State{CurrentPageVersion: 3} // no Roles, no ValidatedVersion
	_ = svc.store.Save("/dormant", st)
	spy.cancels = nil
	spy.creates = nil

	exists := func(p string) bool { return true }
	n, err := svc.ReconcileOrphanTasks(exists)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 0 {
		t.Errorf("touched = %d, want 0 (dormant should be left alone)", n)
	}
	if len(spy.cancels) != 0 || len(spy.creates) != 0 {
		t.Errorf("dormant state should produce no todo work; cancels=%v creates=%v", spy.cancels, spy.creates)
	}
}

// Reconciler is idempotent: once SyncFromMarkdown has produced the
// desired task set, the reconciler diffs against it and sees no drift
// — zero cancels, zero creates, touched stays 0. Second run is the
// same shape. The invariant that matters most: no "assigned" email
// fires on either run because no new task row is written.
func TestReconcileOrphanTasks_Idempotent(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/a", 1, directive2)
	_ = svc.SyncFromMarkdown("/b", 1, directive3)
	// SyncFromMarkdown emits its own Cancel + Create; wipe the spy so
	// the assertions below measure ONLY what the reconciler does.
	spy.cancels = nil
	spy.creates = nil
	spy.ensures = nil

	exists := func(p string) bool { return true }
	// First run: both pages ARE already in the desired shape, so the
	// reconciler should report zero drift.
	n1, _ := svc.ReconcileOrphanTasks(exists)
	if n1 != 0 {
		t.Fatalf("first run touched %d, want 0 (no drift)", n1)
	}
	if len(spy.cancels) != 0 || len(spy.creates) != 0 {
		t.Errorf("first run should emit no Cancel/Create; got cancels=%v creates=%v", spy.cancels, spy.creates)
	}
	// Both pages did get an Ensure call so the diff could run.
	if len(spy.ensures) != 2 {
		t.Errorf("first run should have one Ensure per page; got %d", len(spy.ensures))
	}
	spy.cancels = nil
	spy.creates = nil
	spy.ensures = nil

	// Second run: same shape, same result.
	n2, _ := svc.ReconcileOrphanTasks(exists)
	if n2 != 0 {
		t.Errorf("second run touched %d, want 0", n2)
	}
	if len(spy.cancels) != 0 || len(spy.creates) != 0 {
		t.Errorf("second run should emit no Cancel/Create; got cancels=%v creates=%v", spy.cancels, spy.creates)
	}
}
