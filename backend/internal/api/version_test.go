package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

// TestHandleVersion pins the /api/version response shape so a CI verifier
// can grep for known fields. It also proves the -X ldflags injection path
// works — we swap the package-level BuildCommit/BuildDate vars, hit the
// handler, and assert they show up verbatim.
func TestHandleVersion(t *testing.T) {
	origCommit := BuildCommit
	origDate := BuildDate
	BuildCommit = "abcdef1"
	BuildDate = "2026-01-02T03:04:05Z"
	t.Cleanup(func() {
		BuildCommit = origCommit
		BuildDate = origDate
	})

	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rec := httptest.NewRecorder()
	s.handleVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v — %s", err, rec.Body.String())
	}
	if body["version"] != Version {
		t.Errorf("version = %q, want %q", body["version"], Version)
	}
	if body["commit"] != "abcdef1" {
		t.Errorf("commit = %q, want abcdef1 (ldflags injection broken)", body["commit"])
	}
	if body["build_date"] != "2026-01-02T03:04:05Z" {
		t.Errorf("build_date = %q, want 2026-01-02T03:04:05Z", body["build_date"])
	}
	if body["go_version"] != runtime.Version() {
		t.Errorf("go_version = %q, want %q", body["go_version"], runtime.Version())
	}
}
