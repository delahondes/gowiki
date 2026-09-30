package mcpserver

import (
	"errors"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"gowiki/backend/internal/reviewflow"
)

// The validated-page guard shared by edit_page and write_page: refuse
// when the target page is fully validated by reviewflow, unless the
// caller passed force=true. The regression this exists to prevent is
// an agent running an "innocuous" pass over a namespace and silently
// invalidating every reviewer's signature on every doc.

type fakeReviewflow struct {
	status *reviewflow.Status
	err    error
	// Records the pagePath the guard asked about, so tests can assert
	// no wasteful call when force is already true.
	askedFor string
}

func (f *fakeReviewflow) GetStatus(pagePath string) (*reviewflow.Status, error) {
	f.askedFor = pagePath
	return f.status, f.err
}

func TestValidatedGuard_RefusesWithoutForce(t *testing.T) {
	t.Parallel()
	rf := &fakeReviewflow{status: &reviewflow.Status{
		Roles: map[string]string{
			"author":     "raynald.delahondes",
			"reviewer":   "alice.laporte",
			"validation": "etienne.formstecher",
		},
		VersionTag:       "1.2",
		CurrentPageVer:   61,
		ValidatedVersion: 61,
		IsFullyValidated: true,
	}}
	res := refuseIfValidatedWithoutForce(rf, "regulatory/qms/sop06", false)
	if res == nil {
		t.Fatal("expected refusal on validated page without force")
	}
	// Result text must name the signed roles so the LLM can surface
	// the actual cost. Format is stable — sorted, "role=user".
	body := textContent(t, res)
	for _, want := range []string{
		"author=raynald.delahondes",
		"reviewer=alice.laporte",
		"validation=etienne.formstecher",
		"v61",
		"1.2",
		"force=true",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("refusal missing %q; body was:\n%s", want, body)
		}
	}
}

func TestValidatedGuard_AllowsWithForce(t *testing.T) {
	t.Parallel()
	rf := &fakeReviewflow{status: &reviewflow.Status{
		IsFullyValidated: true,
		Roles:            map[string]string{"author": "x"},
	}}
	if res := refuseIfValidatedWithoutForce(rf, "any/page", true); res != nil {
		t.Errorf("force=true must allow the write; got refusal: %s", textContent(t, res))
	}
	// Cheap-path optimization: with force=true, the guard shouldn't
	// even hit the reviewflow service.
	if rf.askedFor != "" {
		t.Errorf("force=true must skip the GetStatus call; askedFor=%q", rf.askedFor)
	}
}

func TestValidatedGuard_AllowsPartialValidation(t *testing.T) {
	t.Parallel()
	// One role signed but not all → NOT fully validated → write is fine.
	// The guard's purpose is preventing the *cliff* of losing every
	// signature, not adding friction to normal in-flight review work.
	rf := &fakeReviewflow{status: &reviewflow.Status{
		Roles:            map[string]string{"author": "x", "reviewer": "y"},
		IsFullyValidated: false,
	}}
	if res := refuseIfValidatedWithoutForce(rf, "any/page", false); res != nil {
		t.Errorf("partial validation must pass the guard; got: %s", textContent(t, res))
	}
}

func TestValidatedGuard_AllowsPageWithoutReviewflow(t *testing.T) {
	t.Parallel()
	// Empty Roles = no {reviewflow} directive on this page. No signatures
	// to protect, no refusal.
	rf := &fakeReviewflow{status: &reviewflow.Status{
		Roles:            map[string]string{},
		IsFullyValidated: false,
	}}
	if res := refuseIfValidatedWithoutForce(rf, "any/page", false); res != nil {
		t.Errorf("no-reviewflow page must pass the guard; got: %s", textContent(t, res))
	}
}

func TestValidatedGuard_AllowsWhenServiceMissing(t *testing.T) {
	t.Parallel()
	// A deployment without the reviewflow service must not brick every
	// write. Guard degrades to a no-op in that configuration.
	if res := refuseIfValidatedWithoutForce(nil, "any/page", false); res != nil {
		t.Errorf("nil reviewflow service must pass; got: %s", textContent(t, res))
	}
}

func TestValidatedGuard_AllowsOnGetStatusError(t *testing.T) {
	t.Parallel()
	// GetStatus error is not a green light to lose signatures, but the
	// current semantics deliberately fall open (rather than fail closed)
	// to avoid taking every write hostage to a metadata store hiccup.
	// If we ever flip this to fail-closed, this test needs updating in
	// the same commit — it exists to make the choice visible.
	rf := &fakeReviewflow{err: errors.New("state file missing")}
	if res := refuseIfValidatedWithoutForce(rf, "any/page", false); res != nil {
		t.Errorf("GetStatus error must pass the guard (fail-open); got: %s", textContent(t, res))
	}
}

// textContent extracts the plain text from an MCP tool result.
// mcpgo.CallToolResult.Content is []Content (discriminated union);
// errorResult produces a result whose Content is a single TextContent.
func textContent(t *testing.T, res *mcpgo.CallToolResult) string {
	t.Helper()
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcpgo.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
