package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func (s *Server) handleExportPDF(w http.ResponseWriter, r *http.Request) {
	if s.browserAllocCtx == nil {
		http.Error(w, "PDF export is not available — no Chrome/Chromium found. Check server logs.", http.StatusServiceUnavailable)
		return
	}

	pagePath := strings.TrimPrefix(r.URL.Path, "/api/export/pdf/")
	if pagePath == "" {
		pagePath = "index"
	}

	// Determine the base URL for the wiki.
	// In production (serve-web), the backend serves HTML — use the request host.
	// In dev mode, the frontend is on Vite (port 5173) — Chrome must hit that.
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
		// Dev mode: Vite serves the frontend on port 5173.
		baseURL = "http://localhost:5173"
	}
	pageURL := fmt.Sprintf("%s/%s?export=pdf", baseURL, pagePath)

	// Extract host without port for cookie domain.
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Forward session cookie if present.
	var sessionCookie *http.Cookie
	if cookie, err := r.Cookie("session"); err == nil {
		sessionCookie = cookie
	}

	// Create a new tab context from the persistent browser.
	ctx, cancel := chromedp.NewContext(s.browserAllocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var tasks chromedp.Tasks

	// Set cookies before navigating.
	if sessionCookie != nil {
		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookie(sessionCookie.Name, sessionCookie.Value).
				WithDomain(host).
				WithPath("/").
				Do(ctx)
		}))
	}

	// Navigate and wait for export-ready signal.
	tasks = append(tasks,
		chromedp.Navigate(pageURL),
		chromedp.WaitVisible(`[data-export-ready]`, chromedp.ByQuery),
	)

	var pdfBuf []byte
	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		pdfBuf, _, err = page.PrintToPDF().
			WithPrintBackground(true).
			WithMarginTop(0.5).
			WithMarginBottom(0.5).
			WithMarginLeft(0.5).
			WithMarginRight(0.5).
			WithPaperWidth(8.27). // A4
			WithPaperHeight(11.69).
			Do(ctx)
		return err
	}))

	if err := chromedp.Run(ctx, tasks); err != nil {
		http.Error(w, fmt.Sprintf("PDF generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, pagePath))
	w.Write(pdfBuf)
}
