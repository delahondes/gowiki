package reviewflow

import (
	"testing"
	"time"

	"gowiki/backend/internal/config"
	"gowiki/backend/internal/storage"
)

// todoSpy records CancelReviewTasks/CreateReviewTasks/CompleteReviewTasks
// calls so tests can assert that reviewflow's todo integration fires at
// the right moments.
type todoSpy struct {
	cancels        []string       // pagePath
	creates        []createCall   // pagePath + roles
	completes      []completeCall // pagePath + confirmedByRole
	completeReturn map[string]int // per-pagePath count returned by CompleteReviewTasks
}

type createCall struct {
	pagePath   string
	roles      map[string]string
	versionTag string
}

type completeCall struct {
	pagePath        string
	confirmedByRole map[string]string
}

func (s *todoSpy) CancelReviewTasks(pagePath string) error {
	s.cancels = append(s.cancels, pagePath)
	return nil
}

func (s *todoSpy) CreateReviewTasks(pagePath string, roles map[string]string, versionTag, dueDate string) error {
	// Copy the map so mutations after the call don't leak into the recorded state.
	r := make(map[string]string, len(roles))
	for k, v := range roles {
		r[k] = v
	}
	s.creates = append(s.creates, createCall{pagePath: pagePath, roles: r, versionTag: versionTag})
	return nil
}

func (s *todoSpy) CompleteReviewTasks(pagePath string, confirmedByRole map[string]string) (int, error) {
	c := make(map[string]string, len(confirmedByRole))
	for k, v := range confirmedByRole {
		c[k] = v
	}
	s.completes = append(s.completes, completeCall{pagePath: pagePath, confirmedByRole: c})
	if s.completeReturn != nil {
		return s.completeReturn[pagePath], nil
	}
	return len(c), nil
}

// pageReaderStub returns pre-canned pages, backing EnsureState.
type pageReaderStub struct {
	pages map[string]storage.Page
}

func (p *pageReaderStub) Get(pagePath string) (storage.Page, error) {
	if pg, ok := p.pages[pagePath]; ok {
		return pg, nil
	}
	return storage.Page{}, storage.ErrPageNotFound
}

// newSvcWithSpy wires a Service with a temp reviewflow.Store, a real
// (empty) config.Store on disk, and a spy TodoIntegrator. The pageReader
// is empty by default — tests can populate it before calling.
func newSvcWithSpy(t *testing.T) (*Service, *todoSpy, *pageReaderStub) {
	t.Helper()
	meta := t.TempDir()
	store := NewStore(meta)

	cfgPath := t.TempDir() + "/config.yaml"
	cfgStore, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	attic := storage.NewAttic(t.TempDir())
	svc := NewService(store, attic, cfgStore)
	spy := &todoSpy{}
	svc.SetTodoIntegrator(spy)
	reader := &pageReaderStub{pages: make(map[string]storage.Page)}
	svc.SetPageReader(reader)
	return svc, spy, reader
}

const directive2 = "{reviewflow author=alice reviewer=bob}\n"
const directive3 = "{reviewflow author=alice reviewer=bob validator=cathy}\n"
const directive2Tagged = "{reviewflow author=alice reviewer=bob version=v1.0}\n"

// ── SyncFromMarkdown ─────────────────────────────────────

func TestSyncFromMarkdown_FirstSaveCreatesState(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	if err := svc.SyncFromMarkdown("/doc", 1, directive2); err != nil {
		t.Fatalf("SyncFromMarkdown: %v", err)
	}

	st, _ := svc.store.Load("/doc")
	if st == nil {
		t.Fatal("state not written")
	}
	if len(st.Roles) != 2 || st.Roles["author"] != "alice" || st.Roles["reviewer"] != "bob" {
		t.Errorf("roles = %+v", st.Roles)
	}
	if st.CurrentPageVersion != 1 {
		t.Errorf("CurrentPageVersion = %d, want 1", st.CurrentPageVersion)
	}
	if len(spy.creates) != 1 || spy.creates[0].pagePath != "/doc" {
		t.Errorf("expected one CreateReviewTasks for /doc, got %+v", spy.creates)
	}
	if len(spy.cancels) != 1 || spy.cancels[0] != "/doc" {
		// Cancel-then-create is the pattern; the first cancel is a no-op
		// on an empty state but still fires.
		t.Errorf("expected one CancelReviewTasks for /doc, got %+v", spy.cancels)
	}
}

