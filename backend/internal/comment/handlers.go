package comment

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RegisterReadRoutes registers read-only comment endpoints.
func RegisterReadRoutes(r chi.Router, svc *Service) {
	r.Get("/*", handleList(svc))
}

// RegisterWriteRoutes registers write comment endpoints.
func RegisterWriteRoutes(r chi.Router, svc *Service, extractUsername func(*http.Request) string, isAdmin func(*http.Request) bool) {
	r.Post("/*", handleCreate(svc, extractUsername))
	r.Put("/{commentID}", handleUpdate(svc, extractUsername, isAdmin))
	r.Patch("/{commentID}/resolve", handleResolve(svc, extractUsername))
	r.Patch("/{commentID}/toggle-ai", handleToggleAI(svc, extractUsername))
	r.Delete("/{commentID}", handleDelete(svc, extractUsername, isAdmin))
	r.Delete("/", handleDeleteAIComments(svc))
}

func extractPagePath(r *http.Request) string {
	return "/" + strings.TrimLeft(chi.URLParam(r, "*"), "/")
}

func handleList(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := extractPagePath(r)
		comments, err := svc.List(pagePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"comments": comments})
	}
}

type createRequest struct {
	Anchor Anchor `json:"anchor"`
	Text   string `json:"text"`
	AI     bool   `json:"ai,omitempty"`
}

func handleCreate(svc *Service, extractUsername func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := extractPagePath(r)
		username := extractUsername(r)
		if username == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}

		var req createRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}

		comment, err := svc.Create(pagePath, req.Anchor, req.Text, username, req.AI)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, comment)
	}
}

type updateRequest struct {
	Text string `json:"text"`
}

func handleUpdate(svc *Service, extractUsername func(*http.Request) string, isAdmin func(*http.Request) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The page path is embedded in the URL before the comment ID.
		// Route: PUT /api/plugin/comment/v1/{commentID}
		// We need the page path from a query param.
		pagePath := r.URL.Query().Get("page")
		if pagePath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "page query parameter is required"})
			return
		}
		pagePath = "/" + strings.TrimLeft(pagePath, "/")

		commentID := chi.URLParam(r, "commentID")
		username := extractUsername(r)
		if username == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}

		var req updateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
			return
		}

		if err := svc.Update(pagePath, commentID, req.Text, username, isAdmin(r)); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}

func handleResolve(svc *Service, extractUsername func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := r.URL.Query().Get("page")
		if pagePath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "page query parameter is required"})
			return
		}
		pagePath = "/" + strings.TrimLeft(pagePath, "/")

		commentID := chi.URLParam(r, "commentID")
		username := extractUsername(r)
		if username == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}

		if err := svc.Resolve(pagePath, commentID, username); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}

func handleDelete(svc *Service, extractUsername func(*http.Request) string, isAdmin func(*http.Request) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := r.URL.Query().Get("page")
		if pagePath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "page query parameter is required"})
			return
		}
		pagePath = "/" + strings.TrimLeft(pagePath, "/")

		commentID := chi.URLParam(r, "commentID")
		username := extractUsername(r)
		if username == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}

		if err := svc.Delete(pagePath, commentID, username, isAdmin(r)); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}

func handleToggleAI(svc *Service, extractUsername func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := r.URL.Query().Get("page")
		if pagePath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "page query parameter is required"})
			return
		}
		pagePath = "/" + strings.TrimLeft(pagePath, "/")

		commentID := chi.URLParam(r, "commentID")
		username := extractUsername(r)
		if username == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}

		if err := svc.ToggleAI(pagePath, commentID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}

func handleDeleteAIComments(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pagePath := r.URL.Query().Get("page")
		if pagePath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "page query parameter is required"})
			return
		}
		pagePath = "/" + strings.TrimLeft(pagePath, "/")

		removed, err := svc.DeleteAIComments(pagePath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
