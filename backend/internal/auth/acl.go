package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ValidPermissions is the set of permissions that ACL rules may grant.
var ValidPermissions = map[string]bool{
	"view":   true,
	"edit":   true,
	"delete": true,
}

// ValidSubjectTypes is the set of allowed subject types for ACL rules.
var ValidSubjectTypes = map[string]bool{
	"user":    true,
	"group":   true,
	"special": true,
}

// ValidSpecialSubjects is the set of allowed subjects when subject_type is "special".
var ValidSpecialSubjects = map[string]bool{
	"@all":           true,
	"@authenticated": true,
	"@self":          true,
	"@ai":            true,
}

// ACLRule represents a single access control entry.
type ACLRule struct {
	Pattern     string   `json:"pattern"`      // Go regexp on page/namespace path
	SubjectType string   `json:"subject_type"` // "user", "group", or "special"
	Subject     string   `json:"subject"`      // username, group name, "@all", or "@authenticated"
	Permissions []string `json:"permissions"`  // subset of: "view", "edit", "delete"
}

// ACLStore manages the ACL ruleset with file-based persistence.
type ACLStore struct {
	mu    sync.RWMutex
	rules []ACLRule
	path  string
}

// defaultACLRules returns the bootstrap ruleset used when no acl.json exists.
func defaultACLRules() []ACLRule {
	return []ACLRule{
		{Pattern: ".*", SubjectType: "group", Subject: "admin", Permissions: []string{"view", "edit", "delete"}},
		{Pattern: ".*", SubjectType: "group", Subject: "editors", Permissions: []string{"view", "edit"}},
		{Pattern: ".*", SubjectType: "special", Subject: "@all", Permissions: []string{"view"}},
	}
}

// NewACLStore loads or bootstraps the ACL store from metaRoot/acl.json.
func NewACLStore(metaRoot string) (*ACLStore, error) {
	path := filepath.Join(metaRoot, "acl.json")
	s := &ACLStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ACLStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.bootstrap()
	}
	if err != nil {
		return fmt.Errorf("read acl file: %w", err)
	}
	var rules []ACLRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return fmt.Errorf("parse acl file: %w", err)
	}
	// Validate loaded rules.
	if err := validateRules(rules); err != nil {
		return fmt.Errorf("invalid acl file: %w", err)
	}
	// A duplicate (pattern, subject) on disk must never brick startup: dedupe it
	// (keeping the first occurrence) and warn loudly. The strict rejection lives
	// in Replace, so admins editing through the API get immediate feedback and
	// cannot introduce one — this only forgives legacy or hand-edited files.
	rules = dedupeRules(rules)
	sortRules(rules)
	s.rules = rules
	return nil
}

func (s *ACLStore) bootstrap() error {
	s.rules = defaultACLRules()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create acl dir: %w", err)
	}
	if err := s.save(); err != nil {
		return fmt.Errorf("write default acl file: %w", err)
	}
	log.Printf("created default acl.json with bootstrap rules")
	return nil
}

func (s *ACLStore) save() error {
	data, err := json.MarshalIndent(s.rules, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal acl: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: write to temp file, then rename.
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write acl tmp: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename acl tmp: %w", err)
	}
	return nil
}

// List returns a copy of all ACL rules.
func (s *ACLStore) List() []ACLRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ACLRule, len(s.rules))
	copy(out, s.rules)
	return out
}

// Replace replaces the entire ACL ruleset after validation.
func (s *ACLStore) Replace(rules []ACLRule) error {
	if err := validateRules(rules); err != nil {
		return err
	}
	if err := checkNoDuplicates(rules); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.rules = make([]ACLRule, len(rules))
	copy(s.rules, rules)
	sortRules(s.rules)

	if err := s.save(); err != nil {
		return fmt.Errorf("save acl: %w", err)
	}
	return nil
}

