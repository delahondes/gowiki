package reviewflow

import (
	"errors"
	"time"
)

// Manual release: a `{reviewflow ... manual_release=yes}` page holds
// every review task back until someone ticks "Ready for signature".
// Until then no signer is alerted and Confirm is refused. The tick is
// kept in the reviewflow state (with who and when), not in the
// Markdown: ticking is not a content edit, so it creates no page
// version and invalidates no signature.

var (
	// ErrNotReleased is returned by Confirm on a manual_release page
	// that nobody has released yet.
	ErrNotReleased = errors.New("this document is not yet released for signature")
	// ErrReleaseNotEnabled is returned by SetReleased on a page whose
	// directive has no manual_release=yes.
	ErrReleaseNotEnabled = errors.New("this document does not use manual release (add manual_release=yes to its reviewflow block)")
	// ErrReleaseForbidden is returned when the caller neither holds the
	// first role of the reviewflow nor has edit rights on the page.
	ErrReleaseForbidden = errors.New("only the first signer or someone with edit rights on the page can change its release state")
	// ErrReleaseLocked is returned when withdrawing a release after a
	// signature was recorded on the current version.
	ErrReleaseLocked = errors.New("cannot withdraw the release: a signature has already been recorded on this version")
)

func (st *State) clearRelease() {
	st.Released = false
	st.ReleasedBy = ""
	st.ReleasedAt = nil
}

// isFirstSigner reports whether user holds the first role in the
// directive's declared order.
func isFirstSigner(st *State, user string) bool {
	if st == nil || len(st.RoleOrder) == 0 || user == "" {
		return false
	}
	return st.Roles[st.RoleOrder[0]] == user
}

// wantedReviewTasks returns the role → user set that should have an
// open review task right now: nothing while the page awaits release,
// every unconfirmed role in parallel mode, otherwise only the head of
// the queue.
func wantedReviewTasks(st *State) map[string]string {
	want := make(map[string]string)
	if st == nil || st.AwaitingRelease() {
		return want
	}
	confirmed := make(map[string]bool)
	for _, c := range st.Confirmations {
		if c.PageVersion == st.CurrentPageVersion {
			confirmed[c.Role] = true
		}
	}
	if st.Parallel {
		for role, user := range st.Roles {
			if !confirmed[role] {
				want[role] = user
			}
		}
		return want
	}
	if next := nextUnconfirmedRole(st); next != "" {
		want[next] = st.Roles[next]
	}
	return want
}

// SetReleased ticks or unticks "Ready for signature" on a
// manual_release page. canEdit is the caller's edit permission on the
// page, resolved by the HTTP layer from the ACL.
//
// Releasing creates the first signer's review task (all signers' in
// parallel mode), which sends the usual assignment notification.
// Withdrawing is allowed only while no signature exists on the current
// version, and cancels the open review tasks.
func (svc *Service) SetReleased(pagePath, user string, released, canEdit bool) (*Status, error) {
	st, err := svc.EnsureState(pagePath)
	if err != nil {
		return nil, err
	}
	if !st.ManualRelease {
		return nil, ErrReleaseNotEnabled
	}
	if !canEdit && !isFirstSigner(st, user) {
		return nil, ErrReleaseForbidden
	}
	if released == st.Released {
		return svc.computeStatus(pagePath, st)
	}

	if released {
		now := time.Now().UTC()
		st.Released = true
		st.ReleasedBy = user
		st.ReleasedAt = &now
	} else {
		for _, c := range st.Confirmations {
			if c.PageVersion == st.CurrentPageVersion {
				return nil, ErrReleaseLocked
			}
		}
		st.clearRelease()
	}

	if err := svc.store.Save(pagePath, st); err != nil {
		return nil, err
	}
	if svc.todo != nil {
		_, _, _, _ = svc.todo.EnsureReviewTasks(pagePath, wantedReviewTasks(st), st.VersionTag, svc.computeDueDate(st.Roles))
	}
	return svc.computeStatus(pagePath, st)
}