func TestSyncFromMarkdown_SameVersion_PreservesConfirmations(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	// First save creates state at v1.
	if err := svc.SyncFromMarkdown("/doc", 1, directive2); err != nil {
		t.Fatal(err)
	}
	// Alice confirms as author at v1.
	if _, err := svc.Confirm("/doc", "author", "alice", nil); err != nil {
		t.Fatalf("Confirm: %v", err)
	}

	// Second sync with SAME pageVersion — confirmations must survive.
	spy.creates = nil
	spy.cancels = nil
	if err := svc.SyncFromMarkdown("/doc", 1, directive2); err != nil {
		t.Fatal(err)
	}

	st, _ := svc.store.Load("/doc")
	if len(st.Confirmations) != 1 || st.Confirmations[0].Role != "author" {
		t.Errorf("confirmations wiped: %+v", st.Confirmations)
	}
	if len(spy.creates) != 0 {
		t.Errorf("unexpected CreateReviewTasks on same-version sync: %+v", spy.creates)
	}
	if len(spy.cancels) != 0 {
		t.Errorf("unexpected CancelReviewTasks on same-version sync: %+v", spy.cancels)
	}
}

func TestSyncFromMarkdown_VersionBump_ClearsConfirmationsAndRebuildsTodos(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)

	_ = svc.SyncFromMarkdown("/doc", 1, directive2)
	_, _ = svc.Confirm("/doc", "author", "alice", nil)

	spy.cancels = nil
	spy.creates = nil

	// Bump to v2 — content changed, prior confirmations invalidated.
	if err := svc.SyncFromMarkdown("/doc", 2, directive2); err != nil {
		t.Fatal(err)
	}

	st, _ := svc.store.Load("/doc")
	if st.CurrentPageVersion != 2 {
		t.Errorf("CurrentPageVersion = %d, want 2", st.CurrentPageVersion)
	}
	if len(st.Confirmations) != 0 {
		t.Errorf("confirmations should have been cleared, got %+v", st.Confirmations)
	}
	if len(spy.cancels) != 1 || spy.cancels[0] != "/doc" {
		t.Errorf("expected CancelReviewTasks on version bump, got %+v", spy.cancels)
	}
	if len(spy.creates) != 1 || spy.creates[0].roles["author"] != "alice" {
		t.Errorf("expected CreateReviewTasks with same roles, got %+v", spy.creates)
	}
}

func TestSyncFromMarkdown_DirectiveRemoved_ClearsRolesAndCancels(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, directive2)
	spy.cancels = nil

	// Page saved without a directive.
	if err := svc.SyncFromMarkdown("/doc", 2, "just body content\n"); err != nil {
		t.Fatal(err)
	}
	st, _ := svc.store.Load("/doc")
	if len(st.Roles) != 0 {
		t.Errorf("roles should have been cleared, got %+v", st.Roles)
	}
	if len(spy.cancels) != 1 {
		t.Errorf("expected CancelReviewTasks after directive removal, got %+v", spy.cancels)
	}
}

// ── Confirm ──────────────────────────────────────────────

func TestConfirm_StampsCurrentPageVersion(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 7, directive2)

	if _, err := svc.Confirm("/doc", "reviewer", "bob", nil); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	st, _ := svc.store.Load("/doc")
	if len(st.Confirmations) != 1 {
		t.Fatalf("expected 1 confirmation, got %d", len(st.Confirmations))
	}
	c := st.Confirmations[0]
	if c.PageVersion != 7 {
		t.Errorf("PageVersion = %d, want 7 (current)", c.PageVersion)
	}
	if c.Role != "reviewer" || c.User != "bob" {
		t.Errorf("confirmation = %+v", c)
	}
	if time.Since(c.Timestamp) > 5*time.Second {
		t.Errorf("timestamp too old: %v", c.Timestamp)
	}
}

