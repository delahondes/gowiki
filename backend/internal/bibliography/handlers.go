package bibliography

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// RegisterRoutes mounts the bibliography endpoints on the given router.
// The router is expected to already be behind whatever authentication
// middleware the caller wants (none → public read; requireAuth → logged in).
func RegisterRoutes(r chi.Router, svc *Service) {
	r.Get("/resolve", svc.handleResolve)
	r.Get("/list", svc.handleList)
}

var _ = chi.URLParam // keep chi referenced for potential future param-based routes

// handleResolve returns the metadata for one identifier, fetching it from
// the source if it's not in the cache.
//
//	GET /resolve?pmid=<id>
//	GET /resolve?doi=<id>
func (s *Service) handleResolve(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "bibliography plugin disabled")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	pmid := r.URL.Query().Get("pmid")
	doi := r.URL.Query().Get("doi")
	switch {
	case pmid != "" && doi != "":
		writeError(w, http.StatusBadRequest, "pmid and doi are mutually exclusive")
		return
	case pmid == "" && doi == "":
		writeError(w, http.StatusBadRequest, "missing pmid or doi")
		return
	}

	var (
		entry *Entry
		err   error
	)
	if pmid != "" {
		entry, err = s.ResolvePMID(ctx, pmid)
	} else {
		entry, err = s.ResolveDOI(ctx, doi)
	}

	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, entry)
	case errors.Is(err, ErrInvalidIdentifier):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "identifier not found")
	case errors.Is(err, ErrSourceUnreachable):
		writeError(w, http.StatusServiceUnavailable, "source unreachable")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// handleList returns every cached entry. Intended for admin tooling.
//
//	GET /list
func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "bibliography plugin disabled")
		return
	}
	entries, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entries == nil {
		entries = []Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
