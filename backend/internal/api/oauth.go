package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"gowiki/backend/internal/auth"
)

// handleAuthProviders returns which login methods are available.
// GET /api/auth/providers
func (s *Server) handleAuthProviders(w http.ResponseWriter, r *http.Request) {
	cfg := s.configStore.Get()
	providers := []map[string]string{}

	if cfg.Auth.OAuth.Provider == "azure" && cfg.Auth.OAuth.ClientID != "" {
		providers = append(providers, map[string]string{
			"name":  "azure",
			"label": "Microsoft 365",
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"local":     true,
		"providers": providers,
	})
}

// handleOAuthLogin redirects the user to the OAuth provider's authorization page.
// GET /api/auth/oauth/login
func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.oauthClient == nil {
		// Try to initialize the client on the fly from config.
		if err := s.initOAuthClient(); err != nil {
			writeError(w, http.StatusServiceUnavailable, "OAuth is not configured")
			return
		}
	}

	// Build absolute callback URL from the request (Azure requires absolute URIs).
	origin := requestOrigin(r)
	callbackURL := origin + oauthCallbackPath
	s.oauthClient.SetRedirectURL(callbackURL)

	// Preserve the page the user was on so we can redirect back after login.
	returnTo := r.URL.Query().Get("return_to")
	returnURL := origin
	if returnTo != "" {
		returnURL = origin + returnTo
	}

	url, _ := s.oauthClient.AuthorizationURL(callbackURL, returnURL)
	http.Redirect(w, r, url, http.StatusFound)
}

