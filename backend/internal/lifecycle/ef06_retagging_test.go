package lifecycle

import (
	"testing"

	"gowiki/backend/internal/storage"
)

// EF06 regulatory audit: retagging a document as `archived` must be
// the single action that removes it from BOTH sides of the wiki's
// classification — the tag-driven listings AND any lifecycle rule
// firing on the same tag. This test walks the SOP01-scripted flow
// end-to-end at the storage + selector layer:
//
//  1. A document tagged `sop` is picked up by a `{lifecycle}` rule
//     targeting `tags=sop exclude_tags=archived`.
//  2. The document also appears in listings that filter on `sop`.
//  3. Retagging the document as `sop,archived` (the SOP01 archival
//     gesture) removes it from BOTH sides in one step, with no
//     other operation.
//
// Wiring the two subsystems into one test at the API layer would need
// a full Server scaffold; the tag index + PageMatches selector are
// the atoms the higher layers compose, and the guarantee is the same.
func TestEF06_Retagging_Archived_RemovesFromListingsAndRules(t *testing.T) {
	t.Parallel()

	// Set up a small tag universe: one SOP, one archived SOP already,
	// one unrelated page. Then the SOP under audit is retagged mid-test.
	idx := storage.NewTagIndex(t.TempDir())
	idx.UpdatePage("/qms/sop/live", []string{"sop"}, "Live SOP")
	idx.UpdatePage("/qms/sop/other", []string{"sop", "archived"}, "Already archived")
	idx.UpdatePage("/qms/notes", []string{"note"}, "Unrelated")

	// The lifecycle rule an admin page declares once — see the QARA
	// managing page's `{lifecycle scope=… tags=sop exclude_tags=archived ...}`.
	rule := mkRule("ef06", "^/qms/.*", []string{"sop"}, []string{"archived"})

	// Before archival: /qms/sop/live is inside both surfaces.
	if !PageMatches(rule, "/qms/sop/live", TagSet(idx.GetTagsForPage("/qms/sop/live"))) {
		t.Error("live SOP should be picked up by the lifecycle rule")
	}
	listing := pagePathsOf(idx.GetPagesForTag("sop", "", []string{"archived"}))
	if !contains(listing, "/qms/sop/live") {
		t.Errorf("live SOP should appear in tag=sop / exclude=archived listing, got %v", listing)
	}
	if contains(listing, "/qms/sop/other") {
		t.Errorf("already-archived SOP must not appear in the listing, got %v", listing)
	}

	// Archival gesture: retag with the extra `archived` marker. This
	// is the ONE action SOP01 prescribes.
	idx.UpdatePage("/qms/sop/live", []string{"sop", "archived"}, "Live SOP")

	// After archival: the lifecycle rule no longer picks it up.
	if PageMatches(rule, "/qms/sop/live", TagSet(idx.GetTagsForPage("/qms/sop/live"))) {
		t.Error("archived SOP must no longer be picked up by the lifecycle rule — exclude_tags=archived is the whole point of retagging")
	}
	// And it disappears from listings that filter by tag=sop /
	// exclude=archived.
	listing = pagePathsOf(idx.GetPagesForTag("sop", "", []string{"archived"}))
	if contains(listing, "/qms/sop/live") {
		t.Errorf("archived SOP still appears in the sop / exclude=archived listing: %v", listing)
	}
	// But a listing that DOESN'T exclude archived still surfaces it —
	// audit trails must remain reachable, retagging is not deletion.
	listingAll := pagePathsOf(idx.GetPagesForTag("sop", "", nil))
	if !contains(listingAll, "/qms/sop/live") {
		t.Errorf("archived SOP must remain reachable to a query that does NOT exclude=archived (audit trail), got %v", listingAll)
	}
}

func pagePathsOf(entries []storage.PageEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
