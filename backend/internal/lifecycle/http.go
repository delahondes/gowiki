package lifecycle

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// StatusResponse is what /status returns per rule on the source document.
// Rendered by the frontend NodeView to show a live human summary: "all
// documents in scope are within the rule" vs "12 documents need review."
type StatusResponse struct {
	Rules []RuleStatus `json:"rules"`
}

// RuleStatus is the per-rule state the reader sees on-screen. `Rule`
// carries the source attributes so the frontend can match its own PM
// node against the right rule (attrs are the identity carrier the
// frontend has; RuleID is a deterministic backend hash the frontend
// doesn't compute).
type RuleStatus struct {
	Rule        RuleView `json:"rule"`
	FiresCount  int      `json:"fires_count"`
	SamplePages []string `json:"sample_pages,omitempty"`
	SourcePage  string   `json:"source_page"`
}

// RuleView mirrors the attribute set of a Rule for the frontend's
// matching pass. Kept flat and JSON-friendly rather than reusing Rule
// directly so we don't leak internal fields (ID, compiled regex,
// timestamps).
type RuleView struct {
	Scope       string   `json:"scope"`
	Tags        []string `json:"tags"`
	ExcludeTags []string `json:"exclude_tags,omitempty"`
	When        string   `json:"when"`
	Title       string   `json:"title"`
	Assign      string   `json:"assign"`
	Priority    string   `json:"priority,omitempty"`
	Action      string   `json:"action,omitempty"`
	// Kind is the semantic label the panel uses when breaking down
	// firing rules ("staleness", "reviewflow overdue"). Derived from
	// the parsed Condition.Kind so the frontend doesn't have to
	// re-parse `when=`.
	Kind      string `json:"kind"`
	AlertOnly bool   `json:"alert_only,omitempty"`
}

// RegisterRoutes wires the lifecycle plugin's HTTP surface. The scanner
// is optional — without one, the endpoint reports rules but always
// zero fires (safe fallback: the frontend renders the passive "rule
// active" state).
func RegisterRoutes(r chi.Router, store *Store, scanner *Scanner) {
	r.Get("/status", func(w http.ResponseWriter, req *http.Request) {
		sourcePage := strings.TrimSpace(req.URL.Query().Get("source_page"))
		if sourcePage == "" {
			http.Error(w, `{"error":"source_page query parameter required"}`, http.StatusBadRequest)
			return
		}

		rules, err := store.RulesForSourcePage(sourcePage)
		if err != nil {
			http.Error(w, `{"error":"load rules"}`, http.StatusInternalServerError)
			return
		}

		resp := StatusResponse{Rules: make([]RuleStatus, 0, len(rules))}
		for _, r := range rules {
			view := RuleStatus{
				Rule: RuleView{
					Scope:       r.ScopeRegex,
					Tags:        r.Tags,
					ExcludeTags: r.ExcludeTags,
					When:        formatCondition(r.Condition),
					Title:       r.Todo.Title,
					Assign:      r.Todo.Assign,
					Priority:    r.Todo.Priority,
					Action:      r.Todo.Action,
					Kind:        r.Condition.Kind,
					AlertOnly:   r.Condition.IsAlertOnly(),
				},
				SourcePage: r.SourcePage,
			}
			if scanner != nil {
				fires := scanner.DryRun(r)
				view.FiresCount = len(fires)
				if n := len(fires); n > 0 {
					// The frontend renders a collapsible list per rule
					// so the reader can drill into which documents are
					// flagged. Cap generously so hundreds of firing
					// pages still fit; anything above the cap gets
					// truncated with the count preserved. Anything
					// above 500 firing pages is a config problem the
					// list wouldn't be usable for anyway.
					const cap = 500
					if n > cap {
						fires = fires[:cap]
					}
					view.SamplePages = fires
				}
			}
			resp.Rules = append(resp.Rules, view)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

// formatCondition re-serialises a parsed Condition back into the raw
// `when=` text shape (e.g. "stale:900d"). The unit choice is days
// because that's what parseCondition stores internally — we don't try
// to reconstruct the author's original "30m" (months) because the
// original text isn't kept and the frontend only needs a canonical
// value it can pretty-print for the reader.
func formatCondition(c Condition) string {
	if c.Kind == "" {
		return ""
	}
	if c.Kind == "stale" && c.Duration > 0 {
		days := int(c.Duration / (24 * time.Hour))
		return fmt.Sprintf("stale:%dd", days)
	}
	// reviewflow_overdue and any other alert-only kinds carry no
	// payload — the bare kind name IS the canonical when= text.
	return c.Kind
}
