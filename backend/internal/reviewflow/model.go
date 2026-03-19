package reviewflow

import "time"

// Confirmation records a single role confirmation for a page version.
type Confirmation struct {
	PageVersion int64     `json:"page_version"`
	Role        string    `json:"role"`
	User        string    `json:"user"`
	Timestamp   time.Time `json:"timestamp"`
	VersionTag  string    `json:"version_tag"`
	// X.509 signing fields (empty for click-only confirmations).
	Signature       string `json:"signature,omitempty"`
	Digest          string `json:"digest,omitempty"`
	CertFingerprint string `json:"cert_fingerprint,omitempty"`
}

// ConfirmOpts holds optional signing data for a confirmation.
type ConfirmOpts struct {
	Signature       string
	Digest          string
	CertFingerprint string
}

// VersionRecord records a fully-validated version.
type VersionRecord struct {
	PageVersion int64             `json:"page_version"`
	Timestamp   time.Time         `json:"timestamp"`
	ConfirmedBy map[string]string `json:"confirmed_by"` // role -> user
	VersionTag  string            `json:"version_tag"`
}

// State is the persisted reviewflow state for a page.
type State struct {
	Roles              map[string]string `json:"roles"`
	VersionTag         string            `json:"version_tag"`
	CurrentPageVersion int64             `json:"current_page_version"`
	Confirmations      []Confirmation    `json:"confirmations"`
	VersionHistory     []VersionRecord   `json:"version_history,omitempty"`
	ValidatedVersion   int64             `json:"validated_page_version"`
}

// Status is the computed response sent to the frontend.
type Status struct {
	Roles            map[string]string `json:"roles"`
	VersionTag       string            `json:"version_tag"`
	CurrentPageVer   int64             `json:"current_page_version"`
	ValidatedVersion int64             `json:"validated_page_version"`
	MissingRoles     map[string]string `json:"missing_roles"`
	Deadlines        map[string]string `json:"deadlines,omitempty"`
	OverdueRoles     []string          `json:"overdue_roles,omitempty"`
	IsFullyValidated bool              `json:"is_fully_validated"`
	VersionHistory   []VersionRecord   `json:"version_history,omitempty"`
	SigningEnabled   bool              `json:"signing_enabled,omitempty"`
	SigningRequired  bool              `json:"signing_required,omitempty"`
	SignedRoles      []string          `json:"signed_roles,omitempty"`
}

// UserCertificate holds a user's X.509 signing certificate.
type UserCertificate struct {
	Username       string     `json:"username"`
	CertificatePEM string     `json:"certificate_pem"`
	Fingerprint    string     `json:"fingerprint"`
	Issuer         string     `json:"issuer"`
	Subject        string     `json:"subject"`
	NotBefore      time.Time  `json:"not_before"`
	NotAfter       time.Time  `json:"not_after"`
	UploadedAt     time.Time  `json:"uploaded_at"`
	Revoked        bool       `json:"revoked"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

// AtticMeta is stored in AtticEntry.PluginMeta["reviewflow"].
type AtticMeta struct {
	VersionTag  string            `json:"version_tag,omitempty"`
	ConfirmedBy map[string]string `json:"confirmed_by,omitempty"`
	IsValidated bool              `json:"is_validated"`
}
