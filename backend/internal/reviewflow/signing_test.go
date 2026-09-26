package reviewflow

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gowiki/backend/internal/config"
)

// --- helpers ----------------------------------------------------------

// signedFixture packages everything a VerifySignature test needs in one
// value: the user's cert PEM, the signature (in P1363 form as Web Crypto
// produces), the digest, and the private key (in case a test wants to
// re-sign different payloads).
type signedFixture struct {
	priv     *ecdsa.PrivateKey
	certPEM  string
	certObj  *x509.Certificate
	markdown []byte
	digest   string
	sigB64   string
}

// sigP1363 signs `payload` with ECDSA-SHA256 in P1363 (r || s) form, the
// same wire format Web Crypto produces in the browser. VerifySignature
// converts back to ASN.1 for Go's ecdsa.VerifyASN1.
func sigP1363(t *testing.T, priv *ecdsa.PrivateKey, payload []byte) string {
	t.Helper()
	hash := sha256.Sum256(payload)
	r, s, err := ecdsa.Sign(rand.Reader, priv, hash[:])
	if err != nil {
		t.Fatalf("ecdsa.Sign: %v", err)
	}
	byteSize := (priv.Curve.Params().BitSize + 7) / 8
	sig := make([]byte, 2*byteSize)
	rb := r.Bytes()
	sb := s.Bytes()
	copy(sig[byteSize-len(rb):byteSize], rb)
	copy(sig[2*byteSize-len(sb):], sb)
	return base64.StdEncoding.EncodeToString(sig)
}

// newSigningFixture spins up a CA + certstore + user cert + valid
// signature over `markdown` — the whole happy-path prerequisite set.
func newSigningFixture(t *testing.T, markdown string) (*CAStore, *CertStore, signedFixture) {
	t.Helper()
	meta := t.TempDir()
	ca := NewCAStore(meta)
	cs := NewCertStore(meta)
	if _, err := ca.GenerateCA("Acme", "Acme CA"); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	priv, _ := mustGenSPKI(t)
	// mustGenSPKI returned a fresh keypair — but we already have `priv`,
	// so build SPKI from it directly for clarity.
	spki, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal SPKI: %v", err)
	}
	certPEM, err := ca.SignUserKey("alice", "", spki)
	if err != nil {
		t.Fatalf("SignUserKey: %v", err)
	}
	if _, err := cs.Save("alice", certPEM); err != nil {
		t.Fatalf("cert Save: %v", err)
	}

	certObj := mustParseCert(t, certPEM)
	digest := ComputeDigest([]byte(markdown))
	sig := sigP1363(t, priv, []byte(markdown))

	return ca, cs, signedFixture{
		priv:     priv,
		certPEM:  certPEM,
		certObj:  certObj,
		markdown: []byte(markdown),
		digest:   digest,
		sigB64:   sig,
	}
}

// newVerifier wires a Verifier over a tmp config file and the given
// certstore. Signing is Enabled but not Required (Required only gates
// whether unsigned commits are refused, not how VerifySignature
// evaluates a request).
func newVerifier(t *testing.T, cs *CertStore, trustStorePaths ...string) *SigningVerifier {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	store, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := store.Get()
	cfg.Reviewflow.Signing.Enabled = true
	cfg.Reviewflow.Signing.TrustStore = trustStorePaths
	if err := store.Update(cfg); err != nil {
		t.Fatalf("config.Update: %v", err)
	}
	return NewSigningVerifier(store, cs)
}

// --- tests ------------------------------------------------------------

func TestComputeDigest_MatchesSHA256Hex(t *testing.T) {
	got := ComputeDigest([]byte("hello world"))
	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got != want {
		t.Errorf("ComputeDigest = %s, want %s", got, want)
	}
}

func TestVerifySignature_HappyPath(t *testing.T) {
	_, cs, f := newSigningFixture(t, "Page body\n")
	v := newVerifier(t, cs)

	if err := v.VerifySignature(f.certPEM, f.sigB64, f.digest, "alice", f.markdown); err != nil {
		t.Errorf("happy-path VerifySignature failed: %v", err)
	}
}

func TestVerifySignature_TamperedPayloadFails(t *testing.T) {
	_, cs, f := newSigningFixture(t, "Page body\n")
	v := newVerifier(t, cs)

	tampered := append([]byte{}, f.markdown...)
	tampered[0] ^= 0x01 // flip one bit

	err := v.VerifySignature(f.certPEM, f.sigB64, f.digest, "alice", tampered)
	if err == nil {
		t.Fatal("tampered payload must be rejected")
	}
	// The digest check fires first (cheaper than crypto). Whichever
	// fires, we just need a rejection.
	if !strings.Contains(err.Error(), "digest") && !strings.Contains(err.Error(), "verification") {
		t.Logf("tampered-payload rejection message: %v (accepted, just documenting)", err)
	}
}

