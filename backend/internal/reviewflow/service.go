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
	// ListOpenPagesWithReviewTasks returns the distinct source_page values
	// of every open or in-progress reviewflow task. Used by the orphan-task
	// reconciler to find tasks whose state file no longer exists.
	ListOpenPagesWithReviewTasks() ([]string, error)
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

// ReconcileStaleSignatures sweeps every reviewflow state and invalidates
// signed Confirmations whose stored Digest doesn't match the current
// page content. One-shot cleanup for states where some write path
// landed new content without going through SyncFromMarkdown's version-
// change branch — the digest-invariance check added to SyncFromMarkdown
// prevents the drift GOING FORWARD, but pre-existing stale signatures
// stay in state files until the next normal save of the page triggers
// a sync. Running this at startup fixes them all in one pass.
//
// Mirrors the SyncFromMarkdown invariant: a signed Confirmation whose
// Digest doesn't equal the current content's digest is removed; the
// pre-wipe state is snapshotted so the audit trail is kept;
// ValidatedVersion drops to 0 when the dropped set would otherwise
// leave a "validated with no signatures" state.
//
// Returns the number of state files that had at least one signature
// invalidated. Idempotent — a second run reports zero.
func (svc *Service) ReconcileStaleSignatures() (int, error) {
	if svc.pageReader == nil {
		return 0, nil
	}
	touched := 0
	err := svc.store.WalkStates(func(pagePath string, st *State) error {
		if st == nil || len(st.Confirmations) == 0 {
			return nil
		}
		page, err := svc.pageReader.Get(pagePath)
		if err != nil {
			// Page gone or unreadable — orphan-task reconciler handles
			// the state file, nothing more to do here.
			return nil
		}
		currentDigest := ComputeDigest([]byte(page.Markdown))
		staleCount := 0
		for _, c := range st.Confirmations {
			if c.Signature != "" && c.Digest != "" && c.Digest != currentDigest {
				staleCount++
			}
		}
		if staleCount == 0 {
			return nil
		}
		if findVersionRecord(st.VersionHistory, st.CurrentPageVersion) < 0 {
			st.VersionHistory = append(st.VersionHistory, snapshotVersionRecord(st))
		}
		kept := st.Confirmations[:0]
		for _, c := range st.Confirmations {
			if c.Signature != "" && c.Digest != "" && c.Digest != currentDigest {
				continue
			}
			kept = append(kept, c)
		}
		st.Confirmations = kept
		if st.ValidatedVersion > 0 && st.ValidatedVersion == st.CurrentPageVersion && !svc.allConfirmed(st) {
			st.ValidatedVersion = 0
		}
		if err := svc.store.Save(pagePath, st); err != nil {
			return nil
		}
		touched++
		return nil
	})
	return touched, err
}

