package reviewflow

import (
	"encoding/json"
	"fmt"
	"sort"
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
	store           *Store
	attic           *storage.Attic
	configStore     *config.Store
	pageReader      PageReader
	todo            TodoIntegrator
	signingVerifier *SigningVerifier
	certStore       *CertStore
	caStore         *CAStore
	groupResolver   func(username string) []string
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

// AddRevokedFingerprint appends fingerprint to the config revocation list
// if it is not already present. Both the explicit revoke handler and the
// cascade-on-re-issue callback route through here so the "already listed?
// then append" logic lives in one place. Safe to call with a nil
// configStore — it becomes a no-op, matching the pattern the rest of the
// service uses for optional deps.
func (svc *Service) AddRevokedFingerprint(fingerprint string, at time.Time) {
	if svc.configStore == nil || fingerprint == "" {
		return
	}
	cfg := svc.configStore.Get()
	for _, rc := range cfg.Reviewflow.Signing.RevokedCerts {
		if rc.Fingerprint == fingerprint {
			return
		}
	}
	cfg.Reviewflow.Signing.RevokedCerts = append(cfg.Reviewflow.Signing.RevokedCerts, config.RevokedCert{
		Fingerprint: fingerprint,
		RevokedAt:   at.UTC().Format(time.RFC3339),
	})
	svc.configStore.Update(cfg)
}

// IsInvolved returns true if the user is part of a page's reviewflow
// chain in any capacity: assigned to any role (author, reviewer,
// validator, or custom), OR a global observer. Used by the page-serve
// path to decide whether an uninvolved reader should be gated away
// from an unsigned draft.
//
// The `pagePath` may reference a page with no reviewflow — in that
// case there are no roles and the observer check still applies (a
// global observer sees everything, whether or not the page has a
// {reviewflow} directive). An empty username → not involved.
func (svc *Service) IsInvolved(pagePath, username string, groups []string) bool {
	if username == "" {
		return false
	}
	// Global observers always count as involved.
	if svc.IsObserver(username, groups) {
		return true
	}
	st, err := svc.store.Load(pagePath)
	if err != nil || st == nil {
		return false
	}
	// A user is involved if they hold ANY role on this page. Role
	// values may reference a group with the "@group" prefix per the
	// group resolver's convention; expand those and check membership.
	for _, assignee := range st.Roles {
		if assignee == "" {
			continue
		}
		if assignee == username {
			return true
		}
		if strings.HasPrefix(assignee, "@") {
			groupName := strings.TrimPrefix(assignee, "@")
			for _, g := range groups {
				if g == groupName {
					return true
				}
			}
		}
	}
	return false
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
	dir, found := ParseDirective(markdown)

	st, err := svc.store.Load(pagePath)
	if err != nil {
		return err
	}

	if !found {
		// Directive removed — clean up transient state but keep history.
		if st != nil {
			st.Roles = nil
			st.RoleOrder = nil
			st.VersionTag = ""
			st.Parallel = false
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

	// If page version changed, reset confirmations (content changed) —
	// but first snapshot the outgoing partial-signature state to history
	// so a same-digest restore (discard-draft or restore-from-history)
	// can re-attach the signatures, then look at every historical
	// confirmation whose Digest matches the NEW page content and carry
	// those forward onto the new page version. Signatures are computed
	// over a content digest (see signing.go:ComputeDigest), so a byte-
	// identical restore keeps them cryptographically valid.
	if st.CurrentPageVersion != pageVersion {
		// Snapshot the outgoing state if it carried unsnapshotted
		// confirmations. Skip when a VersionRecord for the outgoing
		// version already exists — the all-confirmed branch of Confirm
		// snapshots at validation time, so we'd otherwise double-write
		// the same fully-validated record on the next edit.
		if len(st.Confirmations) > 0 && findVersionRecord(st.VersionHistory, st.CurrentPageVersion) < 0 {
			st.VersionHistory = append(st.VersionHistory, snapshotVersionRecord(st))
		}

		// Recompute the incoming digest and pull any historical
		// confirmation that matches into the new version. Empty digest
		// means the caller confirmed without signing, so those never
		// re-attach — they wouldn't survive a real audit anyway.
		newDigest := ComputeDigest([]byte(markdown))
		st.Confirmations = reattachByDigest(st, pageVersion, newDigest)

		// Only cancel + recreate review tasks when NOTHING re-attached
		// (i.e. this is a genuine content change, not a same-digest
		// restore). A restore that brought back a fully-validated set
		// leaves the review dashboard alone — no phantom tasks for
		// signatures that are already in place.
		if svc.todo != nil && len(st.Confirmations) == 0 {
			_ = svc.todo.CancelReviewTasks(pagePath)
			dueDate := svc.computeDueDate(dir.Roles)
			if dir.Parallel {
				_ = svc.todo.CreateReviewTasks(pagePath, dir.Roles, dir.VersionTag, dueDate)
			} else {
				first := firstOrderedRole(dir.RoleOrder, dir.Roles)
				if first != "" {
					_ = svc.todo.CreateReviewTasks(pagePath, map[string]string{first: dir.Roles[first]}, dir.VersionTag, dueDate)
				}
			}
		}
	}

	st.Roles = dir.Roles
	st.RoleOrder = dir.RoleOrder
	st.VersionTag = dir.VersionTag
	st.Parallel = dir.Parallel
	st.CurrentPageVersion = pageVersion

	// A same-digest restore may have re-attached confirmations covering
	// every role. In that case the new page version is already validated
	// end-to-end and we need to run the same finalize path Confirm() uses
	// so ValidatedVersion, review tasks, attic meta and the VersionRecord
	// are all consistent.
	if len(st.Confirmations) > 0 && svc.allConfirmed(st) {
		svc.finalizeValidatedVersion(pagePath, st)
	}

	return svc.store.Save(pagePath, st)
}

// firstOrderedRole returns the first role in `order` that also appears in
// `roles`. Both are inputs from the same directive parse, so in practice
// every entry in `order` is present in `roles`; the guard is there to
// tolerate a state file whose RoleOrder was migrated from a nil legacy
// value and might have drifted.
func firstOrderedRole(order []string, roles map[string]string) string {
	for _, r := range order {
		if _, ok := roles[r]; ok {
			return r
		}
	}
	// Fallback: if the order slice is empty (old state file, first save
	// after upgrade), pick any role deterministically so the flow can
	// still make progress. Sorting means we don't depend on Go's random
	// map iteration.
	if len(order) == 0 && len(roles) > 0 {
		var keys []string
		for k := range roles {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys[0]
	}
	return ""
}

// nextUnconfirmedRole returns the next role in state.RoleOrder that has
// not been confirmed for the current page version. Empty string means
// every role is confirmed. Used by the sequential-notification path
// after each Confirm.
func nextUnconfirmedRole(st *State) string {
	if st == nil || len(st.RoleOrder) == 0 {
		return ""
	}
	confirmed := make(map[string]bool)
	for _, c := range st.Confirmations {
		if c.PageVersion == st.CurrentPageVersion {
			confirmed[c.Role] = true
		}
	}
	for _, r := range st.RoleOrder {
		if _, assigned := st.Roles[r]; !assigned {
			continue
		}
		if !confirmed[r] {
			return r
		}
	}
	return ""
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
	dir, found := ParseDirective(page.Markdown)
	if !found || len(dir.Roles) == 0 {
		return nil, fmt.Errorf("no reviewflow directive on page %s", pagePath)
	}

	if st == nil {
		st = &State{}
	}
	st.Roles = dir.Roles
	st.RoleOrder = dir.RoleOrder
	st.VersionTag = dir.VersionTag
	st.Parallel = dir.Parallel
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

	// Sequential-notification chain: if this page is in sequential mode
	// (the default) and the just-confirmed role was the current head of
	// the chain, hand the baton to the next unconfirmed role by creating
	// its review task. `nextUnconfirmedRole` returns "" when the just
	// -confirmed role wasn't the head (unusual — only happens when a
	// role signs out-of-order in parallel mode) or when everyone else
	// is already confirmed (the allConfirmed block below closes the
	// version out).
	if !st.Parallel && svc.todo != nil {
		if next := nextUnconfirmedRole(st); next != "" && next != role {
			dueDate := svc.computeDueDate(st.Roles)
			_ = svc.todo.CreateReviewTasks(pagePath, map[string]string{next: st.Roles[next]}, st.VersionTag, dueDate)
		}
	}

	// Check if all roles are now confirmed.
	if svc.allConfirmed(st) {
		svc.finalizeValidatedVersion(pagePath, st)
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

// ── Signature preservation across version bumps ─────────────────────────
//
// A version bump used to unconditionally wipe st.Confirmations, so a
// discard-draft-that-returned-to-the-last-published-bytes or a restore-
// from-history threw away signatures that were still cryptographically
// valid (they are digest-bound — see signing.go:ComputeDigest — and the
// digest of the restored bytes matches the digest they were signed over).
// The four helpers below let SyncFromMarkdown snapshot the outgoing
// state to history before the wipe and then re-attach any historical
// confirmation whose digest matches the incoming content.

// findVersionRecord returns the index of the VersionRecord for the given
// page version, or -1 if none exists. Used to skip the pre-wipe snapshot
// when the outgoing version is already in history (all-confirmed branch
// of Confirm() snapshots at validation time; without this guard we'd
// double-write the same record on the next edit).
func findVersionRecord(history []VersionRecord, pageVersion int64) int {
	for i, vr := range history {
		if vr.PageVersion == pageVersion {
			return i
		}
	}
	return -1
}

// snapshotVersionRecord captures the current Confirmations set on
// st.CurrentPageVersion as a VersionRecord that will land in history
// right before the wipe. Called only when st.Confirmations is non-empty
// and no record for that version exists yet.
func snapshotVersionRecord(st *State) VersionRecord {
	confirmedBy := make(map[string]string)
	confs := make([]Confirmation, 0, len(st.Confirmations))
	for _, c := range st.Confirmations {
		if c.PageVersion != st.CurrentPageVersion {
			continue
		}
		confirmedBy[c.Role] = c.User
		confs = append(confs, c)
	}
	return VersionRecord{
		PageVersion:   st.CurrentPageVersion,
		Timestamp:     time.Now().UTC(),
		ConfirmedBy:   confirmedBy,
		VersionTag:    st.VersionTag,
		Confirmations: confs,
	}
}

// reattachByDigest scans every historical confirmation on this page (the
// about-to-be-wiped current set plus every VersionRecord in history) and
// re-attaches, onto the new page version, any confirmation whose Digest
// matches the new content's digest. This is what makes a restore or a
// draft discard preserve signatures — the crypto payload is unchanged,
// only the page-version integer is rewritten. Empty digests never match
// (click-only, unsigned confirmations don't survive a bump — they had no
// crypto guarantee to preserve in the first place).
//
// When two candidates cover the same (role, user) pair, the most recent
// timestamp wins. That keeps the semantics of "if the user re-signed
// after their old signature, the new signature is what carries forward".
func reattachByDigest(st *State, newPageVersion int64, newDigest string) []Confirmation {
	if newDigest == "" {
		return nil
	}
	// Collect candidates: current Confirmations plus every historical set.
	candidates := make([]Confirmation, 0, len(st.Confirmations))
	candidates = append(candidates, st.Confirmations...)
	for _, vr := range st.VersionHistory {
		candidates = append(candidates, vr.Confirmations...)
	}
	// Keep only digest matches, dedup by (role, user) keeping newest.
	type key struct{ role, user string }
	best := make(map[key]Confirmation)
	for _, c := range candidates {
		if c.Digest == "" || c.Digest != newDigest {
			continue
		}
		k := key{c.Role, c.User}
		if prev, ok := best[k]; ok && !c.Timestamp.After(prev.Timestamp) {
			continue
		}
		best[k] = c
	}
	if len(best) == 0 {
		return nil
	}
	out := make([]Confirmation, 0, len(best))
	for _, c := range best {
		c.PageVersion = newPageVersion
		out = append(out, c)
	}
	// Stable order for deterministic on-disk output.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].User < out[j].User
	})
	return out
}

// finalizeValidatedVersion is the "all roles confirmed on the current
// page version" wrap-up: record the VersionRecord (with the full crypto
// payload), bump ValidatedVersion, complete review tasks, and stamp the
// attic entry. Called from Confirm() when a role confirmation completes
// the set, and from SyncFromMarkdown when a re-attach after a same-digest
// restore already covers every role.
func (svc *Service) finalizeValidatedVersion(pagePath string, st *State) {
	confirmedBy := make(map[string]string)
	confs := make([]Confirmation, 0, len(st.Confirmations))
	for _, c := range st.Confirmations {
		if c.PageVersion != st.CurrentPageVersion {
			continue
		}
		confirmedBy[c.Role] = c.User
		confs = append(confs, c)
	}
	vr := VersionRecord{
		PageVersion:   st.CurrentPageVersion,
		Timestamp:     time.Now().UTC(),
		ConfirmedBy:   confirmedBy,
		VersionTag:    st.VersionTag,
		Confirmations: confs,
		IsValidated:   true,
	}
	// If a partial-snapshot record for this version already exists (the
	// wipe path pre-snapshotted before re-attach covered everyone), replace
	// it with the fully-validated one; else append.
	if idx := findVersionRecord(st.VersionHistory, st.CurrentPageVersion); idx >= 0 {
		st.VersionHistory[idx] = vr
	} else {
		st.VersionHistory = append(st.VersionHistory, vr)
	}
	st.ValidatedVersion = st.CurrentPageVersion

	if svc.todo != nil {
		_, _ = svc.todo.CompleteReviewTasks(pagePath, confirmedBy)
	}
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
