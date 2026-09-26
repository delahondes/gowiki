package lifecycle

import "time"

// AttestationSource is a per-page timestamp lookup. Returns the zero
// time when this source has no attestation for the page (e.g. a page
// that has never been validated for the reviewflow source).
//
// The scanner takes any number of these and uses the maximum non-zero
// timestamp across them as the page's overall "freshness". Adding a
// new attestation source (e.g. a `{seen}` acknowledgement) is a
// matter of writing a new AttestationSource function and adding it to
// the scanner's list — no plumbing changes elsewhere.
type AttestationSource func(pagePath string) time.Time

// LastAttestation returns the most recent non-zero timestamp across
// every source. Zero return means the page has no attestation at all —
// callers should treat that as "as stale as the page has ever been"
// (typically: compare with page creation time or refuse to evaluate).
func LastAttestation(pagePath string, sources ...AttestationSource) time.Time {
	var best time.Time
	for _, src := range sources {
		if src == nil {
			continue
		}
		t := src(pagePath)
		if t.After(best) {
			best = t
		}
	}
	return best
}
