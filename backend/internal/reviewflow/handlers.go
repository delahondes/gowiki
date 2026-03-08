package reviewflow

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RegisterReadRoutes registers read-only reviewflow endpoints.
func RegisterReadRoutes(r chi.Router, svc *Service) {
	r.Get("/status/*", handleGetStatus(svc))
}

// RegisterWriteRoutes registers write reviewflow endpoints.
func RegisterWriteRoutes(r chi.Router, svc *Service, extractUsername func(*http.Request) string) {
	r.Post("/confirm/*", handleConfirm(svc, extractUsername))
}

func handleGetStatus(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := "/" + strings.TrimLeft(chi.URLParam(r, "*"), "/")
		status, err := svc.GetStatus(pagePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

type confirmRequest struct {
	Role string `json:"role"`
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

		status, err := svc.Confirm(pagePath, req.Role, username)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
