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
}

// RegisterWriteRoutes registers write reviewflow endpoints.
func RegisterWriteRoutes(r chi.Router, svc *Service, extractUsername func(*http.Request) string) {
	r.Post("/confirm/*", handleConfirm(svc, extractUsername))
}

func handleGetStatus(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := "/" + strings.TrimLeft(chi.URLParam(r, "*"), "/")

		// Optional version query param for historical status.
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
