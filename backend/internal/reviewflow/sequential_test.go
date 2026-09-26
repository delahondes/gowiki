package reviewflow

import (
	"testing"
)

// Sequential notification is the default: when a page with a
// {reviewflow} directive is saved, only the FIRST role in source
// order gets its review todo task created. After each Confirm, the
// next unconfirmed role gets its task created. Legacy parallel
// behaviour is opt-in via `parallel=true`.

const directive3Sequential = "{reviewflow author=alice reviewer=bob validator=cathy}\n"
const directive3Parallel = "{reviewflow author=alice reviewer=bob validator=cathy parallel=true}\n"

func TestSequential_FirstSaveCreatesFirstRoleOnly(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	if err := svc.SyncFromMarkdown("/doc", 1, directive3Sequential); err != nil {
		t.Fatalf("SyncFromMarkdown: %v", err)
	}

	if len(spy.creates) != 1 {
		t.Fatalf("expected 1 CreateReviewTasks, got %d", len(spy.creates))
	}
	created := spy.creates[0].roles
	if len(created) != 1 {
		t.Errorf("expected exactly ONE role in the create call (sequential), got %d: %+v", len(created), created)
	}
	if created["author"] != "alice" {
		t.Errorf("first role should be author=alice, got %+v", created)
	}
	if _, present := created["reviewer"]; present {
		t.Errorf("reviewer must NOT be notified until author confirms")
	}
	if _, present := created["validator"]; present {
		t.Errorf("validator must NOT be notified until reviewer confirms")
	}
}

func TestSequential_ConfirmAdvancesToNextRole(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, directive3Sequential)
	spy.creates = nil // ignore the initial create

	// Alice (author) confirms.
	if _, err := svc.Confirm("/doc", "author", "alice", nil); err != nil {
		t.Fatalf("Confirm author: %v", err)
	}

	// Reviewer's task MUST now be created — nobody else.
	if len(spy.creates) != 1 {
		t.Fatalf("expected 1 CreateReviewTasks after author confirms, got %d", len(spy.creates))
	}
	created := spy.creates[0].roles
	if len(created) != 1 || created["reviewer"] != "bob" {
		t.Errorf("expected the next task to be reviewer=bob only, got %+v", created)
	}
	spy.creates = nil

	// Bob (reviewer) confirms.
	if _, err := svc.Confirm("/doc", "reviewer", "bob", nil); err != nil {
		t.Fatalf("Confirm reviewer: %v", err)
	}
	if len(spy.creates) != 1 || spy.creates[0].roles["validator"] != "cathy" {
		t.Errorf("expected the next task to be validator=cathy only, got %+v", spy.creates)
	}
	spy.creates = nil

	// Cathy (validator) confirms — every role now confirmed, no more
	// tasks should be created.
	if _, err := svc.Confirm("/doc", "validator", "cathy", nil); err != nil {
		t.Fatalf("Confirm validator: %v", err)
	}
	if len(spy.creates) != 0 {
		t.Errorf("no more tasks after every role confirms, got %+v", spy.creates)
	}

	// The version should now be fully validated.
	st, _ := svc.store.Load("/doc")
	if st.ValidatedVersion != 1 {
		t.Errorf("ValidatedVersion = %d, want 1", st.ValidatedVersion)
	}
	if len(st.VersionHistory) != 1 {
		t.Errorf("VersionHistory should have 1 entry, got %d", len(st.VersionHistory))
	}
}

func TestParallel_FirstSaveCreatesEveryRoleAtOnce(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	if err := svc.SyncFromMarkdown("/doc", 1, directive3Parallel); err != nil {
		t.Fatalf("SyncFromMarkdown: %v", err)
	}

	if len(spy.creates) != 1 {
		t.Fatalf("expected 1 CreateReviewTasks, got %d", len(spy.creates))
	}
	created := spy.creates[0].roles
	if len(created) != 3 {
		t.Errorf("parallel: expected all THREE roles in one call, got %d: %+v", len(created), created)
	}
	if created["author"] != "alice" || created["reviewer"] != "bob" || created["validator"] != "cathy" {
		t.Errorf("parallel roles = %+v", created)
	}
}

func TestParallel_ConfirmDoesNotSpawnNewTasks(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, directive3Parallel)
	spy.creates = nil

	// In parallel mode, confirming a role should NOT trigger another
	// CreateReviewTasks — everyone already has their task.
	if _, err := svc.Confirm("/doc", "author", "alice", nil); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if len(spy.creates) != 0 {
		t.Errorf("parallel mode should not create more tasks on confirm, got %+v", spy.creates)
	}
}

func TestSequential_VersionBumpRestartsFromFirstRole(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	// v1: author + reviewer confirm.
	_ = svc.SyncFromMarkdown("/doc", 1, directive3Sequential)
	_, _ = svc.Confirm("/doc", "author", "alice", nil)
	_, _ = svc.Confirm("/doc", "reviewer", "bob", nil)

	spy.creates = nil
	spy.cancels = nil

	// v2: content changed, all prior confirmations invalidated. The
	// sequential chain must restart at the FIRST role, not resume at
	// wherever it left off in v1.
	if err := svc.SyncFromMarkdown("/doc", 2, directive3Sequential); err != nil {
		t.Fatal(err)
	}
	if len(spy.cancels) != 1 {
		t.Errorf("expected cancel on version bump, got %+v", spy.cancels)
	}
	if len(spy.creates) != 1 {
		t.Fatalf("expected 1 create after version bump, got %d", len(spy.creates))
	}
	created := spy.creates[0].roles
	if len(created) != 1 || created["author"] != "alice" {
		t.Errorf("v2 sequential should restart at author=alice only, got %+v", created)
	}
}

func TestSequential_ChainSurvivesReviewflowStoreRoundTrip(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, directive3Sequential)
	_, _ = svc.Confirm("/doc", "author", "alice", nil)

	// Reload the state from disk — RoleOrder + Parallel must persist so
	// the next Confirm still knows where to advance.
	st, _ := svc.store.Load("/doc")
	if len(st.RoleOrder) != 3 || st.RoleOrder[0] != "author" {
		t.Errorf("RoleOrder not persisted: %+v", st.RoleOrder)
	}
	if st.Parallel {
		t.Errorf("Parallel should be false on disk")
	}

	spy.creates = nil
	// Confirm reviewer using the reloaded state — validator's task
	// should still be created.
	if _, err := svc.Confirm("/doc", "reviewer", "bob", nil); err != nil {
		t.Fatal(err)
	}
	if len(spy.creates) != 1 || spy.creates[0].roles["validator"] != "cathy" {
		t.Errorf("expected validator's task after reviewer confirms, got %+v", spy.creates)
	}
}
