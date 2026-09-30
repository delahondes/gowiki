package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gowiki/backend/internal/auth"
)

// formatRetryAfter renders a duration in the format RFC 9110 §10.2.3 asks
// of the Retry-After header — an integer count of seconds. time.Duration
// stringifies as e.g. "15.644403776s", which no HTTP client parses. Round
// up (a client waiting a fraction less than the server said still hits
// the closed window) and floor to 1 (a Retry-After of 0 tells the client
// to retry immediately and reset a burst that just ran out).
func formatRetryAfter(d time.Duration) string {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}

type contextKey string

const (
	usernameKey  contextKey = "username"
	tokenIDKey   contextKey = "token_id"
	tokenAuthKey contextKey = "token_auth"
)

func UsernameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(usernameKey).(string)
	return v
}

// IsTokenAuth returns true if the request was authenticated via API token.
func IsTokenAuth(ctx context.Context) bool {
	v, _ := ctx.Value(tokenAuthKey).(bool)
	return v
}

// TokenIDFromContext returns the API token ID if authenticated via token.
func TokenIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(tokenIDKey).(string)
	return v
}

// tryBearerAuth checks for a Bearer token in the Authorization header.
// Returns the username and token ID on success, or empty strings if not present/invalid.
// Returns an error only when a token is present but invalid (so the caller can reject).
func (s *Server) tryBearerAuth(r *http.Request) (username, tokenID string, err error) {
	if s.tokenStore == nil {
		return "", "", nil
	}

	// Check Authorization header first, then fall back to ?token= query param.
	var rawToken string
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer gwk_") {
		rawToken = strings.TrimPrefix(header, "Bearer ")
	} else if qToken := r.URL.Query().Get("token"); strings.HasPrefix(qToken, "gwk_") {
		rawToken = qToken
	} else {
		return "", "", nil
	}

	// Check if AI API is enabled.
	cfg := s.configStore.Get()
	if !cfg.AIAPI.Enabled {
		return "", "", errors.New("AI API is not enabled")
	}

	token, verifyErr := s.tokenStore.Verify(rawToken)
	if verifyErr != nil {
		if errors.Is(verifyErr, auth.ErrTokenExpired) {
			return "", "", errors.New("token expired")
		}
		return "", "", errors.New("invalid token")
	}

	// Check if user is disabled.
	user, userErr := s.userStore.Get(token.User)
	if userErr != nil {
		return "", "", errors.New("token user not found")
	}
	if user.Disabled {
		return "", "", errors.New("user account is disabled")
	}

	return token.User, token.ID, nil
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
	s.sessionStore.SetSessionCookie(w, r, sessionID)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.CookieName)
	if err == nil {
		s.sessionStore.Delete(cookie.Value)
	}
	auth.ClearSessionCookie(w, r)
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
		auth.ClearSessionCookie(w, r)
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

// mcpAuthChallenge wraps the MCP endpoint so 401 responses carry a
// WWW-Authenticate header pointing at the RFC 9728 protected-resource
// metadata document. Claude.ai and other MCP clients use this to discover
// the OAuth authorization server automatically — without it, the "Add
// connector" UI has no way to know how to authenticate.
func (s *Server) mcpAuthChallenge(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := requestOrigin(r)
		challenge := `Bearer resource_metadata="` + origin + `/.well-known/oauth-protected-resource"`
		next.ServeHTTP(&wwwAuthResponseWriter{ResponseWriter: w, challenge: challenge}, r)
	})
}

// wwwAuthResponseWriter injects WWW-Authenticate on any 401 that flows
// through the underlying handler. Set once, before WriteHeader is called
// downstream — that's the last chance to add response headers.
type wwwAuthResponseWriter struct {
	http.ResponseWriter
	challenge string
	set       bool
}

