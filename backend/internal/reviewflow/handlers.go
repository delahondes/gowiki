package reviewflow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// RegisterReadRoutes registers read-only reviewflow endpoints.
func RegisterReadRoutes(r chi.Router, svc *Service) {
	r.Get("/status/*", handleGetStatus(svc))
	r.Get("/digest/*", handleGetDigest(svc))
	r.Get("/signatures/*", handleGetSignatures(svc))
	r.Get("/cert/{username}", handleGetUserCert(svc))
	r.Get("/audit/*", handleAuditExport(svc))
}

func handleGetUserCert(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := chi.URLParam(r, "username")
		if svc.certStore == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no cert store"})
			return
		}
		uc, err := svc.certStore.Load(username)
		if err != nil || uc == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no certificate found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"certificate_pem": uc.CertificatePEM,
			"fingerprint":     uc.Fingerprint,
			"issuer":          uc.Issuer,
			"not_after":       uc.NotAfter,
		})
	}
}

// RegisterWriteRoutes registers write reviewflow endpoints.
func RegisterWriteRoutes(r chi.Router, svc *Service, extractUsername func(*http.Request) string) {
	r.Post("/confirm/*", handleConfirm(svc, extractUsername))
	r.Put("/cert", handleUploadCert(svc, extractUsername))
	r.Post("/self-sign", handleSelfSign(svc, extractUsername))
}

func handleGetStatus(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := "/" + strings.TrimLeft(chi.URLParam(r, "*"), "/")

		var status *Status
		var err error
		if vStr := r.URL.Query().Get("v"); vStr != "" {
			v, parseErr := strconv.ParseInt(vStr, 10, 64)
			if parseErr != nil || v < 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version number"})
				return
			}
			status, err = svc.GetStatusForVersion(pagePath, v)
		} else {
			status, err = svc.GetStatus(pagePath)
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

type confirmRequest struct {
	Role        string `json:"role"`
	Signature   string `json:"signature,omitempty"`
	Certificate string `json:"certificate,omitempty"`
	Digest      string `json:"digest,omitempty"`
}

func handleConfirm(svc *Service, extractUsername func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := "/" + strings.TrimLeft(chi.URLParam(r, "*"), "/")

		var req confirmRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		if req.Role == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role is required"})
			return
		}

		username := extractUsername(r)
		if username == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}

		// Signing verification.
		var opts *ConfirmOpts
		if svc.signingVerifier != nil && svc.signingVerifier.IsEnabled() {
			if req.Signature != "" {
				// Verify the signature.
				page, err := svc.pageReader.Get(pagePath)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read page for digest verification"})
					return
				}
				if err := svc.signingVerifier.VerifySignature(req.Certificate, req.Signature, req.Digest, username, []byte(page.Markdown)); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "signature verification failed: " + err.Error()})
					return
				}
				cert, _ := parsePEMCertificate(req.Certificate)
				fp := ""
				if cert != nil {
					fp = certFingerprint(cert)
				}

				// Request a trusted timestamp from an external TSA.
				var tsToken string
				tsDigest, tsErr := ComputeTimestampDigest(req.Signature)
				if tsErr == nil {
					tsToken, _ = RequestTimestamp(tsDigest, "") // uses FreeTSA.org
				}

				opts = &ConfirmOpts{
					Signature:       req.Signature,
					Digest:          req.Digest,
					CertFingerprint: fp,
					CertificatePEM:  req.Certificate,
					TimestampToken:  tsToken,
				}
			} else if svc.signingVerifier.IsRequired() {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cryptographic signature required"})
				return
			}
		}

		status, err := svc.Confirm(pagePath, req.Role, username, opts)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func handleGetDigest(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := "/" + strings.TrimLeft(chi.URLParam(r, "*"), "/")

		if svc.pageReader == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "page reader not configured"})
			return
		}
		page, err := svc.pageReader.Get(pagePath)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "page not found"})
			return
		}

		digest := ComputeDigest([]byte(page.Markdown))
		writeJSON(w, http.StatusOK, map[string]any{
			"digest":       digest,
			"page_version": page.Meta.Version,
		})
	}
}

