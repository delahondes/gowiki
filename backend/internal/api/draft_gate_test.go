package api

import (
	"testing"

	"gowiki/backend/internal/config"
	"gowiki/backend/internal/reviewflow"
)

// draftGate is the pure decision function behind
// hide_drafts_from_uninvolved. Rather than assemble a full Server
// with reviewflow + attic + config + user store just to prove the
// policy, we test the decision here and rely on the caller wiring
// (a two-line adapter in handleGetPage) staying trivial.

// status builds a Status that looks like a real reviewflow: the
// Roles map is non-empty so the "no-reviewflow-directive" short
// circuit doesn't fire. Tests that want to prove the short-circuit
// pass an empty-Roles status directly.
func status(fullyValidated bool, validatedVersion int64) *reviewflow.Status {
	return &reviewflow.Status{
		Roles:            map[string]string{"author": "alice", "reviewer": "bob"},
		IsFullyValidated: fullyValidated,
		ValidatedVersion: validatedVersion,
	}
}

func TestDraftGate_FeatureOff_NoAction(t *testing.T) {
	t.Parallel()
	cfg := config.ReviewflowConfig{HideDraftsFromUninvolved: false}
	// Feature off overrides everything else — even an obvious draft
	// case leaks nothing extra.
	d := draftGate(cfg, status(false, 3), false)
	if d.SwapToVersion != 0 || d.Return404 {
		t.Errorf("feature off must return zero decision, got %+v", d)
	}
}

func TestDraftGate_NoReviewflowStatus_NoAction(t *testing.T) {
	t.Parallel()
	cfg := config.ReviewflowConfig{HideDraftsFromUninvolved: true}
	// A page with no {reviewflow} directive has no Status. The gate
	// only bites on pages that opted into review; general documents
	// stay visible.
	d := draftGate(cfg, nil, false)
	if d.SwapToVersion != 0 || d.Return404 {
		t.Errorf("nil status must return zero decision, got %+v", d)
	}
}

// Regression: a page without a {reviewflow} directive reaches the
// gate as an empty-Roles Status (that's what computeStatus returns
// when st.Roles is empty). Before this test, the gate saw
// IsFullyValidated=false + Roles={} + not-involved and 404'd every
// ordinary wiki page. The fix short-circuits when Roles is empty —
// no directive means not a draft, means no gate.
func TestDraftGate_EmptyRolesStatus_NoAction(t *testing.T) {
	t.Parallel()
	cfg := config.ReviewflowConfig{HideDraftsFromUninvolved: true}
	emptyStatus := &reviewflow.Status{
		Roles:            map[string]string{},
		MissingRoles:     map[string]string{},
		IsFullyValidated: false,
		ValidatedVersion: 0,
	}
	d := draftGate(cfg, emptyStatus, false)
	if d.SwapToVersion != 0 || d.Return404 {
		t.Errorf("empty-Roles status must return zero decision (no reviewflow on the page), got %+v", d)
	}
}

func TestDraftGate_FullyValidated_NoAction(t *testing.T) {
	t.Parallel()
	cfg := config.ReviewflowConfig{HideDraftsFromUninvolved: true}
	// Current version is signed off — nothing to hide.
	d := draftGate(cfg, status(true, 4), false)
	if d.SwapToVersion != 0 || d.Return404 {
		t.Errorf("fully-validated page must return zero decision, got %+v", d)
	}
}

func TestDraftGate_Involved_SeesDraft(t *testing.T) {
	t.Parallel()
	cfg := config.ReviewflowConfig{HideDraftsFromUninvolved: true}
	// Author / reviewer / validator / observer always sees the WIP —
	// that's the whole point of being on the review chain.
	d := draftGate(cfg, status(false, 3), true)
	if d.SwapToVersion != 0 || d.Return404 {
		t.Errorf("involved reader must see the draft, got %+v", d)
	}
}

func TestDraftGate_UninvolvedWithValidated_SwapsToVersion(t *testing.T) {
	t.Parallel()
	cfg := config.ReviewflowConfig{HideDraftsFromUninvolved: true}
	d := draftGate(cfg, status(false, 7), false)
	if d.SwapToVersion != 7 {
		t.Errorf("swap to validated version 7, got %+v", d)
	}
	if d.Return404 {
		t.Errorf("must NOT 404 when a validated version exists, got %+v", d)
	}
}

func TestDraftGate_UninvolvedFirstDraft_Returns404(t *testing.T) {
	t.Parallel()
	cfg := config.ReviewflowConfig{HideDraftsFromUninvolved: true}
	// No prior validated version — for regulated wikis, an unsigned
	// document does not exist for outsiders.
	d := draftGate(cfg, status(false, 0), false)
	if !d.Return404 {
		t.Errorf("uninvolved reader on never-validated draft must 404, got %+v", d)
	}
	if d.SwapToVersion != 0 {
		t.Errorf("nothing to swap to when ValidatedVersion=0, got %+v", d)
	}
}
