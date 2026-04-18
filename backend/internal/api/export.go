package api

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"gowiki/backend/internal/auth"
)

func (s *Server) handleExportPDF(w http.ResponseWriter, r *http.Request) {
	if s.browserAllocCtx == nil {
		http.Error(w, "PDF export is not available — no Chrome/Chromium found. Check server logs.", http.StatusServiceUnavailable)
		return
	}

	pagePath := "/" + strings.TrimPrefix(r.URL.Path, "/api/export/pdf/")
	if pagePath == "/" {
		pagePath = "/index"
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

	// Forward session cookie if present. Without this, Chrome loads the
	// export URL unauthenticated and the frontend's /api/pages fetch fails
	// with 403 on any ACL-protected page.
	var sessionCookie *http.Cookie
	if cookie, err := r.Cookie(auth.CookieName); err == nil {
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

	// Determine the wiki's site title for the running header. Falls back to
	// "Gowiki" if no configuration is loaded (e.g. dev mode without a store).
	siteTitle := "Gowiki"
	if s.configStore != nil {
		if t := strings.TrimSpace(s.configStore.Get().Site.Title); t != "" {
			siteTitle = t
		}
	}

	var pdfBuf []byte
	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		pdfBuf, _, err = page.PrintToPDF().
			WithPrintBackground(true).
			WithMarginTop(0.7).
			WithMarginBottom(0.7).
			WithMarginLeft(0.5).
			WithMarginRight(0.5).
			WithPaperWidth(8.27). // A4
			WithPaperHeight(11.69).
			WithDisplayHeaderFooter(true).
			WithHeaderTemplate(pdfHeaderTemplate(siteTitle)).
			WithFooterTemplate(pdfFooterTemplate()).
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

// pdfHeaderTemplate returns the running-header HTML Chrome injects on every
// printed page. Chrome only respects a handful of classes in these templates
// (.title, .date, .url, .pageNumber, .totalPages); any inline CSS must be
// explicit as @media print rules don't apply here.
//
// The page title is substituted by Chrome from document.title; the frontend
// sets that to the page's first heading in export mode.
func pdfHeaderTemplate(siteTitle string) string {
	return `<div style="font-size:9px; color:#888; width:100%; padding:4px 0.5in 0; -webkit-print-color-adjust:exact; box-sizing:border-box; font-family:-apple-system,BlinkMacSystemFont,sans-serif; display:flex; justify-content:space-between; align-items:center;">
  <span class="title" style="overflow:hidden; text-overflow:ellipsis; white-space:nowrap; max-width:60%;"></span>
  <span>` + html.EscapeString(siteTitle) + `</span>
</div>`
}

// pdfFooterTemplate returns the running-footer HTML. It shows the document
// generation date on the left and the page count on the right. Chrome
// substitutes `.date`, `.pageNumber` and `.totalPages` at print time.
func pdfFooterTemplate() string {
	return `<div style="font-size:9px; color:#888; width:100%; padding:0 0.5in 4px; -webkit-print-color-adjust:exact; box-sizing:border-box; font-family:-apple-system,BlinkMacSystemFont,sans-serif; display:flex; justify-content:space-between;">
  <span>Generated <span class="date"></span></span>
  <span>Page <span class="pageNumber"></span> / <span class="totalPages"></span></span>
</div>`
}