func TestConfirm_UnknownRole_Errors(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, directive2)
	if _, err := svc.Confirm("/doc", "validator", "cathy", nil); err == nil {
		t.Error("expected error for role not in directive")
	}
}

func TestConfirm_WrongUserForRole_Errors(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, directive2) // reviewer=bob
	if _, err := svc.Confirm("/doc", "reviewer", "someoneelse", nil); err == nil {
		t.Error("expected error when the wrong user tries to confirm a role")
	}
}

func TestConfirm_DoubleConfirmSameVersion_Idempotent(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, directive2)
	_, _ = svc.Confirm("/doc", "author", "alice", nil)
	_, _ = svc.Confirm("/doc", "author", "alice", nil)

	st, _ := svc.store.Load("/doc")
	if len(st.Confirmations) != 1 {
		t.Errorf("expected 1 confirmation after double confirm, got %d", len(st.Confirmations))
	}
}

// ── Validation transition ────────────────────────────────

func TestConfirm_AllRolesValidatesAndAppendsVersionRecord(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 4, directive2Tagged) // version=v1.0

	if _, err := svc.Confirm("/doc", "author", "alice", nil); err != nil {
		t.Fatal(err)
	}
	// Not yet validated — reviewer missing.
	st1, _ := svc.store.Load("/doc")
	if st1.ValidatedVersion != 0 {
		t.Errorf("ValidatedVersion should still be 0, got %d", st1.ValidatedVersion)
	}
	if len(st1.VersionHistory) != 0 {
		t.Errorf("VersionHistory should still be empty, got %+v", st1.VersionHistory)
	}

	if _, err := svc.Confirm("/doc", "reviewer", "bob", nil); err != nil {
		t.Fatal(err)
	}
	st2, _ := svc.store.Load("/doc")
	if st2.ValidatedVersion != 4 {
		t.Errorf("ValidatedVersion = %d, want 4", st2.ValidatedVersion)
	}
	if len(st2.VersionHistory) != 1 {
		t.Fatalf("VersionHistory has %d records, want 1", len(st2.VersionHistory))
	}
	vr := st2.VersionHistory[0]
	if vr.PageVersion != 4 || vr.VersionTag != "v1.0" {
		t.Errorf("VersionRecord = %+v", vr)
	}
	if len(vr.ConfirmedBy) != 2 || vr.ConfirmedBy["author"] != "alice" || vr.ConfirmedBy["reviewer"] != "bob" {
		t.Errorf("ConfirmedBy = %+v", vr.ConfirmedBy)
	}
	// The final confirm triggers a CompleteReviewTasks with the full map.
	found := false
	for _, c := range spy.completes {
		if len(c.confirmedByRole) == 2 && c.confirmedByRole["author"] == "alice" && c.confirmedByRole["reviewer"] == "bob" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CompleteReviewTasks with both roles, got %+v", spy.completes)
	}
}

func TestConfirm_ValidationRecordsRoleAtomically(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, directive3)
	_, _ = svc.Confirm("/doc", "author", "alice", nil)
	_, _ = svc.Confirm("/doc", "reviewer", "bob", nil)
	_, _ = svc.Confirm("/doc", "validator", "cathy", nil)

	st, _ := svc.store.Load("/doc")
	if st.ValidatedVersion != 1 {
		t.Fatalf("ValidatedVersion = %d, want 1", st.ValidatedVersion)
	}
	if len(st.VersionHistory) != 1 {
		t.Fatalf("expected 1 VersionRecord, got %d", len(st.VersionHistory))
	}
	if len(st.VersionHistory[0].ConfirmedBy) != 3 {
		t.Errorf("ConfirmedBy has %d roles, want 3", len(st.VersionHistory[0].ConfirmedBy))
	}
}

