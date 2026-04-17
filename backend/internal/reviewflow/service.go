package reviewflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gowiki/backend/internal/config"
	"gowiki/backend/internal/storage"
)

// PageReader provides read access to page content for bootstrapping state.
type PageReader interface {
	Get(pagePath string) (storage.Page, error)
}

// TodoIntegrator creates and manages todo tasks for reviewflow roles.
type TodoIntegrator interface {
	// CreateReviewTasks creates one todo task per role that needs confirmation.
	// Called when a page is invalidated (new version saved).
	CreateReviewTasks(pagePath string, roles map[string]string, versionTag string, dueDate string) error
	// CancelReviewTasks cancels any open reviewflow todo tasks for a page.
	// Used when the reviewflow directive is removed or the page content changes
	// (old review obsolete, not performed).
	CancelReviewTasks(pagePath string) error
	// CompleteReviewTasks marks the reviewflow tasks whose (role, user) pair
	// appears in confirmedByRole as done. Used when all roles confirm and the
	// version becomes fully validated — the review actually happened.
	CompleteReviewTasks(pagePath string, confirmedByRole map[string]string) (int, error)
}

// Service implements reviewflow business logic.
type Service struct {
	store            *Store
	attic            *storage.Attic
	configStore      *config.Store
	pageReader       PageReader
	todo             TodoIntegrator
	signingVerifier  *SigningVerifier
	certStore        *CertStore
	caStore          *CAStore
	groupResolver    func(username string) []string
}

// SetSigningVerifier sets the signing verifier for cryptographic confirmations.
func (svc *Service) SetSigningVerifier(sv *SigningVerifier) {
	svc.signingVerifier = sv
}

// SetCertStore sets the certificate store.
func (svc *Service) SetCertStore(cs *CertStore) {
	svc.certStore = cs
}

// SetCAStore sets the CA store for audit exports.
func (svc *Service) SetCAStore(cas *CAStore) {
	svc.caStore = cas
}

// SetGroupResolver sets the function that resolves a user's effective groups.
func (svc *Service) SetGroupResolver(resolver func(string) []string) {
	svc.groupResolver = resolver
}

