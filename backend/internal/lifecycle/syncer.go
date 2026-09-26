package lifecycle

import "log"

// Syncer wires the parse + store pipeline behind the storage.LifecycleSyncer
// hook. On every page save the storage layer hands us the (path, markdown)
// pair; we extract any {lifecycle} directives from that page and replace
// the store's per-page rule set with the fresh parse.
//
// Parse errors are LOGGED but do NOT fail the page save — a broken
// {lifecycle} on a page shouldn't prevent unrelated content from being
// written. The rule simply won't be in the index until the author fixes
// the directive, at which point the next save reconciles.
type Syncer struct {
	store *Store
}

// NewSyncer wraps a Store. Returns a value that satisfies
// storage.LifecycleSyncer.
func NewSyncer(store *Store) *Syncer {
	return &Syncer{store: store}
}

// SyncFromMarkdown extracts every {lifecycle} directive from `markdown`
// and replaces the source page's rule set with the fresh parse. A page
// that no longer contains any directive has its entry removed (empty
// slice is treated as delete by Store.SetPageRules).
func (s *Syncer) SyncFromMarkdown(sourcePage, markdown string) error {
	rules, errs := Parse(sourcePage, markdown)
	for _, err := range errs {
		log.Printf("lifecycle: parse: %v", err)
	}
	return s.store.SetPageRules(sourcePage, rules)
}

// RemovePageRules drops every rule attached to a source page. Called
// when the page itself is deleted.
func (s *Syncer) RemovePageRules(sourcePage string) error {
	return s.store.SetPageRules(sourcePage, nil)
}
