package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

type tagQueryResult struct {
	Tag   string           `json:"tag"`
	Pages []tagQueryPage   `json:"pages"`
}

type tagQueryPage struct {
	Path                string `json:"path"`
	Title               string `json:"title"`
	Version             int64  `json:"version"`
	Author              string `json:"author"`
	ValidatedVersionTag string `json:"validated_version_tag,omitempty"`
	ValidatedPageVer    int64  `json:"validated_page_version,omitempty"`
}

func (s *Server) handleTagQuery(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	if tag == "" {
		http.Error(w, "tag parameter required", http.StatusBadRequest)
		return
	}
	pathPrefix := r.URL.Query().Get("path")

	var excludeTags []string
	if exc := r.URL.Query().Get("exclude"); exc != "" {
		for _, t := range strings.Split(exc, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				excludeTags = append(excludeTags, t)
			}
		}
	}

	if s.tagIndex == nil {
		http.Error(w, "tag queries not supported", http.StatusNotImplemented)
		return
	}

	entries := s.tagIndex.GetPagesForTag(tag, pathPrefix, excludeTags)

	userDisplay := s.configStore.Get().Site.UserDisplay

	pages := make([]tagQueryPage, 0, len(entries))
	for _, e := range entries {
		var version int64
		var author string
		page, err := s.store.Get(e.Path)
		if err == nil {
			version = page.Meta.Version
			author = s.resolveAuthorDisplay(page.Meta.Author, userDisplay)
		}
		qp := tagQueryPage{
			Path:    e.Path,
			Title:   e.Title,
			Version: version,
			Author:  author,
		}
		// If reviewflow is active, look up the latest validated version tag.
		if s.reviewflowService != nil {
			if st, err := s.reviewflowService.GetStatus(e.Path); err == nil && st != nil {
				if len(st.VersionHistory) > 0 {
					latest := st.VersionHistory[len(st.VersionHistory)-1]
					qp.ValidatedVersionTag = latest.VersionTag
					qp.ValidatedPageVer = latest.PageVersion
				}
			}
		}
		pages = append(pages, qp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tagQueryResult{
		Tag:   tag,
		Pages: pages,
	})
}

// resolveAuthorDisplay converts a login username to the configured display format.
func (s *Server) resolveAuthorDisplay(login, displayMode string) string {
	if login == "" || s.userStore == nil {
		return login
	}
	user, err := s.userStore.Get(login)
	if err != nil {
		return login
	}
	switch displayMode {
	case "fullname":
		if user.DisplayName != "" {
			return user.DisplayName
		}
	case "email":
		if user.Email != "" {
			return user.Email
		}
	}
	return login
}
