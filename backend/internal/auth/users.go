package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type User struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
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
	return nil
}

func (s *UserStore) bootstrap() error {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash default password: %w", err)
	}
	s.users = []User{{Username: "admin", PasswordHash: string(hash)}}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create users dir: %w", err)
	}
	data, _ := json.MarshalIndent(s.users, "", "  ")
	data = append(data, '\n')
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write default users file: %w", err)
	}
	log.Printf("WARNING: created default users.json with admin/admin — change the password!")
	return nil
}

func (s *UserStore) Verify(username, password string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Username == username {
			if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
				return ErrInvalidCredentials
			}
			return nil
		}
	}
	return ErrInvalidCredentials
}
