package lifecycle

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// directiveRe matches a lifecycle directive on its own line.
// The body is anything between `{lifecycle ` and the closing `}` — parsed
// with a small key=value + key="quoted value" tokenizer below.
var directiveRe = regexp.MustCompile(`(?m)^\{lifecycle\s+(.*?)\}\s*$`)

// ParseError describes why a directive couldn't be turned into a Rule.
// Kept as a plain error string with structured context so callers (the
// wiki-hook, admin UI, tests) can surface why a rule was rejected.
type ParseError struct {
	SourcePage string
	Reason     string
}

func (e *ParseError) Error() string {
	if e.SourcePage != "" {
		return fmt.Sprintf("lifecycle directive on %s: %s", e.SourcePage, e.Reason)
	}
	return "lifecycle directive: " + e.Reason
}

// Parse extracts every {lifecycle} directive from markdown and returns
// one Rule per valid directive. Invalid directives are collected into
// errs but do NOT stop parsing — a mistake in one rule shouldn't blank
// the whole set for the page. Both slices are always non-nil.
func Parse(sourcePage, markdown string) (rules []Rule, errs []error) {
	rules = []Rule{}
	errs = []error{}

	matches := directiveRe.FindAllStringSubmatch(markdown, -1)
	for i, m := range matches {
		body := m[1]
		rule, err := parseOne(sourcePage, body)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		// Discriminate multiple rules on the same source page by mixing
		// the directive body index into the ID hash — otherwise two
		// rules with identical bodies (unlikely but possible) collide.
		rule.ID = ruleID(sourcePage, body, i)
		rules = append(rules, rule)
	}
	return rules, errs
}

// ruleID returns a deterministic hash for a rule at (sourcePage, index).
// A pure body hash is not enough — two source pages could legitimately
// carry identical directive bodies (a template stamp, say) and we want
// each occurrence to fan out to its own todos. Including the source
// page ensures uniqueness across pages; the index disambiguates within
// a page.
func ruleID(sourcePage, body string, index int) string {
	seed := sourcePage + "\x00" + strconv.Itoa(index) + "\x00" + body
	h := sha1.Sum([]byte(seed))
	return hex.EncodeToString(h[:])
}

// parseOne turns one directive body into a Rule. Validation errors
// return a *ParseError; the caller decides whether to log, surface, or
// bubble up.
func parseOne(sourcePage, body string) (Rule, error) {
	kvs := parseKV(body)

	rule := Rule{SourcePage: sourcePage}

	for _, kv := range kvs {
		switch kv.Key {
		case "scope":
			rule.ScopeRegex = kv.Value
		case "tags":
			rule.Tags = splitCSV(kv.Value)
		case "exclude_tags":
			rule.ExcludeTags = splitCSV(kv.Value)
		case "when":
			cond, err := parseCondition(kv.Value)
			if err != nil {
				return Rule{}, &ParseError{SourcePage: sourcePage, Reason: err.Error()}
			}
			rule.Condition = cond
		case "title":
			rule.Todo.Title = kv.Value
		case "assign":
			rule.Todo.Assign = kv.Value
		case "priority":
			rule.Todo.Priority = kv.Value
		case "action", "do":
			// Both spellings accepted. `do` matches the natural "when
			// stale, DO edit" phrasing but `action` matches the todo
			// plugin's own `action=` — either is fine.
			rule.Todo.Action = kv.Value
		case "todo_tags":
			rule.Todo.Tags = kv.Value
		default:
			// Unknown keys are refused early so a typo doesn't silently
			// void a selector. Better a loud parse error than a rule
			// that fires against the entire wiki because the author
			// wrote `scop=` instead of `scope=`.
			return Rule{}, &ParseError{SourcePage: sourcePage, Reason: fmt.Sprintf("unknown attribute %q", kv.Key)}
		}
	}

	// Validate: condition is required.
	if rule.Condition.Kind == "" {
		return Rule{}, &ParseError{SourcePage: sourcePage, Reason: "missing when= condition"}
	}
	// Validate: at least one POSITIVE selector must be present. A rule
	// with only exclude_tags= would nag every page in the wiki that
	// isn't tagged with the excluded set — dangerous default.
	if rule.ScopeRegex == "" && len(rule.Tags) == 0 {
		return Rule{}, &ParseError{SourcePage: sourcePage, Reason: "at least one of scope= or tags= must be provided (exclude_tags= alone would target the whole wiki)"}
	}
	// Compile the scope regex now so a bad regex is a parse error, not
	// a scanner-time surprise.
	if rule.ScopeRegex != "" {
		if _, err := regexp.Compile(rule.ScopeRegex); err != nil {
			return Rule{}, &ParseError{SourcePage: sourcePage, Reason: fmt.Sprintf("invalid scope regex: %v", err)}
		}
	}
	// Todo template minimum: title + assign. Everything else is optional.
	if strings.TrimSpace(rule.Todo.Title) == "" {
		return Rule{}, &ParseError{SourcePage: sourcePage, Reason: "missing title="}
	}
	if strings.TrimSpace(rule.Todo.Assign) == "" {
		return Rule{}, &ParseError{SourcePage: sourcePage, Reason: "missing assign="}
	}

	// Normalise: sort tag lists for deterministic ID hashing across
	// re-parses of the same directive with reordered CSV values.
	sort.Strings(rule.Tags)
	sort.Strings(rule.ExcludeTags)

	return rule, nil
}