func (w *wwwAuthResponseWriter) WriteHeader(code int) {
	if code == http.StatusUnauthorized && !w.set {
		w.Header().Set("WWW-Authenticate", w.challenge)
		w.set = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// requireAuth is middleware that protects endpoints requiring authentication.
// It checks Bearer token first, then falls back to session cookie.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try Bearer token first.
		username, tokenID, err := s.tryBearerAuth(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		if username != "" {
			ctx := r.Context()
			ctx = context.WithValue(ctx, usernameKey, username)
			ctx = context.WithValue(ctx, tokenAuthKey, true)
			ctx = context.WithValue(ctx, tokenIDKey, tokenID)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Fall back to session cookie.
		cookie, err := r.Cookie(auth.CookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		sess, ok := s.sessionStore.Get(cookie.Value)
		if !ok {
			auth.ClearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		// Mirror the bearer-auth guard: a disabled account must lose its
		// browser sessions too, otherwise disabling doesn't cut off a
		// user who was already logged in until the session TTL expires.
		if user, uErr := s.userStore.Get(sess.Username); uErr == nil && user.Disabled {
			s.sessionStore.Delete(cookie.Value)
			auth.ClearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "user account is disabled")
			return
		}
		ctx := context.WithValue(r.Context(), usernameKey, sess.Username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// optionalAuth reads the session cookie or Bearer token if present and sets
// username in context, but does not reject unauthenticated requests.
func (s *Server) optionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try Bearer token first.
		username, tokenID, tryErr := s.tryBearerAuth(r)
		if tryErr != nil {
			// A token was presented but is invalid — reject rather than
			// silently falling through to anonymous access.
			writeError(w, http.StatusUnauthorized, tryErr.Error())
			return
		}
		if username != "" {
			ctx := r.Context()
			ctx = context.WithValue(ctx, usernameKey, username)
			ctx = context.WithValue(ctx, tokenAuthKey, true)
			ctx = context.WithValue(ctx, tokenIDKey, tokenID)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Fall back to session cookie.
		cookie, err := r.Cookie(auth.CookieName)
		if err == nil {
			if sess, ok := s.sessionStore.Get(cookie.Value); ok {
				// Disabled account: drop the session cookie and fall through
				// to anonymous access rather than passing the disabled
				// username on to the downstream handler.
				if user, uErr := s.userStore.Get(sess.Username); uErr == nil && user.Disabled {
					s.sessionStore.Delete(cookie.Value)
					auth.ClearSessionCookie(w, r)
				} else {
					ctx := context.WithValue(r.Context(), usernameKey, sess.Username)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireSessionAuth is middleware that requires session cookie authentication.
// Token auth is explicitly rejected — used for token management endpoints
// to prevent a leaked token from minting new tokens.
func (s *Server) requireSessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject Bearer token auth.
		header := r.Header.Get("Authorization")
		if strings.HasPrefix(header, "Bearer gwk_") {
			writeError(w, http.StatusForbidden, "this endpoint requires browser session authentication, not API token")
			return
		}

		cookie, err := r.Cookie(auth.CookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		sess, ok := s.sessionStore.Get(cookie.Value)
		if !ok {
			auth.ClearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		ctx := context.WithValue(r.Context(), usernameKey, sess.Username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rateLimitToken applies rate limiting to token-authenticated requests.
func (s *Server) rateLimitToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsTokenAuth(r.Context()) {
			next.ServeHTTP(w, r)
			return
		}

		tokenID := TokenIDFromContext(r.Context())
		if tokenID == "" || s.rateLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		cfg := s.configStore.Get()
		// Classify by HTTP verb for the AI Content API. MCP is JSON-RPC over
		// POST, so a verb-based split would land every read tool (get_page,
		// list_namespace, get_reviewflow_status, …) in the tighter write
		// bucket and make a corpus-scale scan impossible. Bucket all MCP
		// traffic against the read limit; per-tool write authorization
		// remains enforced by each mutation tool's handler.
		isWrite := r.Method != http.MethodGet && r.Method != http.MethodHead
		if strings.HasPrefix(r.URL.Path, "/api/mcp/v1") {
			isWrite = false
		}

		allowed, retryAfter := s.rateLimiter.Allow(tokenID, isWrite, cfg.AIAPI.RateLimitRead, cfg.AIAPI.RateLimitWrite)
		if !allowed {
			w.Header().Set("Retry-After", formatRetryAfter(retryAfter))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}