// After validation, a version bump must NOT drop the past VersionRecord —
// this is what makes "restore from history preserves the audit trail" work.
func TestVersionBump_PreservesVersionHistory(t *testing.T) {
	t.Parallel()
	svc, _, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, directive2)
	_, _ = svc.Confirm("/doc", "author", "alice", nil)
	_, _ = svc.Confirm("/doc", "reviewer", "bob", nil)

	// Version bump.
	if err := svc.SyncFromMarkdown("/doc", 2, directive2); err != nil {
		t.Fatal(err)
	}
	st, _ := svc.store.Load("/doc")
	if len(st.VersionHistory) != 1 {
		t.Errorf("VersionHistory got wiped on version bump: %+v", st.VersionHistory)
	}
	if st.VersionHistory[0].PageVersion != 1 {
		t.Errorf("VersionHistory[0].PageVersion = %d, want 1", st.VersionHistory[0].PageVersion)
	}
}

// ── EnsureState / bootstrap ──────────────────────────────

func TestEnsureState_BootstrapsFromPageReader(t *testing.T) {
	t.Parallel()
	svc, _, reader := newSvcWithSpy(t)
	reader.pages["/bootstrap"] = storage.Page{
		Markdown: directive2Tagged,
		Meta:     storage.PageMetadata{Version: 3},
	}

	st, err := svc.EnsureState("/bootstrap")
	if err != nil {
		t.Fatalf("EnsureState: %v", err)
	}
	if st.CurrentPageVersion != 3 {
		t.Errorf("CurrentPageVersion = %d, want 3 (from page meta)", st.CurrentPageVersion)
	}
	if st.VersionTag != "v1.0" {
		t.Errorf("VersionTag = %q, want v1.0", st.VersionTag)
	}
}

// ── ReconcileValidatedTasks ─────────────────────────────

func TestReconcileValidatedTasks_CompletesForCurrentVersionConfirmations(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	_ = svc.SyncFromMarkdown("/doc", 1, directive2)
	_, _ = svc.Confirm("/doc", "author", "alice", nil)
	_, _ = svc.Confirm("/doc", "reviewer", "bob", nil)

	// The Confirm calls above already emitted CompleteReviewTasks. Now
	// simulate a subsequent Reconcile — it should also emit a Complete
	// call for the current-version confirmations. The important invariant
	// is that Reconcile identifies the right (role, user) pairs from the
	// state file it walks.
	before := len(spy.completes)
	n, err := svc.ReconcileValidatedTasks()
	if err != nil {
		t.Fatalf("ReconcileValidatedTasks: %v", err)
	}
	if len(spy.completes) <= before {
		t.Fatal("expected an additional CompleteReviewTasks call from Reconcile")
	}
	// The extra call must carry the full confirmed-by map.
	last := spy.completes[len(spy.completes)-1]
	if last.pagePath != "/doc" || last.confirmedByRole["author"] != "alice" || last.confirmedByRole["reviewer"] != "bob" {
		t.Errorf("reconcile Complete call = %+v", last)
	}
	// n is the returned count — should equal the spy's completeReturn (default: len of map).
	if n < 2 {
		t.Errorf("reconcile returned n=%d, want at least 2 (two roles)", n)
	}
}

func TestReconcileValidatedTasks_SkipsStatesWithoutConfirmations(t *testing.T) {
	t.Parallel()
	svc, spy, _ := newSvcWithSpy(t)
	// State exists but no confirmations.
	_ = svc.SyncFromMarkdown("/quiet", 1, directive2)

	before := len(spy.completes)
	n, err := svc.ReconcileValidatedTasks()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("reconcile n = %d, want 0", n)
	}
	if len(spy.completes) != before {
		t.Errorf("reconcile fired Complete on a page with no confirmations: %+v", spy.completes[before:])
	}
}
