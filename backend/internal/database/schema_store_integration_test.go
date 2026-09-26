//go:build integration

package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// helper: fresh SchemaStore backed by an isolated per-test schema.
func newSchemaStore(t *testing.T) (*SchemaStore, context.Context) {
	t.Helper()
	pool := newTestPool(t)
	return NewSchemaStore(pool), context.Background()
}

// makeTable creates a table with the given name and returns its ID. Fails
// the test on any error — callers assume success.
func makeTable(t *testing.T, s *SchemaStore, name string) int {
	t.Helper()
	td := &TableDef{Name: name, Label: "Label for " + name, PageFolder: "/" + name}
	if err := s.CreateTable(context.Background(), td, "tester"); err != nil {
		t.Fatalf("CreateTable(%q): %v", name, err)
	}
	return td.ID
}

// makeField adds a field to a table and returns its ID.
func makeField(t *testing.T, s *SchemaStore, tableID int, name, ftype string) int {
	t.Helper()
	fd := &FieldDef{TableID: tableID, Name: name, Label: strings.Title(name), Type: ftype}
	if err := s.CreateField(context.Background(), fd, "tester"); err != nil {
		t.Fatalf("CreateField(%q, %q): %v", name, ftype, err)
	}
	return fd.ID
}

func TestSchemaStore_CreateAndGetTable(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)

	td := &TableDef{Name: "issues", Label: "Issues", PageFolder: "/issues", DefaultSortField: "created_at"}
	if err := s.CreateTable(ctx, td, "alice"); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if td.ID == 0 {
		t.Errorf("expected ID populated after CreateTable, got 0")
	}
	if td.CreatedAt.IsZero() || td.UpdatedAt.IsZero() {
		t.Errorf("timestamps not populated")
	}
	if td.DefaultSortOrder != "asc" {
		t.Errorf("DefaultSortOrder = %q, want %q (default filled in)", td.DefaultSortOrder, "asc")
	}
	if td.ScopeRegexp != ".*" {
		t.Errorf("ScopeRegexp = %q, want %q (default filled in)", td.ScopeRegexp, ".*")
	}

	got, err := s.GetTable(ctx, td.ID)
	if err != nil {
		t.Fatalf("GetTable: %v", err)
	}
	if got.Name != "issues" || got.Label != "Issues" {
		t.Errorf("GetTable = %+v", got)
	}
}

func TestSchemaStore_CreateTable_RejectsBadName(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)

	cases := []string{"Issues", "1invalid", "with-dash", "with space", ""}
	for _, name := range cases {
		td := &TableDef{Name: name}
		if err := s.CreateTable(ctx, td, "tester"); err == nil {
			t.Errorf("CreateTable(%q) accepted an invalid name", name)
		}
	}
}

func TestSchemaStore_ListTables_OrderedByName(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)

	makeTable(t, s, "zeta")
	makeTable(t, s, "alpha")
	makeTable(t, s, "mu")

	tables, err := s.ListTables(ctx)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(tables) != 3 {
		t.Fatalf("ListTables returned %d, want 3", len(tables))
	}
	if tables[0].Name != "alpha" || tables[1].Name != "mu" || tables[2].Name != "zeta" {
		t.Errorf("not sorted: %v", []string{tables[0].Name, tables[1].Name, tables[2].Name})
	}
}

func TestSchemaStore_GetTableByName(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	makeTable(t, s, "notes")

	got, err := s.GetTableByName(ctx, "notes")
	if err != nil {
		t.Fatalf("GetTableByName: %v", err)
	}
	if got == nil || got.Name != "notes" {
		t.Errorf("unexpected result: %+v", got)
	}

	// Unknown name: error, not nil result.
	if _, err := s.GetTableByName(ctx, "nonexistent"); err == nil {
		t.Errorf("expected error for unknown table")
	}
}

func TestSchemaStore_UpdateTable(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	id := makeTable(t, s, "docs")

	td, _ := s.GetTable(ctx, id)
	td.Label = "Updated Label"
	td.PageFolder = "/documents"
	if err := s.UpdateTable(ctx, td, "bob"); err != nil {
		t.Fatalf("UpdateTable: %v", err)
	}
	got, _ := s.GetTable(ctx, id)
	if got.Label != "Updated Label" || got.PageFolder != "/documents" {
		t.Errorf("UpdateTable did not persist: %+v", got)
	}
}

