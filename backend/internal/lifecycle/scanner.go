package lifecycle

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
)

// TodoRequest is the shape the scanner hands to the todo layer when a
// rule fires on a page. Kept package-local so lifecycle doesn't import
// internal/todo — the wiring in main.go bridges the two.
type TodoRequest struct {
	NodeKey    string // deterministic: sha1("lifecycle:"+ruleID+":"+targetPage)
	SourcePage string // the target — the page the rule fired against
	Title      string
	Assign     string
	Priority   string
	Action     string
	Tags       string
}

// ExistingLifecycleTodo is the minimum the scanner needs to know about a
// todo already in the store: enough to reconcile against the desired
// set and cancel orphans. The bridging code in main.go fills this by
// listing todos whose NodeKey is in the "lifecycle:*" family.
type ExistingLifecycleTodo struct {
	ID      string
	NodeKey string
}

// Deps is the surface the scanner needs. Injected as functions so the
// package stays testable without any of the concrete dependencies (todo
// store, tag index, reviewflow state, page store).
type Deps struct {
	// Rules is the source-of-truth loader — usually Store.AllRules.
	Rules func() ([]Rule, error)

	// ListPages returns every page path currently in the wiki. The
	// scanner walks the whole list per rule (rule count × page count
	// is small enough — hundreds × thousands = manageable). If that
	// scales badly later we can add a per-rule fast path indexed by
	// (path prefix, tags) but it's not needed today.
	ListPages func() []string

	// PageTags returns the tags carried by a page. Return nil for
	// pages with no tags — the scanner treats nil and empty
	// identically.
	PageTags func(pagePath string) []string

	// Attesters is the freshness sources the evaluator consults. Order
	// doesn't matter — the max non-zero timestamp wins.
	Attesters []AttestationSource

	// CreateTodo is called for every (rule, page) that fires. The
	// implementation MUST be idempotent by NodeKey: if a todo with the
	// same NodeKey already exists (and is not cancelled), it should
	// leave it alone rather than duplicate.
	CreateTodo func(ctx context.Context, req TodoRequest) error

	// ExistingLifecycleTodos returns every todo whose NodeKey belongs
	// to the "lifecycle:*" family — used to reconcile away todos whose
	// rule no longer exists or whose target page is no longer stale.
	ExistingLifecycleTodos func(ctx context.Context) ([]ExistingLifecycleTodo, error)

	// CancelTodo cancels a todo by its ID. Called during reconciliation
	// for orphans (rule deleted, page no longer stale).
	CancelTodo func(ctx context.Context, todoID string) error

	// Now provides the current time. Tests inject a fixed clock; the
	// scanner defaults to time.Now().UTC() if nil.
	Now func() time.Time
}

// Scanner walks the rules store and reconciles the todo store against
// the set of (rule, page) pairs that currently satisfy their condition.
type Scanner struct {
	deps Deps
}

// NewScanner wires a scanner with its dependencies. Nil deps are
// tolerated only for `Now` (which defaults to time.Now); everything
// else is required and a nil ExistingLifecycleTodos disables the
// reconciliation-away path (missing todos are still created).
func NewScanner(deps Deps) *Scanner {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Scanner{deps: deps}
}

// NodeKey returns the deterministic identifier for a (rule, page) todo.
// Exposed so the todo layer can identify lifecycle-owned todos when
// filtering the store for the reconciliation path.
func NodeKey(ruleID, targetPage string) string {
	h := sha1.Sum([]byte("lifecycle:" + ruleID + ":" + targetPage))
	return hex.EncodeToString(h[:])
}

// LifecycleNodeKeyPrefix is the well-known SHA-1-preimage prefix used
// by every lifecycle-owned todo's node_key. NodeKey feeds the whole
// string through sha1 so the hex output isn't literally prefixed — the
// prefix is what the SOURCE string starts with, not the hex. The todo
// layer distinguishes lifecycle todos by tag (below) rather than by
// hex prefix, but we document the shape here for reference.
const LifecycleTagMarker = "lifecycle"

// DryRun evaluates one rule against the current page + attestation
// state WITHOUT creating or cancelling any todos. Returns the target
// pages the rule currently fires on. Used by the frontend's rendering
// box to answer "is my rule doing anything right now" so the reader
// gets a live status instead of a static description of intent.
func (s *Scanner) DryRun(r Rule) []string {
	pages := s.deps.ListPages()
	now := s.deps.Now()
	var fires []string
	for _, pagePath := range pages {
		if pagePath == r.SourcePage {
			continue
		}
		tags := TagSet(s.deps.PageTags(pagePath))
		if !PageMatches(r, pagePath, tags) {
			continue
		}
		v := Evaluate(r, pagePath, now, s.deps.Attesters...)
		if v.Fires {
			fires = append(fires, pagePath)
		}
	}
	return fires
}

