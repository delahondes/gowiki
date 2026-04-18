package bibliography

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"gowiki/backend/internal/config"
)

// Service coordinates cache lookups, rate-limited network fetches, and
// singleflight deduplication so concurrent page renders don't hit the source
// APIs for the same identifier.
type Service struct {
	cfg             config.BibliographyConfig
	store           *Store
	httpClient      *http.Client
	pubmedLimiter   *rate.Limiter
	crossrefLimiter *rate.Limiter
	group           singleflight.Group
	serverURL       string
	mu              sync.Mutex
}

// NewService builds a bibliography service.
//
// metaRoot is the data/meta directory; cfg carries the plugin's config;
// serverURL is the wiki's public URL (used in the User-Agent per NIH etiquette).
func NewService(metaRoot string, cfg config.BibliographyConfig, serverURL string) *Service {
	pubmedRate := 3.0
	if cfg.PubmedAPIKey != "" {
		pubmedRate = 10.0
	}
	return &Service{
		cfg:             cfg,
		store:           NewStore(metaRoot),
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		pubmedLimiter:   rate.NewLimiter(rate.Limit(pubmedRate), 1),
		crossrefLimiter: rate.NewLimiter(rate.Limit(50), 5),
		serverURL:       serverURL,
	}
}

// Enabled reports whether the plugin is switched on in config.
func (s *Service) Enabled() bool {
	return s.cfg.Enabled
}

// Store exposes the underlying cache for list/management endpoints.
func (s *Service) Store() *Store { return s.store }

// ResolvePMID returns the entry for a PMID, hitting the cache first and the
// network only on a miss. Concurrent resolves for the same identifier are
// deduplicated.
func (s *Service) ResolvePMID(ctx context.Context, pmid string) (*Entry, error) {
	if err := ValidatePMID(pmid); err != nil {
		return nil, err
	}
	if e, err := s.store.GetPMID(pmid); err == nil {
		return e, nil
	} else if !errors.Is(err, ErrNotCached) {
		return nil, err
	}
	v, err, _ := s.group.Do("pmid:"+pmid, func() (any, error) {
		e, err := s.fetchPubMed(ctx, pmid)
		if err != nil {
			return nil, err
		}
		if err := s.store.Save(e); err != nil {
			return nil, err
		}
		return e, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Entry), nil
}

// ResolveDOI returns the entry for a DOI, same cache-first semantics.
func (s *Service) ResolveDOI(ctx context.Context, doi string) (*Entry, error) {
	if err := ValidateDOI(doi); err != nil {
		return nil, err
	}
	if e, err := s.store.GetDOI(doi); err == nil {
		return e, nil
	} else if !errors.Is(err, ErrNotCached) {
		return nil, err
	}
	v, err, _ := s.group.Do("doi:"+doi, func() (any, error) {
		e, err := s.fetchDOI(ctx, doi)
		if err != nil {
			return nil, err
		}
		if err := s.store.Save(e); err != nil {
			return nil, err
		}
		return e, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Entry), nil
}

func (s *Service) userAgent() string {
	contact := s.cfg.AdminContactEmail
	if contact == "" {
		contact = "noreply@localhost"
	}
	url := s.serverURL
	if url == "" {
		url = "local"
	}
	return fmt.Sprintf("Gowiki-Bibliography/1.0 (+%s; mailto:%s)", url, contact)
}
