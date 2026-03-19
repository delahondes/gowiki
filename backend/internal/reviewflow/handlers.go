package reviewflow

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RegisterReadRoutes registers read-only reviewflow endpoints.
func RegisterReadRoutes(r chi.Router, svc *Service) {
	r.Get("/status/*", handleGetStatus(svc))
	r.Get("/digest/*", handleGetDigest(svc))
	r.Get("/signatures/*", handleGetSignatures(svc))
	r.Get("/cert/{username}", handleGetUserCert(svc))
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
				opts = &ConfirmOpts{
					Signature:       req.Signature,
					Digest:          req.Digest,
					CertFingerprint: certFingerprint(cert),
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
