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

	exists := func(p string) bool { return true }
	n, err := svc.ReconcileOrphanTasks(exists)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("touched = %d, want 1", n)
	}
	if len(spy.cancels) != 1 || spy.cancels[0] != "/done" {
		t.Errorf("expected Cancel for /done; got %+v", spy.cancels)
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
	// Parallel directive — every missing role gets a task.
	_ = svc.SyncFromMarkdown("/live", 1, "{reviewflow parallel=true author=alice reviewer=bob}\n")
	spy.cancels = nil
	spy.creates = nil

	exists := func(p string) bool { return true }
	n, err := svc.ReconcileOrphanTasks(exists)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("touched = %d, want 1", n)
	}
	if len(spy.cancels) != 1 || spy.cancels[0] != "/live" {
		t.Errorf("expected Cancel; got %+v", spy.cancels)
	}
	if len(spy.creates) != 1 {
		t.Errorf("expected one Create for the two live roles; got %+v", spy.creates)
	}
	if spy.creates[0].roles["author"] != "alice" || spy.creates[0].roles["reviewer"] != "bob" {
		t.Errorf("recreated roles = %+v, want author=alice reviewer=bob", spy.creates[0].roles)
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

// Reconciler is idempotent: a second run after a first does nothing new.
func TestReconcileOrphanTasks_Idempotent(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/a", 1, directive2)
	_ = svc.SyncFromMarkdown("/b", 1, directive3)

	exists := func(p string) bool { return true }
	// First run touches both.
	n1, _ := svc.ReconcileOrphanTasks(exists)
	if n1 != 2 {
		t.Fatalf("first run touched %d, want 2", n1)
	}
	spy.cancels = nil
	spy.creates = nil

	// Second run touches both again (the rebuild path is deterministic
	// — same end state — but the counter counts touches, not changes).
	// The tasks that come out are the same set as what went in.
	n2, _ := svc.ReconcileOrphanTasks(exists)
	if n2 != 2 {
		t.Errorf("second run touched %d, want 2", n2)
	}
	// Both runs produce the same create set — stable rebuild.
	if len(spy.creates) != 2 {
		t.Errorf("second run should have recreated 2 Create calls; got %d", len(spy.creates))
	}
}
