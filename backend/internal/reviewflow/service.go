package reviewflow

import (
	"encoding/json"
	"fmt"
	"time"

	"gowiki/backend/internal/config"
	"gowiki/backend/internal/storage"
)

// PageReader provides read access to page content for bootstrapping state.
type PageReader interface {
	Get(pagePath string) (storage.Page, error)
}

// Service implements reviewflow business logic.
type Service struct {
	store       *Store
	attic       *storage.Attic
	configStore *config.Store
	pageReader  PageReader
}

func NewService(store *Store, attic *storage.Attic, configStore *config.Store) *Service {
	return &Service{
		store:       store,
		attic:       attic,
		configStore: configStore,
	}
}

// SetPageReader sets the page reader for bootstrapping state from existing pages.
func (svc *Service) SetPageReader(pr PageReader) {
	svc.pageReader = pr
}

// SyncFromMarkdown parses the reviewflow directive from page markdown and
// updates the stored state. Called on every page save.
func (svc *Service) SyncFromMarkdown(pagePath string, pageVersion int64, markdown string) error {
	roles, versionTag, found := ParseDirective(markdown)

	st, err := svc.store.Load(pagePath)
	if err != nil {
		return err
	}

	if !found {
		// Directive removed — clean up transient state but keep history.
		if st != nil {
			st.Roles = nil
			st.VersionTag = ""
			st.Confirmations = nil
			st.CurrentPageVersion = pageVersion
			return svc.store.Save(pagePath, st)
		}
		return nil
	}

	if st == nil {
		st = &State{}
	}

	// If page version changed, reset confirmations (content changed).
	if st.CurrentPageVersion != pageVersion {
		st.Confirmations = nil
	}

	st.Roles = roles
	st.VersionTag = versionTag
	st.CurrentPageVersion = pageVersion

	return svc.store.Save(pagePath, st)
}

// EnsureState loads the reviewflow state for a page, or bootstraps it from
// the current page content if no state file exists yet. This handles pages
// that were saved before the reviewflow plugin was deployed.
func (svc *Service) EnsureState(pagePath string) (*State, error) {
	st, err := svc.store.Load(pagePath)
	if err != nil {
		return nil, err
	}
	if st != nil && len(st.Roles) > 0 {
		return st, nil
	}

	// State doesn't exist or has no roles — try to bootstrap from the page content.
	if svc.pageReader == nil {
		return nil, fmt.Errorf("no reviewflow state for page %s (page reader not configured)", pagePath)
	}
	page, err := svc.pageReader.Get(pagePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read page %s to bootstrap reviewflow: %w", pagePath, err)
	}
	roles, versionTag, found := ParseDirective(page.Markdown)
	if !found || len(roles) == 0 {
		return nil, fmt.Errorf("no reviewflow directive on page %s", pagePath)
	}

	if st == nil {
		st = &State{}
	}
	st.Roles = roles
	st.VersionTag = versionTag
	st.CurrentPageVersion = page.Meta.Version
	if err := svc.store.Save(pagePath, st); err != nil {
		return nil, err
	}
	return st, nil
}

// Confirm records a role confirmation for the current page version.
func (svc *Service) Confirm(pagePath, role, user string) (*Status, error) {
	st, err := svc.EnsureState(pagePath)
	if err != nil {
		return nil, err
	}

	// Check that the role exists and is assigned to this user.
	assignedUser, ok := st.Roles[role]
	if !ok {
		return nil, fmt.Errorf("role %q not defined for page %s", role, pagePath)
	}
	if assignedUser != user {
		return nil, fmt.Errorf("role %q is assigned to %q, not %q", role, assignedUser, user)
	}

	// Check not already confirmed for this version.
	for _, c := range st.Confirmations {
		if c.Role == role && c.PageVersion == st.CurrentPageVersion {
			return svc.computeStatus(pagePath, st)
		}
	}

	// Record confirmation.
	st.Confirmations = append(st.Confirmations, Confirmation{
		PageVersion: st.CurrentPageVersion,
		Role:        role,
		User:        user,
		Timestamp:   time.Now().UTC(),
		VersionTag:  st.VersionTag,
	})

	// Check if all roles are now confirmed.
	if svc.allConfirmed(st) {
		confirmedBy := make(map[string]string)
		for _, c := range st.Confirmations {
			if c.PageVersion == st.CurrentPageVersion {
				confirmedBy[c.Role] = c.User
			}
		}
		vr := VersionRecord{
			PageVersion: st.CurrentPageVersion,
			Timestamp:   time.Now().UTC(),
			ConfirmedBy: confirmedBy,
			VersionTag:  st.VersionTag,
		}
		st.VersionHistory = append(st.VersionHistory, vr)
		st.ValidatedVersion = st.CurrentPageVersion

		// Update attic entry with reviewflow metadata.
		if svc.attic != nil {
			meta := AtticMeta{
				VersionTag:  st.VersionTag,
				ConfirmedBy: confirmedBy,
				IsValidated: true,
			}
			metaJSON, _ := json.Marshal(meta)
			_ = svc.attic.UpdateEntryMeta(pagePath, st.CurrentPageVersion, "reviewflow", metaJSON)
		}
	}

	if err := svc.store.Save(pagePath, st); err != nil {
		return nil, err
	}

	return svc.computeStatus(pagePath, st)
}

