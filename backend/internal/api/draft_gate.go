package api

import (
	"gowiki/backend/internal/config"
	"gowiki/backend/internal/reviewflow"
)

// draftGateDecision tells handleGetPage what to do about a
// potentially-draft page. Zero value = no action (default: serve the
// current content unchanged).
type draftGateDecision struct {
	// SwapToVersion, when > 0, is the version number the handler
	// should read from the attic and serve in place of the current
	// content. Empty/zero = don't swap.
	SwapToVersion int64
	// Return404, when true, means the requester should be told the
	// page does not exist. Used for uninvolved readers on a page
	// whose reviewflow has never validated any version.
	Return404 bool
}

// draftGate is the pure decision function for the
// hide-drafts-from-uninvolved feature. Splits the policy from the
// I/O plumbing in handleGetPage so it can be exhaustively unit
// tested without a full Server scaffold.
//
// Rules, evaluated in order:
//
//  1. Feature off (or no reviewflow at all) → no action.
//  2. Reviewflow status is fully validated → no action (nothing to
//     hide).
//  3. Requester is involved (any role assignee or global observer) →
//     no action (authors see what they're editing).
//  4. Requester is uninvolved AND a validated version exists → swap
//     content for that version.
//  5. Requester is uninvolved AND no validated version exists yet →
//     404 (an unsigned document does not exist for outsiders).
func draftGate(cfg config.ReviewflowConfig, status *reviewflow.Status, involved bool) draftGateDecision {
	if !cfg.HideDraftsFromUninvolved {
		return draftGateDecision{}
	}
	if status == nil || status.IsFullyValidated {
		return draftGateDecision{}
	}
	if involved {
		return draftGateDecision{}
	}
	if status.ValidatedVersion > 0 {
		return draftGateDecision{SwapToVersion: status.ValidatedVersion}
	}
	return draftGateDecision{Return404: true}
}
