//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/database"
	"gowiki/backend/internal/storage"
)

// ─── Minimal test doubles ────────────────────────────────
//
// The database handlers need a PageStore (for page-bound row auto-sync)
// and a DraftManager (for the conflict guard). Wiring the full production
// Server would drag in config, auth, sessions, etc. — none of which the
// handlers under test touch. These in-file stubs give the handlers exactly
// what they read, nothing more.

// memPageStore stores markdown in a map. Records what pages were
// Put/PutWithSummary so tests can assert on side effects.
type memPageStore struct {
	mu    sync.Mutex
	pages map[string]string
	puts  []memPut
}

type memPut struct {
	Path, Markdown, Author, Summary string
}

func newMemPageStore() *memPageStore {
	return &memPageStore{pages: map[string]string{}}
}

func (m *memPageStore) Get(path string) (storage.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	md, ok := m.pages[path]
	if !ok {
		return storage.Page{}, storage.ErrPageNotFound
	}
	return storage.Page{Markdown: md, Meta: storage.PageMetadata{Version: 1}}, nil
}
func (m *memPageStore) Put(path, md, author string) (storage.PutResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pages[path] = md
	m.puts = append(m.puts, memPut{path, md, author, ""})
	return storage.PutResult{Page: storage.Page{Path: path, Markdown: md, Meta: storage.PageMetadata{Version: 1}}}, nil
}
func (m *memPageStore) PutWithSummary(path, md, author, summary string) (storage.PutResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pages[path] = md
	m.puts = append(m.puts, memPut{path, md, author, summary})
	return storage.PutResult{Page: storage.Page{Path: path, Markdown: md, Meta: storage.PageMetadata{Version: 1}}}, nil
}
func (m *memPageStore) Delete(path, author string) (storage.DeleteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pages, path)
	return storage.DeleteResult{}, nil
}
func (m *memPageStore) CheckNamespaceConflict(path string) error { return nil }
func (m *memPageStore) Exists(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.pages[path]
	return ok
}

// stubDraftManager returns no lock by default. Tests that want to
// exercise the conflict path set locks explicitly.
type stubDraftManager struct {
	mu    sync.Mutex
	locks map[string]storage.DraftLock
}

func newStubDraftManager() *stubDraftManager {
	return &stubDraftManager{locks: map[string]storage.DraftLock{}}
}
func (s *stubDraftManager) setLock(path string, lock storage.DraftLock) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locks[path] = lock
}
func (s *stubDraftManager) EnterEditMode(pagePath, username string, force bool, currentPublished string) (string, string, error) {
	return "", "", nil
}
func (s *stubDraftManager) SaveDraft(pagePath, username, editToken, markdown string) error {
	return nil
}
func (s *stubDraftManager) ReadDraft(pagePath, username string) (string, error) { return "", nil }
func (s *stubDraftManager) DiscardDraft(pagePath, username, editToken string) error {
	return nil
}
func (s *stubDraftManager) Publish(pagePath, username, editToken string) (string, error) {
	return "", nil
}
func (s *stubDraftManager) GetLock(pagePath string) storage.DraftLock {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locks[pagePath]
}
func (s *stubDraftManager) FindAnyDraft(pagePath string) (storage.DraftInfo, bool) {
	return storage.DraftInfo{}, false
}
func (s *stubDraftManager) ListLocks() []storage.LockInfo         { return nil }
func (s *stubDraftManager) ListDrafts() []storage.DraftInfo       { return nil }
func (s *stubDraftManager) AdminDiscardDraft(pagePath, draftOwner string) error {
	return nil
}
func (s *stubDraftManager) AdminReadDraft(pagePath, owner string) (string, error) {
	return "", nil
}
func (s *stubDraftManager) AdminReclaimDraft(pagePath, fromUser, toUser string) error {
	return nil
}
func (s *stubDraftManager) TakeoverDraft(pagePath, newOwner, currentPublished string) (string, string, error) {
	return "", "", nil
}

// ─── Test fixtures ───────────────────────────────────────

// newTestDBPool wraps the database-package test helper so this file can
// grab a per-test Postgres DB without duplicating the schema-isolation
// logic. Skips the test when no DB is reachable.
func newTestDBPool(t *testing.T) *database.Pool {
	t.Helper()
	return dbTestPool(t)
}

