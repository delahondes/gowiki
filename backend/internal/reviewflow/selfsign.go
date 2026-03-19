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
	"fmt"
	"math/big"
	"net/http"
	"time"
)

// handleSelfSign creates a self-signed certificate wrapping the user's
// browser-generated public key. The private key stays in the browser's
// IndexedDB — it never touches the server.
//
// For signing the certificate itself, we generate an ephemeral CA key.
// This is Level 1 (self-attestation) — suitable for testing and internal use.
func handleSelfSign(svc *Service, extractUsername func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := extractUsername(r)
		if username == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}

		var req struct {
			Username     string `json:"username"`
			PublicKeySPKI string `json:"public_key_spki"` // base64-encoded SPKI
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		if req.Username != username {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "can only generate certificate for yourself"})
			return
		}
		if req.PublicKeySPKI == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "public_key_spki is required"})
			return
		}

		// Decode the SPKI public key.
		spkiBytes, err := base64.StdEncoding.DecodeString(req.PublicKeySPKI)
		if err != nil {
			// Try raw base64 (no padding)
			spkiBytes, err = base64.RawStdEncoding.DecodeString(req.PublicKeySPKI)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid base64 public key"})
				return
			}
		}

		pubKey, err := x509.ParsePKIXPublicKey(spkiBytes)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid SPKI public key: " + err.Error()})
			return
		}

		// Generate an ephemeral signing key for the self-signed certificate.
		signingKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "signing key generation failed"})
			return
		}

		// Create the certificate with the user's public key, signed by the ephemeral key.
		serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		template := &x509.Certificate{
			SerialNumber: serialNumber,
			Subject: pkix.Name{
				CommonName:   username,
				Organization: []string{"Gowiki Self-Signed"},
			},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(365 * 24 * time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
			BasicConstraintsValid: true,
		}

		certDER, err := x509.CreateCertificate(rand.Reader, template, template, pubKey, signingKey)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "certificate creation failed: " + err.Error()})
			return
		}

		certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))

		// Save the certificate to the cert store.
		if svc.certStore != nil {
			if _, err := svc.certStore.Save(username, certPEM); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "certificate save failed: " + err.Error()})
				return
			}
		}

		// Parse the cert for fingerprint.
		cert, _ := x509.ParseCertificate(certDER)
		fp := ""
		if cert != nil {
			fp = certFingerprint(cert)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"certificate_pem": certPEM,
			"fingerprint":     fp,
			"subject":         fmt.Sprintf("CN=%s,O=Gowiki Self-Signed", username),
			"not_after":       template.NotAfter.Format(time.RFC3339),
		})
	}
}
