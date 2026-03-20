package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	if s.browserAllocCtx == nil {
		http.Error(w, "Render endpoint is not available — no Chrome/Chromium found.", http.StatusServiceUnavailable)
		return
	}

	pagePath := "/" + strings.TrimPrefix(r.URL.Path, "/api/render/")
	if pagePath == "/" {
		pagePath = "/index"
	}

	// Determine the base URL (same logic as PDF export).
	var baseURL string
	if s.serveWeb {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, r.Host)
	} else {
		baseURL = "http://localhost:5173"
	}
	pageURL := fmt.Sprintf("%s/%s?export=pdf", baseURL, pagePath)

	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Resolve a session cookie for the headless browser.
	// If the request has a session cookie, forward it.
	// Otherwise (e.g. API token auth), create a temporary session
	// so the headless browser can access the page.
	sessionID := ""
	tempSession := false

	if cookie, err := r.Cookie("session"); err == nil {
		sessionID = cookie.Value
	} else {
		// No cookie — create a temporary session for the requesting user.
		username := UsernameFromContext(r.Context())
		if username != "" {
			sessionID = s.sessionStore.Create(username)
			tempSession = true
		}
	}

	if tempSession {
		defer s.sessionStore.Delete(sessionID)
	}

	ctx, cancel := chromedp.NewContext(s.browserAllocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var tasks chromedp.Tasks

	// Collect JS console errors.
	var jsErrors []string
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if e, ok := ev.(*runtime.EventExceptionThrown); ok {
			msg := e.ExceptionDetails.Text
			if e.ExceptionDetails.Exception != nil && e.ExceptionDetails.Exception.Description != "" {
				msg = e.ExceptionDetails.Exception.Description
			}
			jsErrors = append(jsErrors, msg)
		}
	})

	// Set session cookie before navigating.
	if sessionID != "" {
		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookie("session", sessionID).
				WithDomain(host).
				WithPath("/").
				Do(ctx)
		}))
	}

	tasks = append(tasks,
		chromedp.Navigate(pageURL),
		chromedp.WaitVisible(`[data-export-ready]`, chromedp.ByQuery),
	)

	var html string
	tasks = append(tasks, chromedp.OuterHTML(`#content`, &html, chromedp.ByID))

	if err := chromedp.Run(ctx, tasks); err != nil {
		// If we collected JS errors, report those instead of the generic timeout.
		if len(jsErrors) > 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"page":      pagePath,
				"errors":    jsErrors,
				"html":      "",
				"rendering": "failed",
			})
			return
		}
		http.Error(w, fmt.Sprintf("Render failed: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"page":      pagePath,
		"html":      html,
		"errors":    jsErrors,
		"rendering": "ok",
	})
}