// Run does one full scan pass. Returns (created, cancelled, err).
//   - created: number of todos created this pass (existing ones are left
//     alone by the idempotent CreateTodo).
//   - cancelled: number of orphan todos reconciled away.
//
// Safe to call concurrently only if the underlying CreateTodo and
// CancelTodo are safe to call concurrently. The scheduler drives one
// call at a time; tests do the same.
func (s *Scanner) Run(ctx context.Context) (created, cancelled int, err error) {
	rules, err := s.deps.Rules()
	if err != nil {
		return 0, 0, fmt.Errorf("load rules: %w", err)
	}
	pages := s.deps.ListPages()
	now := s.deps.Now()

	// desired[nodeKey] = TodoRequest for the (rule, page) that should
	// exist right now. Populated by walking every rule × every matching
	// page. Reconciliation compares this against existing lifecycle
	// todos and creates / cancels the delta.
	desired := make(map[string]TodoRequest)

	for _, r := range rules {
		for _, pagePath := range pages {
			// Don't create a todo on the rule's OWN source page — the
			// author would nag themselves.
			if pagePath == r.SourcePage {
				continue
			}
			tags := TagSet(s.deps.PageTags(pagePath))
			if !PageMatches(r, pagePath, tags) {
				continue
			}
			verdict := Evaluate(r, pagePath, now, s.deps.Attesters...)
			if !verdict.Fires {
				continue
			}
			req := s.buildRequest(r, pagePath, verdict, now)
			desired[req.NodeKey] = req
		}
	}

	// Fetch the current existing set BEFORE any create/cancel so we
	// use one consistent snapshot for both operations:
	//   - Skip creates whose NodeKey is already live.
	//   - Cancel existing keys that aren't in `desired`.
	// This puts idempotency in the scanner itself; wiring code just
	// implements a bare CreateTodo without a dedup pre-check.
	var existing []ExistingLifecycleTodo
	existingByKey := make(map[string]ExistingLifecycleTodo)
	if s.deps.ExistingLifecycleTodos != nil {
		var err error
		existing, err = s.deps.ExistingLifecycleTodos(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("list existing lifecycle todos: %w", err)
		}
		for _, e := range existing {
			existingByKey[e.NodeKey] = e
		}
	}

	// Create missing todos. Skip any whose NodeKey already has a live
	// entry in the store — nothing to do; leaving the existing todo in
	// place preserves its history and assignee state.
	for _, req := range desired {
		if _, live := existingByKey[req.NodeKey]; live {
			continue
		}
		if err := s.deps.CreateTodo(ctx, req); err != nil {
			log.Printf("lifecycle: create todo for rule=%s page=%s: %v", req.NodeKey[:8], req.SourcePage, err)
			continue
		}
		created++
	}

	// Reconcile: cancel any existing lifecycle todo whose NodeKey is
	// not in `desired`. Covers three cases at once:
	//   - The page was edited / re-validated and is no longer stale.
	//   - The rule was deleted from its source page.
	//   - The rule's scope no longer includes this page.
	for _, e := range existing {
		if _, keep := desired[e.NodeKey]; keep {
			continue
		}
		if err := s.deps.CancelTodo(ctx, e.ID); err != nil {
			log.Printf("lifecycle: cancel orphan todo id=%s key=%s: %v", e.ID, e.NodeKey[:8], err)
			continue
		}
		cancelled++
	}

	return created, cancelled, nil
}

// buildRequest turns a (rule, page, verdict) into the todo the scanner
// asks the todo layer to create. Renders template variables in title
// and other user-supplied fields.
func (s *Scanner) buildRequest(r Rule, pagePath string, v Verdict, now time.Time) TodoRequest {
	vars := templateVars(pagePath, v, now)
	todoTags := "lifecycle"
	if extra := strings.TrimSpace(r.Todo.Tags); extra != "" {
		todoTags = todoTags + "," + extra
	}
	return TodoRequest{
		NodeKey:    NodeKey(r.ID, pagePath),
		SourcePage: pagePath,
		Title:      renderTemplate(r.Todo.Title, vars),
		Assign:     r.Todo.Assign,
		Priority:   r.Todo.Priority,
		Action:     renderTemplate(r.Todo.Action, vars),
		Tags:       todoTags,
	}
}

// templateVars is the {{name}} substitution table for one (page, verdict)
// pass. Kept tiny on purpose — every added variable is another surface
// area to document and test.
func templateVars(pagePath string, v Verdict, now time.Time) map[string]string {
	vars := map[string]string{"path": pagePath}
	if !v.LastAttested.IsZero() {
		vars["last_attested"] = v.LastAttested.UTC().Format(time.RFC3339)
		days := int(now.Sub(v.LastAttested).Hours() / 24)
		vars["stale_days"] = fmt.Sprintf("%d", days)
	} else {
		vars["last_attested"] = "never"
		vars["stale_days"] = "∞"
	}
	return vars
}

// renderTemplate does a single-pass {{name}} substitution — same
// non-recursive contract as the frontend interpolateVars. Unknown names
// stay literal so a typo is visible in the generated todo title
// (rather than silently becoming "").
func renderTemplate(s string, vars map[string]string) string {
	if s == "" || !strings.Contains(s, "{{") {
		return s
	}
	var out strings.Builder
	i := 0
	for i < len(s) {
		start := strings.Index(s[i:], "{{")
		if start < 0 {
			out.WriteString(s[i:])
			break
		}
		out.WriteString(s[i : i+start])
		i += start
		end := strings.Index(s[i:], "}}")
		if end < 0 {
			out.WriteString(s[i:])
			break
		}
		name := strings.TrimSpace(s[i+2 : i+end])
		if v, ok := vars[name]; ok {
			out.WriteString(v)
		} else {
			// Unknown: pass through literally.
			out.WriteString(s[i : i+end+2])
		}
		i += end + 2
	}
	return out.String()
}