// newDBHandlerServer builds a minimal Server that carries just what the
// handleDatabase* handlers read: the two DB stores, the in-memory
// PageStore, and a stub DraftManager. Uses a per-test isolated Postgres
// DB — every test starts empty.
func newDBHandlerServer(t *testing.T) (*Server, *memPageStore, *stubDraftManager, *database.SchemaStore) {
	t.Helper()
	pool := newTestDBPool(t)
	schema := database.NewSchemaStore(pool)
	data := database.NewDataStore(pool, schema)
	pageStore := newMemPageStore()
	dm := newStubDraftManager()
	s := &Server{
		store:        pageStore,
		draftManager: dm,
		schemaStore:  schema,
		dataStore:    data,
	}
	return s, pageStore, dm, schema
}

// makeAPITestTable creates a "widgets" table with a couple of fields.
func makeAPITestTable(t *testing.T, schema *database.SchemaStore, name string, pageFolder string) int {
	t.Helper()
	ctx := context.Background()
	td := &database.TableDef{Name: name, Label: strings.Title(name), PageFolder: pageFolder}
	if err := schema.CreateTable(ctx, td, "tester"); err != nil {
		t.Fatalf("CreateTable(%q): %v", name, err)
	}
	// Two fields for realistic requests.
	for _, f := range []struct{ name, ftype string }{
		{"title", database.FieldTypeText},
		{"priority", database.FieldTypeInteger},
	} {
		fd := &database.FieldDef{TableID: td.ID, Name: f.name, Type: f.ftype}
		if err := schema.CreateField(ctx, fd, "tester"); err != nil {
			t.Fatalf("CreateField(%q): %v", f.name, err)
		}
	}
	return td.ID
}

// newAPIRequest builds a request with chi URL params pre-populated.
func newAPIRequest(method, url string, body []byte, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

// decodeBody unmarshals a JSON response body into v.
func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode body: %v — %s", err, rec.Body.String())
	}
}

// ─── handleDatabaseSchema ────────────────────────────────

func TestHTTP_DatabaseSchema_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")

	req := newAPIRequest(http.MethodGet, "/api/database/widgets/schema", nil, map[string]string{"table": "widgets"})
	rec := httptest.NewRecorder()
	s.handleDatabaseSchema(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var td database.TableDef
	decodeBody(t, rec, &td)
	if td.Name != "widgets" || len(td.Fields) != 2 {
		t.Errorf("unexpected schema: %+v", td)
	}
}

