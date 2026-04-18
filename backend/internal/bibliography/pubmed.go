package bibliography

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// pubmedEndpoint is the NCBI E-utilities eSummary URL for PubMed.
const pubmedEndpoint = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi"

// esummaryEnvelope mirrors the eSummary 2.0 JSON response for db=pubmed.
type esummaryEnvelope struct {
	Result map[string]json.RawMessage `json:"result"`
}

type esummaryArticle struct {
	UID       string `json:"uid"`
	PubDate   string `json:"pubdate"`
	EPubDate  string `json:"epubdate"`
	Source    string `json:"source"`
	Title     string `json:"title"`
	Volume    string `json:"volume"`
	Issue     string `json:"issue"`
	Pages     string `json:"pages"`
	Authors   []struct {
		Name string `json:"name"` // "Derosa L"
	} `json:"authors"`
	FullJournalName string `json:"fulljournalname"`
}

// fetchPubMed calls NCBI eSummary for the given PMID and returns a normalised
// Entry. Errors indicate permanent failures (not-found, invalid response);
// transient network failures are wrapped so the caller can distinguish.
func (s *Service) fetchPubMed(ctx context.Context, pmid string) (*Entry, error) {
	if err := ValidatePMID(pmid); err != nil {
		return nil, err
	}
	if err := s.pubmedLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("db", "pubmed")
	q.Set("id", pmid)
	q.Set("retmode", "json")
	q.Set("tool", "Gowiki-Bibliography")
	if s.cfg.AdminContactEmail != "" {
		q.Set("email", s.cfg.AdminContactEmail)
	}
	if s.cfg.PubmedAPIKey != "" {
		q.Set("api_key", s.cfg.PubmedAPIKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pubmedEndpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pubmed: %w: %v", ErrSourceUnreachable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("pubmed: %w: %v", ErrSourceUnreachable, err)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("pubmed: %w: status %d", ErrSourceUnreachable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pubmed: status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var env esummaryEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("pubmed: parse envelope: %w", err)
	}
	raw, ok := env.Result[pmid]
	if !ok {
		return nil, ErrNotFound
	}
	// Detect the "error" shape returned when a PMID is unknown.
	var maybeErr struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &maybeErr)
	if maybeErr.Error != "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, maybeErr.Error)
	}

	var art esummaryArticle
	if err := json.Unmarshal(raw, &art); err != nil {
		return nil, fmt.Errorf("pubmed: parse article: %w", err)
	}
	if art.UID == "" && art.Title == "" {
		return nil, ErrNotFound
	}

	entry := &Entry{
		IdentifierType: "pmid",
		Identifier:     pmid,
		Title:          strings.TrimSpace(art.Title),
		Year:           parseYearFromPubDate(art.PubDate, art.EPubDate),
		Journal:        firstNonEmpty(art.FullJournalName, art.Source),
		Volume:         art.Volume,
		Issue:          art.Issue,
		Pages:          art.Pages,
		URL:            "https://pubmed.ncbi.nlm.nih.gov/" + pmid + "/",
		FetchedAt:      time.Now().UTC(),
		Source:         "pubmed_esummary",
	}
	for _, a := range art.Authors {
		entry.Authors = append(entry.Authors, parsePubMedAuthor(a.Name))
	}
	return entry, nil
}

// parsePubMedAuthor splits a PubMed "Family Initials" string into family and
// given. PubMed's format is e.g. "Derosa L" or "Silva CAC".
func parsePubMedAuthor(name string) Author {
	s := strings.TrimSpace(name)
	if s == "" {
		return Author{}
	}
	parts := strings.Fields(s)
	if len(parts) == 1 {
		return Author{Family: parts[0]}
	}
	// Last element is the initials; everything before is the family name
	// (covers compound family names like "van der Meer").
	family := strings.Join(parts[:len(parts)-1], " ")
	given := parts[len(parts)-1]
	return Author{Family: family, Given: given}
}

func parseYearFromPubDate(pub, epub string) int {
	for _, s := range []string{pub, epub} {
		s = strings.TrimSpace(s)
		if len(s) < 4 {
			continue
		}
		y, err := strconv.Atoi(s[:4])
		if err == nil && y > 1800 && y < 3000 {
			return y
		}
	}
	return 0
}

// ErrNotFound and ErrSourceUnreachable are declared at package level so the
// HTTP handler can map them to 404 / 503 respectively.
var (
	ErrNotFound          = errors.New("bibliography: identifier not found")
	ErrSourceUnreachable = errors.New("bibliography: source unreachable")
)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
