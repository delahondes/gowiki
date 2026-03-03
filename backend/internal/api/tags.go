package api

import (
	"encoding/json"
	"net/http"

	"gowiki/backend/internal/markdown"
)

type tagQueryResult struct {
	Tag   string           `json:"tag"`
	Pages []tagQueryPage   `json:"pages"`
}

type tagQueryPage struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Extract string `json:"extract"`
}

func (s *Server) handleTagQuery(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	if tag == "" {
		http.Error(w, "tag parameter required", http.StatusBadRequest)
		return
	}
	pathPrefix := r.URL.Query().Get("path")

	if s.tagIndex == nil {
		http.Error(w, "tag queries not supported", http.StatusNotImplemented)
		return
	}

	entries := s.tagIndex.GetPagesForTag(tag, pathPrefix)

	pages := make([]tagQueryPage, 0, len(entries))
	for _, e := range entries {
		// Try to read page content for extract.
		extract := ""
		page, err := s.store.Get(e.Path)
		if err == nil {
			plain := markdown.StripMarkdown(page.Markdown)
			if len(plain) > 150 {
				// Truncate at a word boundary.
				cut := 150
				for cut > 0 && plain[cut] != ' ' {
					cut--
				}
				if cut == 0 {
					cut = 150
				}
				extract = plain[:cut] + "..."
			} else {
				extract = plain
			}
		}
		pages = append(pages, tagQueryPage{
			Path:    e.Path,
			Title:   e.Title,
			Extract: extract,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tagQueryResult{
		Tag:   tag,
		Pages: pages,
	})
}
