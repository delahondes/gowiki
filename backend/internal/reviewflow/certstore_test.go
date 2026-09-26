package reviewflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// issueForUser creates a CA (if the store doesn't have one), signs a fresh
// user key, saves the cert in the given CertStore, and returns the saved
// UserCertificate. Test helper — every certstore test needs a real cert.
func issueForUser(t *testing.T, ca *CAStore, cs *CertStore, username string) *UserCertificate {
	t.Helper()
	if !ca.HasCA() {
		if _, err := ca.GenerateCA("Acme", "Acme CA"); err != nil {
			t.Fatalf("GenerateCA: %v", err)
		}
	}
	_, spki := mustGenSPKI(t)
	certPEM, err := ca.SignUserKey(username, "", spki)
	if err != nil {
		t.Fatalf("SignUserKey(%q): %v", username, err)
	}
	uc, err := cs.Save(username, certPEM)
	if err != nil {
		t.Fatalf("cert Save(%q): %v", username, err)
	}
	return uc
}

func TestCertStore_Save_ParsesAndFingerprints(t *testing.T) {
	meta := t.TempDir()
	uc := issueForUser(t, NewCAStore(meta), NewCertStore(meta), "alice")

	if uc.Username != "alice" {
		t.Errorf("Username = %q, want alice", uc.Username)
	}
	if len(uc.Fingerprint) != 64 {
		t.Errorf("Fingerprint length = %d, want 64 hex chars", len(uc.Fingerprint))
	}
	if uc.NotAfter.Before(uc.NotBefore) {
		t.Errorf("NotAfter %v is before NotBefore %v", uc.NotAfter, uc.NotBefore)
	}
	if uc.Revoked {
		t.Errorf("newly saved cert is unexpectedly Revoked=true")
	}
}

func TestCertStore_Save_RejectsInvalidPEM(t *testing.T) {
	cs := NewCertStore(t.TempDir())
	if _, err := cs.Save("alice", "this is not a PEM certificate"); err == nil {
		t.Errorf("Save must reject non-PEM input")
	} else if !errors.Is(err, ErrInvalidPEM) {
		t.Errorf("Save error = %v, want ErrInvalidPEM", err)
	}
}

func TestCertStore_Load_MissingReturnsNilNil(t *testing.T) {
	cs := NewCertStore(t.TempDir())
	uc, err := cs.Load("nobody")
	if err != nil || uc != nil {
		t.Errorf("Load of missing cert: got uc=%v err=%v, want both nil", uc, err)
	}
}

func TestCertStore_Load_ReturnsSaved(t *testing.T) {
	meta := t.TempDir()
	saved := issueForUser(t, NewCAStore(meta), NewCertStore(meta), "alice")

	loaded, err := NewCertStore(meta).Load("alice")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.Fingerprint != saved.Fingerprint {
		t.Errorf("Loaded fingerprint = %q, want %q", loaded.Fingerprint, saved.Fingerprint)
	}
	if loaded.CertificatePEM != saved.CertificatePEM {
		t.Errorf("Loaded PEM differs from saved")
	}
}

func TestCertStore_Save_OverwritesPreviousCert(t *testing.T) {
	// The certstore holds ONE cert per user — a re-issue replaces the
	// previous. This is a design decision, not a bug: if you want the
	// old cert to still count for audit, keep it out of band. This test
	// documents the current behaviour so a future change is intentional.
	meta := t.TempDir()
	ca := NewCAStore(meta)
	cs := NewCertStore(meta)
	first := issueForUser(t, ca, cs, "alice")
	second := issueForUser(t, ca, cs, "alice")
	if first.Fingerprint == second.Fingerprint {
		t.Fatalf("re-issued cert shares the fingerprint of the first — CA is not randomising serials?")
	}
	loaded, err := cs.Load("alice")
	if err != nil || loaded == nil {
		t.Fatalf("Load after re-issue: uc=%v err=%v", loaded, err)
	}
	if loaded.Fingerprint != second.Fingerprint {
		t.Errorf("after re-issue, Load returned the first cert's fingerprint — Save did not replace")
	}
}

func TestCertStore_Delete(t *testing.T) {
	meta := t.TempDir()
	cs := NewCertStore(meta)
	issueForUser(t, NewCAStore(meta), cs, "alice")

	if err := cs.Delete("alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := cs.Delete("alice"); !errors.Is(err, ErrCertNotFound) {
		t.Errorf("second Delete: got %v, want ErrCertNotFound", err)
	}
	loaded, _ := cs.Load("alice")
	if loaded != nil {
		t.Errorf("Load after Delete returned a cert")
	}
}

func TestCertStore_List(t *testing.T) {
	meta := t.TempDir()
	ca := NewCAStore(meta)
	cs := NewCertStore(meta)
	issueForUser(t, ca, cs, "alice")
	issueForUser(t, ca, cs, "bob")

	// Non-JSON garbage sitting in the cert dir must not break List. This
	// happened in practice when an admin dropped a file by hand.
	if err := os.WriteFile(filepath.Join(meta, "_certs", "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write junk file: %v", err)
	}

	certs, err := cs.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(certs) != 2 {
		t.Errorf("List returned %d certs, want 2", len(certs))
	}
	seen := map[string]bool{}
	for _, c := range certs {
		seen[c.Username] = true
	}
	if !seen["alice"] || !seen["bob"] {
		t.Errorf("List missing user(s); got %v", seen)
	}
}