// CheckPermission evaluates whether the given user (with groups) has the
// requested action on pagePath, based on the current ACL rules.
//
// Evaluation logic:
//  1. Collect all rules whose Pattern regexp matches pagePath.
//  2. Filter to rules that apply to this user (by subject matching).
//  3. Group by pattern specificity (longest pattern first).
//  4. Take the most specific group. If multiple rules share the same
//     specificity, union their permissions (most permissive wins).
//  5. Check if the requested action is in the resulting permission set.
//  6. If no rules match: deny by default.
func (s *ACLStore) CheckPermission(username string, groups []string, pagePath string, action string) bool {
	return s.evaluate(pagePath, action, func(rule ACLRule) (string, bool) {
		// For @self rules, substitute @self in the pattern with the actual
		// username. Anonymous users can never match @self rules.
		pattern := rule.Pattern
		if rule.SubjectType == "special" && rule.Subject == "@self" {
			if username == "" {
				return "", false
			}
			pattern = strings.ReplaceAll(pattern, "@self", regexp.QuoteMeta(username))
		}
		return pattern, subjectMatches(rule, username, groups)
	})
}

// CheckAIPermission evaluates whether the AI-agent capability allows the action
// on pagePath, considering ONLY rules whose subject is @ai. @all and
// @authenticated baselines never participate in this axis: a broad @all grant
// or deny must not widen or restrict what the AI may do.
//
// This is the second gate of the AI's dual check. The AI's effective access is
// this gate intersected with the permission of the human user it acts for, so
// the AI can never exceed that user — but an everyone-baseline rule no longer
// silently strips the AI of access the user has. @ai-specific rules (e.g. a
// "/bioit/.* @ai []" deny) still apply and remain the way to restrict the AI.
func (s *ACLStore) CheckAIPermission(pagePath string, action string) bool {
	return s.evaluate(pagePath, action, func(rule ACLRule) (string, bool) {
		applies := rule.SubjectType == "special" && rule.Subject == "@ai"
		return rule.Pattern, applies
	})
}

// evaluate runs the shared most-specific-match-then-union algorithm. resolve
// reports, for each rule, the pattern to match against (after any @self
// substitution) and whether the rule's subject applies to the caller. Rules for
// which applies is false are ignored entirely.
func (s *ACLStore) evaluate(pagePath string, action string, resolve func(ACLRule) (pattern string, applies bool)) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type matchedRule struct {
		patternLen  int
		permissions []string
	}

	var matches []matchedRule

	for _, rule := range s.rules {
		pattern, applies := resolve(rule)
		if !applies {
			continue
		}

		// Check if the pattern matches the page path.
		re, err := regexp.Compile("^(?:" + pattern + ")$")
		if err != nil {
			// Invalid pattern — skip it (should not happen after validation).
			continue
		}
		if !re.MatchString(pagePath) {
			continue
		}

		matches = append(matches, matchedRule{
			patternLen:  len(rule.Pattern),
			permissions: rule.Permissions,
		})
	}

	if len(matches) == 0 {
		return false // deny by default
	}

	// Sort by pattern length descending to find the most specific.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].patternLen > matches[j].patternLen
	})

	// Find the most specific pattern length.
	mostSpecific := matches[0].patternLen

	// Union permissions from all rules at the most specific level.
	permSet := make(map[string]bool)
	for _, m := range matches {
		if m.patternLen < mostSpecific {
			break // done with the most specific group
		}
		for _, p := range m.permissions {
			permSet[p] = true
		}
	}

	return permSet[action]
}

// sortRules orders rules deterministically for readability. Evaluation is
// order-independent (see CheckPermission — it selects by pattern length and
// unions, never by list position), so this purely affects how the ruleset is
// displayed and persisted: it has no effect on access decisions.
//
// Rules are grouped by the literal path prefix of their pattern (the leading
// run before the first regexp metacharacter), so all rules targeting the same
// namespace land next to each other. Catch-all patterns (empty literal prefix,
// e.g. ".*") sort last as the base layer. Ties break by subject then pattern.
func sortRules(rules []ACLRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		pi, pj := literalPrefix(rules[i].Pattern), literalPrefix(rules[j].Pattern)
		// Empty prefix (catch-all) sorts after any concrete prefix.
		if (pi == "") != (pj == "") {
			return pi != ""
		}
		if pi != pj {
			return pi < pj
		}
		if rules[i].Pattern != rules[j].Pattern {
			return rules[i].Pattern < rules[j].Pattern
		}
		if rules[i].SubjectType != rules[j].SubjectType {
			return rules[i].SubjectType < rules[j].SubjectType
		}
		return rules[i].Subject < rules[j].Subject
	})
}

