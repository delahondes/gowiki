package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserExists         = errors.New("user already exists")
	ErrUserDisabled       = errors.New("user account is disabled")
)

type User struct {
	Username     string   `json:"username"`
	PasswordHash string   `json:"password_hash"`
	Email        string   `json:"email"`
	DisplayName  string   `json:"display_name"`
	Groups       []string `json:"groups"`
	Disabled     bool     `json:"disabled"`
	CreatedAt    string   `json:"created_at"`
	LastLogin    string   `json:"last_login,omitempty"`
}

type UserStore struct {
	mu    sync.RWMutex
	users []User
	path  string
}

func NewUserStore(metaRoot string) (*UserStore, error) {
	path := filepath.Join(metaRoot, "users.json")
	s := &UserStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *UserStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.bootstrap()
	}
	if err != nil {
		return fmt.Errorf("read users file: %w", err)
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("parse users file: %w", err)
	}
	s.users = users

	// Migrate existing users: ensure admin group and created_at are set.
	migrated := false
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range s.users {
		if len(s.users[i].Groups) == 0 && i == 0 {
			s.users[i].Groups = []string{"admin"}
			migrated = true
		}
		if s.users[i].CreatedAt == "" {
			s.users[i].CreatedAt = now
			migrated = true
		}
	}
	if migrated {
		if err := s.saveLocked(); err != nil {
			log.Printf("WARNING: failed to save migrated users: %v", err)
		}
	}
	return nil
}

func (s *UserStore) bootstrap() error {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash default password: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.users = []User{{
		Username:     "admin",
		PasswordHash: string(hash),
		Groups:       []string{"admin"},
		CreatedAt:    now,
	}}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create users dir: %w", err)
	}
	if err := s.saveLocked(); err != nil {
		return fmt.Errorf("write default users file: %w", err)
	}
	log.Printf("WARNING: created default users.json with admin/admin — change the password!")
	return nil
}

// saveLocked writes users to disk atomically (temp file + rename).
// Caller must hold s.mu (write lock) or be in a context where concurrent access is impossible (e.g., bootstrap/load).
func (s *UserStore) saveLocked() error {
	data, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal users: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create users dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "users-*.json.tmp")
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

// Verify checks username and password. Returns ErrUserDisabled if the account is disabled.
func (s *UserStore) Verify(username, password string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Username == username {
			if u.Disabled {
				return ErrUserDisabled
			}
			if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
				return ErrInvalidCredentials
			}
			return nil
		}
	}
	return ErrInvalidCredentials
}

// IsAdmin returns true if the user belongs to the "admin" group.
func (s *UserStore) IsAdmin(username string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Username == username {
			for _, g := range u.Groups {
				if g == "admin" {
					return true
				}
			}
			return false
		}
	}
	return false
}

// Get returns a single user by username.
func (s *UserStore) Get(username string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Username == username {
			return u, nil
		}
	}
	return User{}, ErrUserNotFound
}

// GetByEmail returns the first user matching the given email (case-insensitive).
func (s *UserStore) GetByEmail(email string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	lower := strings.ToLower(email)
	for _, u := range s.users {
		if strings.ToLower(u.Email) == lower {
			return u, nil
		}
	}
	return User{}, ErrUserNotFound
}

// List returns all users. Caller should strip password hashes before sending to clients.
func (s *UserStore) List() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]User, len(s.users))
	copy(result, s.users)
	return result
}

// Create adds a new user. The Password field in the provided user is expected to be
// already hashed in PasswordHash, OR the caller should call SetPassword separately.
// If PasswordHash is empty and a raw password is provided via the rawPassword parameter, it will be hashed.
func (s *UserStore) Create(user User, rawPassword string) error {
	if user.Username == "" {
		return fmt.Errorf("username is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check uniqueness.
	for _, u := range s.users {
		if u.Username == user.Username {
			return ErrUserExists
		}
	}

	// Hash password if raw password is provided.
	if rawPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(rawPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		user.PasswordHash = string(hash)
	}

	if user.CreatedAt == "" {
		user.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if user.Groups == nil {
		user.Groups = []string{}
	}

	s.users = append(s.users, user)
	return s.saveLocked()
}

// UserUpdate holds the fields that can be updated on a user.
type UserUpdate struct {
	Email       *string   `json:"email"`
	DisplayName *string   `json:"display_name"`
	Groups      *[]string `json:"groups"`
	Disabled    *bool     `json:"disabled"`
}

// Update modifies an existing user's mutable fields.
func (s *UserStore) Update(username string, updates UserUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, u := range s.users {
		if u.Username == username {
			if updates.Email != nil {
				s.users[i].Email = *updates.Email
			}
			if updates.DisplayName != nil {
				s.users[i].DisplayName = *updates.DisplayName
			}
			if updates.Groups != nil {
				s.users[i].Groups = *updates.Groups
			}
			if updates.Disabled != nil {
				s.users[i].Disabled = *updates.Disabled
			}
			return s.saveLocked()
		}
	}
	return ErrUserNotFound
}

// Delete removes a user by username.
func (s *UserStore) Delete(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, u := range s.users {
		if u.Username == username {
			s.users = append(s.users[:i], s.users[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrUserNotFound
}

// SetPassword hashes and stores a new password for the given user.
func (s *UserStore) SetPassword(username, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("password is required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, u := range s.users {
		if u.Username == username {
			s.users[i].PasswordHash = string(hash)
			return s.saveLocked()
		}
	}
	return ErrUserNotFound
}

// UpdateLastLogin sets the last_login timestamp to now for the given user.
func (s *UserStore) UpdateLastLogin(username string) {
	now := time.Now().UTC().Format(time.RFC3339)

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, u := range s.users {
		if u.Username == username {
			s.users[i].LastLogin = now
			if err := s.saveLocked(); err != nil {
				log.Printf("WARNING: failed to update last_login for %s: %v", username, err)
			}
			return
		}
	}
}
