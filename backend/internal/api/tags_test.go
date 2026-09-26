package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gowiki/backend/internal/auth"
	"gowiki/backend/internal/config"
	"gowiki/backend/internal/storage"
)

// The handler is small and DB-free — it composes tagIndex + PageStore +
// UserStore + configStore. Tests build a minimal Server carrying just
// those fields with in-memory doubles for the PageStore and real
// TagIndex / UserStore / Store instances (they're all TempDir-backed).

// tagsTestServer holds the pieces a test needs to interact with beyond
// the *Server itself — the index (to seed tags) and the PageStore (to
// register pages so the handler's PageStore.Get finds them).
type tagsTestServer struct {
	server    *Server
	tagIndex  *storage.TagIndex
	pageStore *memPageStoreForTags
	userStore *auth.UserStore
	config    *config.Store
}

func newTagsTestServer(t *testing.T) *tagsTestServer {
	t.Helper()
	meta := t.TempDir()

	tagIndex := storage.NewTagIndex(meta)
	pageStore := newMemPageStoreForTags()
	userStore, err := auth.NewUserStore(meta)
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	configStore, err := config.Load(t.TempDir() + "/config.yaml")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	return &tagsTestServer{
		server: &Server{
			store:       pageStore,
			tagIndex:    tagIndex,
			userStore:   userStore,
			configStore: configStore,
		},
		tagIndex:  tagIndex,
		pageStore: pageStore,
		userStore: userStore,
		config:    configStore,
	}
}

// setUserDisplay swaps the site's user-display mode via a full Config
// round-trip through configStore.Update (mirrors how the admin panel
// changes it in production).
func (ts *tagsTestServer) setUserDisplay(t *testing.T, mode string) {
	t.Helper()
	cfg := ts.config.Get()
	cfg.Site.UserDisplay = mode
	if err := ts.config.Update(cfg); err != nil {
		t.Fatalf("Update config: %v", err)
	}
}

func (ts *tagsTestServer) get(t *testing.T, query string) (int, tagQueryResult) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/tags?"+query, nil)
	rec := httptest.NewRecorder()
	ts.server.handleTagQuery(rec, req)
	if rec.Code != http.StatusOK {
		return rec.Code, tagQueryResult{}
	}
	var body tagQueryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

// ─── happy paths ─────────────────────────────────────────

func TestHandleTagQuery_MissingTagParam_Returns400(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	code, _ := ts.get(t, "")
	if code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}

func TestHandleTagQuery_NilTagIndex_Returns501(t *testing.T) {
	t.Parallel()
	// Build a bare Server without wiring tagIndex. handler checks nil
	// before touching it.
	configStore, err := config.Load(t.TempDir() + "/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{configStore: configStore}
	req := httptest.NewRequest(http.MethodGet, "/api/tags?tag=x", nil)
	rec := httptest.NewRecorder()
	s.handleTagQuery(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("code = %d, want 501", rec.Code)
	}
}

func TestHandleTagQuery_HappyPath_SortedByPath(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	ts.tagIndex.UpdatePage("/z/last", []string{"docs"}, "Z Last")
	ts.tagIndex.UpdatePage("/a/first", []string{"docs"}, "A First")
	ts.tagIndex.UpdatePage("/m/middle", []string{"docs"}, "M Middle")

	code, body := ts.get(t, "tag=docs")
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if body.Tag != "docs" {
		t.Errorf("Tag = %q, want docs", body.Tag)
	}
	gotPaths := make([]string, len(body.Pages))
	for i, p := range body.Pages {
		gotPaths[i] = p.Path
	}
	wantPaths := []string{"/a/first", "/m/middle", "/z/last"}
	for i, want := range wantPaths {
		if i >= len(gotPaths) || gotPaths[i] != want {
			t.Errorf("Pages = %v, want sorted %v", gotPaths, wantPaths)
			break
		}
	}
}

func TestHandleTagQuery_UnknownTag_ReturnsEmptyArray(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	// No pages have the tag "does-not-exist".
	code, body := ts.get(t, "tag=does-not-exist")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if body.Tag != "does-not-exist" {
		t.Errorf("Tag = %q", body.Tag)
	}
	if body.Pages == nil {
		t.Errorf("Pages is nil — must be [] to serialize as JSON array")
	}
	if len(body.Pages) != 0 {
		t.Errorf("Pages = %v, want []", body.Pages)
	}
}

// ─── filters ─────────────────────────────────────────────

func TestHandleTagQuery_PathPrefixFilter(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	ts.tagIndex.UpdatePage("/docs/one", []string{"topic"}, "One")
	ts.tagIndex.UpdatePage("/docs/two", []string{"topic"}, "Two")
	ts.tagIndex.UpdatePage("/blog/one", []string{"topic"}, "Blog")

	code, body := ts.get(t, "tag=topic&path=/docs")
	if code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Pages) != 2 {
		t.Fatalf("got %d pages, want 2", len(body.Pages))
	}
	for _, p := range body.Pages {
		if p.Path[:len("/docs")] != "/docs" {
			t.Errorf("path %q leaked past /docs prefix", p.Path)
		}
	}
}