func TestVerifySignature_WrongUserCertFails(t *testing.T) {
	// Alice has a cert, Bob has a different cert; verify Bob's signed
	// payload but tell the verifier the caller is Alice. The verifier
	// must refuse because the cert fingerprint won't match Alice's
	// registered cert.
	meta := t.TempDir()
	ca := NewCAStore(meta)
	cs := NewCertStore(meta)
	if _, err := ca.GenerateCA("Acme", "Acme CA"); err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	// Alice: fresh key + registered cert.
	alicePriv, _ := mustGenSPKI(t)
	aliceSPKI, _ := x509.MarshalPKIXPublicKey(&alicePriv.PublicKey)
	aliceCert, _ := ca.SignUserKey("alice", "", aliceSPKI)
	cs.Save("alice", aliceCert)

	// Bob: also has a cert, but Alice's certstore entry stays as-is.
	bobPriv, _ := mustGenSPKI(t)
	bobSPKI, _ := x509.MarshalPKIXPublicKey(&bobPriv.PublicKey)
	bobCert, _ := ca.SignUserKey("bob", "", bobSPKI)

	v := newVerifier(t, cs)

	// Sign a payload with Bob's key, submit with Bob's cert, but claim
	// to be Alice.
	body := []byte("Alice's page")
	bobSig := sigP1363(t, bobPriv, body)
	digest := ComputeDigest(body)

	err := v.VerifySignature(bobCert, bobSig, digest, "alice", body)
	if err == nil {
		t.Fatal("wrong-user cert must be rejected")
	}
	if !strings.Contains(err.Error(), "fingerprint") {
		t.Errorf("wrong-user rejection = %v, want a fingerprint-mismatch message", err)
	}
}

func TestVerifySignature_UnknownCA_TrustStoreRefuses(t *testing.T) {
	// Set up a legitimate trust store, then submit a cert issued by a
	// SEPARATE CA. Verification must refuse.
	meta := t.TempDir()
	trustedCA := NewCAStore(meta)
	trustedCA.GenerateCA("Trusted", "Trusted CA")
	trustedPath := filepath.Join(meta, "_ca", "ca.crt")

	// Rogue CA in a separate dir, issues its own cert for "alice".
	rogueCA := NewCAStore(t.TempDir())
	rogueCA.GenerateCA("Rogue", "Rogue CA")
	rogueKey, _ := mustGenSPKI(t)
	rogueSPKI, _ := x509.MarshalPKIXPublicKey(&rogueKey.PublicKey)
	rogueCert, err := rogueCA.SignUserKey("alice", "", rogueSPKI)
	if err != nil {
		t.Fatalf("rogue SignUserKey: %v", err)
	}

	cs := NewCertStore(meta) // no cert registered — force TrustStore path only
	v := newVerifier(t, cs, trustedPath)

	body := []byte("payload")
	sig := sigP1363(t, rogueKey, body)
	digest := ComputeDigest(body)

	if err := v.VerifySignature(rogueCert, sig, digest, "alice", body); err == nil {
		t.Fatal("cert signed by an untrusted CA must be rejected when trust_store is configured")
	}
}

func TestVerifySignature_ExpiredCertFails(t *testing.T) {
	// Hand-craft a user cert with NotBefore/NotAfter both in the past.
	// The CAStore always issues future-dated certs, so we skip it and
	// sign the user cert directly.
	meta := t.TempDir()
	ca := NewCAStore(meta)
	ca.GenerateCA("Acme", "Acme CA")

	// Load the CA key/cert to sign a manually-constructed expired cert.
	caCertPEM := ca.GetCACert()
	caCertBlock, _ := pem.Decode([]byte(caCertPEM))
	caCert, _ := x509.ParseCertificate(caCertBlock.Bytes)

	// Read the CA key off disk.
	caKeyBytes, err := os.ReadFile(filepath.Join(meta, "_ca", "ca.key"))
	if err != nil {
		t.Fatalf("read CA key: %v", err)
	}
	caKeyBlock, _ := pem.Decode(caKeyBytes)
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		t.Fatalf("parse CA key: %v", err)
	}

	userPriv, _ := mustGenSPKI(t)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: "alice"},
		NotBefore:             time.Now().Add(-2 * time.Hour),
		NotAfter:              time.Now().Add(-1 * time.Hour), // expired an hour ago
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	expiredDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &userPriv.PublicKey, caKey)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	expiredPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: expiredDER}))

	// Register the expired cert so the fingerprint check doesn't fire
	// first (we want the expiry check to be what refuses).
	cs := NewCertStore(meta)
	cs.Save("alice", expiredPEM)
	v := newVerifier(t, cs)

	body := []byte("payload")
	sig := sigP1363(t, userPriv, body)
	digest := ComputeDigest(body)

	err = v.VerifySignature(expiredPEM, sig, digest, "alice", body)
	if err == nil {
		t.Fatal("expired cert must be rejected")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expired-cert rejection = %v, want a message mentioning expiry", err)
	}
}

