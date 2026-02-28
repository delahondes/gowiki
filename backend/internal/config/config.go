package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the full site configuration.
type Config struct {
	Site     SiteConfig     `yaml:"site" json:"site"`
	Auth     AuthConfig     `yaml:"auth" json:"auth"`
	Drafts   DraftsConfig   `yaml:"drafts" json:"drafts"`
	Database DatabaseConfig `yaml:"database" json:"database"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	DSN     string `yaml:"dsn" json:"dsn"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// SiteConfig holds site-wide display settings.
type SiteConfig struct {
	Title       string `yaml:"title" json:"title"`
	FooterPage  string `yaml:"footer_page" json:"footer_page"`
	SidebarPage string `yaml:"sidebar_page" json:"sidebar_page"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	SessionTTL string `yaml:"session_ttl" json:"session_ttl"` // duration string, e.g. "24h"
}

// DraftsConfig holds draft/lock settings.
type DraftsConfig struct {
	AutoSaveInterval string `yaml:"auto_save_interval" json:"auto_save_interval"` // e.g. "2m"
	StaleLockTimeout string `yaml:"stale_lock_timeout" json:"stale_lock_timeout"` // e.g. "24h"
}

// DefaultConfig returns the configuration with all default values.
func DefaultConfig() Config {
	return Config{
		Site: SiteConfig{
			Title:       "Gowiki",
			FooterPage:  "footer",
			SidebarPage: "sidebar",
		},
		Auth: AuthConfig{
			SessionTTL: "24h",
		},
		Drafts: DraftsConfig{
			AutoSaveInterval: "2m",
			StaleLockTimeout: "24h",
		},
	}
}

// Store manages configuration state, backed by a YAML file on disk.
// The file is the source of truth; the in-memory copy is a cache.
type Store struct {
	mu     sync.RWMutex
	config Config
	path   string
}

// Load reads configuration from a YAML file at the given path.
// If the file does not exist, it is created with default values.
func Load(path string) (*Store, error) {
	s := &Store{path: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// File doesn't exist — create with defaults.
		s.config = DefaultConfig()
		if err := s.save(); err != nil {
			return nil, fmt.Errorf("config: create default file: %w", err)
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read file: %w", err)
	}

	// Start from defaults so that any missing keys get their default value.
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse yaml: %w", err)
	}
	s.config = cfg
	return s, nil
}

// Get returns a snapshot of the current configuration.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// Update validates the new configuration, writes it atomically to disk,
// and updates the in-memory state.
func (s *Store) Update(newConfig Config) error {
	if err := validate(newConfig); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = newConfig
	if err := s.save(); err != nil {
		return fmt.Errorf("config: save: %w", err)
	}
	return nil
}

// save marshals the current config to YAML and writes it atomically
// using a temporary file + rename.
func (s *Store) save() error {
	data, err := yaml.Marshal(s.config)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// validate checks that all duration strings are parseable and that
// required fields are non-empty.
func validate(cfg Config) error {
	if cfg.Site.Title == "" {
		return fmt.Errorf("site.title must not be empty")
	}

	if cfg.Auth.SessionTTL != "" {
		if _, err := time.ParseDuration(cfg.Auth.SessionTTL); err != nil {
			return fmt.Errorf("auth.session_ttl: invalid duration %q: %w", cfg.Auth.SessionTTL, err)
		}
	}
	if cfg.Drafts.AutoSaveInterval != "" {
		if _, err := time.ParseDuration(cfg.Drafts.AutoSaveInterval); err != nil {
			return fmt.Errorf("drafts.auto_save_interval: invalid duration %q: %w", cfg.Drafts.AutoSaveInterval, err)
		}
	}
	if cfg.Drafts.StaleLockTimeout != "" {
		if _, err := time.ParseDuration(cfg.Drafts.StaleLockTimeout); err != nil {
			return fmt.Errorf("drafts.stale_lock_timeout: invalid duration %q: %w", cfg.Drafts.StaleLockTimeout, err)
		}
	}
	return nil
}
