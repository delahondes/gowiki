package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRender_NoBrowser_503 — the render endpoint is gated on a live
// Chrome/Chromium allocator. If the deployment doesn't have one, the
// response must be an immediate 503 so callers know it's structural,
// not a per-page failure.
func TestRender_NoBrowser_503(t *testing.T) {
	t.Parallel()
	s := &Server{} // browserAllocCtx is nil
	req := httptest.NewRequest(http.MethodGet, "/api/render/some/page", nil)
	rec := httptest.NewRecorder()
	s.handleRender(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (no Chrome)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not available") && !strings.Contains(rec.Body.String(), "Chrome") {
		t.Errorf("body = %q, want mention of Chrome availability", rec.Body.String())
	}
}

// TestExport_NoBrowser_503 — same gate as render, different handler.
// Both entry points share the browserAllocCtx nil check.
func TestExport_NoBrowser_503(t *testing.T) {
	t.Parallel()
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/export/pdf/some/page", nil)
	rec := httptest.NewRecorder()
	s.handleExportPDF(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (no Chrome)", rec.Code)
	}
}
