package importer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ImportAuth imports DokuWiki users and ACL rules into Gowiki's JSON format.
// It looks for conf/users.auth.php and conf/acl.auth.php under opts.SrcDir.
// Output: users.json, groups.json, acl.json under opts.DestDir/meta/.
func ImportAuth(opts Options) error {
	confDir := filepath.Join(opts.SrcDir, "conf")
	metaDir := filepath.Join(opts.DestDir, "meta")

	usersFile := filepath.Join(confDir, "users.auth.php")
	aclFile := filepath.Join(confDir, "acl.auth.php")

	// Check if at least one file exists.
	usersExist := fileExists(usersFile)
	aclExists := fileExists(aclFile)
	if !usersExist && !aclExists {
		log.Printf("  No conf/users.auth.php or conf/acl.auth.php found, skipping auth import")
		return nil
	}

	log.Printf("Importing auth data from %s", confDir)

	// Parse users and collect groups.
	var users []gowikiUser
	groupSet := map[string]bool{"admin": true, "editors": true} // always present

	if usersExist {
		var err error
		users, err = parseDokuWikiUsers(usersFile)
		if err != nil {
			return fmt.Errorf("parse users: %w", err)
		}
		for _, u := range users {
			for _, g := range u.Groups {
				groupSet[g] = true
			}
		}
		log.Printf("  Parsed %d users", len(users))
	}

	// Parse ACL rules.
	var rules []gowikiACLRule
	skippedUserRules := 0
	if aclExists {
		var err error
		rules, skippedUserRules, err = parseDokuWikiACL(aclFile)
		if err != nil {
			return fmt.Errorf("parse acl: %w", err)
		}
		// Collect groups referenced in ACL rules.
		for _, r := range rules {
			if r.SubjectType == "group" {
				groupSet[r.Subject] = true
			}
		}
		log.Printf("  Parsed %d ACL rules", len(rules))
		if skippedUserRules > 0 {
			log.Printf("  Skipped %d %%USER%% template ACL rules (not supported)", skippedUserRules)
		}
	}

	// Build groups list.
	var groups []gowikiGroup
	for name := range groupSet {
		desc := ""
		switch name {
		case "admin":
			desc = "Administrators"
		case "editors":
			desc = "Can edit all pages"
		}
		groups = append(groups, gowikiGroup{Name: name, Description: desc})
	}

	if opts.DryRun {
		log.Printf("  DRY RUN: would write %d users, %d groups, %d ACL rules", len(users), len(groups), len(rules))
		return nil
	}

	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return fmt.Errorf("create meta dir: %w", err)
	}

	// Write users.json.
	if len(users) > 0 {
		if err := writeJSON(filepath.Join(metaDir, "users.json"), users); err != nil {
			return fmt.Errorf("write users.json: %w", err)
		}
		log.Printf("  Wrote %d users to users.json", len(users))
	}

	// Write groups.json.
	if err := writeJSON(filepath.Join(metaDir, "groups.json"), groups); err != nil {
		return fmt.Errorf("write groups.json: %w", err)
	}
	log.Printf("  Wrote %d groups to groups.json", len(groups))

	// Write acl.json.
	if len(rules) > 0 {
		if err := writeJSON(filepath.Join(metaDir, "acl.json"), rules); err != nil {
			return fmt.Errorf("write acl.json: %w", err)
		}
		log.Printf("  Wrote %d ACL rules to acl.json", len(rules))
	}

	return nil
}

// ---- Gowiki JSON types (mirror auth package, but without store logic) ----

type gowikiUser struct {
	Username     string   `json:"username"`
	PasswordHash string   `json:"password_hash"`
	Email        string   `json:"email"`
	DisplayName  string   `json:"display_name"`
	Groups       []string `json:"groups"`
	Disabled     bool     `json:"disabled"`
	CreatedAt    string   `json:"created_at"`
}

type gowikiGroup struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type gowikiACLRule struct {
	Pattern     string   `json:"pattern"`
	SubjectType string   `json:"subject_type"`
	Subject     string   `json:"subject"`
	Permissions []string `json:"permissions"`
}

// ---- DokuWiki users.auth.php parser ----

// parseDokuWikiUsers parses conf/users.auth.php.
// Format: login:hash:Real Name:email:group1,group2
// Bcrypt hashes ($2y$) are converted to Go-compatible $2a$ prefix.
func parseDokuWikiUsers(path string) ([]gowikiUser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	var users []gowikiUser
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<?") || strings.HasPrefix(line, "//") {
			continue
		}

		parts := strings.SplitN(line, ":", 5)
		if len(parts) < 5 {
			log.Printf("  WARNING: skipping malformed user line: %s", line)
			continue
		}

		login := parts[0]
		hash := convertBcryptHash(parts[1])
		realName := parts[2]
		email := parts[3]
		groupsStr := parts[4]

		var groups []string
		for _, g := range strings.Split(groupsStr, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				groups = append(groups, g)
			}
		}
		if groups == nil {
			groups = []string{}
		}

		users = append(users, gowikiUser{
			Username:     login,
			PasswordHash: hash,
			Email:        email,
			DisplayName:  realName,
			Groups:       groups,
			Disabled:     false,
			CreatedAt:    now,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read users file: %w", err)
	}

	return users, nil
}

