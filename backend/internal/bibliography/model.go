// Package bibliography resolves and caches publication metadata (PubMed,
// Crossref/DOI) used by the {publication} wiki directive.
package bibliography

import "time"

// Entry is the cached metadata for a single publication.
type Entry struct {
	IdentifierType string    `json:"identifier_type"` // "pmid" or "doi"
	Identifier     string    `json:"identifier"`
	Title          string    `json:"title,omitempty"`
	Authors        []Author  `json:"authors,omitempty"`
	Year           int       `json:"year,omitempty"`
	Journal        string    `json:"journal,omitempty"`
	Volume         string    `json:"volume,omitempty"`
	Issue          string    `json:"issue,omitempty"`
	Pages          string    `json:"pages,omitempty"`
	URL            string    `json:"url"`
	FetchedAt      time.Time `json:"fetched_at"`
	Source         string    `json:"source"`
}

// Author is a single author of a publication.
type Author struct {
	Family string `json:"family"`
	Given  string `json:"given,omitempty"`
}
