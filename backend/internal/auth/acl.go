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

	s.mu.Lock()
	defer s.mu.Unlock()

	s.rules = make([]ACLRule, len(rules))
	copy(s.rules, rules)

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
	s.mu.RLock()
	defer s.mu.RUnlock()

	type matchedRule struct {
		patternLen  int
		permissions []string
	}

	var matches []matchedRule

	for _, rule := range s.rules {
		// Check if the pattern matches the page path.
		re, err := regexp.Compile("^(?:" + rule.Pattern + ")$")
		if err != nil {
			// Invalid pattern — skip it (should not happen after validation).
			continue
		}
		if !re.MatchString(pagePath) {
			continue
		}

		// Check if the subject applies to this user.
		if !subjectMatches(rule, username, groups) {
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

// subjectMatches checks if a rule's subject applies to the given user.
func subjectMatches(rule ACLRule, username string, groups []string) bool {
	switch rule.SubjectType {
	case "special":
		switch rule.Subject {
		case "@all":
			return true
		case "@authenticated":
			return username != ""
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
		if _, err := regexp.Compile(rule.Pattern); err != nil {
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
