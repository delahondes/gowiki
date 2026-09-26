//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"gowiki/backend/internal/config"
	"gowiki/backend/internal/database"
)

// callAdminDB is a small convenience — the admin_database handlers all
// take chi URL params, and several read the caller username from
// context. Wrap the httptest boilerplate so each test reads clean.
func callAdminDB(t *testing.T, handler http.HandlerFunc, method, url string, params map[string]string, body []byte, username string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, url, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if username != "" {
		ctx = context.WithValue(ctx, usernameKey, username)
	}
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// serverWithConfig augments newDBHandlerServer with a real config
// store — the status/test/connect endpoints need it. The other admin
// database handlers only need schemaStore + dataStore, which
// newDBHandlerServer already provides.
func serverWithConfig(t *testing.T) *Server {
	t.Helper()
	s, _, _, _ := newDBHandlerServer(t)
	store, err := config.Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := store.Get()
	cfg.Site.Title = "Test"
	cfg.Database.DSN = "postgres://gowiki:gowiki@localhost:5432/gowiki_test?sslmode=disable"
	cfg.Database.Enabled = true
	if err := store.Update(cfg); err != nil {
		t.Fatalf("config seed: %v", err)
	}
	s.configStore = store
	// Wire dbPool from the underlying schemaStore's pool — it's the
	// same pool the tests use, and handleDatabaseStatus reads
	// s.dbPool.IsConnected().
	s.dbPool = database.NewPool()
	return s
}

// ─── handleDatabaseStatus ────────────────────────────────

func TestAdminDB_Status_NotConnected(t *testing.T) {
	t.Parallel()
	s := serverWithConfig(t)
	rec := callAdminDB(t, s.handleDatabaseStatus, http.MethodGet, "/api/admin/database/status", nil, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["connected"] != false {
		t.Errorf("connected = %v, want false (fresh pool)", body["connected"])
	}
	if body["dsn_configured"] != true {
		t.Errorf("dsn_configured = %v, want true", body["dsn_configured"])
	}
}

// ─── handleDatabaseTest ──────────────────────────────────

func TestAdminDB_Test_MissingDSN_400(t *testing.T) {
	t.Parallel()
	s := serverWithConfig(t)
	body, _ := json.Marshal(map[string]string{"dsn": ""})
	rec := callAdminDB(t, s.handleDatabaseTest, http.MethodPost, "/api/admin/database/test", nil, body, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminDB_Test_UnreachableDSN_ReturnsFailure(t *testing.T) {
	t.Parallel()
	s := serverWithConfig(t)
	body, _ := json.Marshal(map[string]string{"dsn": "postgres://noone@127.0.0.1:1/nowhere?connect_timeout=1&sslmode=disable"})
	rec := callAdminDB(t, s.handleDatabaseTest, http.MethodPost, "/api/admin/database/test", nil, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["success"] != false {
		t.Errorf("success = %v, want false", got["success"])
	}
}

// ─── handleListDatabaseTables ────────────────────────────

func TestAdminDB_ListTables_Empty(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	rec := callAdminDB(t, s.handleListDatabaseTables, http.MethodGet, "/api/admin/database/tables", nil, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Tables []database.TableDef `json:"tables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Tables == nil {
		t.Errorf("tables = null, want empty array (frontend .length would crash)")
	}
	if len(body.Tables) != 0 {
		t.Errorf("len(tables) = %d, want 0", len(body.Tables))
	}
}

func TestAdminDB_ListTables_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	// Server without schemaStore → 503.
	s := &Server{}
	rec := callAdminDB(t, s.handleListDatabaseTables, http.MethodGet, "/api/admin/database/tables", nil, nil, "root")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
}

// ─── handleCreateDatabaseTable ───────────────────────────

func TestAdminDB_CreateTable_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	body, _ := json.Marshal(database.TableDef{Name: "gadgets", Label: "Gadgets"})
	rec := callAdminDB(t, s.handleCreateDatabaseTable, http.MethodPost, "/api/admin/database/tables", nil, body, "root")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var td database.TableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &td); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if td.ID == 0 || td.Name != "gadgets" {
		t.Errorf("returned table = %+v, want id>0 name=gadgets", td)
	}
}

func TestAdminDB_CreateTable_Duplicate_400(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	body, _ := json.Marshal(database.TableDef{Name: "dupe"})
	if rec := callAdminDB(t, s.handleCreateDatabaseTable, http.MethodPost, "/api/admin/database/tables", nil, body, "root"); rec.Code != http.StatusCreated {
		t.Fatalf("first create failed: %s", rec.Body.String())
	}
	rec := callAdminDB(t, s.handleCreateDatabaseTable, http.MethodPost, "/api/admin/database/tables", nil, body, "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("second create status = %d, want 400 (duplicate name)", rec.Code)
	}
}

// ─── handleGetDatabaseTable / handleUpdateDatabaseTable ──

func TestAdminDB_GetTable_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	id := makeAPITestTable(t, schema, "read_me", "")

	rec := callAdminDB(t, s.handleGetDatabaseTable, http.MethodGet,
		fmt.Sprintf("/api/admin/database/tables/%d", id),
		map[string]string{"id": strconv.Itoa(id)}, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var td database.TableDef
	if err := json.Unmarshal(rec.Body.Bytes(), &td); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if td.Name != "read_me" || len(td.Fields) != 2 {
		t.Errorf("table = %+v, want name=read_me + 2 fields", td)
	}
}

func TestAdminDB_GetTable_UnknownID_404(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	rec := callAdminDB(t, s.handleGetDatabaseTable, http.MethodGet,
		"/api/admin/database/tables/99999",
		map[string]string{"id": "99999"}, nil, "root")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestAdminDB_UpdateTable_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	id := makeAPITestTable(t, schema, "editable", "")

	td := database.TableDef{Name: "editable", Label: "New Label", PageFolder: "/edited/@id"}
	body, _ := json.Marshal(td)
	rec := callAdminDB(t, s.handleUpdateDatabaseTable, http.MethodPut,
		fmt.Sprintf("/api/admin/database/tables/%d", id),
		map[string]string{"id": strconv.Itoa(id)}, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	// Read back through the store to confirm persistence, not just the echoed body.
	got, err := schema.GetTable(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTable: %v", err)
	}
	if got.Label != "New Label" {
		t.Errorf("Label = %q, want New Label", got.Label)
	}
	if got.PageFolder != "/edited/@id" {
		t.Errorf("PageFolder = %q, want /edited/@id", got.PageFolder)
	}
}

// ─── handleDeleteDatabaseTable ───────────────────────────

func TestAdminDB_DeleteTable_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	id := makeAPITestTable(t, schema, "trash", "")

	rec := callAdminDB(t, s.handleDeleteDatabaseTable, http.MethodDelete,
		fmt.Sprintf("/api/admin/database/tables/%d", id),
		map[string]string{"id": strconv.Itoa(id)}, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	// Confirm gone.
	if _, err := schema.GetTable(context.Background(), id); err == nil {
		t.Errorf("table still readable after delete")
	}
}

// ─── handleCreateDatabaseField / handleUpdateDatabaseField / handleArchiveDatabaseField ──

func TestAdminDB_CreateField_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	id := makeAPITestTable(t, schema, "grow", "")

	body, _ := json.Marshal(database.FieldDef{Name: "extra", Type: database.FieldTypeText, Label: "Extra"})
	rec := callAdminDB(t, s.handleCreateDatabaseField, http.MethodPost,
		fmt.Sprintf("/api/admin/database/tables/%d/fields", id),
		map[string]string{"id": strconv.Itoa(id)}, body, "root")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	// Confirm via schema store.
	tbl, err := schema.GetTable(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTable: %v", err)
	}
	names := make([]string, 0, len(tbl.Fields))
	for _, f := range tbl.Fields {
		names = append(names, f.Name)
	}
	if !containsString(names, "extra") {
		t.Errorf("field 'extra' not in %v", names)
	}
}

func TestAdminDB_UpdateField_RenameOnEmptyTable(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	tableID := makeAPITestTable(t, schema, "renameme", "")
	// Grab the id of the "title" field created by makeAPITestTable.
	tbl, _ := schema.GetTable(context.Background(), tableID)
	var fieldID int
	for _, f := range tbl.Fields {
		if f.Name == "title" {
			fieldID = f.ID
		}
	}
	if fieldID == 0 {
		t.Fatalf("could not find title field")
	}

	body, _ := json.Marshal(map[string]any{"name": "headline", "label": "Headline"})
	rec := callAdminDB(t, s.handleUpdateDatabaseField, http.MethodPut,
		fmt.Sprintf("/api/admin/database/tables/%d/fields/%d", tableID, fieldID),
		map[string]string{"id": strconv.Itoa(tableID), "fid": strconv.Itoa(fieldID)}, body, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	tbl, _ = schema.GetTable(context.Background(), tableID)
	found := false
	for _, f := range tbl.Fields {
		if f.Name == "headline" {
			found = true
			if f.Label != "Headline" {
				t.Errorf("Label = %q, want Headline", f.Label)
			}
		}
	}
	if !found {
		t.Errorf("field 'headline' not present after rename")
	}
}

func TestAdminDB_ArchiveField_HappyPath(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	tableID := makeAPITestTable(t, schema, "arch_target", "")
	tbl, _ := schema.GetTable(context.Background(), tableID)
	var fieldID int
	for _, f := range tbl.Fields {
		if f.Name == "priority" {
			fieldID = f.ID
		}
	}

	rec := callAdminDB(t, s.handleArchiveDatabaseField, http.MethodDelete,
		fmt.Sprintf("/api/admin/database/tables/%d/fields/%d", tableID, fieldID),
		map[string]string{"id": strconv.Itoa(tableID), "fid": strconv.Itoa(fieldID)}, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	tbl, _ = schema.GetTable(context.Background(), tableID)
	for _, f := range tbl.Fields {
		if f.ID == fieldID && f.ArchivedAt == nil {
			t.Errorf("field %d still active after archive", fieldID)
		}
	}
}

// ─── handleCountDatabaseRows ─────────────────────────────

func TestAdminDB_CountRows_Empty(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	id := makeAPITestTable(t, schema, "counters", "")
	rec := callAdminDB(t, s.handleCountDatabaseRows, http.MethodGet,
		fmt.Sprintf("/api/admin/database/tables/%d/rows/count", id),
		map[string]string{"id": strconv.Itoa(id)}, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["count"] != 0 {
		t.Errorf("count = %d, want 0", got["count"])
	}
}

func TestAdminDB_CountRows_UnknownID_404(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newDBHandlerServer(t)
	rec := callAdminDB(t, s.handleCountDatabaseRows, http.MethodGet,
		"/api/admin/database/tables/99999/rows/count",
		map[string]string{"id": "99999"}, nil, "root")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// ─── handleDatabaseTableHistory ──────────────────────────

func TestAdminDB_History_ReturnsCreateEntry(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	id := makeAPITestTable(t, schema, "history_target", "")

	rec := callAdminDB(t, s.handleDatabaseTableHistory, http.MethodGet,
		fmt.Sprintf("/api/admin/database/tables/%d/history", id),
		map[string]string{"id": strconv.Itoa(id)}, nil, "root")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		History []database.SchemaHistoryEntry `json:"history"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.History == nil {
		t.Errorf("history = null, want [] (never send null arrays to the frontend)")
	}
	if len(body.History) == 0 {
		t.Errorf("history is empty — creating the table and its 2 fields should have left 3 entries")
	}
}

// ─── handleMigratePagePaths ──────────────────────────────
//
// The mover interface (Move, PreviewMove, …) isn't implemented by
// memPageStore, so the "would_move" dry-run path is what we can
// exercise without dragging in the FileStore. The "actually move"
// path is covered by the storage-layer tests already.

func TestAdminDB_MigratePagePaths_NoPageFolder_400(t *testing.T) {
	t.Parallel()
	s, _, _, schema := newDBHandlerServer(t)
	id := makeAPITestTable(t, schema, "no_folder", "") // page_folder empty

	rec := callAdminDB(t, s.handleMigratePagePaths, http.MethodPost,
		fmt.Sprintf("/api/admin/database/tables/%d/migrate-page-paths", id),
		map[string]string{"id": strconv.Itoa(id)}, []byte(`{"dry_run":true}`), "root")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// ─── Small helpers used above ────────────────────────────

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// ensure strings import survives if a test happens to use it.
var _ = strings.TrimSpace