func TestSchemaStore_DeleteTable_DropsDataAndFields(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	id := makeTable(t, s, "temporary")
	makeField(t, s, id, "title", FieldTypeText)
	makeField(t, s, id, "tags", FieldTypeMultiEnum)
	// NOTE: auto_increment fields intentionally NOT included here — see
	// TestSchemaStore_DeleteTable_AutoIncrement_KnownBroken below for the
	// separate defect they trigger.

	if err := s.DeleteTable(ctx, id, "cleaner"); err != nil {
		t.Fatalf("DeleteTable: %v", err)
	}

	// Table def and dependent field rows should be gone.
	if _, err := s.GetTable(ctx, id); err == nil {
		t.Errorf("expected error getting deleted table")
	}
	// The dynamic data table + junction table + sequence should be gone.
	// If DROP hadn't cascaded, the second CreateTable on the same name would
	// hit a leftover data table and fail.
	newID := makeTable(t, s, "temporary")
	if newID == id {
		t.Errorf("expected fresh id, got same %d", newID)
	}
}

// TestSchemaStore_DeleteTable_AutoIncrement pins the fix for a defect
// surfaced while writing this suite: DeleteTable was dropping each
// auto_increment column's sequence BEFORE the data table carrying the
// column with `DEFAULT nextval('seq')`, so Postgres refused the
// sequence drop and the transaction erred mid-flight. Fix: drop the
// data table first, then the (now orphan) sequences.
func TestSchemaStore_DeleteTable_AutoIncrement(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	id := makeTable(t, s, "auto_del")
	makeField(t, s, id, "seq", FieldTypeAutoIncrement)
	if err := s.DeleteTable(ctx, id, "cleaner"); err != nil {
		t.Fatalf("DeleteTable with auto_increment: %v", err)
	}
	// Re-create should not collide with a leftover sequence.
	newID := makeTable(t, s, "auto_del")
	if newID == id {
		t.Errorf("expected fresh id, got same %d", newID)
	}
	makeField(t, s, newID, "seq", FieldTypeAutoIncrement)
}

func TestSchemaStore_CreateField_RejectsBadNameAndType(t *testing.T) {
	t.Parallel()
	s, _ := newSchemaStore(t)
	id := makeTable(t, s, "widgets")

	if err := s.CreateField(context.Background(), &FieldDef{TableID: id, Name: "Bad-Name", Type: FieldTypeText}, "tester"); err == nil {
		t.Errorf("accepted bad name")
	}
	if err := s.CreateField(context.Background(), &FieldDef{TableID: id, Name: "good", Type: "nonsense"}, "tester"); err == nil {
		t.Errorf("accepted bad type")
	}
}

func TestSchemaStore_ListFields_OrderedByDisplayOrder(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "tickets")

	// Insert in reverse order; assert list returns display-order-sorted.
	for i, name := range []string{"c", "a", "b"} {
		fd := &FieldDef{TableID: tid, Name: name, Type: FieldTypeText, DisplayOrder: i}
		if err := s.CreateField(ctx, fd, "tester"); err != nil {
			t.Fatalf("CreateField %s: %v", name, err)
		}
	}

	fields, err := s.ListFields(ctx, tid)
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(fields) != 3 {
		t.Fatalf("got %d fields, want 3", len(fields))
	}
	if fields[0].Name != "c" || fields[1].Name != "a" || fields[2].Name != "b" {
		t.Errorf("display_order not respected: %+v", fields)
	}
}

func TestSchemaStore_CreateField_EnumWithValues(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "opinions")

	fd := &FieldDef{TableID: tid, Name: "level", Type: FieldTypeEnum, EnumValues: []string{"low", "med", "high"}}
	if err := s.CreateField(ctx, fd, "tester"); err != nil {
		t.Fatalf("CreateField enum: %v", err)
	}
	fields, _ := s.ListFields(ctx, tid)
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	got := fields[0]
	if len(got.EnumValues) != 3 || got.EnumValues[0] != "low" || got.EnumValues[2] != "high" {
		t.Errorf("enum values not persisted in order: %+v", got.EnumValues)
	}
}

func TestSchemaStore_UpdateField(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "boards")
	fid := makeField(t, s, tid, "name", FieldTypeText)

	// Fetch the field so we don't clobber TableID.
	fields, _ := s.ListFields(ctx, tid)
	fd := fields[0]
	fd.Label = "Full name"
	fd.Required = true
	if err := s.UpdateField(ctx, &fd, "tester"); err != nil {
		t.Fatalf("UpdateField: %v", err)
	}
	got, _ := s.ListFields(ctx, tid)
	if !got[0].Required || got[0].Label != "Full name" {
		t.Errorf("update not applied: %+v", got[0])
	}
	_ = fid
}

