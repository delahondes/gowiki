package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OAuthClient manages the OIDC provider connection for Azure AD / Microsoft 365.
type OAuthClient struct {
	mu       sync.RWMutex
	provider *oidc.Provider
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier

	// In-flight state tokens for CSRF protection.
	// Maps state → redirect path (or empty string for default).
	states sync.Map
}

// OAuthConfig holds the parameters needed to configure the OIDC provider.
type OAuthConfig struct {
	TenantID     string
	ClientID     string
	ClientSecret string
	RedirectURL  string // e.g. "http://localhost:8080/api/auth/oauth/callback"
}

// OAuthClaims holds the user info extracted from the ID token.
type OAuthClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	UPN   string `json:"upn"`                // Azure-specific: user principal name
	OID   string `json:"oid"`                // Azure object ID
	Sub   string `json:"sub"`                // Standard subject
	PreferredUsername string `json:"preferred_username"`
}

// NewOAuthClient creates an OIDC client for Azure AD.
// Call this when the OAuth config is set/changed.
func NewOAuthClient(ctx context.Context, cfg OAuthConfig) (*OAuthClient, error) {
	issuerURL := fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", cfg.TenantID)

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	oauthConfig := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile", "GroupMember.Read.All"},
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	return &OAuthClient{
		provider: provider,
		config:   oauthConfig,
		verifier: verifier,
	}, nil
}

// AuthorizationURL generates the URL to redirect the user to for login.
// redirectURL overrides the redirect_uri for this request (must be absolute).
// origin is the base URL the user started from (e.g. http://localhost:5173),
// stored with the state so we can redirect back after callback.
// Returns the URL and the state token.
func (c *OAuthClient) AuthorizationURL(redirectURL, origin string) (string, string) {
	state := generateState()
	c.states.Store(state, origin)
	url := c.config.AuthCodeURL(state, oauth2.SetAuthURLParam("redirect_uri", redirectURL))
	return url, state
}

// SetRedirectURL updates the stored redirect URL (used during code exchange).
func (c *OAuthClient) SetRedirectURL(u string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.RedirectURL = u
}

// OAuthResult holds the result of a successful token exchange.
type OAuthResult struct {
	Claims      *OAuthClaims
	Origin      string // The origin URL the user started from.
	AccessToken string // For calling Microsoft Graph API.
}

// ExchangeAndVerify exchanges the authorization code for tokens,
// verifies the ID token, and extracts the user claims.
func (c *OAuthClient) ExchangeAndVerify(ctx context.Context, code, state string) (*OAuthResult, error) {
	// Verify state to prevent CSRF.
	originVal, ok := c.states.LoadAndDelete(state)
	if !ok {
		return nil, fmt.Errorf("invalid or expired state token")
	}
	origin, _ := originVal.(string)

	// Exchange code for tokens.
	token, err := c.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}

	// Extract and verify the ID token.
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	idToken, err := c.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}

	// Extract claims.
	var claims OAuthClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("extract claims: %w", err)
	}

	// Azure sometimes puts the email in preferred_username or upn instead of email.
	if claims.Email == "" {
		if claims.PreferredUsername != "" {
			claims.Email = claims.PreferredUsername
		} else if claims.UPN != "" {
			claims.Email = claims.UPN
		}
	}

	if claims.Email == "" {
		return nil, fmt.Errorf("no email claim in ID token (checked email, preferred_username, upn)")
	}

	return &OAuthResult{
		Claims:      &claims,
		Origin:      origin,
		AccessToken: token.AccessToken,
	}, nil
}

// FetchGroups calls Microsoft Graph API to get the user's group display names.
func FetchGroups(ctx context.Context, accessToken string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://graph.microsoft.com/v1.0/me/memberOf", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graph request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graph returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Value []struct {
			ODataType   string `json:"@odata.type"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode graph response: %w", err)
	}

	var groups []string
	for _, v := range result.Value {
		if v.ODataType == "#microsoft.graph.group" && v.DisplayName != "" {
			groups = append(groups, strings.ToLower(v.DisplayName))
		}
	}
	return groups, nil
}

func generateState() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
