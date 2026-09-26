package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gowiki/backend/internal/storage"
)

// existsStore is a minimal PageStore that only implements Exists (the
// only method handleCheckPages calls). Keeps the test isolated from disk
// and from every other store dependency.
type existsStore struct {
	pages map[string]bool
}

func (s *existsStore) Get(pagePath string) (storage.Page, error) { return storage.Page{}, nil }
func (s *existsStore) Put(pagePath, markdown, author string) (storage.PutResult, error) {
	return storage.PutResult{}, nil
}
func (s *existsStore) PutWithSummary(pagePath, markdown, author, summary string) (storage.PutResult, error) {
	return storage.PutResult{}, nil
}
func (s *existsStore) Delete(pagePath, author string) (storage.DeleteResult, error) {
	return storage.DeleteResult{}, nil
}
func (s *existsStore) CheckNamespaceConflict(pagePath string) error { return nil }
func (s *existsStore) Exists(pagePath string) bool                  { return s.pages[pagePath] }

func callCheckPages(t *testing.T, body any, existing map[string]bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := &Server{store: &existsStore{pages: existing}}
	req := httptest.NewRequest(http.MethodPost, "/api/pages/check", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	s.handleCheckPages(rec, req)
	return rec
}

// TestCheckPages_EmptyList — well-formed but zero paths → empty map, 200.
func TestCheckPages_EmptyList(t *testing.T) {
	t.Parallel()
	rec := callCheckPages(t, map[string][]string{"paths": {}}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Exists map[string]bool `json:"exists"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Exists) != 0 {
		t.Errorf("exists map = %v, want empty", body.Exists)
	}
}

// TestCheckPages_MixedExistence — the endpoint returns a per-path
// boolean; caller uses it to grey out missing links in the sidebar.
func TestCheckPages_MixedExistence(t *testing.T) {
	t.Parallel()
	rec := callCheckPages(t, map[string][]string{"paths": {"/a", "/b", "/missing"}}, map[string]bool{"/a": true, "/b": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Exists map[string]bool `json:"exists"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Exists["/a"] || !body.Exists["/b"] {
		t.Errorf("/a, /b should be true; got %v", body.Exists)
	}
	if body.Exists["/missing"] {
		t.Errorf("/missing should be false; got %v", body.Exists["/missing"])
	}
}

// TestCheckPages_InvalidJSON_400.
func TestCheckPages_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	s := &Server{store: &existsStore{}}
	req := httptest.NewRequest(http.MethodPost, "/api/pages/check", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	s.handleCheckPages(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestCheckPages_CapAt500 — the endpoint truncates at 500 to prevent
// abuse. Pass 600 paths; assert response only contains 500 of them.
func TestCheckPages_CapAt500(t *testing.T) {
	t.Parallel()
	paths := make([]string, 600)
	existing := map[string]bool{}
	for i := range paths {
		p := "/p" + itoa4(i)
		paths[i] = p
		existing[p] = true
	}
	rec := callCheckPages(t, map[string][]string{"paths": paths}, existing)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Exists map[string]bool `json:"exists"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Exists) != 500 {
		t.Errorf("exists map size = %d, want 500 (cap)", len(body.Exists))
	}
}

func itoa4(i int) string {
	buf := []byte{'0', '0', '0', '0'}
	for j := 3; j >= 0; j-- {
		buf[j] = '0' + byte(i%10)
		i /= 10
	}
	return string(buf)
}
