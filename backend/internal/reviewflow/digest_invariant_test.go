package reviewflow

import (
	"testing"

	"gowiki/backend/internal/storage"
)

// User-reported bug: a page was moved and then edited through MCP; the
// signature's claimed digest no longer covered the current content, yet
// the reviewflow state still carried it as a live Confirmation on the
// current page version. The page displayed as "signed by author" while
// the author never endorsed the current content.
//
// The symptom's exact cause (what wrote content without bumping meta.
// Version) wasn't pinned, but the invariant SyncFromMarkdown must
// enforce is clear: a signed Confirmation is valid iff its stored
// Digest matches the current page content's digest. Trusting the
// version-change proxy alone leaves a hole — any write path that
// lands new content under the SAME version number (buggy, racy, or
// bypassing the normal path) would leave stale signatures in place.
//
// The fix: a belt-and-suspenders digest check at the top of
// SyncFromMarkdown. Fires regardless of version change.

const signedDoc = "{reviewflow version=1.0 author=alice reviewer=bob}\n\n# Doc\n\nbody text\n"
const signedDocEdited = "{reviewflow version=1.0 author=alice reviewer=bob}\n\n# Doc\n\nedited body text\n"

func TestDigestInvariant_SameVersionDifferentContent_WipesSignedConfirmation(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	// Seed: v=1 saved, alice signs it.
	_ = svc.SyncFromMarkdown("/doc", 1, signedDoc)
	v1Digest := ComputeDigest([]byte(signedDoc))
	if _, err := svc.Confirm("/doc", "author", "alice", &ConfirmOpts{
		Signature:      "sig-a",
		Digest:         v1Digest,
		CertificatePEM: "PEM-alice",
	}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	spy.cancels, spy.creates = nil, nil

	// Now simulate a write path that lands new content under the SAME
	// version number — the bug the production incident revealed.
	// SyncFromMarkdown receives pageVersion=1 (unchanged) with content
	// whose digest is DIFFERENT from what alice signed.
	if err := svc.SyncFromMarkdown("/doc", 1, signedDocEdited); err != nil {
		t.Fatalf("SyncFromMarkdown (same version, new content): %v", err)
	}

	st, _ := svc.store.Load("/doc")
	// Alice's signature covered the OLD content; it must be removed
	// from the live set — the signature isn't lost (snapshot keeps
	// the audit trail), just no longer counted as covering the page.
	for _, c := range st.Confirmations {
		if c.Role == "author" && c.User == "alice" {
			t.Errorf("alice's signature survived despite content change: %+v", c)
		}
	}
	// The snapshot must preserve the invalidated signature so the
	// audit record isn't lost.
	foundSnapshot := false
	for _, vr := range st.VersionHistory {
		if vr.PageVersion == 1 {
			if _, hasAuthor := vr.ConfirmedBy["author"]; hasAuthor {
				foundSnapshot = true
			}
		}
	}
	if !foundSnapshot {
		t.Errorf("snapshot of v1 with alice's confirmation missing from VersionHistory; audit trail lost")
	}
}

func TestDigestInvariant_SameVersionSameContent_KeepsSignature(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	// Seed + sign.
	_ = svc.SyncFromMarkdown("/doc", 1, signedDoc)
	digest := ComputeDigest([]byte(signedDoc))
	if _, err := svc.Confirm("/doc", "author", "alice", &ConfirmOpts{
		Signature:      "sig-a",
		Digest:         digest,
		CertificatePEM: "PEM-alice",
	}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// A sync pass with IDENTICAL content and the same version — common
	// case when a non-content-mutating path (e.g. a background
	// reconcile) calls SyncFromMarkdown. Signatures must survive.
	if err := svc.SyncFromMarkdown("/doc", 1, signedDoc); err != nil {
		t.Fatalf("sync (noop): %v", err)
	}

	st, _ := svc.store.Load("/doc")
	foundSig := false
	for _, c := range st.Confirmations {
		if c.Role == "author" && c.Digest == digest {
			foundSig = true
		}
	}
	if !foundSig {
		t.Errorf("alice's valid signature was dropped by the invariant check; content was unchanged")
	}
}

func TestDigestInvariant_UnsignedConfirmationNotAffected(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, signedDoc)
	// Confirm WITHOUT a signature — click-only acknowledgement.
	// Nothing to verify against content; the invariant must leave it
	// alone (it's a weaker claim, not making a cryptographic promise
	// about content coverage).
	if _, err := svc.Confirm("/doc", "author", "alice", &ConfirmOpts{}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Same-version content change.
	if err := svc.SyncFromMarkdown("/doc", 1, signedDocEdited); err != nil {
		t.Fatalf("sync: %v", err)
	}

	st, _ := svc.store.Load("/doc")
	foundAuthor := false
	for _, c := range st.Confirmations {
		if c.Role == "author" && c.Signature == "" {
			foundAuthor = true
		}
	}
	if !foundAuthor {
		t.Errorf("unsigned confirmation was dropped — it carries no digest claim and the invariant must not touch it")
	}
}

func TestDigestInvariant_FullyValidatedPageInvalidated_ClearsValidatedVersion(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	// Seed + both roles sign + page becomes validated.
	_ = svc.SyncFromMarkdown("/doc", 1, signedDoc)
	digest := ComputeDigest([]byte(signedDoc))
	for _, role := range []string{"author", "reviewer"} {
		user := map[string]string{"author": "alice", "reviewer": "bob"}[role]
		if _, err := svc.Confirm("/doc", role, user, &ConfirmOpts{
			Signature:      "sig-" + role,
			Digest:         digest,
			CertificatePEM: "PEM-" + user,
		}); err != nil {
			t.Fatalf("Confirm %s: %v", role, err)
		}
	}

	st, _ := svc.store.Load("/doc")
	if st.ValidatedVersion != 1 {
		t.Fatalf("setup: ValidatedVersion should be 1 after both signatures, got %d", st.ValidatedVersion)
	}

	// Content change under the same version → every signed
	// confirmation drops. ValidatedVersion was pinned to this version
	// by the dropped set, so it must drop too — otherwise the page
	// would still say "validated at v1" with zero signatures.
	if err := svc.SyncFromMarkdown("/doc", 1, signedDocEdited); err != nil {
		t.Fatalf("sync: %v", err)
	}

	st, _ = svc.store.Load("/doc")
	if st.ValidatedVersion != 0 {
		t.Errorf("ValidatedVersion = %d, want 0 — all signatures dropped, page is no longer validated", st.ValidatedVersion)
	}
}

func TestReconcileStaleSignatures_InvalidatesMismatchedSignatures(t *testing.T) {
	t.Parallel()
	svc, _, reader := newSvcWithSpy(t)

	// Seed: page with signed Confirmation whose Digest doesn't match
	// the current page content. Simulates the shape every pre-fix
	// state file could be in — the one-shot reconciler at startup
	// drains them.
	st := &State{
		Roles:              map[string]string{"author": "alice", "reviewer": "bob"},
		VersionTag:         "1.0",
		CurrentPageVersion: 5,
		Confirmations: []Confirmation{
			{
				PageVersion: 5,
				Role:        "author",
				User:        "alice",
				Digest:      "stale-digest-does-not-match-current-content",
				Signature:   "sig-a",
			},
		},
	}
	if err := svc.store.Save("/doc", st); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Current page content hashes to something else; the reader just
	// needs to return SOME markdown — its digest won't match.
	reader.pages["/doc"] = storage.Page{
		Path:     "/doc",
		Markdown: "content that doesn't hash to the stale digest\n",
	}

	n, err := svc.ReconcileStaleSignatures()
	if err != nil {
		t.Fatalf("ReconcileStaleSignatures: %v", err)
	}
	if n != 1 {
		t.Errorf("touched = %d, want 1 (one page had a stale signature)", n)
	}

	after, _ := svc.store.Load("/doc")
	if len(after.Confirmations) != 0 {
		t.Errorf("stale signature survived reconciliation: %+v", after.Confirmations)
	}
	// Audit trail preserved.
	foundSnapshot := false
	for _, vr := range after.VersionHistory {
		if vr.PageVersion == 5 && vr.ConfirmedBy["author"] == "alice" {
			foundSnapshot = true
		}
	}
	if !foundSnapshot {
		t.Errorf("snapshot of the invalidated state missing from VersionHistory")
	}

	// Idempotent: a second run reports zero.
	n2, _ := svc.ReconcileStaleSignatures()
	if n2 != 0 {
		t.Errorf("second run touched %d pages, want 0 (idempotence)", n2)
	}
}

func TestDigestInvariant_NormalVersionBump_StillUsesTheReattachPath(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	// Seed + sign on v1.
	_ = svc.SyncFromMarkdown("/doc", 1, signedDoc)
	digest := ComputeDigest([]byte(signedDoc))
	_, _ = svc.Confirm("/doc", "author", "alice", &ConfirmOpts{
		Signature:      "sig-a",
		Digest:         digest,
		CertificatePEM: "PEM-alice",
	})

	// Normal content change via a proper version bump: same content
	// digest (so reattach brings the signature forward), new version.
	// The invariant must NOT trip on this — the reattach path is
	// already correct and should produce an alice-signed v2.
	if err := svc.SyncFromMarkdown("/doc", 2, signedDoc); err != nil {
		t.Fatalf("sync: %v", err)
	}

	st, _ := svc.store.Load("/doc")
	foundReattached := false
	for _, c := range st.Confirmations {
		if c.Role == "author" && c.PageVersion == 2 && c.Digest == digest {
			foundReattached = true
		}
	}
	if !foundReattached {
		t.Errorf("same-digest version bump failed to re-attach the signature (invariant check interfered with the normal reattach path)")
	}
}