// parseCondition turns a `when=` value into a Condition. Format today:
// `stale:<duration>` where duration is <int><unit> and unit is one of
// `d` (day), `m` (month, 30 days), `y` (year, 365 days). Matches the
// reviewflow deadline convention.
func parseCondition(raw string) (Condition, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Condition{}, fmt.Errorf("empty when=")
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return Condition{}, fmt.Errorf("condition must be <kind>:<value>, got %q", raw)
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	payload := strings.TrimSpace(parts[1])

	switch kind {
	case "stale":
		d, err := parseDuration(payload)
		if err != nil {
			return Condition{}, fmt.Errorf("stale duration: %w", err)
		}
		if d <= 0 {
			return Condition{}, fmt.Errorf("stale duration must be positive, got %q", payload)
		}
		return Condition{Kind: "stale", Duration: d}, nil
	default:
		return Condition{}, fmt.Errorf("unknown condition kind %q (supported: stale)", kind)
	}
}

// parseDuration parses a wiki-friendly duration string: <int><unit>
// where unit ∈ {d, m, y}. Not overloaded with Go's time.ParseDuration
// units because "m = minutes" would be catastrophic in this context —
// a rule meant "30 months stale" that fired at "30 minutes stale" would
// flood the todo store instantly.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	// Split trailing unit letter from the numeric prefix.
	unit := s[len(s)-1:]
	digits := s[:len(s)-1]
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, fmt.Errorf("bad numeric prefix in %q: %w", s, err)
	}
	var days int
	switch strings.ToLower(unit) {
	case "d":
		days = n
	case "m":
		days = n * 30
	case "y":
		days = n * 365
	default:
		return 0, fmt.Errorf("unknown duration unit %q in %q (expected d/m/y)", unit, s)
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// splitCSV splits a comma-separated list, trimming whitespace and
// dropping empty entries. Order is preserved on the way in but the
// caller sorts before hashing.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// kv is the internal (key, value) pair yielded by parseKV.
type kv struct {
	Key, Value string
}

// parseKV parses `key=value` or `key="value with spaces"` pairs. Adapted
// from reviewflow's parser to keep the lifecycle package free of a
// cross-package dependency on it; the two implementations are simple
// enough that duplicating is cheaper than plumbing.
func parseKV(s string) []kv {
	var out []kv
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		key := strings.TrimSpace(s[:eq])
		s = s[eq+1:]
		s = strings.TrimLeft(s, " \t")
		var val string
		if len(s) > 0 && s[0] == '"' {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				val = s[1:]
				s = ""
			} else {
				val = s[1 : end+1]
				s = s[end+2:]
			}
		} else {
			sp := strings.IndexAny(s, " \t")
			if sp < 0 {
				val = s
				s = ""
			} else {
				val = s[:sp]
				s = s[sp:]
			}
		}
		if key != "" {
			out = append(out, kv{Key: key, Value: val})
		}
		s = strings.TrimLeft(s, " \t")
	}
	return out
}