// GetStatus returns the computed status for a page.
func (svc *Service) GetStatus(pagePath string) (*Status, error) {
	st, err := svc.EnsureState(pagePath)
	if err != nil {
		// If bootstrap fails, return empty status (page may not have a directive).
		return &Status{
			Roles:        make(map[string]string),
			MissingRoles: make(map[string]string),
		}, nil
	}
	return svc.computeStatus(pagePath, st)
}

func (svc *Service) allConfirmed(st *State) bool {
	confirmed := make(map[string]bool)
	for _, c := range st.Confirmations {
		if c.PageVersion == st.CurrentPageVersion {
			confirmed[c.Role] = true
		}
	}
	for role := range st.Roles {
		if !confirmed[role] {
			return false
		}
	}
	return true
}

func (svc *Service) computeStatus(pagePath string, st *State) (*Status, error) {
	if st == nil || len(st.Roles) == 0 {
		return &Status{
			Roles:        make(map[string]string),
			MissingRoles: make(map[string]string),
		}, nil
	}

	// Find confirmed roles for current version.
	confirmed := make(map[string]bool)
	for _, c := range st.Confirmations {
		if c.PageVersion == st.CurrentPageVersion {
			confirmed[c.Role] = true
		}
	}

	missing := make(map[string]string)
	for role, user := range st.Roles {
		if !confirmed[role] {
			missing[role] = user
		}
	}

	status := &Status{
		Roles:            st.Roles,
		VersionTag:       st.VersionTag,
		CurrentPageVer:   st.CurrentPageVersion,
		ValidatedVersion: st.ValidatedVersion,
		MissingRoles:     missing,
		IsFullyValidated: len(missing) == 0,
		VersionHistory:   st.VersionHistory,
	}

	// Compute deadlines and overdue roles.
	cfg := svc.configStore.Get()
	if cfg.Reviewflow.Enabled && len(cfg.Reviewflow.Deadlines) > 0 && len(missing) > 0 {
		deadlines := make(map[string]string)
		var overdue []string
		now := time.Now().UTC()

		// Find the baseline time for deadlines: earliest confirmation
		// for the current version, or the page save time from the attic.
		var baseline time.Time
		for _, c := range st.Confirmations {
			if c.PageVersion == st.CurrentPageVersion {
				if baseline.IsZero() || c.Timestamp.Before(baseline) {
					baseline = c.Timestamp
				}
			}
		}
		if baseline.IsZero() && svc.attic != nil {
			entry, _ := svc.attic.GetEntry(pagePath, st.CurrentPageVersion)
			if entry != nil {
				if t, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
					baseline = t
				}
			}
		}

		if !baseline.IsZero() {
			for role := range missing {
				durStr := cfg.Reviewflow.Deadlines[role]
				if durStr == "" {
					durStr = cfg.Reviewflow.Deadlines["_default"]
				}
				if durStr == "" {
					continue
				}
				dur, err := time.ParseDuration(durStr)
				if err != nil {
					continue
				}
				deadline := baseline.Add(dur)
				deadlines[role] = deadline.Format(time.RFC3339)
				if now.After(deadline) {
					overdue = append(overdue, role)
				}
			}
		}

		if len(deadlines) > 0 {
			status.Deadlines = deadlines
		}
		if len(overdue) > 0 {
			status.OverdueRoles = overdue
		}
	}

	return status, nil
}
