package auth

import (
	"testing"
	"time"
)

func TestSessionStore_DeleteByUsername(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSessionStore(dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Two sessions for alice, one for bob, one for cathy.
	a1 := s.Create("alice")
	a2 := s.Create("alice")
	b1 := s.Create("bob")
	c1 := s.Create("cathy")

	// Disabling alice should nuke exactly her two sessions and leave the
	// rest intact — the requireAuth per-request check is the front line;
	// this is the belt on top so a live session can't outrun the disable.
	n := s.DeleteByUsername("alice")
	if n != 2 {
		t.Errorf("expected 2 revoked, got %d", n)
	}
	if _, ok := s.Get(a1); ok {
		t.Errorf("alice session #1 should be gone")
	}
	if _, ok := s.Get(a2); ok {
		t.Errorf("alice session #2 should be gone")
	}
	if _, ok := s.Get(b1); !ok {
		t.Errorf("bob's session should have survived")
	}
	if _, ok := s.Get(c1); !ok {
		t.Errorf("cathy's session should have survived")
	}

	// Second call is a no-op — nothing to revoke.
	if n := s.DeleteByUsername("alice"); n != 0 {
		t.Errorf("no-op DeleteByUsername returned %d", n)
	}
}
