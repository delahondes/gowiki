package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/highlight/highlighter/ansi"

	"gowiki/backend/internal/markdown"
)

// SearchResult represents a single search hit.
type SearchResult struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// searchDocument is the struct indexed by Bleve.
type searchDocument struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// SearchIndex wraps a Bleve index for full-text search.
type SearchIndex struct {
	index bleve.Index
}

// OpenSearchIndex opens an existing Bleve index at indexPath, or creates a new one.
func OpenSearchIndex(indexPath string) (*SearchIndex, error) {
	idx, err := bleve.Open(indexPath)
	if err == bleve.ErrorIndexPathDoesNotExist {
		m := buildIndexMapping()
		idx, err = bleve.New(indexPath, m)
		if err != nil {
			return nil, fmt.Errorf("create search index: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("open search index: %w", err)
	}
	return &SearchIndex{index: idx}, nil
}

func buildIndexMapping() mapping.IndexMapping {
	// Field mappings.
	pathField := bleve.NewTextFieldMapping()
	pathField.Analyzer = "keyword"
	pathField.Store = true

	// Name field: path with slashes replaced by spaces so each segment is
	// tokenized by the standard analyzer, making page names searchable.
	nameField := bleve.NewTextFieldMapping()
	nameField.Analyzer = "standard"
	nameField.Store = false

	titleField := bleve.NewTextFieldMapping()
	titleField.Analyzer = "standard"
	titleField.Store = true

	bodyField := bleve.NewTextFieldMapping()
	bodyField.Analyzer = "standard"
	bodyField.Store = true

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("path", pathField)
	docMapping.AddFieldMappingsAt("name", nameField)
	docMapping.AddFieldMappingsAt("title", titleField)
	docMapping.AddFieldMappingsAt("body", bodyField)

	im := bleve.NewIndexMapping()
	im.DefaultMapping = docMapping
	im.DefaultAnalyzer = "standard"
	return im
}

// Close closes the Bleve index.
func (s *SearchIndex) Close() error {
	return s.index.Close()
}

// pathToName converts a page path to a searchable string by replacing
// slashes and underscores with spaces, so each path segment becomes a
// searchable token. e.g. "/regulatory/smq/_template" -> "regulatory smq template"
func pathToName(pagePath string) string {
	s := strings.TrimPrefix(pagePath, "/")
	s = strings.ReplaceAll(s, "/", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	return s
}

// IndexPage upserts a document in the search index.
func (s *SearchIndex) IndexPage(pagePath, title, plaintext string) error {
	doc := searchDocument{
		Path:  pagePath,
		Name:  pathToName(pagePath),
		Title: title,
		Body:  plaintext,
	}
	return s.index.Index(pagePath, doc)
}

// DeletePage removes a document from the search index.
func (s *SearchIndex) DeletePage(pagePath string) error {
	return s.index.Delete(pagePath)
}

// escapeQueryString escapes Bleve query-string special characters so they
// are treated as literal text rather than operators.
func escapeQueryString(q string) string {
	// Characters with special meaning in Bleve query string syntax.
	const specials = `+-=&|><!(){}[]^"~*?:\/`
	var b strings.Builder
	b.Grow(len(q))
	for _, r := range q {
		if strings.ContainsRune(specials, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Search queries the index and returns results with path, title, and snippet.
func (s *SearchIndex) Search(query string, limit int) ([]SearchResult, error) {
	escaped := escapeQueryString(query)
	// Search across name (path segments), title, and body.
	// Boost name and title matches so path-based results appear first.
	nameQ := bleve.NewMatchQuery(escaped)
	nameQ.SetField("name")
	nameQ.SetBoost(10)
	titleQ := bleve.NewMatchQuery(escaped)
	titleQ.SetField("title")
	titleQ.SetBoost(5)
	bodyQ := bleve.NewQueryStringQuery(escaped)
	q := bleve.NewDisjunctionQuery(nameQ, titleQ, bodyQ)
	req := bleve.NewSearchRequestOptions(q, limit, 0, false)
	req.Fields = []string{"path", "title", "body"}
	req.Highlight = bleve.NewHighlightWithStyle(ansi.Name)
	req.Highlight.AddField("body")

	sr, err := s.index.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	results := make([]SearchResult, 0, len(sr.Hits))
	for _, hit := range sr.Hits {
		r := SearchResult{
			Path: hit.ID,
		}

		if v, ok := hit.Fields["title"].(string); ok {
			r.Title = v
		}

		// Use highlighted fragment from body if available.
		if fragments, ok := hit.Fragments["body"]; ok && len(fragments) > 0 {
			// Strip ANSI codes from the snippet since we're serving JSON.
			r.Snippet = stripANSI(fragments[0])
		} else if v, ok := hit.Fields["body"].(string); ok {
			// Fallback: first 200 chars of body.
			if len(v) > 200 {
				r.Snippet = v[:200] + "..."
			} else {
				r.Snippet = v
			}
		}

		results = append(results, r)
	}
	return results, nil
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			// Skip until 'm'.
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// RebuildFromDir walks all .md files under contentDir, strips markdown,
// extracts titles, and indexes each page.
func (s *SearchIndex) RebuildFromDir(contentDir string) error {
	// Delete all existing documents before rebuilding.
	// This prevents stale entries from prior indexing runs.
	searchReq := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	searchReq.Size = 10000
	searchReq.Fields = []string{}
	if sr, err := s.index.Search(searchReq); err == nil {
		delBatch := s.index.NewBatch()
		for _, hit := range sr.Hits {
			delBatch.Delete(hit.ID)
		}
		if delBatch.Size() > 0 {
			_ = s.index.Batch(delBatch)
		}
	}

	batch := s.index.NewBatch()
	count := 0

	err := filepath.Walk(contentDir, func(absPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(absPath, ".md") {
			return nil
		}

		rel, relErr := filepath.Rel(contentDir, absPath)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		pagePath := CanonicalPath(strings.TrimSuffix(rel, ".md"))

		content, readErr := os.ReadFile(absPath)
		if readErr != nil {
			return readErr
		}

		plaintext := markdown.StripMarkdown(string(content))
		title := markdown.ExtractTitle(string(content))

		doc := searchDocument{
			Path:  pagePath,
			Name:  pathToName(pagePath),
			Title: title,
			Body:  plaintext,
		}
		if err := batch.Index(pagePath, doc); err != nil {
			return fmt.Errorf("batch index %s: %w", pagePath, err)
		}
		count++

		// Flush batch every 100 documents.
		if count%100 == 0 {
			if err := s.index.Batch(batch); err != nil {
				return fmt.Errorf("flush batch: %w", err)
			}
			batch = s.index.NewBatch()
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("rebuild search index: %w", err)
	}

	// Flush remaining.
	if batch.Size() > 0 {
		if err := s.index.Batch(batch); err != nil {
			return fmt.Errorf("flush final batch: %w", err)
		}
	}

	return nil
}
