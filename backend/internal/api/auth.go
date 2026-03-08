package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"gowiki/backend/internal/auth"
)

type contextKey string

const usernameKey contextKey = "username"

func UsernameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(usernameKey).(string)
	return v
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password required")
		return
	}

	if err := s.userStore.Verify(req.Username, req.Password); err != nil {
		if errors.Is(err, auth.ErrUserDisabled) {
			writeError(w, http.StatusForbidden, "account is disabled")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	s.userStore.UpdateLastLogin(req.Username)
	sessionID := s.sessionStore.Create(req.Username)
	auth.SetSessionCookie(w, sessionID)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err == nil {
		s.sessionStore.Delete(cookie.Value)
	}
	auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	sess, ok := s.sessionStore.Get(cookie.Value)
	if !ok {
		auth.ClearSessionCookie(w)
		writeError(w, http.StatusUnauthorized, "session expired")
		return
	}
	resp := map[string]any{
		"username": sess.Username,
		"is_admin": s.userStore.IsAdmin(sess.Username),
	}
	if user, err := s.userStore.Get(sess.Username); err == nil {
		resp["display_name"] = user.DisplayName
		resp["email"] = user.Email
	}
	writeJSON(w, http.StatusOK, resp)
}

// requireAuth is middleware that protects write endpoints.
// It sets the username in context for downstream handlers.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.CookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		sess, ok := s.sessionStore.Get(cookie.Value)
		if !ok {
			auth.ClearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		ctx := context.WithValue(r.Context(), usernameKey, sess.Username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// optionalAuth reads the session cookie if present and sets username in context,
// but does not reject unauthenticated requests.
func (s *Server) optionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.CookieName)
		if err == nil {
			if sess, ok := s.sessionStore.Get(cookie.Value); ok {
				ctx := context.WithValue(r.Context(), usernameKey, sess.Username)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
