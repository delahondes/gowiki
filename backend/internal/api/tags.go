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
	Version int64  `json:"version"`
	Author  string `json:"author"`
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
		extract := ""
		var version int64
		var author string
		page, err := s.store.Get(e.Path)
		if err == nil {
			plain := markdown.StripMarkdown(page.Markdown)
			if len(plain) > 150 {
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
			version = page.Meta.Version
			author = page.Meta.Author
		}
		pages = append(pages, tagQueryPage{
			Path:    e.Path,
			Title:   e.Title,
			Extract: extract,
			Version: version,
			Author:  author,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tagQueryResult{
		Tag:   tag,
		Pages: pages,
	})
}