func handleGetSignatures(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := "/" + strings.TrimLeft(chi.URLParam(r, "*"), "/")

		st, err := svc.store.Load(pagePath)
		if err != nil || st == nil {
			writeJSON(w, http.StatusOK, map[string]any{"signatures": []any{}})
			return
		}

		vStr := r.URL.Query().Get("version")
		version := st.CurrentPageVersion
		if vStr != "" {
			if v, err := strconv.ParseInt(vStr, 10, 64); err == nil {
				version = v
			}
		}

		type sigInfo struct {
			Role            string `json:"role"`
			User            string `json:"user"`
			Timestamp       string `json:"timestamp"`
			Digest          string `json:"digest"`
			Signature       string `json:"signature"`
			CertFingerprint string `json:"cert_fingerprint"`
			Signed          bool   `json:"signed"`
		}

		var sigs []sigInfo
		for _, c := range st.Confirmations {
			if c.PageVersion != version {
				continue
			}
			sigs = append(sigs, sigInfo{
				Role:            c.Role,
				User:            c.User,
				Timestamp:       c.Timestamp.Format("2006-01-02T15:04:05Z"),
				Digest:          c.Digest,
				Signature:       c.Signature,
				CertFingerprint: c.CertFingerprint,
				Signed:          c.Signature != "",
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{"signatures": sigs})
	}
}

func handleUploadCert(svc *Service, extractUsername func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username := extractUsername(r)
		if username == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		if svc.certStore == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "certificate store not configured"})
			return
		}

		var req struct {
			CertificatePEM string `json:"certificate_pem"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}
		if req.CertificatePEM == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "certificate_pem is required"})
			return
		}

		uc, err := svc.certStore.Save(username, req.CertificatePEM)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"fingerprint": uc.Fingerprint,
			"subject":     uc.Subject,
			"issuer":      uc.Issuer,
			"not_after":   uc.NotAfter,
		})
	}
}

func handleAuditExport(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := "/" + strings.TrimLeft(chi.URLParam(r, "*"), "/")

		st, err := svc.store.Load(pagePath)
		if err != nil || st == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no reviewflow state for this page"})
			return
		}

		// Determine which version to export.
		version := st.CurrentPageVersion
		if vStr := r.URL.Query().Get("version"); vStr != "" {
			if v, err := strconv.ParseInt(vStr, 10, 64); err == nil {
				version = v
			}
		}

		// Get the page content.
		var markdownDigest string
		var markdownContent string
		var pageURL string
		if svc.pageReader != nil {
			page, err := svc.pageReader.Get(pagePath)
			if err == nil {
				markdownContent = page.Markdown
				markdownDigest = ComputeDigest([]byte(page.Markdown))
			}
		}
		// Build page URL from config base_url if available.
		if svc.configStore != nil {
			baseURL := svc.configStore.Get().Site.BaseURL
			if baseURL != "" {
				pageURL = strings.TrimRight(baseURL, "/") + pagePath
			}
		}

		// Collect signed confirmations for this version.
		type auditConfirmation struct {
			Role               string `json:"role"`
			User               string `json:"user"`
			Timestamp          string `json:"timestamp"`
			SignatureBase64    string `json:"signature_base64,omitempty"`
			CertificatePEM     string `json:"certificate_pem,omitempty"`
			CertFingerprint    string `json:"certificate_fingerprint,omitempty"`
			Digest             string `json:"digest,omitempty"`
			TimestampToken     string `json:"timestamp_token,omitempty"`
			Signed             bool   `json:"signed"`
		}

		var confirmations []auditConfirmation
		for _, c := range st.Confirmations {
			if c.PageVersion != version {
				continue
			}
			// If the confirmation doesn't have the PEM inline, try the cert store.
			certPEM := c.CertificatePEM
			if certPEM == "" && c.CertFingerprint != "" && svc.certStore != nil {
				uc, err := svc.certStore.Load(c.User)
				if err == nil && uc != nil && uc.Fingerprint == c.CertFingerprint {
					certPEM = uc.CertificatePEM
				}
			}
			confirmations = append(confirmations, auditConfirmation{
				Role:            c.Role,
				User:            c.User,
				Timestamp:       c.Timestamp.Format("2006-01-02T15:04:05Z"),
				SignatureBase64: c.Signature,
				CertificatePEM:  certPEM,
				CertFingerprint: c.CertFingerprint,
				Digest:          c.Digest,
				TimestampToken:  c.TimestampToken,
				Signed:          c.Signature != "",
			})
		}

		// Include the CA certificate so the export is self-sufficient.
		var caPEM string
		if svc.caStore != nil {
			caPEM = svc.caStore.GetCACert()
		}

		// Include the revocation list from config.
		var revokedCerts []string
		if svc.configStore != nil {
			revokedCerts = svc.configStore.Get().Reviewflow.Signing.RevokedCerts
		}

		// Build the audit export.
		export := map[string]any{
			"page":               pagePath,
			"page_url":           pageURL,
			"version":            version,
			"version_tag":        st.VersionTag,
			"markdown_sha256":    markdownDigest,
			"markdown":           markdownContent,
			"ca_certificate_pem": caPEM,
			"revoked_certs":      revokedCerts,
			"signed_payload_spec": "SHA-256 of raw UTF-8 markdown bytes, signed with ECDSA P-256 (IEEE P1363 format, 64 bytes: r || s, 32 bytes each)",
			"exported_at":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			"confirmations":      confirmations,
		}

		// Set download filename.
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=\"audit-%s-v%d.json\"",
				strings.ReplaceAll(strings.Trim(pagePath, "/"), "/", "-"), version))
		writeJSON(w, http.StatusOK, export)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
