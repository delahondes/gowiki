package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrTokenNotFound = errors.New("token not found")
	ErrTokenExpired  = errors.New("token expired")
	ErrTooManyTokens = errors.New("maximum number of tokens reached")
)

// APIToken represents a personal API token for AI/programmatic access.
type APIToken struct {
	ID         string  `json:"id"`
	User       string  `json:"user"`
	Name       string  `json:"name"`
	TokenHash  string  `json:"token_hash"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt string  `json:"last_used_at,omitempty"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
}

// TokenStore manages API tokens, backed by a JSON file on disk.
type TokenStore struct {
	mu     sync.RWMutex
	tokens []APIToken
	path   string
}

// NewTokenStore creates or loads the token store from metaRoot/api_tokens.json.
func NewTokenStore(metaRoot string) (*TokenStore, error) {
	path := filepath.Join(metaRoot, "api_tokens.json")
	s := &TokenStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *TokenStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.tokens = []APIToken{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read tokens file: %w", err)
	}
	var tokens []APIToken
	if err := json.Unmarshal(data, &tokens); err != nil {
		return fmt.Errorf("parse tokens file: %w", err)
	}
	s.tokens = tokens
	return nil
}

func (s *TokenStore) saveLocked() error {
	data, err := json.MarshalIndent(s.tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tokens: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create tokens dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "api_tokens-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// Create generates a new API token for the given user. Returns the token
// metadata and the plaintext token value (shown once, never stored).
func (s *TokenStore) Create(user, name string, maxPerUser int) (APIToken, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxPerUser > 0 {
		count := 0
		for _, t := range s.tokens {
			if t.User == user {
				count++
			}
		}
		if count >= maxPerUser {
			return APIToken{}, "", ErrTooManyTokens
		}
	}

	// Generate token ID: tok_ + 12 hex chars.
	idBytes := make([]byte, 6)
	if _, err := rand.Read(idBytes); err != nil {
		return APIToken{}, "", fmt.Errorf("generate token id: %w", err)
	}
	id := "tok_" + hex.EncodeToString(idBytes)

	// Generate token value: gwk_ + 32 random bytes base64url-encoded.
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return APIToken{}, "", fmt.Errorf("generate token: %w", err)
	}
	plaintext := "gwk_" + base64.RawURLEncoding.EncodeToString(tokenBytes)

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return APIToken{}, "", fmt.Errorf("hash token: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	token := APIToken{
		ID:        id,
		User:      user,
		Name:      name,
		TokenHash: string(hash),
		CreatedAt: now,
	}

	s.tokens = append(s.tokens, token)
	if err := s.saveLocked(); err != nil {
		return APIToken{}, "", err
	}

	// Return a copy without the hash.
	safe := token
	safe.TokenHash = ""
	return safe, plaintext, nil
}

// Verify checks a raw token string against all stored tokens.
// Returns the matching token (without hash) on success.
func (s *TokenStore) Verify(rawToken string) (APIToken, error) {
	s.mu.RLock()
	tokens := make([]APIToken, len(s.tokens))
	copy(tokens, s.tokens)
	s.mu.RUnlock()

	for _, t := range tokens {
		// Check expiry first (cheap).
		if t.ExpiresAt != nil {
			exp, err := time.Parse(time.RFC3339, *t.ExpiresAt)
			if err == nil && time.Now().After(exp) {
				continue
			}
		}

		if err := bcrypt.CompareHashAndPassword([]byte(t.TokenHash), []byte(rawToken)); err == nil {
			// Match found — update last used asynchronously.
			go s.updateLastUsed(t.ID)
			safe := t
			safe.TokenHash = ""
			return safe, nil
		}
	}
	return APIToken{}, ErrTokenNotFound
}

func (s *TokenStore) updateLastUsed(tokenID string) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tokens {
		if s.tokens[i].ID == tokenID {
			s.tokens[i].LastUsedAt = now
			if err := s.saveLocked(); err != nil {
				log.Printf("WARNING: failed to update last_used_at for token %s: %v", tokenID, err)
			}
			return
		}
	}
}

// List returns all tokens with hashes stripped.
func (s *TokenStore) List() []APIToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]APIToken, len(s.tokens))
	for i, t := range s.tokens {
		t.TokenHash = ""
		result[i] = t
	}
	return result
}

// ListForUser returns tokens for a specific user, hashes stripped.
func (s *TokenStore) ListForUser(username string) []APIToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []APIToken
	for _, t := range s.tokens {
		if t.User == username {
			t.TokenHash = ""
			result = append(result, t)
		}
	}
	if result == nil {
		return []APIToken{}
	}
	return result
}

// Delete removes a token by ID. Returns ErrTokenNotFound if not found.
func (s *TokenStore) Delete(tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tokens {
		if t.ID == tokenID {
			s.tokens = append(s.tokens[:i], s.tokens[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrTokenNotFound
}

// DeleteForUser removes a token by ID, but only if it belongs to the given user.
func (s *TokenStore) DeleteForUser(tokenID, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tokens {
		if t.ID == tokenID {
			if t.User != username {
				return ErrTokenNotFound
			}
			s.tokens = append(s.tokens[:i], s.tokens[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrTokenNotFound
}