func TestCertStore_Revoke_FlipsFlagAndPersists(t *testing.T) {
	meta := t.TempDir()
	cs := NewCertStore(meta)
	issueForUser(t, NewCAStore(meta), cs, "alice")

	uc, err := cs.Revoke("alice")
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !uc.Revoked || uc.RevokedAt == nil {
		t.Errorf("Revoke result: Revoked=%v RevokedAt=%v, want true and non-nil", uc.Revoked, uc.RevokedAt)
	}

	// Persist across a fresh store instance.
	reloaded, err := NewCertStore(meta).Load("alice")
	if err != nil || reloaded == nil {
		t.Fatalf("reload: %v %v", reloaded, err)
	}
	if !reloaded.Revoked {
		t.Errorf("revocation did not persist to disk")
	}
}

func TestCertStore_Revoke_Idempotent(t *testing.T) {
	meta := t.TempDir()
	cs := NewCertStore(meta)
	issueForUser(t, NewCAStore(meta), cs, "alice")

	if _, err := cs.Revoke("alice"); err != nil {
		t.Fatalf("first Revoke: %v", err)
	}
	if _, err := cs.Revoke("alice"); err != nil {
		t.Errorf("second Revoke on already-revoked cert: %v (should be idempotent)", err)
	}
}

func TestCertStore_Revoke_MissingReturnsErrNotFound(t *testing.T) {
	cs := NewCertStore(t.TempDir())
	if _, err := cs.Revoke("ghost"); !errors.Is(err, ErrCertNotFound) {
		t.Errorf("Revoke of missing cert: got %v, want ErrCertNotFound", err)
	}
}

// Overwriting the on-disk cert file with a new issuance clears the
// per-file Revoked flag — that flag lives on the JSON blob that Save
// replaces atomically. The durable revocation record is the config
// fingerprint list; the cascade callback (tested below) is what keeps
// that list in sync when a re-issue supersedes an old cert.
func TestCertStore_ReIssueClearsPerFileRevokedFlag(t *testing.T) {
	meta := t.TempDir()
	ca := NewCAStore(meta)
	cs := NewCertStore(meta)
	issueForUser(t, ca, cs, "alice")

	if _, err := cs.Revoke("alice"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	issueForUser(t, ca, cs, "alice")

	loaded, _ := cs.Load("alice")
	if loaded == nil {
		t.Fatal("cert missing after re-issue")
	}
	if loaded.Revoked {
		t.Errorf("expected per-file Revoked flag to be cleared by overwrite, got Revoked=true")
	}
}

// Cascade: Save invokes OnPreviousCertOverwritten with the OLD
// fingerprint when a re-issue supersedes a prior cert. This is the hook
// production wires to config.Reviewflow.Signing.RevokedCerts so admins
// don't have to manually revoke the old cert before issuing a new one.
func TestCertStore_Save_CascadesPreviousFingerprint(t *testing.T) {
	meta := t.TempDir()
	ca := NewCAStore(meta)
	cs := NewCertStore(meta)

	var cascaded []string
	cs.OnPreviousCertOverwritten = func(fp string) {
		cascaded = append(cascaded, fp)
	}

	// First issue: no previous cert, no cascade.
	first := issueForUser(t, ca, cs, "alice")
	if len(cascaded) != 0 {
		t.Errorf("first issuance cascaded %v — expected no callback on empty prior", cascaded)
	}

	// Re-issue: the old fingerprint must be handed to the callback.
	second := issueForUser(t, ca, cs, "alice")
	if first.Fingerprint == second.Fingerprint {
		t.Fatalf("test invariant broken: two consecutive issuances produced identical fingerprints")
	}
	if len(cascaded) != 1 {
		t.Fatalf("re-issue cascaded %v — expected exactly one callback", cascaded)
	}
	if cascaded[0] != first.Fingerprint {
		t.Errorf("cascade fingerprint = %q, want %q (the previous cert's fingerprint)", cascaded[0], first.Fingerprint)
	}
}

// Saving the exact same PEM byte-for-byte (idempotent republish) must
// NOT cascade — nothing was superseded.
func TestCertStore_Save_IdempotentReplayDoesNotCascade(t *testing.T) {
	meta := t.TempDir()
	ca := NewCAStore(meta)
	cs := NewCertStore(meta)

	// Issue and grab the PEM.
	uc := issueForUser(t, ca, cs, "alice")

	cascaded := 0
	cs.OnPreviousCertOverwritten = func(string) { cascaded++ }

	if _, err := cs.Save("alice", uc.CertificatePEM); err != nil {
		t.Fatalf("Save (replay): %v", err)
	}
	if cascaded != 0 {
		t.Errorf("idempotent replay cascaded %d times — expected 0 (same fingerprint means nothing was superseded)", cascaded)
	}
}

// A nil callback stays a no-op — pure-store tests and any embedding
// that doesn't wire the callback see the previous, unchanged behaviour.
func TestCertStore_Save_NoCallbackIsNoOp(t *testing.T) {
	meta := t.TempDir()
	ca := NewCAStore(meta)
	cs := NewCertStore(meta) // no OnPreviousCertOverwritten set

	issueForUser(t, ca, cs, "alice")
	// Second issue — must not panic even though callback is nil.
	issueForUser(t, ca, cs, "alice")
}