// ReconcileOrphanTasks sweeps every reviewflow state file and brings its
// open review todos back in sync with the current state of the world. It
// handles three drift sources that accumulated before the lifecycle
// cleanups were in place:
//
//  1. The page was deleted but its review tasks survived.
//  2. The page is fully validated (ValidatedVersion == CurrentPageVersion)
//     but tasks were never cancelled or completed.
//  3. The page is still under review but its reviewflow directive changed
//     role assignments or version tag — tasks for the old (role, user, tag)
//     tuples are stale.
//
// For (1): cancel every open reviewflow task for the page AND delete the
// state file.
// For (2): cancel every open reviewflow task for the page (the page is
// done; no open task is legitimate).
// For (3): cancel every open task for the page and re-create one task per
// role NOT already satisfied by a re-attached confirmation for the current
// page version — the same end state a fresh SyncFromMarkdown would reach.
//
// The caller passes an `exists` predicate (storage.FileStore.Exists is the
// natural fit). Returns the number of state files whose task set was
// touched. Idempotent: a second run reports zero.
func (svc *Service) ReconcileOrphanTasks(exists func(pagePath string) bool) (int, error) {
	if svc.todo == nil || exists == nil {
		return 0, nil
	}
	touched := 0
	err := svc.store.WalkStates(func(pagePath string, st *State) error {
		if st == nil {
			return nil
		}
		// 1. Page gone.
		if !exists(pagePath) {
			_ = svc.todo.CancelReviewTasks(pagePath)
			_ = svc.store.Delete(pagePath)
			touched++
			return nil
		}
		// 2. Fully validated — no open task should remain.
		if st.CurrentPageVersion > 0 && st.ValidatedVersion == st.CurrentPageVersion {
			_ = svc.todo.CancelReviewTasks(pagePath)
			touched++
			return nil
		}
		// 3. Live state: cancel everything, recreate only missing-role
		//    tasks. Mirrors SyncFromMarkdown's version-bump branch so
		//    the invariant is written once.
		if len(st.Roles) == 0 {
			// Dormant state (directive removed earlier) — SyncFromMarkdown
			// already cancelled the tasks; nothing to do.
			return nil
		}
		_ = svc.todo.CancelReviewTasks(pagePath)
		satisfied := make(map[string]bool, len(st.Confirmations))
		for _, c := range st.Confirmations {
			if c.PageVersion == st.CurrentPageVersion {
				satisfied[c.Role] = true
			}
		}
		missing := make(map[string]string)
		for role, user := range st.Roles {
			if !satisfied[role] {
				missing[role] = user
			}
		}
		if len(missing) > 0 {
			dueDate := svc.computeDueDate(st.Roles)
			if st.Parallel {
				_ = svc.todo.CreateReviewTasks(pagePath, missing, st.VersionTag, dueDate)
			} else {
				for _, role := range st.RoleOrder {
					if user, ok := missing[role]; ok {
						_ = svc.todo.CreateReviewTasks(pagePath, map[string]string{role: user}, st.VersionTag, dueDate)
						break
					}
				}
			}
		}
		touched++
		return nil
	})
	return touched, err
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

// ReconcileStatelessReviewTasks cancels open reviewflow tasks whose
// source_page has no live state file AND no live content file. These
// are tasks the state-file-walking reconciler cannot see: the page
// was deleted (and its state file with it, in older code paths that
// didn't tombstone) before the OnPageDelete hook existed, leaving
// zombie tasks forever showing in the todo calendar as "pending
// signatures" for a page that no longer exists.
//
// Both slash forms of the page path are checked, so a legacy task
// stored under /foo while the current content lives at /foo/
// (namespace index) is kept — matched by exists OR state. Only tasks
// whose page truly has no state anywhere get cancelled.
//
// Idempotent. Returns the number of tasks cancelled.
func (svc *Service) ReconcileStatelessReviewTasks(exists func(pagePath string) bool) (int, error) {
	if svc.todo == nil || exists == nil {
		return 0, nil
	}
	pages, err := svc.todo.ListOpenPagesWithReviewTasks()
	if err != nil {
		return 0, err
	}
	cancelled := 0
	for _, pagePath := range pages {
		if pageHasAnyLiveState(pagePath, exists, svc.store) {
			continue
		}
		if err := svc.todo.CancelReviewTasks(pagePath); err != nil {
			continue
		}
		cancelled++
	}
	return cancelled, nil
}

// pageHasAnyLiveState reports whether a page path still has either a
// content file or a reviewflow state file on disk, in either slash
// form. Used by ReconcileStatelessReviewTasks to spare pages that are
// alive under the "other" spelling.
func pageHasAnyLiveState(pagePath string, exists func(string) bool, store *Store) bool {
	candidates := []string{pagePath}
	if pagePath != "" && pagePath != "/" {
		if pagePath[len(pagePath)-1] == '/' {
			candidates = append(candidates, pagePath[:len(pagePath)-1])
		} else {
			candidates = append(candidates, pagePath+"/")
		}
	}
	for _, p := range candidates {
		if exists(p) {
			return true
		}
		if st, _ := store.Load(p); st != nil {
			return true
		}
	}
	return false
}

// rolesEqual reports whether two role→user maps are byte-for-byte equal.
// Used by SyncFromMarkdown to tell "same directive, content changed"
// (signatures must re-attach, tasks preserved) from "directive changed"
// (old tasks are stale, rebuild from scratch).
func rolesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// stringSliceEqual reports whether two string slices are element-by-element
// equal. For reviewflow.State.RoleOrder: a reordering of the same roles is
// a directive change (sequential-notification order differs).
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// OnPageDelete is called by the page store immediately after a page's
// content and metadata have been removed. The two side-effects a reviewflow
// state carries past the page's own lifetime are review todos (visible in
// list_todos with a dead source_page) and the state file sitting in
// data/meta/.../page.reviewflow.json. Both go here.
func (svc *Service) OnPageDelete(pagePath string) error {
	if svc.todo != nil {
		_ = svc.todo.CancelReviewTasks(pagePath)
	}
	// Best-effort state removal. The caller already lost the page; a
	// stray state file is cosmetic noise, not a correctness issue, so
	// errors from Delete don't propagate.
	_ = svc.store.Delete(pagePath)
	return nil
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

	// Belt-and-suspenders digest check. The version-change branch
	// below handles the normal "content edited → version bumped →
	// snapshot + reattach-by-digest + wipe non-matching" flow. But
	// trusting the version-change proxy alone leaves a hole: any
	// write path that lands new content under the SAME page
	// version (e.g. buggy, concurrent writes, or a store caller
	// that forgets to bump) would leave signed Confirmations
	// unchanged — yet their stored Digest no longer covers the
	// page. Enforce the invariant directly: a signed Confirmation
	// is valid iff its Digest matches the current content digest.
	// Mismatches get snapshotted and dropped BEFORE the version-
	// change logic runs so the two paths don't fight.
	currentDigest := ComputeDigest([]byte(markdown))
	staleSigned := 0
	for _, c := range st.Confirmations {
		if c.Signature != "" && c.Digest != "" && c.Digest != currentDigest {
			staleSigned++
		}
	}
	if staleSigned > 0 {
		// Snapshot once so the audit trail keeps the invalidated
		// signatures. findVersionRecord guards against double-
		// writing a snapshot on top of a prior one for the same
		// version (defensive — happens if the invariant fires more
		// than once for the same version due to repeated same-
		// version writes).
		if findVersionRecord(st.VersionHistory, st.CurrentPageVersion) < 0 {
			st.VersionHistory = append(st.VersionHistory, snapshotVersionRecord(st))
		}
		kept := st.Confirmations[:0]
		for _, c := range st.Confirmations {
			if c.Signature != "" && c.Digest != "" && c.Digest != currentDigest {
				continue
			}
			kept = append(kept, c)
		}
		st.Confirmations = kept
		// ValidatedVersion may have been pinned to the current
		// version by the dropped set. Clear it if the surviving
		// Confirmations no longer cover every role — the page is
		// no longer fully validated.
		if st.ValidatedVersion > 0 && st.ValidatedVersion == st.CurrentPageVersion && !svc.allConfirmed(st) {
			st.ValidatedVersion = 0
		}
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

		// Pull any historical confirmation whose stored digest matches
		// the current page content and carry it forward onto the new
		// page version. currentDigest was computed above for the
		// belt-and-suspenders stale-digest check; reuse it. Empty
		// digest means the caller confirmed without signing, so those
		// never re-attach — they wouldn't survive a real audit anyway.
		st.Confirmations = reattachByDigest(st, pageVersion, currentDigest)

		// Review-task bookkeeping. Three cases:
		//
		// a) Directive changed (roles, tag, parallel, or order): the
		//    existing task set may be stale — reviewer=bob at tag 2.1
		//    becoming reviewer=alice at tag 3.0 leaves bob's task
		//    dangling forever otherwise. Cancel every open task and
		//    recreate only the missing-role ones.
		//
		// b) Directive unchanged but re-attach brought nothing back
		//    (genuine content edit, confirmations wiped): same —
		//    cancel + recreate.
		//
		// c) Directive unchanged and re-attach restored confirmations
		//    (discard-draft / same-digest restore): the existing open
		//    tasks for still-missing roles are still legitimate and
		//    their IDs matter (users have them bookmarked). Leave the
		//    task set alone. This is the signature-preservation path.
		if svc.todo != nil {
			directiveChanged := !rolesEqual(st.Roles, dir.Roles) ||
				st.VersionTag != dir.VersionTag ||
				st.Parallel != dir.Parallel ||
				!stringSliceEqual(st.RoleOrder, dir.RoleOrder)
			if directiveChanged || len(st.Confirmations) == 0 {
				_ = svc.todo.CancelReviewTasks(pagePath)
				// Roles already satisfied by a re-attached confirmation
				// get no new task — the signature stands.
				satisfied := make(map[string]bool, len(st.Confirmations))
				for _, c := range st.Confirmations {
					if c.PageVersion == pageVersion {
						satisfied[c.Role] = true
					}
				}
				missing := make(map[string]string)
				for role, user := range dir.Roles {
					if !satisfied[role] {
						missing[role] = user
					}
				}
				if len(missing) > 0 {
					dueDate := svc.computeDueDate(dir.Roles)
					if dir.Parallel {
						_ = svc.todo.CreateReviewTasks(pagePath, missing, dir.VersionTag, dueDate)
					} else {
						for _, role := range dir.RoleOrder {
							if user, ok := missing[role]; ok {
								_ = svc.todo.CreateReviewTasks(pagePath, map[string]string{role: user}, dir.VersionTag, dueDate)
								break
							}
						}
					}
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
//
// Two subtleties the pre-rc.3 version missed:
//
//  1. VersionHistory holds BOTH fully-validated snapshots AND partial-
//     signature snapshots (bookkeeping entries written when an edit is
//     about to wipe mid-review confirmations — see snapshotVersionRecord
//     in SyncFromMarkdown). The all-roles-confirmed short-circuit must
//     check VersionRecord.IsValidated, not just "a snapshot for this
//     version exists". Otherwise a partial snapshot for v67 made the
//     historical view show every role as "Confirmed" and the panel as
//     "Validated" even though the API's own validated_page_version was
//     still 0.
//
//  2. The confirmation source for a historical version is the SNAPSHOT'S
//     ConfirmedBy map, not st.Confirmations. The live Confirmations slice
//     gets wiped at the next edit (unless re-attach brings signatures
//     back by digest match), so for an edited-past version it holds
//     nothing. The snapshot is the only record of who had signed what
//     at the moment the edit landed. Fall back to st.Confirmations only
//     when no snapshot exists — the no-snapshot case covers unedited
//     mid-review views where the live confirmations ARE the truth.
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

	// Locate the snapshot for this version, if any.
	var snapshot *VersionRecord
	for i := range st.VersionHistory {
		if st.VersionHistory[i].PageVersion == version {
			snapshot = &st.VersionHistory[i]
			break
		}
	}

	// Fully-validated short-circuit: only when the snapshot explicitly
	// says so. Partial snapshots fall through to the per-role path.
	if snapshot != nil && snapshot.IsValidated {
		return &Status{
			Roles:            st.Roles,
			VersionTag:       snapshot.VersionTag,
			CurrentPageVer:   version,
			ValidatedVersion: version,
			MissingRoles:     make(map[string]string),
			IsFullyValidated: true,
			VersionHistory:   st.VersionHistory,
		}, nil
	}

	// Compute the confirmed-by set from the authoritative source:
	// snapshot.ConfirmedBy if the snapshot exists (edited-past view),
	// else the live Confirmations slice (unedited mid-review view).
	confirmed := make(map[string]bool)
	versionTag := st.VersionTag
	if snapshot != nil {
		for role := range snapshot.ConfirmedBy {
			confirmed[role] = true
		}
		if snapshot.VersionTag != "" {
			versionTag = snapshot.VersionTag
		}
	} else {
		for _, c := range st.Confirmations {
			if c.PageVersion == version {
				confirmed[c.Role] = true
			}
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
		VersionTag:       versionTag,
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

	// NextRoles: what's actionable NOW — parallel → all missing;
	// sequential → head of queue only. Empty when nothing's missing.
	var nextRoles []string
	if len(missing) > 0 {
		if st.Parallel {
			for role := range missing {
				nextRoles = append(nextRoles, role)
			}
			sort.Strings(nextRoles)
		} else if next := nextUnconfirmedRole(st); next != "" {
			nextRoles = []string{next}
		}
	}

	status := &Status{
		Roles:            st.Roles,
		VersionTag:       st.VersionTag,
		CurrentPageVer:   st.CurrentPageVersion,
		ValidatedVersion: st.ValidatedVersion,
		MissingRoles:     missing,
		NextRoles:        nextRoles,
		Parallel:         st.Parallel,
		RoleOrder:        st.RoleOrder,
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
		// Belt + suspenders: anything the Complete pass missed is a
		// stale task (role reassigned, legacy signature, etc.). Once
		// the page is fully validated no open review task is
		// legitimate — cancel the rest. CancelReviewTasks ignores
		// tasks already in done status, so the ones we just completed
		// stay done.
		_ = svc.todo.CancelReviewTasks(pagePath)
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