// convertBcryptHash converts PHP bcrypt ($2y$) hashes to Go-compatible ($2a$) format.
// Both use the same algorithm; only the version prefix differs.
// Non-bcrypt hashes (MD5-crypt, phpass) are returned empty since they can't be used.
func convertBcryptHash(hash string) string {
	if strings.HasPrefix(hash, "$2y$") {
		return "$2a$" + hash[4:]
	}
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") {
		return hash
	}
	// Non-bcrypt hash — can't reuse.
	return ""
}

// ---- DokuWiki acl.auth.php parser ----

// DokuWiki permission levels (bitmask):
//
//	0 = none, 1 = read, 2 = edit, 4 = create, 8 = upload, 16 = delete, 255 = admin
const (
	dokuPermRead   = 1
	dokuPermEdit   = 2
	dokuPermDelete = 16
)

// dokuPermToGowiki maps a DokuWiki numeric permission level to Gowiki permissions.
func dokuPermToGowiki(level int) []string {
	if level <= 0 {
		return []string{}
	}
	if level >= dokuPermDelete {
		return []string{"view", "edit", "delete"}
	}
	if level >= dokuPermEdit {
		return []string{"view", "edit"}
	}
	if level >= dokuPermRead {
		return []string{"view"}
	}
	return []string{}
}

// parseDokuWikiACL parses conf/acl.auth.php.
// Format: path\t@group_or_user\tpermission_level
// Returns (rules, skippedUserTemplateCount, error).
func parseDokuWikiACL(path string) ([]gowikiACLRule, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var rules []gowikiACLRule
	skippedUserRules := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<?") {
			continue
		}

		// Split on whitespace (tabs or spaces).
		fields := strings.Fields(line)
		if len(fields) < 3 {
			log.Printf("  WARNING: skipping malformed ACL line: %s", line)
			continue
		}

		dokuPath := fields[0]
		subject := fields[1]
		levelStr := fields[2]

		// Skip %USER% template rules — DokuWiki per-user ACL placeholders
		// that have no direct equivalent in Gowiki's regex-based ACL.
		if strings.Contains(dokuPath, "%USER%") || subject == "%USER%" {
			skippedUserRules++
			continue
		}

		level, err := strconv.Atoi(levelStr)
		if err != nil {
			log.Printf("  WARNING: skipping ACL line with non-numeric level: %s", line)
			continue
		}

		// URL-decode path and subject (DokuWiki uses %5f for _, etc.)
		dokuPath = dokuURLDecode(dokuPath)
		subject = dokuURLDecode(subject)

		// Convert DokuWiki path to Gowiki regex pattern.
		pattern := dokuACLPathToPattern(dokuPath)

		// Convert subject: @group -> group subject, else user subject.
		subjectType := "user"
		subjectName := subject
		if strings.HasPrefix(subject, "@") {
			subjectType = "group"
			subjectName = strings.TrimPrefix(subject, "@")
		}
		// DokuWiki uses @ALL for unauthenticated/all users.
		if strings.EqualFold(subjectName, "ALL") && subjectType == "group" {
			subjectType = "special"
			subjectName = "@all"
		}

		permissions := dokuPermToGowiki(level)

		rules = append(rules, gowikiACLRule{
			Pattern:     pattern,
			SubjectType: subjectType,
			Subject:     subjectName,
			Permissions: permissions,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("read acl file: %w", err)
	}

	return rules, skippedUserRules, nil
}

// dokuURLDecode decodes DokuWiki's URL-encoded strings (e.g., %5f -> _).
func dokuURLDecode(s string) string {
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s // fall back to original on error
	}
	return decoded
}

// dokuACLPathToPattern converts a DokuWiki ACL path to a Gowiki regex pattern.
//
// DokuWiki uses colon as namespace separator:
//
//	*          -> .* (all pages, root)
//	ns:*       -> ns/.* (all pages under ns/)
//	ns:page    -> ns/page (exact page)
//	ns:sub:*   -> ns/sub/.* (all pages under ns/sub/)
func dokuACLPathToPattern(dokuPath string) string {
	// Replace colon separators with slashes.
	p := strings.ReplaceAll(dokuPath, ":", "/")

	// Handle wildcard.
	if p == "*" {
		return ".*"
	}
	if strings.HasSuffix(p, "/*") {
		// ns/* -> ns/.*
		return strings.TrimSuffix(p, "/*") + "/.*"
	}

	// Exact page match — escape regex metacharacters in the path.
	return regexpEscapePath(p)
}

// regexpEscapePath escapes regex metacharacters in a path string,
// preserving / as literal.
func regexpEscapePath(s string) string {
	var b strings.Builder
	for _, ch := range s {
		switch ch {
		case '.', '+', '?', '(', ')', '[', ']', '{', '}', '\\', '^', '$', '|':
			b.WriteByte('\\')
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// ---- helpers ----

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