// literalPrefix returns the leading run of a regexp pattern up to the first
// metacharacter — the fixed path segment a pattern is anchored on. For example
// "regulatory/.*" → "regulatory/", "regulatory/qms/sop01" → the whole string,
// and ".*" → "" (matches anywhere, so no concrete prefix).
func literalPrefix(pattern string) string {
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$', '\\':
			return pattern[:i]
		}
	}
	return pattern
}

// subjectMatches checks if a rule's subject applies to the given user.
func subjectMatches(rule ACLRule, username string, groups []string) bool {
	switch rule.SubjectType {
	case "special":
		switch rule.Subject {
		case "@all":
			return true
		case "@authenticated":
			return username != ""
		case "@self":
			return username != ""
		case "@ai":
			return username == "@ai"
		}
	case "user":
		return rule.Subject == username
	case "group":
		for _, g := range groups {
			if g == rule.Subject {
				return true
			}
		}
	}
	return false
}

// validateRules checks that all rules in the set are well-formed.
func validateRules(rules []ACLRule) error {
	for i, rule := range rules {
		// Validate pattern is a valid Go regexp.
		// For @self rules, @self in the pattern is a placeholder substituted at
		// evaluation time, so replace it with a dummy value for validation.
		patternToValidate := rule.Pattern
		if rule.SubjectType == "special" && rule.Subject == "@self" {
			patternToValidate = strings.ReplaceAll(patternToValidate, "@self", "dummy")
		}
		if _, err := regexp.Compile(patternToValidate); err != nil {
			return fmt.Errorf("rule %d: invalid pattern %q: %w", i, rule.Pattern, err)
		}

		// Validate subject type.
		if !ValidSubjectTypes[rule.SubjectType] {
			return fmt.Errorf("rule %d: invalid subject_type %q", i, rule.SubjectType)
		}

		// Validate special subjects.
		if rule.SubjectType == "special" {
			if !ValidSpecialSubjects[rule.Subject] {
				return fmt.Errorf("rule %d: invalid special subject %q (must be @all or @authenticated)", i, rule.Subject)
			}
		}

		// Validate permissions (empty list is valid — it means "deny all").
		for _, p := range rule.Permissions {
			if !ValidPermissions[p] {
				return fmt.Errorf("rule %d: invalid permission %q", i, p)
			}
		}
	}
	return nil
}

// ruleKey identifies a rule by the triple that determines whether two rules
// co-fire during evaluation: same pattern + same subject means they always
// match together and have their permissions unioned.
func ruleKey(rule ACLRule) string {
	return rule.Pattern + "\x00" + rule.SubjectType + "\x00" + rule.Subject
}

// checkNoDuplicates rejects a ruleset containing two rules with the same pattern
// and subject. Such a pair always co-fires, so a second rule is at best
// redundant and at worst a silent contradiction (a deny and a grant on the same
// target resolve to the grant — the bug that leaked anonymous access). This is
// enforced on Replace so admins editing through the API cannot introduce one.
func checkNoDuplicates(rules []ACLRule) error {
	seen := make(map[string]int, len(rules))
	for i, rule := range rules {
		key := ruleKey(rule)
		if first, dup := seen[key]; dup {
			return fmt.Errorf("rule %d duplicates rule %d: same pattern %q and subject %s:%s — merge them into a single rule with the permissions you want",
				i, first, rule.Pattern, rule.SubjectType, rule.Subject)
		}
		seen[key] = i
	}
	return nil
}

// dedupeRules drops rules whose (pattern, subject) already appeared, keeping the
// first occurrence, and logs each drop. Used on load so a duplicate on disk
// degrades gracefully instead of refusing to start.
func dedupeRules(rules []ACLRule) []ACLRule {
	seen := make(map[string]int, len(rules))
	out := rules[:0:0]
	for i, rule := range rules {
		key := ruleKey(rule)
		if first, dup := seen[key]; dup {
			log.Printf("acl: dropping duplicate rule %d (pattern %q subject %s:%s); first defined at rule %d",
				i, rule.Pattern, rule.SubjectType, rule.Subject, first)
			continue
		}
		seen[key] = i
		out = append(out, rule)
	}
	return out
}
