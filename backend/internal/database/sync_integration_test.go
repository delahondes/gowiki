//go:build integration

package database

import (
	"context"
	"strings"
	"testing"
)

// A block-form {database-row} block that ExtractDatabaseRows parses.
func rowBlock(table string, kv ...[2]string) string {
	var b strings.Builder
	b.WriteString("{database-row table=" + table + "}\n\n")
	b.WriteString("| Field | Value |\n| --- | --- |\n")
	for _, p := range kv {
		b.WriteString("| " + p[0] + " | " + p[1] + " |\n")
	}
	return b.String()
}

func TestDatabaseSync_SyncPageRows_ExplicitBlockUpserts(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	tid := makeTable(t, schema, "notes")
	makeField(t, schema, tid, "title", FieldTypeText)
	makeField(t, schema, tid, "score", FieldTypeInteger)

	md := "# Header\n\n" + rowBlock("notes", [2]string{"title", "First note"}, [2]string{"score", "42"})
	sync.SyncPageRows("/n/first", md)

	got, err := data.GetRowByPagePath(context.Background(), "notes", "/n/first")
	if err != nil {
		t.Fatalf("GetRowByPagePath: %v", err)
	}
	if got.Fields["title"] != "First note" {
		t.Errorf("title = %v", got.Fields["title"])
	}
	if got.Fields["score"].(int64) != 42 {
		t.Errorf("score = %v", got.Fields["score"])
	}
}

func TestDatabaseSync_SyncPageRows_SystemColumnsIgnored(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	tid := makeTable(t, schema, "notes")
	makeField(t, schema, tid, "title", FieldTypeText)

	// Author supplies id + page_path + timestamps — sync must strip them.
	md := rowBlock("notes",
		[2]string{"id", "999999"},
		[2]string{"page_path", "/somewhere/else"},
		[2]string{"created_at", "1970-01-01"},
		[2]string{"title", "kept"},
	)
	sync.SyncPageRows("/n/sys", md)

	row, err := data.GetRowByPagePath(context.Background(), "notes", "/n/sys")
	if err != nil {
		t.Fatalf("GetRowByPagePath: %v", err)
	}
	if row.ID == 999999 {
		t.Errorf("system id smuggled through: %d", row.ID)
	}
	if row.PagePath != "/n/sys" {
		t.Errorf("system page_path smuggled through: %q", row.PagePath)
	}
	if row.Fields["title"] != "kept" {
		t.Errorf("normal field lost: %v", row.Fields["title"])
	}
}

func TestDatabaseSync_SyncPageRows_UnknownTableIsLogged(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	// Table doesn't exist — sync must not panic, log-and-continue.
	sync.SyncPageRows("/n/x", rowBlock("does_not_exist", [2]string{"foo", "bar"}))
	// If we get here without panic, that's the invariant this test pins.
}

func TestDatabaseSync_SyncPageRows_PageFolderAutoCreatesRow(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	// Table with page_folder="/deviations" — any page under it auto-gets a row
	// even without an explicit {database-row} block.
	ctx := context.Background()
	td := &TableDef{Name: "deviations", PageFolder: "/deviations"}
	if err := schema.CreateTable(ctx, td, "tester"); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	makeField(t, schema, td.ID, "summary", FieldTypeText)

	// Page inside the folder, no directive.
	sync.SyncPageRows("/deviations/dev001", "# just a page\n\nno directive here\n")

	row, err := data.GetRowByPagePath(ctx, "deviations", "/deviations/dev001")
	if err != nil {
		t.Fatalf("GetRowByPagePath: %v", err)
	}
	if row == nil || row.PagePath != "/deviations/dev001" {
		t.Errorf("auto-created row missing or wrong path: %+v", row)
	}
}

func TestDatabaseSync_SyncPageRows_PageOutsideFolderNoRow(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	ctx := context.Background()
	td := &TableDef{Name: "docs_only", PageFolder: "/docs"}
	if err := schema.CreateTable(ctx, td, "tester"); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	makeField(t, schema, td.ID, "field", FieldTypeText)

	// Page NOT in the folder — no auto-create.
	sync.SyncPageRows("/elsewhere/p", "unrelated content\n")

	row, _ := data.GetRowByPagePath(ctx, "docs_only", "/elsewhere/p")
	if row != nil {
		t.Errorf("unexpected auto-created row: %+v", row)
	}
}

