package api

import (
	"strings"
	"testing"
	"time"
)

// The rate-limit middleware writes Retry-After via formatRetryAfter. RFC
// 9110 §10.2.3 accepts a non-negative integer of seconds or an HTTP-date;
// no compliant client parses time.Duration.String() like "15.644403776s".
// If a client can't parse the value it drops to exponential backoff and
// waits much longer than the server asked for, so this format is contract.
func TestFormatRetryAfter_IsIntegerSeconds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "1"},                              // floor to 1 — a zero-second Retry-After means "retry now" which just re-opens the same closed bucket
		{500 * time.Millisecond, "1"},         // sub-second rounds up to 1
		{1 * time.Second, "1"},                // integer stays integer
		{15644403776 * time.Nanosecond, "16"}, // this is exactly time.Duration.String()'s "15.644403776s" — must NOT leak through
		{45 * time.Second, "45"},
		{2*time.Minute + 30*time.Second, "150"},
	}
	for _, tc := range cases {
		got := formatRetryAfter(tc.in)
		if got != tc.want {
			t.Errorf("formatRetryAfter(%v) = %q, want %q", tc.in, got, tc.want)
		}
		// Structural guard: never emit anything that isn't a bare
		// integer, no matter the input. A stray decimal point or
		// unit letter (`s`, `ms`) breaks conforming HTTP clients.
		for _, ch := range got {
			if ch < '0' || ch > '9' {
				t.Errorf("formatRetryAfter(%v) = %q — contains non-digit %q", tc.in, got, string(ch))
			}
		}
		if strings.Contains(got, ".") || strings.ContainsAny(got, "s") {
			t.Errorf("formatRetryAfter(%v) = %q — must be bare seconds, no unit", tc.in, got)
		}
	}
}

// Sliding-window sanity: five hits under the limit all pass, and the
// (limit+1)th hit is refused with a positive Retry-After. This pins the
// RateLimiter itself (independent of how the middleware wires it).
func TestRateLimiter_AllowsThenBlocks(t *testing.T) {
	t.Parallel()
	rl := &RateLimiter{windows: map[string]*tokenWindow{}}

	// Read limit = 3, so calls 1–3 pass, 4 fails.
	for i := 1; i <= 3; i++ {
		ok, _ := rl.Allow("tok", false, 3, 3)
		if !ok {
			t.Fatalf("call %d: unexpected block under limit=3", i)
		}
	}
	ok, retryAfter := rl.Allow("tok", false, 3, 3)
	if ok {
		t.Fatalf("call 4: expected block, got pass")
	}
	if retryAfter <= 0 {
		t.Errorf("blocked call: retryAfter = %v, want > 0", retryAfter)
	}
	if retryAfter > time.Minute+time.Second {
		t.Errorf("blocked call: retryAfter = %v, want <= 61s", retryAfter)
	}
}

// MCP is JSON-RPC over POST, so a plain verb-based rule sends every read
// tool through the write bucket and makes corpus-scale scans impossible.
// The middleware forces all /api/mcp/v1/* traffic into the read bucket;
// this test pins that policy so a well-meaning refactor can't undo it.
func TestClassifyRequestAsWrite(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method string
		path   string
		want   bool
		note   string
	}{
		// AI Content API — verb rules as usual.
		{"GET", "/api/pages/foo", false, "REST GET is a read"},
		{"HEAD", "/api/pages/foo", false, "HEAD is a read"},
		{"POST", "/api/ai/v1/pages/foo", true, "AI API POST is a write"},
		{"PUT", "/api/pages/foo", true, "PUT is a write"},
		{"PATCH", "/api/pages/foo", true, "PATCH is a write"},
		{"DELETE", "/api/pages/foo", true, "DELETE is a write"},
		// MCP over JSON-RPC — every call is POST, but we bucket as read.
		{"POST", "/api/mcp/v1/", false, "MCP root JSON-RPC POST is bucketed as read"},
		{"POST", "/api/mcp/v1/messages", false, "MCP messages POST is bucketed as read"},
		{"GET", "/api/mcp/v1/sse", false, "MCP SSE GET is a read (and the classifier agrees)"},
		{"DELETE", "/api/mcp/v1/session", false, "even DELETE on an MCP subpath stays in the read bucket"},
	}
	for _, tc := range cases {
		got := classifyRequestAsWrite(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("%s %s: got isWrite=%v, want %v (%s)", tc.method, tc.path, got, tc.want, tc.note)
		}
	}
}

// A separate token must have its own window — one token exhausting its
// budget can't collateral-damage another caller.
func TestRateLimiter_PerTokenIsolation(t *testing.T) {
	t.Parallel()
	rl := &RateLimiter{windows: map[string]*tokenWindow{}}

	for i := 0; i < 5; i++ {
		if ok, _ := rl.Allow("noisy", false, 5, 5); !ok {
			t.Fatalf("noisy call %d: unexpected block", i)
		}
	}
	if ok, _ := rl.Allow("noisy", false, 5, 5); ok {
		t.Fatal("noisy: expected block after exhausting budget")
	}
	// Quiet token has its own bucket — first hit still passes.
	if ok, _ := rl.Allow("quiet", false, 5, 5); !ok {
		t.Fatal("quiet: first call blocked; token windows leaked between callers")
	}
}
