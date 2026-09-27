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
// includes pages that have no reviewflow at all — those are handled by
// a different rule kind if needed).
//
// Passed separately from AttestationSource because it isn't a
// timestamp; it's a categorical yes/no signal for the
// reviewflow_overdue condition.
type OverdueProbe func(pagePath string) []string

// Evaluate runs one rule's condition against one page. `now` is passed
// explicitly so tests can freeze the clock; the scanner just passes
// time.Now().UTC(). `overdue` is optional and only consulted for the
// reviewflow_overdue kind — passing nil there yields Fires=false.
//
// Adding a new condition kind is a new case here — the store needs no
// change and the scanner only needs to inject the probe(s) the new
// kind consults.
func Evaluate(r Rule, pagePath string, now time.Time, overdue OverdueProbe, sources ...AttestationSource) Verdict {
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
		if overdue == nil {
			return Verdict{Fires: false}
		}
		roles := overdue(pagePath)
		return Verdict{Fires: len(roles) > 0}
	default:
		// Unknown condition kind: refuse silently rather than crash the
		// scanner. Parse-time validation should catch this earlier;
		// this is defence-in-depth against corrupt on-disk rules.
		return Verdict{Fires: false}
	}
}
