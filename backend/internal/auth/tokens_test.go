package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewTokenStore_Bootstrap(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTokenStore(dir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	if len(store.List()) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(store.List()))
	}
}

func TestNewTokenStore_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api_tokens.json")
	os.WriteFile(path, []byte(`[]`), 0644)

	store, err := NewTokenStore(dir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	if len(store.List()) != 0 {
		t.Errorf("expected 0 tokens")
	}
}

func TestTokenStore_Create(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewTokenStore(dir)

	token, plaintext, err := store.Create("alice", "My AI", 5)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !strings.HasPrefix(token.ID, "tok_") {
		t.Errorf("ID should start with tok_, got %q", token.ID)
	}
	if !strings.HasPrefix(plaintext, "gwk_") {
		t.Errorf("plaintext should start with gwk_, got %q", plaintext)
	}
	if token.User != "alice" {
		t.Errorf("user should be alice, got %q", token.User)
	}
	if token.Name != "My AI" {
		t.Errorf("name should be My AI, got %q", token.Name)
	}
	if token.TokenHash != "" {
		t.Errorf("returned token should have empty hash")
	}
}

func TestTokenStore_Verify(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewTokenStore(dir)

	_, plaintext, _ := store.Create("alice", "Test", 5)

	got, err := store.Verify(plaintext)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.User != "alice" {
		t.Errorf("expected user alice, got %q", got.User)
	}
}

func TestTokenStore_VerifyInvalid(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewTokenStore(dir)

	store.Create("alice", "Test", 5)

	_, err := store.Verify("gwk_invalid_token_value")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestTokenStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewTokenStore(dir)

	token, _, _ := store.Create("alice", "Test", 5)
	if len(store.List()) != 1 {
		t.Fatal("expected 1 token after create")
	}

	if err := store.Delete(token.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.List()) != 0 {
		t.Errorf("expected 0 tokens after delete")
	}
}

func TestTokenStore_DeleteForUser(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewTokenStore(dir)

	token, _, _ := store.Create("alice", "Test", 5)

	// Bob cannot delete Alice's token.
	if err := store.DeleteForUser(token.ID, "bob"); err == nil {
		t.Error("expected error when bob deletes alice's token")
	}

	// Alice can delete her own token.
	if err := store.DeleteForUser(token.ID, "alice"); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}
}

func TestTokenStore_MaxPerUser(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewTokenStore(dir)

	for i := 0; i < 3; i++ {
		_, _, err := store.Create("alice", "Token", 3)
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	_, _, err := store.Create("alice", "Token", 3)
	if err != ErrTooManyTokens {
		t.Errorf("expected ErrTooManyTokens, got %v", err)
	}

	// Different user should still work.
	_, _, err = store.Create("bob", "Token", 3)
	if err != nil {
		t.Fatalf("bob Create: %v", err)
	}
}

func TestTokenStore_ListForUser(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewTokenStore(dir)

	store.Create("alice", "A1", 5)
	store.Create("alice", "A2", 5)
	store.Create("bob", "B1", 5)

	aliceTokens := store.ListForUser("alice")
	if len(aliceTokens) != 2 {
		t.Errorf("expected 2 alice tokens, got %d", len(aliceTokens))
	}

	bobTokens := store.ListForUser("bob")
	if len(bobTokens) != 1 {
		t.Errorf("expected 1 bob token, got %d", len(bobTokens))
	}

	allTokens := store.List()
	if len(allTokens) != 3 {
		t.Errorf("expected 3 total tokens, got %d", len(allTokens))
	}
}

func TestTokenStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	store1, _ := NewTokenStore(dir)
	_, plaintext, _ := store1.Create("alice", "Persistent", 5)

	// Reload from disk.
	store2, err := NewTokenStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(store2.List()) != 1 {
		t.Fatalf("expected 1 token after reload, got %d", len(store2.List()))
	}

	// Verify still works after reload.
	got, err := store2.Verify(plaintext)
	if err != nil {
		t.Fatalf("Verify after reload: %v", err)
	}
	if got.User != "alice" {
		t.Errorf("expected alice, got %q", got.User)
	}
	// Wait for async updateLastUsed goroutine to finish before temp dir cleanup.
	time.Sleep(200 * time.Millisecond)
}
