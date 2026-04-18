package bibliography

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// crossrefEndpoint is Crossref's REST API for looking up a single work by DOI.
const crossrefEndpoint = "https://api.crossref.org/works/"

type crossrefResponse struct {
	Message crossrefWork `json:"message"`
}

type crossrefWork struct {
	Title           []string            `json:"title"`
	ContainerTitle  []string            `json:"container-title"`
	Volume          string              `json:"volume"`
	Issue           string              `json:"issue"`
	Page            string              `json:"page"`
	URL             string              `json:"URL"`
	Author          []crossrefAuthor    `json:"author"`
	Issued          crossrefDateParts   `json:"issued"`
	Created         crossrefDateParts   `json:"created"`
	Published       crossrefDateParts   `json:"published"`
	PublishedOnline crossrefDateParts   `json:"published-online"`
	PublishedPrint  crossrefDateParts   `json:"published-print"`
}

type crossrefAuthor struct {
	Family string `json:"family"`
	Given  string `json:"given"`
}

type crossrefDateParts struct {
	DateParts [][]int `json:"date-parts"`
}

func (d crossrefDateParts) Year() int {
	if len(d.DateParts) == 0 || len(d.DateParts[0]) == 0 {
		return 0
	}
	return d.DateParts[0][0]
}

// fetchDOI calls Crossref for the given DOI and returns a normalised Entry.
func (s *Service) fetchDOI(ctx context.Context, doi string) (*Entry, error) {
	if err := ValidateDOI(doi); err != nil {
		return nil, err
	}
	if err := s.crossrefLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	// Crossref wants the raw DOI in the URL path; URL-escape each segment.
	escapedDOI := (&url.URL{Path: doi}).EscapedPath()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, crossrefEndpoint+escapedDOI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", s.userAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crossref: %w: %v", ErrSourceUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("crossref: %w: status %d", ErrSourceUnreachable, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("crossref: status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("crossref: %w: %v", ErrSourceUnreachable, err)
	}
	var env crossrefResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("crossref: parse: %w", err)
	}
	work := env.Message

	entry := &Entry{
		IdentifierType: "doi",
		Identifier:     doi,
		Title:          strings.TrimSpace(joinOne(work.Title)),
		Journal:        strings.TrimSpace(joinOne(work.ContainerTitle)),
		Volume:         work.Volume,
		Issue:          work.Issue,
		Pages:          work.Page,
		URL:            firstNonEmpty(work.URL, "https://doi.org/"+doi),
		FetchedAt:      time.Now().UTC(),
		Source:         "crossref",
	}
	for _, a := range work.Author {
		entry.Authors = append(entry.Authors, Author{Family: a.Family, Given: a.Given})
	}
	entry.Year = firstNonZero(
		work.Issued.Year(),
		work.PublishedPrint.Year(),
		work.PublishedOnline.Year(),
		work.Published.Year(),
		work.Created.Year(),
	)
	return entry, nil
}

func joinOne(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}