func TestHandleTagQuery_ExcludeFilter_SingleAndMulti(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	ts.tagIndex.UpdatePage("/a", []string{"topic", "draft"}, "A")
	ts.tagIndex.UpdatePage("/b", []string{"topic"}, "B")
	ts.tagIndex.UpdatePage("/c", []string{"topic", "archived"}, "C")

	_, body := ts.get(t, "tag=topic&exclude=draft")
	if len(body.Pages) != 2 || body.Pages[0].Path != "/b" || body.Pages[1].Path != "/c" {
		t.Errorf("exclude=draft: got %v, want [/b /c]", pathsOf(body.Pages))
	}

	// Multiple excludes, comma-separated.
	_, body = ts.get(t, "tag=topic&exclude=draft,archived")
	if len(body.Pages) != 1 || body.Pages[0].Path != "/b" {
		t.Errorf("exclude=draft,archived: got %v, want [/b]", pathsOf(body.Pages))
	}

	// Whitespace around commas is tolerated.
	_, body = ts.get(t, "tag=topic&exclude=draft%20,%20archived")
	if len(body.Pages) != 1 || body.Pages[0].Path != "/b" {
		t.Errorf("exclude with spaces: got %v, want [/b]", pathsOf(body.Pages))
	}
}

// ─── page metadata + author display ─────────────────────

func TestHandleTagQuery_PageMetadataFromStore(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	ts.tagIndex.UpdatePage("/p", []string{"t"}, "P")
	ts.pageStore.setPage("/p", storage.Page{
		Markdown: "body",
		Meta:     storage.PageMetadata{Version: 42, Author: "alice"},
	})

	_, body := ts.get(t, "tag=t")
	if len(body.Pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(body.Pages))
	}
	if body.Pages[0].Version != 42 {
		t.Errorf("Version = %d, want 42", body.Pages[0].Version)
	}
	if body.Pages[0].Author != "alice" {
		// Default user_display is "" — falls through to login (alice).
		t.Errorf("Author = %q, want alice", body.Pages[0].Author)
	}
}

func TestHandleTagQuery_PageMissingFromStore_ZeroMetadata(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	// Tag index knows about /p, but PageStore does not. The handler
	// silently swallows PageStore.Get errors and returns zero version
	// + empty author for that entry — the row still appears.
	ts.tagIndex.UpdatePage("/p", []string{"t"}, "P")

	_, body := ts.get(t, "tag=t")
	if len(body.Pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(body.Pages))
	}
	if body.Pages[0].Version != 0 || body.Pages[0].Author != "" {
		t.Errorf("expected zero metadata for missing page, got version=%d author=%q",
			body.Pages[0].Version, body.Pages[0].Author)
	}
}

