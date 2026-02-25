package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrGroupNotFound = errors.New("group not found")
	ErrGroupExists   = errors.New("group already exists")
)

type Group struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type GroupStore struct {
	mu     sync.RWMutex
	groups []Group
	path   string
}

func NewGroupStore(metaRoot string) (*GroupStore, error) {
	path := filepath.Join(metaRoot, "groups.json")
	s := &GroupStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *GroupStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.bootstrap()
	}
	if err != nil {
		return fmt.Errorf("read groups file: %w", err)
	}
	var groups []Group
	if err := json.Unmarshal(data, &groups); err != nil {
		return fmt.Errorf("parse groups file: %w", err)
	}
	s.groups = groups
	return nil
}

func (s *GroupStore) bootstrap() error {
	s.groups = []Group{
		{Name: "admin", Description: "Administrators"},
		{Name: "editors", Description: "Can edit all pages"},
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create groups dir: %w", err)
	}
	if err := s.saveLocked(); err != nil {
		return fmt.Errorf("write default groups file: %w", err)
	}
	log.Printf("created default groups.json with admin and editors groups")
	return nil
}

// saveLocked writes groups to disk atomically (temp file + rename).
// Caller must hold s.mu (write lock) or be in a context where concurrent access is impossible.
func (s *GroupStore) saveLocked() error {
	data, err := json.MarshalIndent(s.groups, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal groups: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create groups dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "groups-*.json.tmp")
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

// List returns all groups.
func (s *GroupStore) List() []Group {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Group, len(s.groups))
	copy(result, s.groups)
	return result
}

// Create adds a new group.
func (s *GroupStore) Create(group Group) error {
	if group.Name == "" {
		return fmt.Errorf("group name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, g := range s.groups {
		if g.Name == group.Name {
			return ErrGroupExists
		}
	}

	s.groups = append(s.groups, group)
	return s.saveLocked()
}

// Update modifies an existing group's description.
func (s *GroupStore) Update(name string, description string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, g := range s.groups {
		if g.Name == name {
			s.groups[i].Description = description
			return s.saveLocked()
		}
	}
	return ErrGroupNotFound
}

// Delete removes a group by name.
// Note: this does NOT automatically remove the group from users — the caller handles that.
func (s *GroupStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, g := range s.groups {
		if g.Name == name {
			s.groups = append(s.groups[:i], s.groups[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrGroupNotFound
}