func TestDatabaseSync_RemovePageRows(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	tid := makeTable(t, schema, "notes")
	makeField(t, schema, tid, "title", FieldTypeText)

	sync.SyncPageRows("/n/one", rowBlock("notes", [2]string{"title", "keep"}))
	sync.SyncPageRows("/n/two", rowBlock("notes", [2]string{"title", "drop"}))

	sync.RemovePageRows("/n/two")

	if _, err := data.GetRowByPagePath(context.Background(), "notes", "/n/two"); err == nil {
		t.Errorf("row for removed page still exists")
	}
	if _, err := data.GetRowByPagePath(context.Background(), "notes", "/n/one"); err != nil {
		t.Errorf("unrelated row was deleted: %v", err)
	}
}

func TestDatabaseSync_RenamePageRows_PreservesID(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	tid := makeTable(t, schema, "notes")
	makeField(t, schema, tid, "title", FieldTypeText)

	sync.SyncPageRows("/before", rowBlock("notes", [2]string{"title", "same row"}))
	before, _ := data.GetRowByPagePath(context.Background(), "notes", "/before")

	sync.RenamePageRows("/before", "/after")

	after, err := data.GetRowByPagePath(context.Background(), "notes", "/after")
	if err != nil {
		t.Fatalf("row not found at new path: %v", err)
	}
	if after.ID != before.ID {
		t.Errorf("rename changed the row id: %d -> %d", before.ID, after.ID)
	}
	// Old path is gone.
	if _, err := data.GetRowByPagePath(context.Background(), "notes", "/before"); err == nil {
		t.Errorf("row still exists at old path")
	}
}

func TestDatabaseSync_ValidatePageContent_NoRowNoError(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	tid := makeTable(t, schema, "notes")
	makeField(t, schema, tid, "title", FieldTypeText)

	// Author includes an id, but no row exists yet for this page —
	// validation must NOT error (new page path, id will be assigned).
	md := rowBlock("notes", [2]string{"id", "42"}, [2]string{"title", "x"})
	if err := sync.ValidatePageContent("/n/new", md); err != nil {
		t.Errorf("expected nil for pre-existing page, got %v", err)
	}
}

func TestDatabaseSync_ValidatePageContent_MatchingIDAccepted(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	tid := makeTable(t, schema, "notes")
	makeField(t, schema, tid, "title", FieldTypeText)

	// Create a real row, then validate content whose id matches it.
	sync.SyncPageRows("/n/keep", rowBlock("notes", [2]string{"title", "hi"}))
	row, _ := data.GetRowByPagePath(context.Background(), "notes", "/n/keep")

	md := rowBlock("notes",
		[2]string{"id", stringifyInt(row.ID)},
		[2]string{"title", "hi again"},
	)
	if err := sync.ValidatePageContent("/n/keep", md); err != nil {
		t.Errorf("matching id rejected: %v", err)
	}
}

func TestDatabaseSync_ValidatePageContent_TamperedIDRejected(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	tid := makeTable(t, schema, "notes")
	makeField(t, schema, tid, "title", FieldTypeText)

	sync.SyncPageRows("/n/keep2", rowBlock("notes", [2]string{"title", "hi"}))
	row, _ := data.GetRowByPagePath(context.Background(), "notes", "/n/keep2")

	// Author swaps the id to some other number — must be rejected.
	badID := row.ID + 1000
	md := rowBlock("notes",
		[2]string{"id", stringifyInt(badID)},
		[2]string{"title", "still hi"},
	)
	err := sync.ValidatePageContent("/n/keep2", md)
	if err == nil {
		t.Fatalf("tampered id was accepted")
	}
	if !strings.Contains(err.Error(), "cannot change the id field") {
		t.Errorf("error message unexpected: %v", err)
	}
}

func TestDatabaseSync_ValidatePageContent_EmptyIDIgnored(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	sync := NewDatabaseSync(schema, data)

	tid := makeTable(t, schema, "notes")
	makeField(t, schema, tid, "title", FieldTypeText)

	// Empty id string is legitimate for new rows — accepted regardless.
	md := rowBlock("notes", [2]string{"id", ""}, [2]string{"title", "x"})
	if err := sync.ValidatePageContent("/n/empty", md); err != nil {
		t.Errorf("empty id was rejected: %v", err)
	}
}

func TestDatabaseSync_ValidatePageContent_NilComponentsIsNoOp(t *testing.T) {
	t.Parallel()
	// A sync with nil schema/data returns nil for any content — used at
	// startup before the DB is wired.
	sync := NewDatabaseSync(nil, nil)
	if err := sync.ValidatePageContent("/anything", "anything"); err != nil {
		t.Errorf("nil-component ValidatePageContent returned %v", err)
	}
}

// stringifyInt: small local helper to keep the sync test file self-contained.
func stringifyInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
