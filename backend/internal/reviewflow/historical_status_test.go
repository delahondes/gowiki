package reviewflow

import (
	"testing"
	"time"
)

// User-reported bug on wiki.gmt.bio: /qara/sop09 at v67 displayed the
// reviewflow panel as "✓ Validated" with all three roles "Confirmed",
// but the live state said validated_page_version=0 and no edit had
// ever marked v67 (or any version) as fully validated — only Raynald
// had signed as reviewer. The cause lives in GetStatusForVersion:
// it treated every VersionHistory entry as a full validation, ignoring
// IsValidated (which distinguishes partial-signature snapshots from
// genuine validations). The partial snapshot for v67 — bookkeeping
// written when the next edit was about to wipe signatures — hit the
// short-circuit and the view said "Validated".
//
// Two complementary fixes pinned here: the IsValidated gate on the
// short-circuit, AND reading the snapshot's ConfirmedBy for the fall-
// through path (st.Confirmations is empty after the wipe).

func TestGetStatusForVersion_PartialSnapshot_NotValidated(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	// State at page version 7, with a PARTIAL snapshot for v6 — only
	// the reviewer signed before v7's edit landed.
	st := &State{
		Roles:              map[string]string{"author": "alice", "reviewer": "bob", "validator": "cathy"},
		VersionTag:         "1.0",
		CurrentPageVersion: 7,
		ValidatedVersion:   0, // never fully validated
		VersionHistory: []VersionRecord{
			{
				PageVersion: 6,
				Timestamp:   time.Now().Add(-24 * time.Hour),
				ConfirmedBy: map[string]string{"reviewer": "bob"},
				VersionTag:  "1.0",
				IsValidated: false, // partial — bob signed, author + validator didn't
			},
		},
	}
	if err := svc.store.Save("/doc", st); err != nil {
		t.Fatalf("save state: %v", err)
	}

	status, err := svc.GetStatusForVersion("/doc", 6)
	if err != nil {
		t.Fatalf("GetStatusForVersion: %v", err)
	}

	// Must NOT be reported as fully validated — the panel was lying.
	if status.IsFullyValidated {
		t.Errorf("IsFullyValidated = true, want false — partial snapshot must not short-circuit")
	}
	// ValidatedVersion must stay at 0 — no validation ever happened.
	if status.ValidatedVersion != 0 {
		t.Errorf("ValidatedVersion = %d, want 0", status.ValidatedVersion)
	}
	// MissingRoles must reflect reality: reviewer signed, the other
	// two didn't.
	if _, has := status.MissingRoles["reviewer"]; has {
		t.Errorf("MissingRoles contains reviewer = %q (bob signed per snapshot)", status.MissingRoles["reviewer"])
	}
	if status.MissingRoles["author"] != "alice" {
		t.Errorf("MissingRoles[author] = %q, want alice", status.MissingRoles["author"])
	}
	if status.MissingRoles["validator"] != "cathy" {
		t.Errorf("MissingRoles[validator] = %q, want cathy", status.MissingRoles["validator"])
	}
}

func TestGetStatusForVersion_FullyValidatedSnapshot_StillShortCircuits(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	// A genuine fully-validated snapshot — must still short-circuit.
	st := &State{
		Roles:              map[string]string{"author": "alice", "reviewer": "bob"},
		VersionTag:         "1.0",
		CurrentPageVersion: 8,
		ValidatedVersion:   7,
		VersionHistory: []VersionRecord{
			{
				PageVersion: 7,
				Timestamp:   time.Now().Add(-24 * time.Hour),
				ConfirmedBy: map[string]string{"author": "alice", "reviewer": "bob"},
				VersionTag:  "1.0",
				IsValidated: true,
			},
		},
	}
	if err := svc.store.Save("/doc", st); err != nil {
		t.Fatalf("save: %v", err)
	}

	status, err := svc.GetStatusForVersion("/doc", 7)
	if err != nil {
		t.Fatalf("GetStatusForVersion: %v", err)
	}
	if !status.IsFullyValidated {
		t.Errorf("IsFullyValidated = false, want true (snapshot IsValidated=true)")
	}
	if status.ValidatedVersion != 7 {
		t.Errorf("ValidatedVersion = %d, want 7", status.ValidatedVersion)
	}
	if len(status.MissingRoles) != 0 {
		t.Errorf("MissingRoles = %+v, want empty on a validated version", status.MissingRoles)
	}
}

func TestGetStatusForVersion_NoSnapshot_FallsBackToLiveConfirmations(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	// Mid-review on the current version — no snapshot, confirmations
	// are live. The historical view of the CURRENT version must
	// reflect them.
	st := &State{
		Roles:              map[string]string{"author": "alice", "reviewer": "bob"},
		VersionTag:         "1.0",
		CurrentPageVersion: 5,
		Confirmations: []Confirmation{
			{Role: "author", User: "alice", PageVersion: 5},
		},
		VersionHistory: nil,
	}
	if err := svc.store.Save("/doc", st); err != nil {
		t.Fatalf("save: %v", err)
	}

	status, err := svc.GetStatusForVersion("/doc", 5)
	if err != nil {
		t.Fatalf("GetStatusForVersion: %v", err)
	}
	if status.IsFullyValidated {
		t.Errorf("IsFullyValidated = true, want false (reviewer hasn't signed)")
	}
	if _, has := status.MissingRoles["author"]; has {
		t.Errorf("MissingRoles contains author (alice confirmed via live Confirmations)")
	}
	if status.MissingRoles["reviewer"] != "bob" {
		t.Errorf("MissingRoles[reviewer] = %q, want bob", status.MissingRoles["reviewer"])
	}
}

func TestGetStatusForVersion_PartialSnapshot_ConfirmationsFromSnapshotNotLive(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)

	// The historical-version case the user hit: v6 is in the past,
	// v7 is current, bob's signature on v6 was wiped by the edit that
	// produced v7 (no re-attach because the content changed), the
	// snapshot captured it. st.Confirmations is now empty for v6.
	// GetStatusForVersion(6) must read the SNAPSHOT, not st.Confirmations.
	st := &State{
		Roles:              map[string]string{"author": "alice", "reviewer": "bob"},
		VersionTag:         "1.0",
		CurrentPageVersion: 7,
		Confirmations:      nil, // post-wipe
		VersionHistory: []VersionRecord{
			{
				PageVersion: 6,
				Timestamp:   time.Now().Add(-24 * time.Hour),
				ConfirmedBy: map[string]string{"reviewer": "bob"},
				VersionTag:  "1.0",
				IsValidated: false,
			},
		},
	}
	if err := svc.store.Save("/doc", st); err != nil {
		t.Fatalf("save: %v", err)
	}

	status, err := svc.GetStatusForVersion("/doc", 6)
	if err != nil {
		t.Fatalf("GetStatusForVersion: %v", err)
	}
	if status.IsFullyValidated {
		t.Errorf("IsFullyValidated = true, want false")
	}
	// reviewer: bob was in snapshot.ConfirmedBy → NOT missing.
	if _, has := status.MissingRoles["reviewer"]; has {
		t.Errorf("MissingRoles contains reviewer — snapshot said bob signed")
	}
	// author: alice never signed v6 → missing.
	if status.MissingRoles["author"] != "alice" {
		t.Errorf("MissingRoles[author] = %q, want alice", status.MissingRoles["author"])
	}
}