func TestResolveAuthorDisplay_Modes(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	if err := ts.userStore.Create(auth.User{
		Username:    "alice",
		DisplayName: "Alice Alpha",
		Email:       "alice@example.com",
	}, "irrelevant"); err != nil {
		t.Fatalf("Create alice: %v", err)
	}

	cases := []struct {
		mode string
		want string
	}{
		{"", "alice"}, // default / login
		{"login", "alice"},
		{"fullname", "Alice Alpha"},
		{"email", "alice@example.com"},
	}
	for _, tc := range cases {
		got := ts.server.resolveAuthorDisplay("alice", tc.mode)
		if got != tc.want {
			t.Errorf("mode %q: got %q, want %q", tc.mode, got, tc.want)
		}
	}

	// Empty login → empty output, regardless of mode.
	if got := ts.server.resolveAuthorDisplay("", "fullname"); got != "" {
		t.Errorf("empty login: got %q, want empty", got)
	}
	// Unknown login → returns the login unchanged (safe fallback).
	if got := ts.server.resolveAuthorDisplay("stranger", "fullname"); got != "stranger" {
		t.Errorf("unknown login: got %q, want stranger", got)
	}
}

func TestHandleTagQuery_AuthorDisplay_FullNameViaConfig(t *testing.T) {
	t.Parallel()
	ts := newTagsTestServer(t)
	if err := ts.userStore.Create(auth.User{
		Username:    "bob",
		DisplayName: "Bob Beta",
		Email:       "bob@example.com",
	}, "x"); err != nil {
		t.Fatalf("Create bob: %v", err)
	}
	ts.tagIndex.UpdatePage("/p", []string{"t"}, "P")
	ts.pageStore.setPage("/p", storage.Page{
		Meta: storage.PageMetadata{Version: 1, Author: "bob"},
	})
	ts.setUserDisplay(t, "fullname")

	_, body := ts.get(t, "tag=t")
	if len(body.Pages) != 1 {
		t.Fatalf("got %d pages", len(body.Pages))
	}
	if body.Pages[0].Author != "Bob Beta" {
		t.Errorf("Author = %q, want Bob Beta (user_display=fullname)", body.Pages[0].Author)
	}
}

// ─── in-file test doubles ────────────────────────────────

// memPageStoreForTags is a bare-bones PageStore backed by a map. Only
// implements what handleTagQuery reads (`Get`) — the other interface
// methods return zero values so the type still satisfies PageStore.
// (The database test file's memPageStore lives behind an integration
// build tag, so we can't share it from a unit-tagged file.)
type memPageStoreForTags struct {
	pages map[string]storage.Page
}

func newMemPageStoreForTags() *memPageStoreForTags {
	return &memPageStoreForTags{pages: map[string]storage.Page{}}
}

func (m *memPageStoreForTags) setPage(path string, p storage.Page) {
	m.pages[path] = p
}

func (m *memPageStoreForTags) Get(path string) (storage.Page, error) {
	if p, ok := m.pages[path]; ok {
		return p, nil
	}
	return storage.Page{}, storage.ErrPageNotFound
}
func (m *memPageStoreForTags) Put(path, md, author string) (storage.PutResult, error) {
	return storage.PutResult{}, nil
}
func (m *memPageStoreForTags) PutWithSummary(path, md, author, summary string) (storage.PutResult, error) {
	return storage.PutResult{}, nil
}
func (m *memPageStoreForTags) Delete(path, author string) (storage.DeleteResult, error) {
	return storage.DeleteResult{}, nil
}
func (m *memPageStoreForTags) CheckNamespaceConflict(path string) error { return nil }
func (m *memPageStoreForTags) Exists(path string) bool {
	_, ok := m.pages[path]
	return ok
}

func pathsOf(pages []tagQueryPage) []string {
	out := make([]string, 0, len(pages))
	for _, p := range pages {
		out = append(out, p.Path)
	}
	return out
}