func TestHTTP_DatabaseSchema_UnknownTable_404(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	req := newAPIRequest(http.MethodGet, "/api/database/nope/schema", nil, map[string]string{"table": "nope"})
	rec := httptest.NewRecorder()
	s.handleDatabaseSchema(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ─── handleDatabaseInsertRow ────────────────────────────

func TestHTTP_DatabaseInsertRow_NoPageBinding(t *testing.T) {
	t.Parallel()
	s, ps, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "") // no page_folder

	body := []byte(`{"page_path":"","fields":{"title":"first","priority":3}}`)
	req := newAPIRequest(http.MethodPost, "/api/database/widgets/rows", body, map[string]string{"table": "widgets"})
	rec := httptest.NewRecorder()
	s.handleDatabaseInsertRow(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var row database.Row
	decodeBody(t, rec, &row)
	if row.ID == 0 || row.Fields["title"] != "first" {
		t.Errorf("unexpected row: %+v", row)
	}
	if len(ps.puts) != 0 {
		t.Errorf("no page_folder → no page create, got puts=%d", len(ps.puts))
	}
}

func TestHTTP_DatabaseInsertRow_PageBound_CreatesPage(t *testing.T) {
	t.Parallel()
	s, ps, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "/widgets")

	body := []byte(`{"fields":{"title":"one","priority":1}}`)
	req := newAPIRequest(http.MethodPost, "/api/database/widgets/rows", body, map[string]string{"table": "widgets"})
	rec := httptest.NewRecorder()
	s.handleDatabaseInsertRow(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(ps.puts) != 1 {
		t.Fatalf("expected 1 page created, got %d", len(ps.puts))
	}
	put := ps.puts[0]
	if !strings.HasPrefix(put.Path, "/widgets/") {
		t.Errorf("page path = %q, want prefix /widgets/", put.Path)
	}
	if !strings.Contains(put.Markdown, "{database-row table=widgets}") {
		t.Errorf("page markdown missing database-row block: %s", put.Markdown)
	}
}

func TestHTTP_DatabaseInsertRow_BadJSON_400(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	req := newAPIRequest(http.MethodPost, "/api/database/widgets/rows", []byte("not json"), map[string]string{"table": "widgets"})
	rec := httptest.NewRecorder()
	s.handleDatabaseInsertRow(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHTTP_DatabaseInsertRow_UnknownTable_400(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	req := newAPIRequest(http.MethodPost, "/api/database/nope/rows", []byte(`{"fields":{}}`), map[string]string{"table": "nope"})
	rec := httptest.NewRecorder()
	s.handleDatabaseInsertRow(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── handleDatabaseGetRow ────────────────────────────────

func TestHTTP_DatabaseGetRow_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")

	// Seed via the datastore directly.
	row := &database.Row{PagePath: "", Fields: map[string]any{"title": "read me"}}
	if err := s.dataStore.InsertRow(context.Background(), "widgets", row); err != nil {
		t.Fatalf("InsertRow: %v", err)
	}

	req := newAPIRequest(http.MethodGet, "/api/database/widgets/rows/1", nil, map[string]string{"table": "widgets", "id": intToStr(row.ID)})
	rec := httptest.NewRecorder()
	s.handleDatabaseGetRow(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got database.Row
	decodeBody(t, rec, &got)
	if got.Fields["title"] != "read me" {
		t.Errorf("title = %v", got.Fields["title"])
	}
}

func TestHTTP_DatabaseGetRow_BadID_400(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	req := newAPIRequest(http.MethodGet, "/api/database/widgets/rows/xxx", nil, map[string]string{"table": "widgets", "id": "xxx"})
	rec := httptest.NewRecorder()
	s.handleDatabaseGetRow(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHTTP_DatabaseGetRow_Missing_404(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	req := newAPIRequest(http.MethodGet, "/api/database/widgets/rows/9999", nil, map[string]string{"table": "widgets", "id": "9999"})
	rec := httptest.NewRecorder()
	s.handleDatabaseGetRow(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ─── handleDatabaseUpdateRow ────────────────────────────

func TestHTTP_DatabaseUpdateRow_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	row := &database.Row{Fields: map[string]any{"title": "old"}}
	_ = s.dataStore.InsertRow(context.Background(), "widgets", row)

	body := []byte(`{"fields":{"title":"new"}}`)
	req := newAPIRequest(http.MethodPut, "/api/database/widgets/rows/1", body, map[string]string{"table": "widgets", "id": intToStr(row.ID)})
	rec := httptest.NewRecorder()
	s.handleDatabaseUpdateRow(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dataStore.GetRow(context.Background(), "widgets", row.ID)
	if got.Fields["title"] != "new" {
		t.Errorf("title not updated: %v", got.Fields["title"])
	}
}

func TestHTTP_DatabaseUpdateRow_DraftConflict_409(t *testing.T) {
	t.Parallel()
	s, _, dm, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "/widgets")
	row := &database.Row{PagePath: "/widgets/held", Fields: map[string]any{"title": "old"}}
	_ = s.dataStore.InsertRow(context.Background(), "widgets", row)

	dm.setLock("/widgets/held", storage.DraftLock{Owner: "otheruser"})

	body := []byte(`{"fields":{"title":"new"}}`)
	req := newAPIRequest(http.MethodPut, "/api/database/widgets/rows/1", body, map[string]string{"table": "widgets", "id": intToStr(row.ID)})
	rec := httptest.NewRecorder()
	s.handleDatabaseUpdateRow(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (draft holds it)", rec.Code)
	}
	var body2 map[string]any
	decodeBody(t, rec, &body2)
	if body2["error"] != "page_draft_conflict" || body2["draft_owner"] != "otheruser" {
		t.Errorf("unexpected 409 body: %+v", body2)
	}
}

func TestHTTP_DatabaseUpdateRow_ForceOverridesDraft(t *testing.T) {
	t.Parallel()
	s, ps, dm, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "/widgets")
	row := &database.Row{PagePath: "/widgets/forced", Fields: map[string]any{"title": "orig"}}
	_ = s.dataStore.InsertRow(context.Background(), "widgets", row)
	// Seed the page in memory so syncRowToPage's Get succeeds.
	_, _ = ps.Put("/widgets/forced", "# forced\n\n{database-row table=widgets}\n\n| Field | Value |\n| --- | --- |\n| title | orig |\n", "seeder")

	dm.setLock("/widgets/forced", storage.DraftLock{Owner: "someone"})

	body := []byte(`{"fields":{"title":"forced"}}`)
	req := newAPIRequest(http.MethodPut, "/api/database/widgets/rows/1?force=true", body, map[string]string{"table": "widgets", "id": intToStr(row.ID)})
	rec := httptest.NewRecorder()
	s.handleDatabaseUpdateRow(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (force overrides), body = %s", rec.Code, rec.Body.String())
	}
	got, _ := s.dataStore.GetRow(context.Background(), "widgets", row.ID)
	if got.Fields["title"] != "forced" {
		t.Errorf("title not updated: %v", got.Fields["title"])
	}
	// force+existing-lock → conflict recorded so publish can warn.
	_, recorded := s.inlineEditConflicts.Load("/widgets/forced")
	if !recorded {
		t.Errorf("expected inlineEditConflicts to record /widgets/forced")
	}
}

func TestHTTP_DatabaseUpdateRow_MissingRow_404(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")

	body := []byte(`{"fields":{"title":"x"}}`)
	req := newAPIRequest(http.MethodPut, "/api/database/widgets/rows/9999", body, map[string]string{"table": "widgets", "id": "9999"})
	rec := httptest.NewRecorder()
	s.handleDatabaseUpdateRow(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ─── handleDatabaseDeleteRow ────────────────────────────

func TestHTTP_DatabaseDeleteRow_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	row := &database.Row{Fields: map[string]any{"title": "delme"}}
	_ = s.dataStore.InsertRow(context.Background(), "widgets", row)

	req := newAPIRequest(http.MethodDelete, "/api/database/widgets/rows/1", nil, map[string]string{"table": "widgets", "id": intToStr(row.ID)})
	rec := httptest.NewRecorder()
	s.handleDatabaseDeleteRow(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := s.dataStore.GetRow(context.Background(), "widgets", row.ID); err == nil {
		t.Errorf("row still exists after delete")
	}
}

func TestHTTP_DatabaseDeleteRow_BadID_400(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	req := newAPIRequest(http.MethodDelete, "/api/database/widgets/rows/nope", nil, map[string]string{"table": "widgets", "id": "nope"})
	rec := httptest.NewRecorder()
	s.handleDatabaseDeleteRow(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// ─── handleDatabaseGetRowByPage ─────────────────────────

func TestHTTP_DatabaseGetRowByPage_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	row := &database.Row{PagePath: "/widgets/1", Fields: map[string]any{"title": "hi"}}
	_ = s.dataStore.InsertRow(context.Background(), "widgets", row)

	req := newAPIRequest(http.MethodGet, "/api/database/widgets/page/widgets/1", nil, map[string]string{"table": "widgets", "*": "widgets/1"})
	rec := httptest.NewRecorder()
	s.handleDatabaseGetRowByPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got database.Row
	decodeBody(t, rec, &got)
	if got.PagePath != "/widgets/1" {
		t.Errorf("PagePath = %q", got.PagePath)
	}
}

func TestHTTP_DatabaseGetRowByPage_MissingPath_400(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	req := newAPIRequest(http.MethodGet, "/api/database/widgets/page/", nil, map[string]string{"table": "widgets", "*": ""})
	rec := httptest.NewRecorder()
	s.handleDatabaseGetRowByPage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHTTP_DatabaseGetRowByPage_UnknownPage_404(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	req := newAPIRequest(http.MethodGet, "/api/database/widgets/page/not/there", nil, map[string]string{"table": "widgets", "*": "not/there"})
	rec := httptest.NewRecorder()
	s.handleDatabaseGetRowByPage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ─── handleDatabaseUpsertRowByPage ──────────────────────

func TestHTTP_DatabaseUpsertRowByPage_CreatesThenUpdates(t *testing.T) {
	t.Parallel()
	s, ps, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	// Seed a page so the syncRowToPage step has content to modify.
	_, _ = ps.Put("/widgets/u", "# u\n\n{database-row table=widgets}\n\n| Field | Value |\n| --- | --- |\n| title |  |\n", "seeder")

	// First call — creates row.
	body := []byte(`{"fields":{"title":"created"}}`)
	req := newAPIRequest(http.MethodPut, "/api/database/widgets/page/widgets/u", body, map[string]string{"table": "widgets", "*": "widgets/u"})
	rec := httptest.NewRecorder()
	s.handleDatabaseUpsertRowByPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status (insert) = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Second call — updates row.
	body2 := []byte(`{"fields":{"title":"updated"}}`)
	req2 := newAPIRequest(http.MethodPut, "/api/database/widgets/page/widgets/u", body2, map[string]string{"table": "widgets", "*": "widgets/u"})
	rec2 := httptest.NewRecorder()
	s.handleDatabaseUpsertRowByPage(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status (update) = %d", rec2.Code)
	}

	got, _ := s.dataStore.GetRowByPagePath(context.Background(), "widgets", "/widgets/u")
	if got.Fields["title"] != "updated" {
		t.Errorf("title = %v, want updated", got.Fields["title"])
	}
}

func TestHTTP_DatabaseUpsertRowByPage_DraftConflict_409(t *testing.T) {
	t.Parallel()
	s, _, dm, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	dm.setLock("/held", storage.DraftLock{Owner: "u"})
	body := []byte(`{"fields":{"title":"x"}}`)
	req := newAPIRequest(http.MethodPut, "/api/database/widgets/page/held", body, map[string]string{"table": "widgets", "*": "held"})
	rec := httptest.NewRecorder()
	s.handleDatabaseUpsertRowByPage(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

// ─── handleDatabaseExportCSV ────────────────────────────

func TestHTTP_DatabaseExportCSV_ShapeAndContent(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	_ = s.dataStore.InsertRow(context.Background(), "widgets", &database.Row{PagePath: "/p/1", Fields: map[string]any{"title": "row1", "priority": 1}})
	_ = s.dataStore.InsertRow(context.Background(), "widgets", &database.Row{PagePath: "/p/2", Fields: map[string]any{"title": "row2", "priority": 2}})

	req := newAPIRequest(http.MethodGet, "/api/database/widgets/export/csv", nil, map[string]string{"table": "widgets"})
	rec := httptest.NewRecorder()
	s.handleDatabaseExportCSV(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got %d lines: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "id,page_path,title,priority") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(rec.Body.String(), "row1") || !strings.Contains(rec.Body.String(), "row2") {
		t.Errorf("row data missing")
	}
}

func TestHTTP_DatabaseExportCSV_UnknownTable_404(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	req := newAPIRequest(http.MethodGet, "/api/database/nope/export/csv", nil, map[string]string{"table": "nope"})
	rec := httptest.NewRecorder()
	s.handleDatabaseExportCSV(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// ─── handleDatabaseQueryRows ────────────────────────────

func TestHTTP_DatabaseQueryRows_FiltersAndTotal(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	makeAPITestTable(t, schema, "widgets", "")
	_ = s.dataStore.InsertRow(context.Background(), "widgets", &database.Row{Fields: map[string]any{"title": "a", "priority": 1}})
	_ = s.dataStore.InsertRow(context.Background(), "widgets", &database.Row{Fields: map[string]any{"title": "b", "priority": 5}})
	_ = s.dataStore.InsertRow(context.Background(), "widgets", &database.Row{Fields: map[string]any{"title": "c", "priority": 10}})

	req := newAPIRequest(http.MethodGet, "/api/database/widgets/rows?filter=priority>3", nil, map[string]string{"table": "widgets"})
	rec := httptest.NewRecorder()
	s.handleDatabaseQueryRows(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	decodeBody(t, rec, &got)
	if int(got["total"].(float64)) != 2 {
		t.Errorf("total = %v, want 2", got["total"])
	}
}

func TestHTTP_DatabaseServiceUnavailable_WhenStoresNil(t *testing.T) {
	t.Parallel()
	// Server without any stores wired — every handler returns 503.
	s := &Server{}
	handlers := []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
	}{
		{"schema", s.handleDatabaseSchema},
		{"query", s.handleDatabaseQueryRows},
		{"insert", s.handleDatabaseInsertRow},
		{"get_row", s.handleDatabaseGetRow},
		{"update_row", s.handleDatabaseUpdateRow},
		{"delete_row", s.handleDatabaseDeleteRow},
		{"get_by_page", s.handleDatabaseGetRowByPage},
		{"upsert_by_page", s.handleDatabaseUpsertRowByPage},
		{"export_csv", s.handleDatabaseExportCSV},
	}
	for _, h := range handlers {
		req := newAPIRequest(http.MethodGet, "/", nil, nil)
		rec := httptest.NewRecorder()
		h.fn(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s handler: status = %d, want 503 (stores nil)", h.name, rec.Code)
		}
	}
}

func intToStr(n int) string { return strconv.Itoa(n) }
