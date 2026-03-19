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
	DataDir    string           `yaml:"data_dir" json:"data_dir"`       // root data directory (contains content/, meta/, attic/, etc.)
	Server     ServerConfig     `yaml:"server" json:"server"`
	Site       SiteConfig       `yaml:"site" json:"site"`
	Auth       AuthConfig       `yaml:"auth" json:"auth"`
	Drafts     DraftsConfig     `yaml:"drafts" json:"drafts"`
	Database   DatabaseConfig   `yaml:"database" json:"database"`
	Todo       TodoConfig       `yaml:"todo" json:"todo"`
	Reviewflow ReviewflowConfig `yaml:"reviewflow" json:"reviewflow"`
	AIAPI      AIAPIConfig      `yaml:"ai_api" json:"ai_api"`
}

// ServerConfig holds network/serving settings.
type ServerConfig struct {
	Addr      string `yaml:"addr" json:"addr"`             // HTTP listen address (default ":8080")
	TLSDomain string `yaml:"tls_domain" json:"tls_domain"` // domain for Let's Encrypt auto-TLS
	WebDir    string `yaml:"web_dir" json:"web_dir"`       // directory containing built frontend assets
}

// ReviewflowConfig holds document validation workflow settings.
type ReviewflowConfig struct {
	Enabled   bool              `yaml:"enabled" json:"enabled"`
	Deadlines map[string]string `yaml:"deadlines" json:"deadlines"` // role name -> duration string (e.g. "72h")
	Signing   SigningConfig     `yaml:"signing" json:"signing"`
}

// SigningConfig holds X.509 document signing settings.
type SigningConfig struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`
	Required     bool     `yaml:"required" json:"required"`
	TrustStore   []string `yaml:"trust_store" json:"trust_store"`     // paths to trusted CA PEM files
	RevokedCerts []string `yaml:"revoked_certs" json:"revoked_certs"` // SHA-256 fingerprints of revoked certs
}

// TodoConfig holds task management plugin settings.
// Todo is automatically enabled when a database connection is active,
// unless explicitly disabled via the "disabled" flag.
type TodoConfig struct {
	Enabled       bool             `yaml:"enabled" json:"enabled"`
	Disabled      bool             `yaml:"disabled" json:"disabled"`
	ReminderHours []int            `yaml:"reminder_hours" json:"reminder_hours"`
	Notify        TodoNotifyConfig `yaml:"notify" json:"notify"`
}

// TodoNotifyConfig holds notification channel settings.
type TodoNotifyConfig struct {
	Email    TodoEmailConfig     `yaml:"email" json:"email"`
	Webhooks []TodoWebhookConfig `yaml:"webhook" json:"webhook"`
}

// TodoEmailConfig holds SMTP email notification settings.
type TodoEmailConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	From     string `yaml:"from" json:"from"`
	SMTPHost string `yaml:"smtp_host" json:"smtp_host"`
	SMTPPort int    `yaml:"smtp_port" json:"smtp_port"`
	SMTPUser string `yaml:"smtp_user" json:"smtp_user"`
	SMTPPass string `yaml:"smtp_pass" json:"smtp_pass"`
}

// TodoWebhookConfig holds a single outbound webhook configuration.
type TodoWebhookConfig struct {
	Name        string `yaml:"name" json:"name"`
	Enabled     bool   `yaml:"enabled" json:"enabled"`
	URL         string `yaml:"url" json:"url"`
	ContentType string `yaml:"content_type" json:"content_type"`
	PayloadTmpl string `yaml:"payload_tmpl" json:"payload_tmpl"`
	HMACSecret  string `yaml:"hmac_secret" json:"hmac_secret"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	DSN     string `yaml:"dsn" json:"dsn"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

// SiteConfig holds site-wide display settings.
type SiteConfig struct {
	Title          string `yaml:"title" json:"title"`
	BaseURL        string `yaml:"base_url" json:"base_url"`               // e.g. "https://wiki.example.com"
	FooterPage     string `yaml:"footer_page" json:"footer_page"`
	SidebarPage    string `yaml:"sidebar_page" json:"sidebar_page"`
	TOCMaxLevel    int    `yaml:"toc_max_level" json:"toc_max_level"`     // 0 = disabled, 1-6 = show headings up to this level
	UserDisplay    string `yaml:"user_display" json:"user_display"`       // "login" (default), "fullname", "email"
	CodeTheme      string `yaml:"code_theme" json:"code_theme"`           // highlight.js theme name
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	SessionTTL string      `yaml:"session_ttl" json:"session_ttl"` // duration string, e.g. "24h"
	OAuth      OAuthConfig `yaml:"oauth" json:"oauth"`
}

// OAuthConfig holds external OAuth/OIDC provider settings.
type OAuthConfig struct {
	Provider        string   `yaml:"provider" json:"provider"`                 // "azure" or "" (disabled)
	TenantID        string   `yaml:"tenant_id" json:"tenant_id"`              // Azure AD tenant ID
	ClientID        string   `yaml:"client_id" json:"client_id"`              // Application (client) ID
	ClientSecret    string   `yaml:"client_secret" json:"client_secret"`      // Client secret value
	AutoCreateUsers bool     `yaml:"auto_create_users" json:"auto_create_users"` // Create user on first OAuth login
	DefaultGroups   []string `yaml:"default_groups" json:"default_groups"`    // Groups for auto-created users
}

// AIAPIConfig holds settings for the AI Content API (token-based access).
type AIAPIConfig struct {
	Enabled          bool `yaml:"enabled" json:"enabled"`
	RateLimitRead    int  `yaml:"rate_limit_read" json:"rate_limit_read"`
	RateLimitWrite   int  `yaml:"rate_limit_write" json:"rate_limit_write"`
	MaxTokensPerUser int  `yaml:"max_tokens_per_user" json:"max_tokens_per_user"`
	RequireSummary   bool `yaml:"require_summary" json:"require_summary"`
}

// DraftsConfig holds draft/lock settings.
type DraftsConfig struct {
	AutoSaveInterval string `yaml:"auto_save_interval" json:"auto_save_interval"` // e.g. "2m"
	StaleLockTimeout string `yaml:"stale_lock_timeout" json:"stale_lock_timeout"` // e.g. "24h"
}

// DefaultConfig returns the configuration with all default values.
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Addr: ":8080",
		},
		Site: SiteConfig{
			Title:       "Gowiki",
			FooterPage:  "footer",
			SidebarPage: "sidebar",
			TOCMaxLevel: 3,
			CodeTheme:   "github",
		},
		Auth: AuthConfig{
			SessionTTL: "24h",
		},
		Drafts: DraftsConfig{
			AutoSaveInterval: "2m",
			StaleLockTimeout: "24h",
		},
		AIAPI: AIAPIConfig{
			Enabled:          false,
			RateLimitRead:    120,
			RateLimitWrite:   30,
			MaxTokensPerUser: 5,
			RequireSummary:   true,
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
	for role, dur := range cfg.Reviewflow.Deadlines {
		if _, err := time.ParseDuration(dur); err != nil {
			return fmt.Errorf("reviewflow.deadlines.%s: invalid duration %q: %w", role, dur, err)
		}
	}
	return nil
}
