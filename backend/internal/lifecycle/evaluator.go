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

// Evaluate runs one rule's condition against one page. `now` is passed
// explicitly so tests can freeze the clock; the scanner just passes
// time.Now().UTC().
//
// Currently only the "stale" condition is defined. Adding a new
// condition kind is a new case here — the scanner and store need no
// change.
func Evaluate(r Rule, pagePath string, now time.Time, sources ...AttestationSource) Verdict {
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
	default:
		// Unknown condition kind: refuse silently rather than crash the
		// scanner. Parse-time validation should catch this earlier;
		// this is defence-in-depth against corrupt on-disk rules.
		return Verdict{Fires: false}
	}
}
