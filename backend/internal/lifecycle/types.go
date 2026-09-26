// Package lifecycle implements the {lifecycle} rule framework: page-scoped
// rules that fire a todo when a page in scope satisfies a condition (today:
// stale — no attestation for a configured duration). The rule lives once
// on a meta page and fans out to per-target-page todos; the fanned-out
// todos are a consequence, not repetition — DRY still holds because the
// RULE is written once.
//
// Design notes:
//
//   - A rule matches a page when every present selector matches: path
//     regex ∧ has-any-of-tags ∧ has-none-of-excluded-tags. Missing
//     selectors are "don't care" for that dimension. `tags=` is OR (a
//     page in scope needs to carry AT LEAST ONE of the listed tags) —
//     the AND-of-tags case is not currently supported; if it ever needs
//     to be, add `all_tags=` alongside without changing `tags=`.
//   - Freshness of a page = max(page.updated_at, reviewflow.validated_at).
//     Reviewflow validation does NOT touch page.updated_at (it writes to
//     the reviewflow state file only), so we must consult both sources.
//     Additional attestation sources can be added later — the Attester
//     interface takes any collection of functions and returns the max
//     non-zero timestamp.
//   - The scanner is idempotent: for each (rule, page) pair that matches
//     and satisfies the condition, ensure ONE todo exists with a
//     deterministic node_key = sha1("lifecycle:" + ruleID + ":" +
//     pagePath). If the page becomes fresh again the todo is cancelled;
//     if the rule is deleted, every todo it spawned is reconciled away
//     on the next scan.
package lifecycle

import (
	"time"
)

// Rule is one {lifecycle} directive after parsing. Each rule lives on
// exactly one source page (the page containing the directive), and it
// may fan out to many target pages when the scanner runs.
type Rule struct {
	// ID is a stable hash of the directive body. Deterministic so that
	// re-parsing an unchanged directive yields the same ID (and thus
	// the same todo node_keys), even across process restarts.
	ID string `json:"id"`

	// SourcePage is the canonical path of the page that carries this
	// directive. When the source page is edited without the directive
	// (or the directive is removed), the rule disappears from the
	// index and its todos are reconciled away.
	SourcePage string `json:"source_page"`

	// Selectors — see package doc for combination semantics.
	ScopeRegex  string   `json:"scope_regex,omitempty"`  // path regex; matched against the page path
	Tags        []string `json:"tags,omitempty"`         // page must have AT LEAST ONE of these tags (OR)
	ExcludeTags []string `json:"exclude_tags,omitempty"` // page must have NONE of these tags

	// Condition. Today the only supported kind is "stale" with a
	// duration payload; extending is a matter of adding a new kind
	// and an evaluator function.
	Condition Condition `json:"condition"`

	// Todo template applied when the rule fires on a target page. All
	// string fields support {{path}}, {{title}}, {{last_modified}},
	// {{stale_age}} template variables resolved against the target
	// page at scan time.
	Todo TodoTemplate `json:"todo"`
}

// Condition is what makes a rule fire on a matching page.
type Condition struct {
	// Kind is "stale" today. Add new kinds by extending the Evaluator
	// switch and this documentation.
	Kind string `json:"kind"`

	// Duration is the payload for the "stale" kind: the page fires if
	// no attestation has occurred in the last Duration. Zero means
	// "always fires" — refused at parse time to prevent nag-storms.
	Duration time.Duration `json:"duration,omitempty"`
}

// TodoTemplate mirrors the shape of a todo.CreateRequest but keeps this
// package free of a hard dependency on internal/todo — the wiring in
// main.go bridges the two.
type TodoTemplate struct {
	Title    string `json:"title"`
	Assign   string `json:"assign"`             // user or "@group" reference
	Priority string `json:"priority,omitempty"` // normal / high / low
	Action   string `json:"action,omitempty"`   // "edit:.", "read:.", etc — WikiAction syntax
	Tags     string `json:"tags,omitempty"`     // comma-separated todo tags (not to be confused with page tags)
}
