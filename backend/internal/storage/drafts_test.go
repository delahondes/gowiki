package storage

import (
	"os"
	"testing"
)

func newTestDraftStore(t *testing.T) *DraftStore {
	t.Helper()
	dataDir := t.TempDir()
	metaDir := t.TempDir()
	return NewDraftStore(dataDir, metaDir)
}

func TestTakeoverDraft_NoPriorLock_SeedsFromPublished(t *testing.T) {
	d := newTestDraftStore(t)

	md, token, err := d.TakeoverDraft("page", "alice", "# hello")
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if md != "# hello" {
		t.Errorf("markdown = %q, want %q", md, "# hello")
	}
	if token == "" {
		t.Errorf("expected a fresh edit token")
	}
	lock := d.GetLock("page")
	if lock.Owner != "alice" || lock.EditToken != token {
		t.Errorf("lock = %+v, want owner=alice + matching token", lock)
	}
	// Draft file must be persisted so the next call reads it, not the seed.
	if _, err := os.Stat(d.draftPath("alice", "page")); err != nil {
		t.Errorf("draft file missing: %v", err)
	}
}

func TestTakeoverDraft_CrossUser_MovesDraftFile(t *testing.T) {
	d := newTestDraftStore(t)
	// Bob starts editing.
	_, bobToken, err := d.EnterEditMode("page", "bob", false, "seed")
	if err != nil {
		t.Fatalf("bob enter: %v", err)
	}
	if err := d.SaveDraft("page", "bob", bobToken, "# bob's work"); err != nil {
		t.Fatalf("bob save: %v", err)
	}

	// Alice takes over (presence-gate approval happens above this layer).
	md, aliceToken, err := d.TakeoverDraft("page", "alice", "# published")
	if err != nil {
		t.Fatalf("alice takeover: %v", err)
	}
	if md != "# bob's work" {
		t.Errorf("takeover markdown = %q, want %q — bob's draft content should move", md, "# bob's work")
	}
	if aliceToken == bobToken {
		t.Errorf("alice token should differ from bob's — otherwise the old browser tab could still write")
	}
	if _, err := os.Stat(d.draftPath("bob", "page")); !os.IsNotExist(err) {
		t.Errorf("bob's draft file should have been removed after takeover, got err=%v", err)
	}
	if _, err := os.Stat(d.draftPath("alice", "page")); err != nil {
		t.Errorf("alice's draft file missing: %v", err)
	}
	lock := d.GetLock("page")
	if lock.Owner != "alice" || lock.EditToken != aliceToken {
		t.Errorf("lock = %+v, want owner=alice + alice's new token", lock)
	}

	// Bob's old token must now be rejected — proves the browser-tab kickoff.
	if err := d.SaveDraft("page", "bob", bobToken, "late edit"); err == nil {
		t.Errorf("expected bob's old token to be rejected after takeover")
	}
}

func TestTakeoverDraft_SameUser_KeepsExistingDraft(t *testing.T) {
	d := newTestDraftStore(t)
	_, token1, err := d.EnterEditMode("page", "alice", false, "seed")
	if err != nil {
		t.Fatalf("alice enter: %v", err)
	}
	if err := d.SaveDraft("page", "alice", token1, "# alice mid-edit"); err != nil {
		t.Fatalf("alice save: %v", err)
	}

	md, token2, err := d.TakeoverDraft("page", "alice", "# published")
	if err != nil {
		t.Fatalf("alice re-takeover: %v", err)
	}
	if md != "# alice mid-edit" {
		t.Errorf("markdown = %q, want alice's saved content", md)
	}
	if token2 == "" || token2 == token1 {
		t.Errorf("expected a fresh token distinct from the previous one; got token1=%q token2=%q", token1, token2)
	}
	if err := d.SaveDraft("page", "alice", token1, "with old token"); err == nil {
		t.Errorf("previous token should have been invalidated")
	}
}
