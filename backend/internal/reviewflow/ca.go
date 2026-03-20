package reviewflow

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/config"
)

// CAStore manages the company Certificate Authority files.
type CAStore struct {
	dir string // data/meta/_ca/
}

// NewCAStore creates a CA store in the meta directory.
func NewCAStore(metaRoot string) *CAStore {
	dir := filepath.Join(metaRoot, "_ca")
	os.MkdirAll(dir, 0o700) // restrictive permissions
	return &CAStore{dir: dir}
}

// HasCA returns true if a company CA exists.
func (cs *CAStore) HasCA() bool {
	_, err := os.Stat(filepath.Join(cs.dir, "ca.key"))
	return err == nil
}

// GenerateCA creates a new company root CA.
func (cs *CAStore) GenerateCA(org, cn string) (certPEM string, err error) {
	if cs.HasCA() {
		return "", errors.New("company CA already exists — delete it first to regenerate")
	}

	// Generate CA private key.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate CA key: %w", err)
	}

	// Create self-signed CA certificate.
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{org},
			CommonName:   cn,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		return "", fmt.Errorf("create CA certificate: %w", err)
	}

	// Save CA private key.
	keyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return "", fmt.Errorf("marshal CA key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(cs.dir, "ca.key"), keyPEM, 0o600); err != nil {
		return "", fmt.Errorf("write CA key: %w", err)
	}

	// Save CA certificate.
	certPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(filepath.Join(cs.dir, "ca.crt"), certPEMBytes, 0o644); err != nil {
		return "", fmt.Errorf("write CA cert: %w", err)
	}

	return string(certPEMBytes), nil
}

// GetCACert returns the CA certificate PEM, or empty if no CA exists.
func (cs *CAStore) GetCACert() string {
	data, err := os.ReadFile(filepath.Join(cs.dir, "ca.crt"))
	if err != nil {
		return ""
	}
	return string(data)
}

// GetCACertPath returns the path to the CA certificate file.
func (cs *CAStore) GetCACertPath() string {
	return filepath.Join(cs.dir, "ca.crt")
}

// SignCSR signs a user's public key (SPKI) with the company CA,
// creating a certificate. The admin calls this after verifying the user's identity.
func (cs *CAStore) SignUserKey(username, email string, pubKeySPKI []byte) (certPEM string, err error) {
	if !cs.HasCA() {
		return "", errors.New("no company CA — generate one first")
	}

	// Load CA key.
	caKeyPEM, err := os.ReadFile(filepath.Join(cs.dir, "ca.key"))
	if err != nil {
		return "", fmt.Errorf("read CA key: %w", err)
	}
	block, _ := pem.Decode(caKeyPEM)
	if block == nil {
		return "", errors.New("invalid CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse CA key: %w", err)
	}

	// Load CA cert.
	caCertPEM, err := os.ReadFile(filepath.Join(cs.dir, "ca.crt"))
	if err != nil {
		return "", fmt.Errorf("read CA cert: %w", err)
	}
	caCertBlock, _ := pem.Decode(caCertPEM)
	if caCertBlock == nil {
		return "", errors.New("invalid CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse CA cert: %w", err)
	}

	// Parse user's public key.
	pubKey, err := x509.ParsePKIXPublicKey(pubKeySPKI)
	if err != nil {
		return "", fmt.Errorf("parse user public key: %w", err)
	}

	// Create user certificate signed by the CA.
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: username,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, pubKey, caKey)
	if err != nil {
		return "", fmt.Errorf("sign user certificate: %w", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})), nil
}

// --- HTTP Handlers ---

// RegisterCARoutes registers admin CA management endpoints.
func RegisterCARoutes(r chi.Router, caStore *CAStore, certStore *CertStore, svc *Service, extractUsername func(*http.Request) string) {
	r.Get("/ca", handleGetCA(caStore))
	r.Post("/ca/generate", handleGenerateCA(caStore))
	r.Post("/ca/sign", handleSignUserKey(caStore, certStore, svc, extractUsername))
	r.Post("/ca/revoke/{username}", handleRevokeCert(certStore, svc))
}

func handleGetCA(caStore *CAStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cert := caStore.GetCACert()
		writeJSON(w, http.StatusOK, map[string]any{
			"has_ca":          caStore.HasCA(),
			"certificate_pem": cert,
		})
	}
}

func handleGenerateCA(caStore *CAStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Organization string `json:"organization"`
			CommonName   string `json:"common_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		if req.Organization == "" {
			req.Organization = "Gowiki"
		}
		if req.CommonName == "" {
			req.CommonName = req.Organization + " Document Signing CA"
		}

		certPEM, err := caStore.GenerateCA(req.Organization, req.CommonName)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"certificate_pem": certPEM,
			"message":         "Company CA generated. Configure the trust store to use it.",
		})
	}
}

func handleSignUserKey(caStore *CAStore, certStore *CertStore, svc *Service, extractUsername func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username      string `json:"username"`
			PublicKeySPKI string `json:"public_key_spki"` // base64
			Email         string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		if req.Username == "" || req.PublicKeySPKI == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and public_key_spki are required"})
			return
		}

		// Decode SPKI.
		spkiBytes, err := base64.StdEncoding.DecodeString(req.PublicKeySPKI)
		if err != nil {
			spkiBytes, err = base64.RawStdEncoding.DecodeString(req.PublicKeySPKI)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid base64 public key"})
				return
			}
		}

		certPEM, err := caStore.SignUserKey(req.Username, req.Email, spkiBytes)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		// Save to cert store.
		if certStore != nil {
			certStore.Save(req.Username, certPEM)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"certificate_pem": certPEM,
			"username":        req.Username,
		})
	}
}

func handleRevokeCert(certStore *CertStore, svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
		if username == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing username"})
			return
		}

		uc, err := certStore.Revoke(username)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "certificate not found"})
			return
		}

		// Also add to config revocation list so it's included in audit exports.
		if svc.configStore != nil {
			cfg := svc.configStore.Get()
			// Check if already in the list.
			found := false
			for _, rc := range cfg.Reviewflow.Signing.RevokedCerts {
				if rc.Fingerprint == uc.Fingerprint {
					found = true
					break
				}
			}
			if !found {
				revokedAt := ""
				if uc.RevokedAt != nil {
					revokedAt = uc.RevokedAt.Format(time.RFC3339)
				}
				cfg.Reviewflow.Signing.RevokedCerts = append(cfg.Reviewflow.Signing.RevokedCerts, config.RevokedCert{
					Fingerprint: uc.Fingerprint,
					RevokedAt:   revokedAt,
				})
				svc.configStore.Update(cfg)
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"revoked":    true,
			"username":   username,
			"revoked_at": uc.RevokedAt,
		})
	}
}
