package reviewflow

import (
	"testing"
	"time"
)

// Signature preservation across version bumps: partial confirmations are
// snapshotted to VersionHistory before wipe (so an audit can find them),
// and any confirmation whose Digest matches the new content's digest is
// re-attached to the new page version (so a discard-draft that returned
// to the last-published bytes, or a restore-from-history to a prior
// signed version, keeps the signatures).
//
// Signatures are already digest-bound in signing.go, so this is safe:
// same content → same digest → the signature is still cryptographically
// valid for the new version's content.

const signedMd = "{reviewflow author=alice reviewer=bob}\n"

// A signed partial confirmation that survives a version bump must appear
// in VersionHistory afterwards — otherwise "whose signature broke today"
// can't be answered from disk (the state files are current-only).
func TestSyncFromMarkdown_VersionBumpWithSignedConfirmation_SnapshotsToHistory(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	if err := svc.SyncFromMarkdown("/doc", 1, signedMd); err != nil {
		t.Fatal(err)
	}
	digestV1 := ComputeDigest([]byte(signedMd))
	opts := &ConfirmOpts{
		Signature:       "sig-a",
		Digest:          digestV1,
		CertFingerprint: "fp-alice",
		CertificatePEM:  "PEM-alice",
	}
	if _, err := svc.Confirm("/doc", "author", "alice", opts); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Content change → new digest → wipe. The pre-wipe snapshot must land.
	if err := svc.SyncFromMarkdown("/doc", 2, signedMd+"\n\n# extra body\n"); err != nil {
		t.Fatal(err)
	}

	st, _ := svc.store.Load("/doc")
	if len(st.VersionHistory) != 1 {
		t.Fatalf("expected one VersionHistory entry after wipe, got %d", len(st.VersionHistory))
	}
	vr := st.VersionHistory[0]
	if vr.PageVersion != 1 {
		t.Errorf("snapshot PageVersion = %d, want 1", vr.PageVersion)
	}
	if vr.IsValidated {
		t.Error("partial snapshot must NOT be marked IsValidated")
	}
	if len(vr.Confirmations) != 1 || vr.Confirmations[0].User != "alice" || vr.Confirmations[0].Signature != "sig-a" {
		t.Errorf("snapshot Confirmations lost data: %+v", vr.Confirmations)
	}
	if len(st.Confirmations) != 0 {
		t.Errorf("new-version state still carries confirmations: %+v", st.Confirmations)
	}
}

// The heart of the restore-preserves-signatures fix: bump to a new page
// version with the SAME content as an earlier signed version. The
// signature re-attaches to the new page version because its stored Digest
// matches the incoming content's digest.
func TestSyncFromMarkdown_SameDigestBump_ReattachesSignedConfirmations(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, signedMd)
	digestV1 := ComputeDigest([]byte(signedMd))
	_, _ = svc.Confirm("/doc", "author", "alice", &ConfirmOpts{
		Signature: "sig-a", Digest: digestV1, CertificatePEM: "PEM-alice",
	})
	spy.cancels = nil
	spy.creates = nil

	// Discard-draft equivalent: version bumps to 2 but content is the
	// SAME bytes as v1. Signature is still valid over the same digest.
	if err := svc.SyncFromMarkdown("/doc", 2, signedMd); err != nil {
		t.Fatal(err)
	}

	st, _ := svc.store.Load("/doc")
	if len(st.Confirmations) != 1 {
		t.Fatalf("re-attach failed: Confirmations = %+v", st.Confirmations)
	}
	if st.Confirmations[0].PageVersion != 2 {
		t.Errorf("re-attached PageVersion = %d, want 2 (rewritten on carry-over)", st.Confirmations[0].PageVersion)
	}
	if st.Confirmations[0].Signature != "sig-a" {
		t.Error("re-attached signature payload mangled")
	}
	// Same-digest restore does not create phantom review tasks — the
	// role is already covered.
	if len(spy.cancels) != 0 || len(spy.creates) != 0 {
		t.Errorf("same-digest restore should not touch review tasks; cancels=%v creates=%v", spy.cancels, spy.creates)
	}
}

// When the re-attach set covers every role on the new version, the state
// is fully validated end-to-end: ValidatedVersion bumps, a VersionRecord
// is written with IsValidated=true, and review tasks are marked complete.
// This is the "restore a fully-validated old revision" case.
func TestSyncFromMarkdown_ReattachCompletesValidation(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, signedMd)
	digestV1 := ComputeDigest([]byte(signedMd))
	_, _ = svc.Confirm("/doc", "author", "alice", &ConfirmOpts{
		Signature: "sig-a", Digest: digestV1, CertificatePEM: "PEM-alice",
	})
	_, _ = svc.Confirm("/doc", "reviewer", "bob", &ConfirmOpts{
		Signature: "sig-b", Digest: digestV1, CertificatePEM: "PEM-bob",
	})

	// v1 is fully validated. Now content changes to v2, then a restore
	// brings it back to v1's bytes as v3.
	_ = svc.SyncFromMarkdown("/doc", 2, signedMd+"\n\n# body\n")
	if err := svc.SyncFromMarkdown("/doc", 3, signedMd); err != nil {
		t.Fatal(err)
	}

	st, _ := svc.store.Load("/doc")
	if st.ValidatedVersion != 3 {
		t.Errorf("ValidatedVersion = %d, want 3 (re-attach covered every role)", st.ValidatedVersion)
	}
	// The v3 VersionRecord must exist and be marked IsValidated.
	idx := findVersionRecord(st.VersionHistory, 3)
	if idx < 0 {
		t.Fatal("no VersionRecord for the newly-validated v3")
	}
	if !st.VersionHistory[idx].IsValidated {
		t.Error("VersionRecord for v3 must be IsValidated=true")
	}
	if len(st.VersionHistory[idx].Confirmations) != 2 {
		t.Errorf("v3 record should carry both re-attached signatures, got %+v", st.VersionHistory[idx].Confirmations)
	}
}

