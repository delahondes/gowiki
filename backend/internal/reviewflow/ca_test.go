package reviewflow

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: generate a fresh ECDSA P-256 keypair and return its SPKI DER.
func mustGenSPKI(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	spki, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal SPKI: %v", err)
	}
	return priv, spki
}

// helper: parse a PEM certificate string into an *x509.Certificate.
func mustParseCert(t *testing.T, certPEM string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("no CERTIFICATE PEM block found in %q", certPEM)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

func TestCAStore_HasCA_EmptyDir(t *testing.T) {
	cs := NewCAStore(t.TempDir())
	if cs.HasCA() {
		t.Errorf("HasCA on a fresh dir must be false")
	}
	if got := cs.GetCACert(); got != "" {
		t.Errorf("GetCACert on a fresh dir returned %d bytes, want empty", len(got))
	}
}

func TestCAStore_GenerateCA_CreatesFilesAndReturnsPEM(t *testing.T) {
	dir := t.TempDir()
	cs := NewCAStore(dir)

	pem, err := cs.GenerateCA("Acme Corp", "Acme Signing CA")
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	if !strings.Contains(pem, "BEGIN CERTIFICATE") {
		t.Errorf("returned PEM has no CERTIFICATE block")
	}

	// Files were written where the store expects them, with tight perms
	// on the private key.
	caDir := filepath.Join(dir, "_ca")
	keyStat, err := os.Stat(filepath.Join(caDir, "ca.key"))
	if err != nil {
		t.Fatalf("ca.key not written: %v", err)
	}
	if perm := keyStat.Mode().Perm(); perm != 0o600 {
		t.Errorf("ca.key perms = %o, want 0600 — private key must not be world-readable", perm)
	}
	if _, err := os.Stat(filepath.Join(caDir, "ca.crt")); err != nil {
		t.Fatalf("ca.crt not written: %v", err)
	}

	if !cs.HasCA() {
		t.Errorf("HasCA after GenerateCA must be true")
	}

	// The cert is self-signed with CA basic constraints and long expiry.
	cert := mustParseCert(t, pem)
	if !cert.IsCA {
		t.Errorf("issued CA cert has IsCA=false")
	}
	if got := cert.Subject.CommonName; got != "Acme Signing CA" {
		t.Errorf("CA CN = %q, want %q", got, "Acme Signing CA")
	}
	if got := cert.Subject.Organization; len(got) != 1 || got[0] != "Acme Corp" {
		t.Errorf("CA O = %v, want [Acme Corp]", got)
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Errorf("CA cert missing KeyUsageCertSign")
	}
}

func TestCAStore_GenerateCA_RefusesWhenPresent(t *testing.T) {
	cs := NewCAStore(t.TempDir())
	if _, err := cs.GenerateCA("Acme", "Acme CA"); err != nil {
		t.Fatalf("first GenerateCA: %v", err)
	}
	if _, err := cs.GenerateCA("Acme", "Acme CA"); err == nil {
		t.Errorf("second GenerateCA must refuse; nil error would silently overwrite the CA private key")
	}
}

func TestCAStore_Reopen_SeesExistingCA(t *testing.T) {
	dir := t.TempDir()
	firstPEM, err := NewCAStore(dir).GenerateCA("Acme", "Acme CA")
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	// Reopen — a fresh CAStore over the same dir should see the CA and
	// return the same certificate bytes.
	second := NewCAStore(dir)
	if !second.HasCA() {
		t.Fatalf("reopened CAStore reports HasCA=false")
	}
	if got := second.GetCACert(); got != firstPEM {
		t.Errorf("GetCACert after reopen returned different PEM — is the CA path stable?")
	}
}

func TestCAStore_SignUserKey_HappyPath(t *testing.T) {
	cs := NewCAStore(t.TempDir())
	if _, err := cs.GenerateCA("Acme", "Acme CA"); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	_, spki := mustGenSPKI(t)

	certPEM, err := cs.SignUserKey("alice", "alice@example.com", spki)
	if err != nil {
		t.Fatalf("SignUserKey: %v", err)
	}
	cert := mustParseCert(t, certPEM)

	// Subject: only CommonName is set today. The email parameter is
	// accepted for API stability but is NOT included in the cert. If
	// somebody adds it (as a SAN or in Subject), update this test AND
	// document the change — silent addition would break audit tooling
	// that assumes only CN carries identity.
	if got := cert.Subject.CommonName; got != "alice" {
		t.Errorf("user cert CN = %q, want %q", got, "alice")
	}
	if len(cert.EmailAddresses) != 0 || strings.Contains(cert.Subject.String(), "alice@example.com") {
		t.Errorf("user cert unexpectedly carries the email address — subject=%q san=%v", cert.Subject, cert.EmailAddresses)
	}

	if cert.IsCA {
		t.Errorf("user cert must not be a CA")
	}
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Errorf("user cert missing KeyUsageDigitalSignature")
	}

	// The cert must verify against the CA.
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM([]byte(cs.GetCACert()))
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		t.Errorf("issued user cert must verify against its CA: %v", err)
	}
}

func TestCAStore_SignUserKey_RefusesWithoutCA(t *testing.T) {
	cs := NewCAStore(t.TempDir())
	_, spki := mustGenSPKI(t)
	if _, err := cs.SignUserKey("alice", "", spki); err == nil {
		t.Errorf("SignUserKey without a CA must refuse")
	}
}

func TestCAStore_SignUserKey_RejectsInvalidSPKI(t *testing.T) {
	cs := NewCAStore(t.TempDir())
	if _, err := cs.GenerateCA("Acme", "Acme CA"); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	if _, err := cs.SignUserKey("alice", "", []byte{0xde, 0xad, 0xbe, 0xef}); err == nil {
		t.Errorf("SignUserKey with garbage SPKI must refuse")
	}
}

func TestCAStore_SignUserKey_CorruptedCAKey(t *testing.T) {
	dir := t.TempDir()
	cs := NewCAStore(dir)
	if _, err := cs.GenerateCA("Acme", "Acme CA"); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	// Corrupt the CA key on disk. SignUserKey must then refuse rather
	// than silently issue an unusable cert.
	keyPath := filepath.Join(dir, "_ca", "ca.key")
	if err := os.WriteFile(keyPath, []byte("not a PEM key"), 0o600); err != nil {
		t.Fatalf("corrupt ca.key: %v", err)
	}
	_, spki := mustGenSPKI(t)
	if _, err := cs.SignUserKey("alice", "", spki); err == nil {
		t.Errorf("SignUserKey with a corrupted CA key must refuse")
	}
}