// IsObserver returns true if the user is in the global observer list
// (either directly by username or via a group membership).
func (svc *Service) IsObserver(username string, groups []string) bool {
	cfg := svc.configStore.Get()
	for _, entry := range cfg.Reviewflow.Observers {
		if entry == username {
			return true
		}
		if strings.HasPrefix(entry, "group:") {
			groupName := strings.TrimPrefix(entry, "group:")
			for _, g := range groups {
				if g == groupName {
					return true
				}
			}
		}
	}
	return false
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

// SetTodoIntegrator sets the todo integration for creating review tasks.
func (svc *Service) SetTodoIntegrator(ti TodoIntegrator) {
	svc.todo = ti
}

// ReconcileValidatedTasks scans every reviewflow state file and marks review
// todo tasks done for each (role, user) confirmation recorded for the current
// page version — including partial confirmations where not all roles have
// signed yet. Repairs historical drift (migrated validations, earlier buggy
// integrations) and is idempotent: tasks already done are skipped.
func (svc *Service) ReconcileValidatedTasks() (int, error) {
	if svc.todo == nil {
		return 0, nil
	}
	total := 0
	err := svc.store.WalkStates(func(pagePath string, st *State) error {
		if st == nil || st.CurrentPageVersion == 0 {
			return nil
		}
		confirmed := make(map[string]string)
		for _, c := range st.Confirmations {
			if c.PageVersion == st.CurrentPageVersion {
				confirmed[c.Role] = c.User
			}
		}
		if len(confirmed) == 0 {
			return nil
		}
		n, err := svc.todo.CompleteReviewTasks(pagePath, confirmed)
		if err != nil {
			return nil
		}
		total += n
		return nil
	})
	return total, err
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
			if svc.todo != nil {
				_ = svc.todo.CancelReviewTasks(pagePath)
			}
			return svc.store.Save(pagePath, st)
		}
		return nil
	}

	if st == nil {
		st = &State{}
	}

	// If page version changed, reset confirmations (content changed)
	// and create todo tasks for each role.
	if st.CurrentPageVersion != pageVersion {
		st.Confirmations = nil
		if svc.todo != nil {
			// Cancel any existing review tasks first.
			_ = svc.todo.CancelReviewTasks(pagePath)
			// Compute due date from deadline config.
			dueDate := svc.computeDueDate(roles)
			_ = svc.todo.CreateReviewTasks(pagePath, roles, versionTag, dueDate)
		}
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
func (svc *Service) Confirm(pagePath, role, user string, opts *ConfirmOpts) (*Status, error) {
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
	conf := Confirmation{
		PageVersion: st.CurrentPageVersion,
		Role:        role,
		User:        user,
		Timestamp:   time.Now().UTC(),
		VersionTag:  st.VersionTag,
	}
	if opts != nil {
		conf.Signature = opts.Signature
		conf.Digest = opts.Digest
		conf.CertFingerprint = opts.CertFingerprint
		conf.CertificatePEM = opts.CertificatePEM
		conf.TimestampToken = opts.TimestampToken
	}
	st.Confirmations = append(st.Confirmations, conf)

	// Mark this user's specific review task as done right away — they
	// performed their review, regardless of whether other roles have confirmed.
	if svc.todo != nil {
		_, _ = svc.todo.CompleteReviewTasks(pagePath, map[string]string{role: user})
	}

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

		// Mark review tasks done — all roles confirmed (reviews actually performed).
		if svc.todo != nil {
			_, _ = svc.todo.CompleteReviewTasks(pagePath, confirmedBy)
		}

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

// GetStatusForVersion returns the reviewflow status as of a specific page version.
// Used when viewing historical versions.
func (svc *Service) GetStatusForVersion(pagePath string, version int64) (*Status, error) {
	st, err := svc.store.Load(pagePath)
	if err != nil {
		return nil, err
	}
	if st == nil || len(st.Roles) == 0 {
		return &Status{
			Roles:        make(map[string]string),
			MissingRoles: make(map[string]string),
		}, nil
	}

	// Check if this version was fully validated in history.
	for _, vr := range st.VersionHistory {
		if vr.PageVersion == version {
			// Fully validated version — all roles confirmed.
			return &Status{
				Roles:            st.Roles,
				VersionTag:       vr.VersionTag,
				CurrentPageVer:   version,
				ValidatedVersion: version,
				MissingRoles:     make(map[string]string),
				IsFullyValidated: true,
				VersionHistory:   st.VersionHistory,
			}, nil
		}
	}

	// Not fully validated — check which roles had confirmations for this version.
	confirmed := make(map[string]bool)
	for _, c := range st.Confirmations {
		if c.PageVersion == version {
			confirmed[c.Role] = true
		}
	}

	missing := make(map[string]string)
	for role, user := range st.Roles {
		if !confirmed[role] {
			missing[role] = user
		}
	}

	return &Status{
		Roles:            st.Roles,
		VersionTag:       st.VersionTag,
		CurrentPageVer:   version,
		ValidatedVersion: st.ValidatedVersion,
		MissingRoles:     missing,
		IsFullyValidated: false,
		VersionHistory:   st.VersionHistory,
	}, nil
}

// computeDueDate returns a YYYY-MM-DD due date based on the shortest
// configured deadline for any of the given roles. Returns "" if no deadlines.
// IsPageReviewPending returns true if the page has a reviewflow with roles
// that are not all confirmed for the current version.
func (svc *Service) IsPageReviewPending(pagePath string) bool {
	st, err := svc.store.Load(pagePath)
	if err != nil || st == nil || len(st.Roles) == 0 {
		return false // no reviewflow on this page
	}
	confirmed := make(map[string]bool)
	for _, c := range st.Confirmations {
		if c.PageVersion == st.CurrentPageVersion {
			confirmed[c.Role] = true
		}
	}
	for role := range st.Roles {
		if !confirmed[role] {
			return true
		}
	}
	return false
}

func (svc *Service) computeDueDate(roles map[string]string) string {
	cfg := svc.configStore.Get()
	if !cfg.Reviewflow.Enabled || len(cfg.Reviewflow.Deadlines) == 0 {
		return ""
	}

	var shortest time.Duration
	for role := range roles {
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
		if shortest == 0 || dur < shortest {
			shortest = dur
		}
	}
	if shortest == 0 {
		return ""
	}
	return time.Now().UTC().Add(shortest).Format("2006-01-02")
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

	// Signing status.
	cfg2 := cfg.Reviewflow.Signing
	if cfg2.Enabled {
		status.SigningEnabled = true
		status.SigningRequired = cfg2.Required
		// Collect roles that have cryptographic signatures.
		for _, c := range st.Confirmations {
			if c.PageVersion == st.CurrentPageVersion && c.Signature != "" {
				status.SignedRoles = append(status.SignedRoles, c.Role)
			}
		}
	}

	return status, nil
}