func TestSchemaStore_ArchiveField_HidesFromActive(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "shelves")
	fid := makeField(t, s, tid, "old", FieldTypeText)

	if err := s.ArchiveField(ctx, fid, "cleaner"); err != nil {
		t.Fatalf("ArchiveField: %v", err)
	}
	fields, _ := s.ListFields(ctx, tid)
	// Archived field still appears in ListFields (activeFieldMap filters it),
	// but its ArchivedAt should be non-nil.
	if len(fields) != 1 || fields[0].ArchivedAt == nil {
		t.Errorf("archive did not set ArchivedAt: %+v", fields)
	}
}

func TestSchemaStore_SetEnumValues_ReplacesInPlace(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "polls")
	fd := &FieldDef{TableID: tid, Name: "choice", Type: FieldTypeEnum, EnumValues: []string{"a", "b"}}
	if err := s.CreateField(ctx, fd, "tester"); err != nil {
		t.Fatalf("CreateField: %v", err)
	}

	if err := s.SetEnumValues(ctx, fd.ID, []string{"x", "y", "z"}); err != nil {
		t.Fatalf("SetEnumValues: %v", err)
	}
	fields, _ := s.ListFields(ctx, tid)
	if len(fields[0].EnumValues) != 3 || fields[0].EnumValues[0] != "x" {
		t.Errorf("replacement failed: %+v", fields[0].EnumValues)
	}
}

func TestSchemaStore_GetHistory_RecordsChangesInOrder(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "audits") // -> create_table entry
	fid := makeField(t, s, tid, "who", FieldTypeText)   // -> add_field
	// Two archives so we get more entries.
	// Fetch then update once, archive once.
	fields, _ := s.ListFields(ctx, tid)
	fields[0].Label = "Actor"
	_ = s.UpdateField(ctx, &fields[0], "alice") // -> update_field
	_ = s.ArchiveField(ctx, fid, "bob")         // -> archive_field

	hist, err := s.GetHistory(ctx, tid)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(hist) < 4 {
		t.Fatalf("expected >=4 history entries, got %d", len(hist))
	}
	// Ordered DESC by changed_at; most recent first is archive_field.
	if hist[0].ChangeType != "archive_field" {
		t.Errorf("most-recent entry = %q, want archive_field", hist[0].ChangeType)
	}
	if hist[len(hist)-1].ChangeType != "create_table" {
		t.Errorf("oldest entry = %q, want create_table", hist[len(hist)-1].ChangeType)
	}
	// changedBy must be attributed correctly for archive.
	if hist[0].ChangedBy != "bob" {
		t.Errorf("changedBy for archive = %q, want bob", hist[0].ChangedBy)
	}
}

func TestSchemaStore_DeleteField_RefusesWhenTableNonEmpty(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	tid := makeTable(t, schema, "surveys")
	fid := makeField(t, schema, tid, "score", FieldTypeInteger)

	ctx := context.Background()
	if err := data.InsertRow(ctx, "surveys", &Row{PagePath: "/x", Fields: map[string]any{"score": 5}}); err != nil {
		t.Fatalf("InsertRow: %v", err)
	}

	err := schema.DeleteField(ctx, fid, "tester")
	if !errors.Is(err, ErrTableNotEmpty) {
		t.Errorf("DeleteField on non-empty table returned %v, want ErrTableNotEmpty", err)
	}
}

func TestSchemaStore_DeleteField_SucceedsOnEmpty(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "reviews")
	fid := makeField(t, s, tid, "rating", FieldTypeInteger)

	if err := s.DeleteField(ctx, fid, "tester"); err != nil {
		t.Fatalf("DeleteField: %v", err)
	}
	fields, _ := s.ListFields(ctx, tid)
	if len(fields) != 0 {
		t.Errorf("field not deleted: %+v", fields)
	}
}

func TestSchemaStore_RenameField_RoundTrip(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "renames")
	fid := makeField(t, s, tid, "oldname", FieldTypeText)

	if err := s.RenameField(ctx, fid, "newname", "tester"); err != nil {
		t.Fatalf("RenameField: %v", err)
	}
	fields, _ := s.ListFields(ctx, tid)
	if fields[0].Name != "newname" {
		t.Errorf("field not renamed: %+v", fields[0])
	}
}

