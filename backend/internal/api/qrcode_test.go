package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// callQR runs the QR handler with a URL query. No auth, no server state.
func callQR(query string) *httptest.ResponseRecorder {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/qrcode?"+query, nil)
	rec := httptest.NewRecorder()
	s.handleQRCode(rec, req)
	return rec
}

// TestQRCode_HappyPath — data present, response is a PNG with a cacheable
// Content-Type. The PNG magic bytes prove Chrome / preview can render it.
func TestQRCode_HappyPath(t *testing.T) {
	t.Parallel()
	rec := callQR("data=https%3A%2F%2Fexample.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	// PNG signature — first 8 bytes are 89 50 4E 47 0D 0A 1A 0A.
	sig := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if !bytes.HasPrefix(rec.Body.Bytes(), sig) {
		t.Errorf("response is not a PNG (first bytes: % x)", rec.Body.Bytes()[:8])
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Errorf("Cache-Control = %q, want public, max-age=3600", got)
	}
}

// TestQRCode_MissingData_400 — the endpoint is used from the PDF footer;
// silently returning an empty PNG would break every export.
func TestQRCode_MissingData_400(t *testing.T) {
	t.Parallel()
	rec := callQR("")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing data") {
		t.Errorf("body = %q, want to mention missing data", rec.Body.String())
	}
}

// TestQRCode_DataTooLong_400 — hard-cap at 2048 chars stops abuse.
func TestQRCode_DataTooLong_400(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 2049)
	rec := callQR("data=" + long)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for oversized data", rec.Code)
	}
}

// TestQRCode_SizeClamped — sizes below 64 or above 1024 are clamped
// silently. We assert the request is served (not rejected) at both bounds.
func TestQRCode_SizeClamped(t *testing.T) {
	t.Parallel()
	for _, size := range []string{"1", "16000", "500"} {
		rec := callQR("data=hi&size=" + size)
		if rec.Code != http.StatusOK {
			t.Errorf("size=%s: status = %d, want 200", size, rec.Code)
		}
	}
}
