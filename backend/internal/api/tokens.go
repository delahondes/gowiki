package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/auth"
)

// handleListTokens returns the current user's API tokens.
// GET /api/tokens
func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())
	tokens := s.tokenStore.ListForUser(username)
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// handleCreateToken creates a new API token for the current user.
// POST /api/tokens
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	cfg := s.configStore.Get()
	token, plaintext, err := s.tokenStore.Create(username, req.Name, cfg.AIAPI.MaxTokensPerUser)
	if err != nil {
		if errors.Is(err, auth.ErrTooManyTokens) {
			writeError(w, http.StatusConflict, "maximum number of tokens reached")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         token.ID,
		"name":       token.Name,
		"token":      plaintext,
		"created_at": token.CreatedAt,
	})
}

// handleDeleteToken revokes one of the current user's API tokens.
// DELETE /api/tokens/{id}
func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())
	tokenID := chi.URLParam(r, "id")

	if err := s.tokenStore.DeleteForUser(tokenID, username); err != nil {
		if errors.Is(err, auth.ErrTokenNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": tokenID})
}

// handleAdminListTokens returns all API tokens across users.
// GET /api/admin/tokens
func (s *Server) handleAdminListTokens(w http.ResponseWriter, _ *http.Request) {
	tokens := s.tokenStore.List()
	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// handleAdminDeleteToken revokes any user's API token.
// DELETE /api/admin/tokens/{id}
func (s *Server) handleAdminDeleteToken(w http.ResponseWriter, r *http.Request) {
	tokenID := chi.URLParam(r, "id")

	if err := s.tokenStore.Delete(tokenID); err != nil {
		if errors.Is(err, auth.ErrTokenNotFound) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": tokenID})
}