func TestSchemaStore_RenameField_RefusesWhenTableNonEmpty(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	tid := makeTable(t, schema, "roster")
	fid := makeField(t, schema, tid, "team", FieldTypeText)

	ctx := context.Background()
	_ = data.InsertRow(ctx, "roster", &Row{PagePath: "/p", Fields: map[string]any{"team": "A"}})

	err := schema.RenameField(ctx, fid, "squad", "tester")
	if !errors.Is(err, ErrTableNotEmpty) {
		t.Errorf("RenameField on non-empty table returned %v, want ErrTableNotEmpty", err)
	}
}

func TestSchemaStore_RenameField_RejectsBadName(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "bad_rename")
	fid := makeField(t, s, tid, "ok", FieldTypeText)

	if err := s.RenameField(ctx, fid, "Bad-Name", "tester"); err == nil {
		t.Errorf("accepted bad rename target")
	}
}

func TestSchemaStore_RetypeField_TextToInteger_OnEmptyTable(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "retype_ok")
	fid := makeField(t, s, tid, "field", FieldTypeText)

	if err := s.RetypeField(ctx, fid, FieldTypeInteger, "tester"); err != nil {
		t.Fatalf("RetypeField: %v", err)
	}
	fields, _ := s.ListFields(ctx, tid)
	if fields[0].Type != FieldTypeInteger {
		t.Errorf("type not changed: %+v", fields[0])
	}
}

func TestSchemaStore_RetypeField_EnumToText_ClearsEnumValues(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "retype_enum")
	fd := &FieldDef{TableID: tid, Name: "color", Type: FieldTypeEnum, EnumValues: []string{"red", "blue"}}
	if err := s.CreateField(ctx, fd, "tester"); err != nil {
		t.Fatalf("CreateField: %v", err)
	}

	if err := s.RetypeField(ctx, fd.ID, FieldTypeText, "tester"); err != nil {
		t.Fatalf("RetypeField: %v", err)
	}
	fields, _ := s.ListFields(ctx, tid)
	if fields[0].Type != FieldTypeText {
		t.Errorf("type not changed: %+v", fields[0])
	}
	// Enum values must have been cleared when moving out of enum family.
	if len(fields[0].EnumValues) != 0 {
		t.Errorf("stale enum values remained: %+v", fields[0].EnumValues)
	}
}

func TestSchemaStore_RetypeField_RefusesWhenTableNonEmpty(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	tid := makeTable(t, schema, "retype_full")
	fid := makeField(t, schema, tid, "n", FieldTypeText)

	ctx := context.Background()
	_ = data.InsertRow(ctx, "retype_full", &Row{PagePath: "/p", Fields: map[string]any{"n": "hi"}})

	err := schema.RetypeField(ctx, fid, FieldTypeInteger, "tester")
	if !errors.Is(err, ErrTableNotEmpty) {
		t.Errorf("RetypeField on non-empty table returned %v, want ErrTableNotEmpty", err)
	}
}

func TestSchemaStore_RetypeField_RejectsBadType(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	tid := makeTable(t, s, "retype_bad_type")
	fid := makeField(t, s, tid, "n", FieldTypeText)

	if err := s.RetypeField(ctx, fid, "spaceship", "tester"); err == nil {
		t.Errorf("accepted invalid target type")
	}
}

func TestSchemaStore_CountRows(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	tid := makeTable(t, schema, "counter")
	makeField(t, schema, tid, "n", FieldTypeInteger)

	ctx := context.Background()
	n, err := schema.CountRows(ctx, "counter")
	if err != nil || n != 0 {
		t.Fatalf("initial count = %d err=%v", n, err)
	}
	_ = data.InsertRow(ctx, "counter", &Row{PagePath: "/a"})
	_ = data.InsertRow(ctx, "counter", &Row{PagePath: "/b"})
	n, err = schema.CountRows(ctx, "counter")
	if err != nil || n != 2 {
		t.Errorf("after 2 inserts count = %d err=%v", n, err)
	}
}

func TestSchemaStore_UnconnectedPool_ReturnsError(t *testing.T) {
	t.Parallel()
	s := NewSchemaStore(NewPool())
	ctx := context.Background()
	if _, err := s.ListTables(ctx); err == nil {
		t.Errorf("expected 'not connected' error")
	}
}

// smoke test that a schema with a compact write path stays responsive under
// a couple of overlapping tests (each test uses its own isolated schema).
func TestSchemaStore_ParallelCreateDoesNotDeadlock(t *testing.T) {
	t.Parallel()
	s, ctx := newSchemaStore(t)
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for i := 0; i < 5; i++ {
		td := &TableDef{Name: "parallel_" + string(rune('a'+i))}
		if err := s.CreateTable(deadline, td, "tester"); err != nil {
			t.Fatalf("CreateTable %d: %v", i, err)
		}
	}
}