func TestVerifySignature_RevokedByConfigFails(t *testing.T) {
	// This is the invariant the "purge stale revoked cert" bug depended on.
	// A revoked fingerprint in config.Reviewflow.Signing.RevokedCerts must
	// cause VerifySignature to refuse, even for a valid signature over the
	// right payload.
	_, cs, f := newSigningFixture(t, "Page body\n")
	v := newVerifier(t, cs)

	cfg := v.configStore.Get()
	cfg.Reviewflow.Signing.RevokedCerts = []config.RevokedCert{
		{Fingerprint: certFingerprint(f.certObj), RevokedAt: time.Now().UTC().Format(time.RFC3339)},
	}
	if err := v.configStore.Update(cfg); err != nil {
		t.Fatalf("config.Update: %v", err)
	}

	err := v.VerifySignature(f.certPEM, f.sigB64, f.digest, "alice", f.markdown)
	if err == nil {
		t.Fatal("revoked cert must be rejected")
	}
	if !strings.Contains(err.Error(), "revoked") {
		t.Errorf("revoked-cert rejection = %v, want 'revoked'", err)
	}
}

func TestVerifySignature_DigestMismatchFails(t *testing.T) {
	// Caller supplies a valid signature and cert but the WRONG digest.
	// Server must refuse — the digest is the anti-tampering handshake.
	_, cs, f := newSigningFixture(t, "Page body\n")
	v := newVerifier(t, cs)

	err := v.VerifySignature(f.certPEM, f.sigB64, "0000000000000000000000000000000000000000000000000000000000000000", "alice", f.markdown)
	if err == nil {
		t.Fatal("wrong digest must be rejected")
	}
	if !strings.Contains(err.Error(), "digest") {
		t.Errorf("digest-mismatch rejection = %v, want 'digest'", err)
	}
}

func TestVerifySignature_InvalidBase64SignatureFails(t *testing.T) {
	_, cs, f := newSigningFixture(t, "Page body\n")
	v := newVerifier(t, cs)

	if err := v.VerifySignature(f.certPEM, "!!!not base64!!!", f.digest, "alice", f.markdown); err == nil {
		t.Fatal("invalid base64 signature must be rejected")
	}
}

func TestVerifySignature_MalformedCertFails(t *testing.T) {
	_, cs, f := newSigningFixture(t, "Page body\n")
	v := newVerifier(t, cs)

	if err := v.VerifySignature("not a PEM cert", f.sigB64, f.digest, "alice", f.markdown); err == nil {
		t.Fatal("malformed cert PEM must be rejected")
	}
}

func TestVerifySignature_Concurrent_NoRace(t *testing.T) {
	// Run many concurrent verifications through one verifier — the
	// race detector will catch shared-state trouble in the config/cert
	// stores. This test is the reason the whole suite is invoked with
	// go test -race.
	_, cs, f := newSigningFixture(t, "Page body\n")
	v := newVerifier(t, cs)

	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := v.VerifySignature(f.certPEM, f.sigB64, f.digest, "alice", f.markdown); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent VerifySignature returned %v", err)
	}
}

// TestSigningVerifier_IsEnabledIsRequired exercises the config wrapper
// helpers separately from VerifySignature so a refactor of either can't
// silently break the middleware that reads them.
func TestSigningVerifier_IsEnabledIsRequired(t *testing.T) {
	meta := t.TempDir()
	cs := NewCertStore(meta)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	store, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	v := NewSigningVerifier(store, cs)

	// Defaults: disabled, not required.
	if v.IsEnabled() || v.IsRequired() {
		t.Errorf("default: IsEnabled=%v IsRequired=%v, want false/false", v.IsEnabled(), v.IsRequired())
	}

	cfg := store.Get()
	cfg.Reviewflow.Signing.Enabled = true
	store.Update(cfg)
	if !v.IsEnabled() || v.IsRequired() {
		t.Errorf("enabled: IsEnabled=%v IsRequired=%v, want true/false", v.IsEnabled(), v.IsRequired())
	}

	cfg = store.Get()
	cfg.Reviewflow.Signing.Required = true
	store.Update(cfg)
	if !v.IsRequired() {
		t.Errorf("required: IsRequired=false, want true")
	}
}