// A genuine content change (different digest) MUST wipe as before. The
// re-attach machinery is a preservation path, not a signature launderer.
func TestSyncFromMarkdown_DifferentDigest_NoReattach(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, signedMd)
	digestV1 := ComputeDigest([]byte(signedMd))
	_, _ = svc.Confirm("/doc", "author", "alice", &ConfirmOpts{
		Signature: "sig-a", Digest: digestV1, CertificatePEM: "PEM-alice",
	})

	// New content, new digest.
	changed := signedMd + "\n\n# entirely different body\n"
	if err := svc.SyncFromMarkdown("/doc", 2, changed); err != nil {
		t.Fatal(err)
	}

	st, _ := svc.store.Load("/doc")
	if len(st.Confirmations) != 0 {
		t.Errorf("different-digest bump must wipe; got %+v", st.Confirmations)
	}
	// Original signature is still in history for the audit trail.
	if len(st.VersionHistory) != 1 || len(st.VersionHistory[0].Confirmations) != 1 {
		t.Errorf("expected the wiped signature preserved in history, got %+v", st.VersionHistory)
	}
}

// Click-only (unsigned) confirmations have no crypto guarantee, so they
// intentionally don't survive a version bump — their previous "yes I saw
// this" is against bytes that no longer exist as the current version.
// The user has to click again.
func TestSyncFromMarkdown_UnsignedConfirmationDoesNotReattach(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, signedMd)
	// No ConfirmOpts → no Signature, no Digest.
	_, _ = svc.Confirm("/doc", "author", "alice", nil)

	// Same content, version bumped. Digest matches but the confirmation
	// has no digest to match against — click-only stays gone.
	if err := svc.SyncFromMarkdown("/doc", 2, signedMd); err != nil {
		t.Fatal(err)
	}

	st, _ := svc.store.Load("/doc")
	if len(st.Confirmations) != 0 {
		t.Errorf("unsigned confirmations must not re-attach; got %+v", st.Confirmations)
	}
	// Still snapshotted so an audit can see who clicked when.
	if len(st.VersionHistory) != 1 {
		t.Errorf("unsigned confirmation should still be snapshot to history, got %+v", st.VersionHistory)
	}
}

// Two candidates for the same (role, user) — the most recent wins. This
// pins the tie-breaker for cases where a user re-signed with a new cert
// or on a later day between two same-digest states.
func TestReattachByDigest_DedupsByMostRecent(t *testing.T) {
	t.Parallel()
	digest := "abc"
	older := Confirmation{
		Role: "reviewer", User: "bob", PageVersion: 5,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Digest:    digest, Signature: "older-sig",
	}
	newer := Confirmation{
		Role: "reviewer", User: "bob", PageVersion: 10,
		Timestamp: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Digest:    digest, Signature: "newer-sig",
	}
	st := &State{
		VersionHistory: []VersionRecord{
			{PageVersion: 5, Confirmations: []Confirmation{older}},
			{PageVersion: 10, Confirmations: []Confirmation{newer}},
		},
	}
	got := reattachByDigest(st, 12, digest)
	if len(got) != 1 {
		t.Fatalf("dedup failed, got %+v", got)
	}
	if got[0].Signature != "newer-sig" {
		t.Errorf("dedup kept older signature: %q", got[0].Signature)
	}
	if got[0].PageVersion != 12 {
		t.Errorf("PageVersion rewrite failed: got %d, want 12", got[0].PageVersion)
	}
}

// findVersionRecord powers the "don't double-snapshot" guard. Test the
// obvious cases so a future refactor doesn't silently regress.
func TestFindVersionRecord(t *testing.T) {
	t.Parallel()
	h := []VersionRecord{{PageVersion: 3}, {PageVersion: 7}, {PageVersion: 11}}
	if got := findVersionRecord(h, 7); got != 1 {
		t.Errorf("hit: got %d, want 1", got)
	}
	if got := findVersionRecord(h, 99); got != -1 {
		t.Errorf("miss: got %d, want -1", got)
	}
	if got := findVersionRecord(nil, 3); got != -1 {
		t.Errorf("nil history: got %d, want -1", got)
	}
}
