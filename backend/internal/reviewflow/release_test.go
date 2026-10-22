package reviewflow

import (
	"errors"
	"testing"
)

const manualDoc = "{reviewflow version=1.0 manual_release=yes author=alice reviewer=bob}\n\n# Interview\n"
const manualDocV2 = "{reviewflow version=1.0 manual_release=yes author=alice reviewer=bob}\n\n# Interview\n\nmore notes\n"

func TestParseDirective_ManualReleaseIsNotARole(t *testing.T) {
	t.Parallel()
	d, ok := ParseDirective(manualDoc)
	if !ok {
		t.Fatal("directive not found")
	}
	if !d.ManualRelease {
		t.Error("ManualRelease = false, want true")
	}
	if _, isRole := d.Roles["manual_release"]; isRole {
		t.Error("manual_release was parsed as a role")
	}
	if len(d.RoleOrder) != 2 || d.RoleOrder[0] != "author" {
		t.Errorf("RoleOrder = %v, want [author reviewer]", d.RoleOrder)
	}
}

func TestManualRelease_NoTaskUntilReleased(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	if err := svc.SyncFromMarkdown("/hr/interview", 1, manualDoc); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(spy.creates) != 0 {
		t.Errorf("unreleased page created review tasks: %+v", spy.creates)
	}
	st, _ := svc.GetStatus("/hr/interview")
	if !st.ManualRelease || st.Released {
		t.Errorf("status manual=%v released=%v, want true/false", st.ManualRelease, st.Released)
	}
	if len(st.NextRoles) != 0 {
		t.Errorf("NextRoles = %v while awaiting release, want empty", st.NextRoles)
	}
}

func TestManualRelease_ConfirmRefusedBeforeRelease(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/hr/interview", 1, manualDoc)
	if _, err := svc.Confirm("/hr/interview", "author", "alice", nil); !errors.Is(err, ErrNotReleased) {
		t.Errorf("Confirm before release: err = %v, want ErrNotReleased", err)
	}
}

func TestManualRelease_FirstSignerReleasesSerially(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/hr/interview", 1, manualDoc)

	st, err := svc.SetReleased("/hr/interview", "alice", true, false)
	if err != nil {
		t.Fatalf("SetReleased by first signer: %v", err)
	}
	if !st.Released || st.ReleasedBy != "alice" || st.ReleasedAt == nil {
		t.Errorf("status after release = released:%v by:%q at:%v", st.Released, st.ReleasedBy, st.ReleasedAt)
	}
	if len(spy.ensures) != 1 {
		t.Fatalf("want one EnsureReviewTasks call, got %d", len(spy.ensures))
	}
	want := spy.ensures[0].want
	if len(want) != 1 || want["author"] != "alice" {
		t.Errorf("released serial page should task only the first signer, got %v", want)
	}
	if len(st.NextRoles) != 1 || st.NextRoles[0] != "author" {
		t.Errorf("NextRoles = %v, want [author]", st.NextRoles)
	}
}

func TestManualRelease_Permissions(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/hr/interview", 1, manualDoc)

	if _, err := svc.SetReleased("/hr/interview", "bob", true, false); !errors.Is(err, ErrReleaseForbidden) {
		t.Errorf("second signer without edit rights: err = %v, want ErrReleaseForbidden", err)
	}
	if _, err := svc.SetReleased("/hr/interview", "carol", true, true); err != nil {
		t.Errorf("non-signer with edit rights: err = %v, want nil", err)
	}
}

func TestManualRelease_WithdrawOnlyBeforeFirstSignature(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/hr/interview", 1, manualDoc)

	// Release then withdraw with no signature: allowed, open tasks
	// are reconciled to none.
	_, _ = svc.SetReleased("/hr/interview", "alice", true, false)
	spy.ensures = nil
	st, err := svc.SetReleased("/hr/interview", "alice", false, false)
	if err != nil {
		t.Fatalf("withdraw before signature: %v", err)
	}
	if st.Released || st.ReleasedBy != "" {
		t.Errorf("after withdraw released=%v by=%q, want false/empty", st.Released, st.ReleasedBy)
	}
	if len(spy.ensures) != 1 || len(spy.ensures[0].want) != 0 {
		t.Errorf("withdraw should reconcile to no tasks, got %+v", spy.ensures)
	}

	// Release again, sign, then withdraw: refused.
	_, _ = svc.SetReleased("/hr/interview", "alice", true, false)
	if _, err := svc.Confirm("/hr/interview", "author", "alice", nil); err != nil {
		t.Fatalf("Confirm after release: %v", err)
	}
	if _, err := svc.SetReleased("/hr/interview", "alice", false, false); !errors.Is(err, ErrReleaseLocked) {
		t.Errorf("withdraw after signature: err = %v, want ErrReleaseLocked", err)
	}
}

func TestManualRelease_SurvivesContentEdit(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/hr/interview", 1, manualDoc)
	_, _ = svc.SetReleased("/hr/interview", "alice", true, false)
	spy.creates = nil

	if err := svc.SyncFromMarkdown("/hr/interview", 2, manualDocV2); err != nil {
		t.Fatalf("sync v2: %v", err)
	}
	st, _ := svc.GetStatus("/hr/interview")
	if !st.Released {
		t.Error("release was lost on a content edit")
	}
	if len(spy.creates) != 1 || spy.creates[0].roles["author"] != "alice" {
		t.Errorf("released page edit should re-task the first signer, got %+v", spy.creates)
	}
}

func TestManualRelease_DroppingOptionClearsRelease(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/hr/interview", 1, manualDoc)
	_, _ = svc.SetReleased("/hr/interview", "alice", true, false)

	plain := "{reviewflow version=1.0 author=alice reviewer=bob}\n\n# Interview\n"
	_ = svc.SyncFromMarkdown("/hr/interview", 2, plain)
	st, _ := svc.GetStatus("/hr/interview")
	if st.ManualRelease || st.Released {
		t.Errorf("after dropping manual_release: manual=%v released=%v, want false/false", st.ManualRelease, st.Released)
	}
	if _, err := svc.SetReleased("/hr/interview", "alice", true, false); !errors.Is(err, ErrReleaseNotEnabled) {
		t.Errorf("SetReleased on a non-manual page: err = %v, want ErrReleaseNotEnabled", err)
	}
}

func TestManualRelease_ReconcilerKeepsUnreleasedPageQuiet(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/hr/interview", 1, manualDoc)
	spy.ensures = nil

	_, _ = svc.ReconcileOrphanTasks(func(string) bool { return true })
	if len(spy.ensures) != 1 || len(spy.ensures[0].want) != 0 {
		t.Errorf("reconciler on unreleased page should want no tasks, got %+v", spy.ensures)
	}
}

// Default (no manual_release) pages keep today's serial behaviour: the
// first save tasks only the first role.
func TestSerial_DefaultPageTasksFirstRoleOnly(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/sop", 1, "{reviewflow version=1.0 author=alice reviewer=bob validator=carol}\n")
	if len(spy.creates) != 1 {
		t.Fatalf("want one create call, got %+v", spy.creates)
	}
	if len(spy.creates[0].roles) != 1 || spy.creates[0].roles["author"] != "alice" {
		t.Errorf("serial page should task only the first role, got %v", spy.creates[0].roles)
	}
}
