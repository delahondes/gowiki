package reviewflow

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"time"

	"gowiki/backend/internal/config"
)

// SigningVerifier verifies X.509 signatures on reviewflow confirmations.
type SigningVerifier struct {
	configStore *config.Store
	certStore   *CertStore
}

// NewSigningVerifier creates a signature verifier.
func NewSigningVerifier(configStore *config.Store, certStore *CertStore) *SigningVerifier {
	return &SigningVerifier{
		configStore: configStore,
		certStore:   certStore,
	}
}

// IsEnabled returns whether signing is enabled in the config.
func (sv *SigningVerifier) IsEnabled() bool {
	return sv.configStore.Get().Reviewflow.Signing.Enabled
}

// IsRequired returns whether signing is mandatory.
func (sv *SigningVerifier) IsRequired() bool {
	cfg := sv.configStore.Get().Reviewflow.Signing
	return cfg.Enabled && cfg.Required
}

// ComputeDigest returns the hex-encoded SHA-256 of the raw markdown bytes.
func ComputeDigest(markdown []byte) string {
	h := sha256.Sum256(markdown)
	return hex.EncodeToString(h[:])
}

// VerifySignature validates a signed confirmation request.
func (sv *SigningVerifier) VerifySignature(certPEM, signatureB64, digest, username string, pageMarkdown []byte) error {
	// Parse the certificate from the request.
	cert, err := parsePEMCertificate(certPEM)
	if err != nil {
		return fmt.Errorf("invalid certificate: %w", err)
	}

	// Check certificate validity period.
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate is not yet valid (valid from %s)", cert.NotBefore)
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate has expired (expired %s)", cert.NotAfter)
	}

	// Check revocation.
	fingerprint := certFingerprint(cert)
	cfg := sv.configStore.Get().Reviewflow.Signing
	for _, revoked := range cfg.RevokedCerts {
		if revoked == fingerprint {
			return fmt.Errorf("certificate has been revoked")
		}
	}

	// If a trust store is configured, verify the chain.
	if len(cfg.TrustStore) > 0 {
		roots := x509.NewCertPool()
		for _, path := range cfg.TrustStore {
			pemData, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read trust store %s: %w", path, err)
			}
			roots.AppendCertsFromPEM(pemData)
		}
		if _, err := cert.Verify(x509.VerifyOptions{
			Roots:       roots,
			CurrentTime: now,
			KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		}); err != nil {
			return fmt.Errorf("certificate chain verification failed: %w", err)
		}
	}

	// Check the certificate matches the registered one for this user (if any).
	if sv.certStore != nil {
		stored, err := sv.certStore.Load(username)
		if err == nil && stored != nil && stored.Fingerprint != fingerprint {
			return fmt.Errorf("certificate fingerprint does not match registered certificate for user %s", username)
		}
	}

	// Verify the digest matches the page content.
	expectedDigest := ComputeDigest(pageMarkdown)
	if digest != expectedDigest {
		return fmt.Errorf("digest mismatch: expected %s, got %s", expectedDigest, digest)
	}

	// Decode the signature.
	sigBytes, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("invalid base64 signature: %w", err)
	}

	// Verify the cryptographic signature.
	// The browser signs the raw markdown bytes with ECDSA + SHA-256 (Web Crypto hashes internally).
	// We need to verify against the SHA-256 hash of the markdown.
	hashBytes := sha256.Sum256(pageMarkdown)

	switch pub := cert.PublicKey.(type) {
	case *ecdsa.PublicKey:
		// Web Crypto produces P1363 format (r || s), Go needs ASN.1 DER.
		asn1Sig, err := p1363ToASN1(sigBytes, pub.Curve.Params().BitSize)
		if err != nil {
			return fmt.Errorf("signature format conversion failed: %w", err)
		}
		if !ecdsa.VerifyASN1(pub, hashBytes[:], asn1Sig) {
			return fmt.Errorf("ECDSA signature verification failed")
		}
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, hashBytes[:], sigBytes); err != nil {
			return fmt.Errorf("RSA signature verification failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported key type: %T", cert.PublicKey)
	}

	return nil
}

// p1363ToASN1 converts a P1363 (r || s) ECDSA signature to ASN.1 DER format.
// Web Crypto API produces P1363; Go's ecdsa.VerifyASN1 expects ASN.1 DER.
func p1363ToASN1(sig []byte, bitSize int) ([]byte, error) {
	byteSize := (bitSize + 7) / 8
	if len(sig) != 2*byteSize {
		return nil, fmt.Errorf("invalid P1363 signature length: expected %d, got %d", 2*byteSize, len(sig))
	}
	r := new(big.Int).SetBytes(sig[:byteSize])
	s := new(big.Int).SetBytes(sig[byteSize:])
	return asn1.Marshal(struct{ R, S *big.Int }{r, s})
}