// handleOAuthCallback handles the OAuth provider's redirect after authentication.
// GET /api/auth/oauth/callback?code=...&state=...
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oauthClient == nil {
		http.Error(w, "OAuth is not configured", http.StatusServiceUnavailable)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errParam := r.URL.Query().Get("error")

	if errParam != "" {
		desc := r.URL.Query().Get("error_description")
		log.Printf("oauth callback error: %s — %s", errParam, desc)
		serveOAuthError(w, fmt.Sprintf("Authentication failed: %s", errParam))
		return
	}

	if code == "" || state == "" {
		serveOAuthError(w, "Missing code or state parameter")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := s.oauthClient.ExchangeAndVerify(ctx, code, state)
	if err != nil {
		log.Printf("oauth exchange error: %v", err)
		serveOAuthError(w, "Authentication failed. Please try again.")
		return
	}

	claims := result.Claims

	// Fetch Azure AD groups via Graph API.
	var azureGroups []string
	if result.AccessToken != "" {
		groups, gErr := auth.FetchGroups(ctx, result.AccessToken)
		if gErr != nil {
			log.Printf("oauth: failed to fetch groups for %q: %v (continuing without group sync)", claims.Email, gErr)
		} else {
			azureGroups = groups
			log.Printf("oauth: fetched %d groups for %q: %v", len(groups), claims.Email, groups)

			// Ensure Azure groups exist in the GroupStore so they appear in the Groups tab.
			for _, g := range groups {
				_ = s.groupStore.Create(auth.Group{
					Name:        g,
					Description: "Imported from Azure AD",
				})
			}
		}
	}

	// Look up user by email.
	user, err := s.userStore.GetByEmail(claims.Email)
	if errors.Is(err, auth.ErrUserNotFound) {
		// Auto-create if configured.
		cfg := s.configStore.Get()
		if !cfg.Auth.OAuth.AutoCreateUsers {
			log.Printf("oauth: no user for email %q and auto-create is disabled", claims.Email)
			serveOAuthError(w, fmt.Sprintf("No account found for %s. Contact an administrator.", claims.Email))
			return
		}

		// Derive username from email (part before @).
		username := strings.Split(claims.Email, "@")[0]
		username = sanitizeUsername(username)

		// Ensure uniqueness.
		if _, existErr := s.userStore.Get(username); existErr == nil {
			username = username + "_oauth"
		}

		displayName := claims.Name
		if displayName == "" {
			displayName = claims.Email
		}

		defaultGroups := cfg.Auth.OAuth.DefaultGroups
		if defaultGroups == nil {
			defaultGroups = []string{}
		}

		newUser := auth.User{
			Username:    username,
			Email:       claims.Email,
			DisplayName: displayName,
			Groups:      defaultGroups,
			OAuthGroups: azureGroups,
		}
		if createErr := s.userStore.Create(newUser, ""); createErr != nil {
			log.Printf("oauth: failed to auto-create user for %q: %v", claims.Email, createErr)
			serveOAuthError(w, "Failed to create account. Contact an administrator.")
			return
		}
		log.Printf("oauth: auto-created user %q for email %q (local groups: %v, oauth groups: %v)", username, claims.Email, defaultGroups, azureGroups)
		user = newUser
	} else if err != nil {
		log.Printf("oauth: user lookup error for %q: %v", claims.Email, err)
		serveOAuthError(w, "Internal error. Please try again.")
		return
	} else if len(azureGroups) > 0 {
		// Existing user — sync only OAuthGroups, leave local Groups untouched.
		if err := s.userStore.Update(user.Username, auth.UserUpdate{OAuthGroups: &azureGroups}); err != nil {
			log.Printf("oauth: failed to sync oauth groups for %q: %v", user.Username, err)
		} else {
			log.Printf("oauth: synced oauth groups for %q: %v (local groups preserved: %v)", user.Username, azureGroups, user.Groups)
			user.OAuthGroups = azureGroups
		}
	}

	if user.Disabled {
		serveOAuthError(w, "Your account is disabled. Contact an administrator.")
		return
	}

	// Create session — same as local login.
	s.userStore.UpdateLastLogin(user.Username)
	sessionID := s.sessionStore.Create(user.Username)
	auth.SetSessionCookie(w, sessionID)

	// Redirect back to the page the user was on before login.
	redirectTo := "/"
	if result.Origin != "" {
		redirectTo = result.Origin
		// Ensure there's at least a trailing slash for bare origins.
		if !strings.Contains(strings.TrimPrefix(strings.TrimPrefix(redirectTo, "https://"), "http://"), "/") {
			redirectTo += "/"
		}
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// initOAuthClient initializes the OAuth client from current config.
func (s *Server) initOAuthClient() error {
	cfg := s.configStore.Get()
	if cfg.Auth.OAuth.Provider != "azure" || cfg.Auth.OAuth.ClientID == "" {
		return fmt.Errorf("oauth not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := auth.NewOAuthClient(ctx, auth.OAuthConfig{
		TenantID:     cfg.Auth.OAuth.TenantID,
		ClientID:     cfg.Auth.OAuth.ClientID,
		ClientSecret: cfg.Auth.OAuth.ClientSecret,
		RedirectURL:  oauthCallbackPath,
	})
	if err != nil {
		return err
	}

	s.oauthClient = client
	return nil
}

// oauthCallbackPath is the fixed path for the OAuth callback.
const oauthCallbackPath = "/api/auth/oauth/callback"

// sanitizeUsername makes an email prefix safe for use as a username.
func sanitizeUsername(raw string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(raw) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			b.WriteRune(c)
		}
	}
	s := b.String()
	if s == "" {
		return "user"
	}
	return s
}

// requestOrigin extracts the origin (scheme://host) the user is actually
// browsing from, accounting for reverse proxies (Vite dev, nginx, etc.)
// that may rewrite the Host header.
func requestOrigin(r *http.Request) string {
	// 1. X-Forwarded-Host (set by well-configured reverse proxies).
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			scheme = "http"
		}
		return scheme + "://" + fh
	}

	// 2. Origin header (set by browsers on navigations triggered by links/forms).
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
		return origin
	}

	// 3. Referer header — extract scheme://host.
	if ref := r.Header.Get("Referer"); ref != "" {
		if i := strings.Index(ref, "//"); i >= 0 {
			// Find the end of the host part (next /).
			rest := ref[i+2:]
			if j := strings.Index(rest, "/"); j >= 0 {
				return ref[:i+2+j]
			}
			return ref
		}
	}

	// 4. Fallback: use r.Host directly.
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// serveOAuthError renders a simple HTML error page (since this is a browser redirect flow).
func serveOAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Login Failed</title>
<style>body{font-family:sans-serif;max-width:500px;margin:80px auto;text-align:center}
h1{color:#c0392b}a{color:#2980b9}</style></head>
<body><h1>Login Failed</h1><p>%s</p><p><a href="/">Return to wiki</a></p></body></html>`, message)
}
