package reviewflow

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrCertNotFound = errors.New("certificate not found")
	ErrInvalidPEM   = errors.New("invalid PEM certificate")
)

// CertStore manages user X.509 certificates on disk.
type CertStore struct {
	dir string // data/meta/_certs/

	// OnPreviousCertOverwritten, if set, is invoked during Save when a
	// previous cert existed for the same username with a different
	// fingerprint. Callers (main.go) wire this to append the old
	// fingerprint to config.Reviewflow.Signing.RevokedCerts, so re-issuing
	// a cert automatically revokes the one it superseded — without
	// requiring an admin to remember a separate revoke step. Pure tests
	// leave it nil and observe the absence of a cascade.
	OnPreviousCertOverwritten func(previousFingerprint string)
}

// NewCertStore creates a certificate store in the meta directory.
func NewCertStore(metaRoot string) *CertStore {
	dir := filepath.Join(metaRoot, "_certs")
	os.MkdirAll(dir, 0o755)
	return &CertStore{dir: dir}
}

// Save persists a user certificate. The PEM is parsed and validated before
// saving. If a previous cert existed for the same username with a
// different fingerprint and OnPreviousCertOverwritten is set, the old
// fingerprint is passed to that callback AFTER the new cert is on disk —
// so the callback firing implies the supersession actually happened.
func (cs *CertStore) Save(username, certPEM string) (*UserCertificate, error) {
	cert, err := parsePEMCertificate(certPEM)
	if err != nil {
		return nil, err
	}

	fingerprint := certFingerprint(cert)

	// Capture the previous cert (if any) BEFORE we overwrite. Errors here
	// are silent: if the previous file is corrupt we can't cascade its
	// fingerprint, but we still want the new cert to write successfully.
	var previousFingerprint string
	if prev, err := cs.Load(username); err == nil && prev != nil {
		previousFingerprint = prev.Fingerprint
	}

	uc := &UserCertificate{
		Username:       username,
		CertificatePEM: certPEM,
		Fingerprint:    fingerprint,
		Issuer:         cert.Issuer.String(),
		Subject:        cert.Subject.String(),
		NotBefore:      cert.NotBefore,
		NotAfter:       cert.NotAfter,
		UploadedAt:     time.Now().UTC(),
	}

	data, err := json.MarshalIndent(uc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal certificate: %w", err)
	}

	path := cs.certPath(username)
	tmp, err := os.CreateTemp(cs.dir, "cert-*.json.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return nil, err
	}
	tmp.Close()
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}

	// Cascade revocation of the superseded cert. Fingerprints differ only
	// when the key material changed (same key + re-signed by the same CA
	// yields the same fingerprint via the whole-cert-DER hash, but
	// re-signing on a new day still changes NotBefore, which changes the
	// DER, which changes the fingerprint — so in practice every re-issue
	// cascades). No self-cascade if the fingerprint is identical (idempotent
	// republish of the same PEM).
	if previousFingerprint != "" && previousFingerprint != fingerprint && cs.OnPreviousCertOverwritten != nil {
		cs.OnPreviousCertOverwritten(previousFingerprint)
	}

	return uc, nil
}

// Load returns the certificate for a user, or nil if not found.
func (cs *CertStore) Load(username string) (*UserCertificate, error) {
	data, err := os.ReadFile(cs.certPath(username))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var uc UserCertificate
	if err := json.Unmarshal(data, &uc); err != nil {
		return nil, err
	}
	return &uc, nil
}

// Delete removes a user's certificate.
func (cs *CertStore) Delete(username string) error {
	err := os.Remove(cs.certPath(username))
	if errors.Is(err, os.ErrNotExist) {
		return ErrCertNotFound
	}
	return err
}

// List returns all stored certificates.
func (cs *CertStore) List() ([]UserCertificate, error) {
	entries, err := os.ReadDir(cs.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var certs []UserCertificate
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cs.dir, e.Name()))
		if err != nil {
			continue
		}
		var uc UserCertificate
		if json.Unmarshal(data, &uc) == nil {
			certs = append(certs, uc)
		}
	}
	return certs, nil
}

// Revoke marks a user's certificate as revoked. It updates the cert store file
// and returns the revoked certificate (with RevokedAt set).
func (cs *CertStore) Revoke(username string) (*UserCertificate, error) {
	uc, err := cs.Load(username)
	if err != nil || uc == nil {
		return nil, ErrCertNotFound
	}
	if uc.Revoked {
		return uc, nil // already revoked
	}
	now := time.Now().UTC()
	uc.Revoked = true
	uc.RevokedAt = &now

	data, err := json.MarshalIndent(uc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal certificate: %w", err)
	}
	if err := os.WriteFile(cs.certPath(username), data, 0o600); err != nil {
		return nil, fmt.Errorf("write certificate: %w", err)
	}
	return uc, nil
}

func (cs *CertStore) certPath(username string) string {
	return filepath.Join(cs.dir, username+".json")
}

// parsePEMCertificate decodes a PEM string and returns the X.509 certificate.
func parsePEMCertificate(pemStr string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, ErrInvalidPEM
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
}

// certFingerprint returns the SHA-256 hex fingerprint of a DER-encoded certificate.
func certFingerprint(cert *x509.Certificate) string {
	h := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(h[:])
}
