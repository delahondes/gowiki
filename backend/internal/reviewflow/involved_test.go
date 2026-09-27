package reviewflow

import "testing"

// IsInvolved answers the draft-visibility question: does this user
// have any stake in the review of this document? Role assignees
// (author/reviewer/validator/custom), group-scoped assignees, and
// global observers all count. Everyone else is uninvolved.

func TestIsInvolved_EmptyUsername(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, "{reviewflow author=alice reviewer=bob}\n")
	if svc.IsInvolved("/doc", "", nil) {
		t.Error("empty username must never be involved")
	}
}

func TestIsInvolved_RoleAssigneeByName(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, "{reviewflow author=alice reviewer=bob validator=cathy}\n")
	for _, u := range []string{"alice", "bob", "cathy"} {
		if !svc.IsInvolved("/doc", u, nil) {
			t.Errorf("%s should be involved (assigned to a role)", u)
		}
	}
	if svc.IsInvolved("/doc", "dave", nil) {
		t.Error("dave holds no role and should not be involved")
	}
}

func TestIsInvolved_GroupAssignee(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	// Group syntax on the assignee side is "@groupname" — matches the
	// group-resolver convention reviewflow uses elsewhere.
	_ = svc.SyncFromMarkdown("/doc", 1, "{reviewflow author=alice reviewer=@quality}\n")
	if !svc.IsInvolved("/doc", "carol", []string{"quality"}) {
		t.Error("member of @quality should be involved via group assignment")
	}
	if svc.IsInvolved("/doc", "carol", []string{"finance"}) {
		t.Error("non-member should not be involved via group assignment")
	}
}

func TestIsInvolved_GlobalObserver(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, "{reviewflow author=alice reviewer=bob}\n")
	// Observer registered globally in the config.
	cfg := svc.configStore.Get()
	cfg.Reviewflow.Observers = []string{"oscar"}
	svc.configStore.Update(cfg)
	if !svc.IsInvolved("/doc", "oscar", nil) {
		t.Error("global observer must count as involved on ANY page")
	}
	// Even a page with no reviewflow at all: observer still sees it.
	if !svc.IsInvolved("/no-reviewflow-here", "oscar", nil) {
		t.Error("global observer stays involved even when the page has no reviewflow state")
	}
}

func TestIsInvolved_NoReviewflowStateAndNotObserver(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	// No page written; nobody involved.
	if svc.IsInvolved("/nowhere", "alice", nil) {
		t.Error("no reviewflow state → not involved (unless observer, which alice is not)")
	}
}

// The gating knob must be OFF by default — the pre-existing behaviour
// (all readers see everything) is preserved on upgrade. Regulated
// installations opt IN by setting the flag in their config file.
func TestConfig_HideDraftsFromUninvolved_DefaultsOff(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	cfg := svc.configStore.Get()
	if cfg.Reviewflow.HideDraftsFromUninvolved {
		t.Error("HideDraftsFromUninvolved must default to false (opt-in), got true")
	}
}
