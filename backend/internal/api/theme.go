package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// handleThemeOverridesCSS serves a small stylesheet containing the admin's
// palette overrides from config.yaml. Applied after theme.css so it cascades
// last on the light theme.
//
// GET /api/theme/overrides.css
func (s *Server) handleThemeOverridesCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	if s.configStore == nil {
		return
	}
	overrides := s.configStore.Get().Themes.PaletteOverrides
	if len(overrides) == 0 {
		return
	}
	// Only apply when the light theme is active — brand overrides are
	// designed for light backgrounds; the dark palette stays "clean".
	fmt.Fprintln(w, `html:not([data-theme="dark"]) {`)
	for key, val := range overrides {
		if !themePaletteKey.MatchString(key) {
			continue
		}
		if !themePaletteValue.MatchString(val) {
			continue
		}
		// Normalize underscores to hyphens so both `primary_fg` and
		// `primary-fg` map to the canonical CSS var name.
		normalized := strings.ReplaceAll(key, "_", "-")
		fmt.Fprintf(w, "  --gw-color-%s: %s;\n", normalized, val)
	}
	fmt.Fprintln(w, `}`)
}

// themePaletteKey restricts override keys to known palette slot names.
// Keeping this conservative prevents the config from injecting arbitrary
// CSS variable names.
var themePaletteKey = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// themePaletteValue accepts hex colors, rgb(a)(), and the common color
// keywords. Anything else is rejected to keep CSS injection safe.
var themePaletteValue = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|rgba?\([^)]+\)|hsla?\([^)]+\)|transparent|inherit|currentColor)$`)

// handleGetMePreferences returns the caller's stored preferences.
//
// GET /api/auth/me/preferences
func (s *Server) handleGetMePreferences(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())
	if username == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if s.userStore == nil {
		writeError(w, http.StatusServiceUnavailable, "user store unavailable")
		return
	}
	user, err := s.userStore.Get(username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"theme_preference": user.ThemePreference,
	})
}

// handlePutMePreferences persists the caller's preferences.
//
// PUT /api/auth/me/preferences
// Body: { "theme_preference": "light" | "dark" | "auto" | "" }
func (s *Server) handlePutMePreferences(w http.ResponseWriter, r *http.Request) {
	username := UsernameFromContext(r.Context())
	if username == "" {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	if s.userStore == nil {
		writeError(w, http.StatusServiceUnavailable, "user store unavailable")
		return
	}

	var req struct {
		ThemePreference string `json:"theme_preference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	pref := strings.TrimSpace(req.ThemePreference)
	switch pref {
	case "", "light", "dark", "auto":
		// ok
	default:
		writeError(w, http.StatusBadRequest, "theme_preference must be light|dark|auto or empty")
		return
	}

	if err := s.userStore.UpdateThemePreference(username, pref); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Mirror to a cookie so the no-flash boot script has the value before
	// the first paint on subsequent loads.
	http.SetCookie(w, &http.Cookie{
		Name:     "gowiki_theme",
		Value:    pref,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 365,
	})

	writeJSON(w, http.StatusOK, map[string]any{"theme_preference": pref})
}
