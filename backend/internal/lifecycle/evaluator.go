package lifecycle

import "time"

// Verdict is the result of evaluating one rule against one page.
type Verdict struct {
	// Fires is true when the rule's condition holds on this page — a
	// todo should be created (or kept if one already exists).
	Fires bool

	// LastAttested is the freshness timestamp the evaluator used. Zero
	// means "no attestation on record at all" (which counts as fully
	// stale for stale-conditions). Exposed on the verdict so the
	// scanner can render {{last_modified}}/{{stale_age}} template
	// variables in the todo title without recomputing.
	LastAttested time.Time
}

// OverdueProbe reports the roles currently in the "overdue" state on a
// page's reviewflow — mirrors reviewflow.Service.Status().OverdueRoles.
// Zero-length return means "no reviewflow overdue on this page" (which
// includes pages that have no reviewflow at all).
type OverdueProbe func(pagePath string) []string

// OpenCommentsProbe returns the number of unresolved top-level comment
// threads on a page. Zero means "nothing to nag about."
type OpenCommentsProbe func(pagePath string) int

// Probes bundles every non-timestamp signal the evaluator can consult.
// Each condition kind reads only the probe(s) it needs; missing probes
// yield Fires=false rather than panicking so a mis-wired instance
// degrades safely.
type Probes struct {
	Overdue      OverdueProbe
	OpenComments OpenCommentsProbe
}

// Evaluate runs one rule's condition against one page. `now` is passed
// explicitly so tests can freeze the clock; the scanner just passes
// time.Now().UTC(). Probes is optional per-kind; a missing probe for
// the kind this rule uses yields Fires=false.
//
// Adding a new condition kind is a new case here — the store needs no
// change and the scanner only needs to inject the probe(s) the new
// kind consults.
func Evaluate(r Rule, pagePath string, now time.Time, probes Probes, sources ...AttestationSource) Verdict {
	switch r.Condition.Kind {
	case "stale":
		last := LastAttestation(pagePath, sources...)
		v := Verdict{LastAttested: last}
		// No attestation at all → fully stale (fires immediately). The
		// zero-time case is what happens for a page that carries no
		// storage metadata AND is not in reviewflow — genuinely
		// unattested, so a todo is warranted.
		if last.IsZero() {
			v.Fires = true
			return v
		}
		v.Fires = now.Sub(last) > r.Condition.Duration
		return v
	case "reviewflow_overdue":
		if probes.Overdue == nil {
			return Verdict{Fires: false}
		}
		roles := probes.Overdue(pagePath)
		return Verdict{Fires: len(roles) > 0}
	case "comments_open":
		if probes.OpenComments == nil {
			return Verdict{Fires: false}
		}
		return Verdict{Fires: probes.OpenComments(pagePath) > 0}
	default:
		// Unknown condition kind: refuse silently rather than crash the
		// scanner. Parse-time validation should catch this earlier;
		// this is defence-in-depth against corrupt on-disk rules.
		return Verdict{Fires: false}
	}
}
