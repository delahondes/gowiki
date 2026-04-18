package bibliography

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var (
	// ErrNotCached is returned when a caller requests a cache entry that
	// hasn't been fetched yet.
	ErrNotCached = errors.New("bibliography: not cached")
	// ErrInvalidIdentifier is returned for malformed identifiers.
	ErrInvalidIdentifier = errors.New("bibliography: invalid identifier")
)

// Store persists bibliography entries to disk under data/meta/bibliography/.
type Store struct {
	root string
	mu   sync.Mutex
}

// NewStore creates a new bibliography store rooted at metaRoot.
// The actual files live under <metaRoot>/bibliography/.
func NewStore(metaRoot string) *Store {
	return &Store{root: filepath.Join(metaRoot, "bibliography")}
}

var pmidRe = regexp.MustCompile(`^[0-9]+$`)

// ValidatePMID returns nil if the given string is a well-formed PMID.
func ValidatePMID(pmid string) error {
	if !pmidRe.MatchString(pmid) {
		return fmt.Errorf("%w: %q is not a PMID", ErrInvalidIdentifier, pmid)
	}
	return nil
}

// ValidateDOI returns nil if the given string looks like a DOI. The check is
// intentionally loose — Crossref will reject anything truly malformed, and
// DOIs can contain nearly any printable character.
func ValidateDOI(doi string) error {
	if len(doi) < 7 || doi[:2] != "10" || doi[2] != '.' {
		return fmt.Errorf("%w: %q does not look like a DOI", ErrInvalidIdentifier, doi)
	}
	return nil
}

// pmidPath returns the cache file path for a PMID entry.
func (s *Store) pmidPath(pmid string) string {
	return filepath.Join(s.root, "pmid", pmid+".json")
}

// doiPath returns the cache file path for a DOI entry. DOIs can contain
// slashes and other special characters, so we key the file by the SHA-1 hash
// of the DOI and stash the DOI itself in the file body.
func (s *Store) doiPath(doi string) string {
	h := sha1.Sum([]byte(doi))
	return filepath.Join(s.root, "doi", hex.EncodeToString(h[:])+".json")
}

// GetPMID returns the cached entry for a PMID, or ErrNotCached.
func (s *Store) GetPMID(pmid string) (*Entry, error) {
	return s.read(s.pmidPath(pmid))
}

// GetDOI returns the cached entry for a DOI, or ErrNotCached.
func (s *Store) GetDOI(doi string) (*Entry, error) {
	return s.read(s.doiPath(doi))
}

// Save writes an entry to disk, keyed by its IdentifierType+Identifier.
func (s *Store) Save(e *Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var p string
	switch e.IdentifierType {
	case "pmid":
		p = s.pmidPath(e.Identifier)
	case "doi":
		p = s.doiPath(e.Identifier)
	default:
		return fmt.Errorf("bibliography: unsupported identifier type %q", e.IdentifierType)
	}

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("bibliography: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("bibliography: encode: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp-*")
	if err != nil {
		return fmt.Errorf("bibliography: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p)
}

// List returns every cached entry in the store.
func (s *Store) List() ([]Entry, error) {
	var entries []Entry
	err := filepath.Walk(s.root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() || filepath.Ext(p) != ".json" {
			return nil
		}
		e, readErr := s.read(p)
		if readErr != nil || e == nil {
			return nil
		}
		entries = append(entries, *e)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return entries, err
}

func (s *Store) read(p string) (*Entry, error) {
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, ErrNotCached
	}
	if err != nil {
		return nil, fmt.Errorf("bibliography: read: %w", err)
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("bibliography: parse: %w", err)
	}
	return &e, nil
}
