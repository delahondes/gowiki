package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/auth"
)

// requireAdmin is middleware that checks the authenticated user belongs to the "admin" group.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := UsernameFromContext(r.Context())
		if username == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !s.userStore.IsAdmin(username) {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- User management handlers ---

func (s *Server) handleListUsers(w http.ResponseWriter, _ *http.Request) {
	users := s.userStore.List()
	// Strip password hashes before returning.
	safe := make([]map[string]any, len(users))
	for i, u := range users {
		oauthGroups := u.OAuthGroups
		if oauthGroups == nil {
			oauthGroups = []string{}
		}
		safe[i] = map[string]any{
			"username":     u.Username,
			"email":        u.Email,
			"display_name": u.DisplayName,
			"groups":       u.Groups,
			"oauth_groups": oauthGroups,
			"disabled":     u.Disabled,
			"created_at":   u.CreatedAt,
			"last_login":   u.LastLogin,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": safe})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		Email       string   `json:"email"`
		DisplayName string   `json:"display_name"`
		Groups      []string `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	user := auth.User{
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Groups:      req.Groups,
	}
	if err := s.userStore.Create(user, req.Password); err != nil {
		if errors.Is(err, auth.ErrUserExists) {
			writeError(w, http.StatusConflict, "user already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "user created", "username": req.Username})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "" {
		writeError(w, http.StatusBadRequest, "missing username")
		return
	}

	var updates auth.UserUpdate
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := s.userStore.Update(username, updates); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "user updated"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "" {
		writeError(w, http.StatusBadRequest, "missing username")
		return
	}

	// Prevent self-deletion.
	caller := UsernameFromContext(r.Context())
	if caller == username {
		writeError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}

	if err := s.userStore.Delete(username); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "user deleted"})
}

func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	if username == "" {
		writeError(w, http.StatusBadRequest, "missing username")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}

	if err := s.userStore.SetPassword(username, req.Password); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "password updated"})
}

// --- Group management handlers ---

func (s *Server) handleListGroups(w http.ResponseWriter, _ *http.Request) {
	groups := s.groupStore.List()
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "group name is required")
		return
	}

	group := auth.Group{
		Name:        req.Name,
		Description: req.Description,
	}
	if err := s.groupStore.Create(group); err != nil {
		if errors.Is(err, auth.ErrGroupExists) {
			writeError(w, http.StatusConflict, "group already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "group created", "name": req.Name})
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing group name")
		return
	}

	var req struct {
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	if err := s.groupStore.Update(name, req.Description); err != nil {
		if errors.Is(err, auth.ErrGroupNotFound) {
			writeError(w, http.StatusNotFound, "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "group updated"})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing group name")
		return
	}

	if err := s.groupStore.Delete(name); err != nil {
		if errors.Is(err, auth.ErrGroupNotFound) {
			writeError(w, http.StatusNotFound, "group not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "group deleted"})
}
