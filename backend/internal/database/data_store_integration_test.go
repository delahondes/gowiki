//go:build integration

package database

import (
	"context"
	"testing"
)

// newFullStack returns pool + schemaStore + dataStore against an isolated
// per-test schema. Standard fixture for DataStore integration tests.
func newFullStack(t *testing.T) (*SchemaStore, *DataStore, context.Context) {
	t.Helper()
	pool := newTestPool(t)
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	return schema, data, context.Background()
}

// makeIssuesTable creates a well-shaped table with common field types,
// used by most row-level tests below.
func makeIssuesTable(t *testing.T, schema *SchemaStore) int {
	t.Helper()
	ctx := context.Background()
	tid := makeTable(t, schema, "issues")
	makeField(t, schema, tid, "title", FieldTypeText)
	makeField(t, schema, tid, "priority", FieldTypeInteger)
	makeField(t, schema, tid, "score", FieldTypeFloat)
	makeField(t, schema, tid, "resolved", FieldTypeBoolean)
	// Enum with values.
	{
		fd := &FieldDef{TableID: tid, Name: "state", Type: FieldTypeEnum, EnumValues: []string{"open", "closed"}}
		if err := schema.CreateField(ctx, fd, "tester"); err != nil {
			t.Fatalf("CreateField state: %v", err)
		}
	}
	// Multi-enum for junction-table coverage.
	{
		fd := &FieldDef{TableID: tid, Name: "labels", Type: FieldTypeMultiEnum, EnumValues: []string{"bug", "feat", "docs"}}
		if err := schema.CreateField(ctx, fd, "tester"); err != nil {
			t.Fatalf("CreateField labels: %v", err)
		}
	}
	// Auto-increment.
	makeField(t, schema, tid, "num", FieldTypeAutoIncrement)
	return tid
}

func TestDataStore_InsertRow_AllTypes(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	row := &Row{
		PagePath: "/issues/one",
		Fields: map[string]any{
			"title":    "First",
			"priority": 3,
			"score":    9.5,
			"resolved": true,
			"state":    "open",
			"labels":   []string{"bug", "feat"},
		},
	}
	if err := data.InsertRow(ctx, "issues", row); err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	if row.ID == 0 {
		t.Errorf("row.ID not populated")
	}
	// Auto-increment must be read back.
	if _, ok := row.Fields["num"]; !ok {
		t.Errorf("expected auto_increment 'num' populated in row.Fields, got %+v", row.Fields)
	}

	got, err := data.GetRow(ctx, "issues", row.ID)
	if err != nil {
		t.Fatalf("GetRow: %v", err)
	}
	if got.Fields["title"] != "First" {
		t.Errorf("title = %v, want First", got.Fields["title"])
	}
	if got.Fields["state"] != "open" {
		t.Errorf("state = %v", got.Fields["state"])
	}
	// multi_enum should come back as []string sorted alphabetically.
	labels, ok := got.Fields["labels"].([]string)
	if !ok {
		t.Fatalf("labels type = %T, want []string", got.Fields["labels"])
	}
	if len(labels) != 2 || labels[0] != "bug" || labels[1] != "feat" {
		t.Errorf("labels = %v", labels)
	}
}

func TestDataStore_InsertRow_UnknownTable(t *testing.T) {
	t.Parallel()
	_, data, ctx := newFullStack(t)
	if err := data.InsertRow(ctx, "no_such_table", &Row{PagePath: "/x"}); err == nil {
		t.Errorf("expected error for unknown table")
	}
}

func TestDataStore_InsertRow_AppliesSchemaDefault(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	tid := makeTable(t, schema, "defaults")
	// Field with a default_value the caller does not supply.
	fd := &FieldDef{TableID: tid, Name: "status", Type: FieldTypeText, DefaultValue: "pending"}
	if err := schema.CreateField(ctx, fd, "tester"); err != nil {
		t.Fatalf("CreateField: %v", err)
	}

	row := &Row{PagePath: "/d/1"} // no fields at all
	if err := data.InsertRow(ctx, "defaults", row); err != nil {
		t.Fatalf("InsertRow: %v", err)
	}
	got, _ := data.GetRow(ctx, "defaults", row.ID)
	if got.Fields["status"] != "pending" {
		t.Errorf("schema default not applied: %v", got.Fields["status"])
	}
}

func TestDataStore_UpdateRow(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	row := &Row{PagePath: "/i/2", Fields: map[string]any{"title": "orig", "priority": 1, "labels": []string{"bug"}}}
	if err := data.InsertRow(ctx, "issues", row); err != nil {
		t.Fatalf("InsertRow: %v", err)
	}

	if err := data.UpdateRow(ctx, "issues", row.ID, map[string]any{
		"title":  "updated",
		"labels": []string{"docs", "feat"}, // fully replaces
	}); err != nil {
		t.Fatalf("UpdateRow: %v", err)
	}

	got, _ := data.GetRow(ctx, "issues", row.ID)
	if got.Fields["title"] != "updated" {
		t.Errorf("title not updated: %v", got.Fields["title"])
	}
	labels, _ := got.Fields["labels"].([]string)
	if len(labels) != 2 || labels[0] != "docs" || labels[1] != "feat" {
		t.Errorf("labels not replaced: %v", labels)
	}
}

func TestDataStore_UpdateRow_UnknownFieldsIgnored(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	row := &Row{PagePath: "/i/3", Fields: map[string]any{"title": "t"}}
	_ = data.InsertRow(ctx, "issues", row)

	// Include a field that doesn't exist — must not error, must not touch anything.
	err := data.UpdateRow(ctx, "issues", row.ID, map[string]any{"nonexistent": "x", "title": "kept"})
	if err != nil {
		t.Fatalf("UpdateRow: %v", err)
	}
	got, _ := data.GetRow(ctx, "issues", row.ID)
	if got.Fields["title"] != "kept" {
		t.Errorf("title = %v, want kept", got.Fields["title"])
	}
}

func TestDataStore_DeleteRow(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	row := &Row{PagePath: "/i/del", Fields: map[string]any{"title": "delme"}}
	_ = data.InsertRow(ctx, "issues", row)

	if err := data.DeleteRow(ctx, "issues", row.ID); err != nil {
		t.Fatalf("DeleteRow: %v", err)
	}
	if _, err := data.GetRow(ctx, "issues", row.ID); err == nil {
		t.Errorf("expected error getting deleted row")
	}
}

func TestDataStore_DeleteRow_UnknownTable(t *testing.T) {
	t.Parallel()
	_, data, ctx := newFullStack(t)
	if err := data.DeleteRow(ctx, "no_such_table", 1); err == nil {
		t.Errorf("expected error for unknown table")
	}
}

func TestDataStore_GetRow_MissingID(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)
	if _, err := data.GetRow(ctx, "issues", 9999); err == nil {
		t.Errorf("expected error for missing id")
	}
}

func TestDataStore_GetRowByPagePath_NormalizesLeadingSlash(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	_ = data.InsertRow(ctx, "issues", &Row{PagePath: "/i/slash", Fields: map[string]any{"title": "t"}})

	// Fetch without the leading slash — must still find the row.
	got, err := data.GetRowByPagePath(ctx, "issues", "i/slash")
	if err != nil {
		t.Fatalf("GetRowByPagePath: %v", err)
	}
	if got.PagePath != "/i/slash" {
		t.Errorf("PagePath = %q, want /i/slash", got.PagePath)
	}
}

func TestDataStore_UpsertPageRow_CreatesThenUpdates(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	// First call: insert.
	r1, err := data.UpsertPageRow(ctx, "issues", "/u/1", map[string]any{"title": "one"})
	if err != nil {
		t.Fatalf("UpsertPageRow (insert): %v", err)
	}
	if r1.ID == 0 {
		t.Errorf("no id after insert")
	}

	// Second call: update, id preserved.
	r2, err := data.UpsertPageRow(ctx, "issues", "/u/1", map[string]any{"title": "two"})
	if err != nil {
		t.Fatalf("UpsertPageRow (update): %v", err)
	}
	if r2.ID != r1.ID {
		t.Errorf("id changed on update: %d -> %d", r1.ID, r2.ID)
	}
	if r2.Fields["title"] != "two" {
		t.Errorf("title = %v, want two", r2.Fields["title"])
	}
}

func TestDataStore_UpsertPageRow_EmptyFieldsLeavesExisting(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	_, _ = data.UpsertPageRow(ctx, "issues", "/u/e", map[string]any{"title": "keep"})
	// Empty second call: existing row returned unchanged.
	got, err := data.UpsertPageRow(ctx, "issues", "/u/e", nil)
	if err != nil {
		t.Fatalf("UpsertPageRow: %v", err)
	}
	if got.Fields["title"] != "keep" {
		t.Errorf("title clobbered: %v", got.Fields["title"])
	}
}

func TestDataStore_QueryRows_FiltersAndSort(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	for i, spec := range []struct {
		path     string
		title    string
		priority int
		state    string
	}{
		{"/q/a", "alpha", 1, "open"},
		{"/q/b", "beta", 3, "closed"},
		{"/q/c", "gamma", 2, "open"},
	} {
		row := &Row{PagePath: spec.path, Fields: map[string]any{"title": spec.title, "priority": spec.priority, "state": spec.state}}
		if err := data.InsertRow(ctx, "issues", row); err != nil {
			t.Fatalf("InsertRow %d: %v", i, err)
		}
	}

	// = filter on enum.
	rows, total, err := data.QueryRows(ctx, "issues", QueryParams{Filters: []Filter{{Field: "state", Operator: "=", Value: "open"}}})
	if err != nil || total != 2 {
		t.Fatalf("state=open total=%d err=%v", total, err)
	}
	_ = rows

	// > filter on integer.
	_, total, _ = data.QueryRows(ctx, "issues", QueryParams{Filters: []Filter{{Field: "priority", Operator: ">", Value: "1"}}})
	if total != 2 {
		t.Errorf("priority>1 total = %d, want 2", total)
	}

	// ~ filter with implicit % wrapping.
	_, total, _ = data.QueryRows(ctx, "issues", QueryParams{Filters: []Filter{{Field: "title", Operator: "~", Value: "amm"}}})
	if total != 1 {
		t.Errorf("title~amm total = %d, want 1", total)
	}

	// != filter.
	_, total, _ = data.QueryRows(ctx, "issues", QueryParams{Filters: []Filter{{Field: "state", Operator: "!=", Value: "open"}}})
	if total != 1 {
		t.Errorf("state!=open total = %d, want 1", total)
	}

	// Sort desc.
	rows, _, _ = data.QueryRows(ctx, "issues", QueryParams{Sort: "priority", Order: "desc"})
	if len(rows) != 3 || rows[0].Fields["priority"].(int64) != 3 {
		t.Errorf("sort desc first priority = %v, want 3", rows[0].Fields["priority"])
	}

	// Limit + offset.
	rows, _, _ = data.QueryRows(ctx, "issues", QueryParams{Sort: "id", Limit: 1, Offset: 1})
	if len(rows) != 1 {
		t.Errorf("limit=1 returned %d rows", len(rows))
	}
}

func TestDataStore_QueryRows_UnknownFieldIgnored(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)
	_ = data.InsertRow(ctx, "issues", &Row{PagePath: "/q/u", Fields: map[string]any{"title": "x"}})

	// Filter references a field that doesn't exist → filter silently dropped,
	// query still succeeds and returns all rows.
	_, total, err := data.QueryRows(ctx, "issues", QueryParams{Filters: []Filter{{Field: "nope", Operator: "=", Value: "anything"}}})
	if err != nil {
		t.Fatalf("QueryRows: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
}

func TestDataStore_QueryRows_MultiEnumFilter(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)

	_ = data.InsertRow(ctx, "issues", &Row{PagePath: "/m/1", Fields: map[string]any{"title": "a", "labels": []string{"bug"}}})
	_ = data.InsertRow(ctx, "issues", &Row{PagePath: "/m/2", Fields: map[string]any{"title": "b", "labels": []string{"feat", "docs"}}})
	_ = data.InsertRow(ctx, "issues", &Row{PagePath: "/m/3", Fields: map[string]any{"title": "c"}})

	// = on multi-enum: exists.
	_, total, _ := data.QueryRows(ctx, "issues", QueryParams{Filters: []Filter{{Field: "labels", Operator: "=", Value: "feat"}}})
	if total != 1 {
		t.Errorf("labels=feat total = %d, want 1", total)
	}
	// @null on multi-enum with = : rows with no labels.
	_, total, _ = data.QueryRows(ctx, "issues", QueryParams{Filters: []Filter{{Field: "labels", Operator: "=", Value: "@null"}}})
	if total != 1 {
		t.Errorf("labels=@null total = %d, want 1", total)
	}
	// @null with != : rows that DO have at least one label.
	_, total, _ = data.QueryRows(ctx, "issues", QueryParams{Filters: []Filter{{Field: "labels", Operator: "!=", Value: "@null"}}})
	if total != 2 {
		t.Errorf("labels!=@null total = %d, want 2", total)
	}
}

func TestDataStore_UpdatePagePath(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)
	row := &Row{PagePath: "/old", Fields: map[string]any{"title": "t"}}
	_ = data.InsertRow(ctx, "issues", row)

	if err := data.UpdatePagePath(ctx, "issues", row.ID, "/new"); err != nil {
		t.Fatalf("UpdatePagePath: %v", err)
	}
	got, _ := data.GetRow(ctx, "issues", row.ID)
	if got.PagePath != "/new" {
		t.Errorf("path not updated: %q", got.PagePath)
	}
}

func TestDataStore_UpdatePagePath_UnknownTable(t *testing.T) {
	t.Parallel()
	_, data, ctx := newFullStack(t)
	if err := data.UpdatePagePath(ctx, "nope", 1, "/new"); err == nil {
		t.Errorf("expected error for unknown table")
	}
}

func TestDataStore_FindReferencesTo(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)

	// Target table (people).
	peopleID := makeTable(t, schema, "people")
	makeField(t, schema, peopleID, "name", FieldTypeText)

	// Source table (posts) with a lookup pointing at people.
	postsID := makeTable(t, schema, "posts")
	makeField(t, schema, postsID, "title", FieldTypeText)
	author := &FieldDef{TableID: postsID, Name: "author", Type: FieldTypeLookup, ForeignKey: "people", DisplayColumn: "name"}
	if err := schema.CreateField(ctx, author, "tester"); err != nil {
		t.Fatalf("CreateField author: %v", err)
	}

	// Insert a person and two posts referencing them.
	alice := &Row{PagePath: "/people/alice", Fields: map[string]any{"name": "Alice"}}
	if err := data.InsertRow(ctx, "people", alice); err != nil {
		t.Fatalf("InsertRow alice: %v", err)
	}
	_ = data.InsertRow(ctx, "posts", &Row{PagePath: "/posts/1", Fields: map[string]any{"title": "p1", "author": alice.ID}})
	_ = data.InsertRow(ctx, "posts", &Row{PagePath: "/posts/2", Fields: map[string]any{"title": "p2", "author": alice.ID}})
	// Unreferenced person to prove the query is selective.
	bob := &Row{PagePath: "/people/bob", Fields: map[string]any{"name": "Bob"}}
	_ = data.InsertRow(ctx, "people", bob)

	refs, err := data.FindReferencesTo(ctx, "people", alice.ID)
	if err != nil {
		t.Fatalf("FindReferencesTo: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs to alice, got %d: %+v", len(refs), refs)
	}
	for _, r := range refs {
		if r.TableName != "posts" || r.FieldName != "author" {
			t.Errorf("unexpected ref: %+v", r)
		}
	}

	// Bob is unreferenced.
	refs, _ = data.FindReferencesTo(ctx, "people", bob.ID)
	if len(refs) != 0 {
		t.Errorf("bob is unreferenced, got %+v", refs)
	}
}

func TestDataStore_DeleteRowsByPagePath(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)
	_ = data.InsertRow(ctx, "issues", &Row{PagePath: "/del", Fields: map[string]any{"title": "a"}})
	_ = data.InsertRow(ctx, "issues", &Row{PagePath: "/del", Fields: map[string]any{"title": "b"}})
	_ = data.InsertRow(ctx, "issues", &Row{PagePath: "/keep", Fields: map[string]any{"title": "c"}})

	if err := data.DeleteRowsByPagePath(ctx, "issues", "/del"); err != nil {
		t.Fatalf("DeleteRowsByPagePath: %v", err)
	}
	_, total, _ := data.QueryRows(ctx, "issues", QueryParams{})
	if total != 1 {
		t.Errorf("after delete, total = %d, want 1", total)
	}
}

func TestDataStore_RenameRowsByPagePath(t *testing.T) {
	t.Parallel()
	schema, data, ctx := newFullStack(t)
	makeIssuesTable(t, schema)
	original := &Row{PagePath: "/before", Fields: map[string]any{"title": "t"}}
	_ = data.InsertRow(ctx, "issues", original)

	n, err := data.RenameRowsByPagePath(ctx, "issues", "/before", "/after")
	if err != nil || n != 1 {
		t.Fatalf("Rename n=%d err=%v", n, err)
	}
	got, _ := data.GetRow(ctx, "issues", original.ID)
	if got.PagePath != "/after" {
		t.Errorf("PagePath = %q, want /after", got.PagePath)
	}
	// Idempotent: no rows to rename returns 0, no error.
	n, err = data.RenameRowsByPagePath(ctx, "issues", "/nowhere", "/anywhere")
	if err != nil || n != 0 {
		t.Errorf("no-op rename n=%d err=%v", n, err)
	}
}

func TestDataStore_UnconnectedPool_ReturnsError(t *testing.T) {
	t.Parallel()
	pool := NewPool()
	schema := NewSchemaStore(pool)
	data := NewDataStore(pool, schema)
	if err := data.DeleteRow(context.Background(), "any", 1); err == nil {
		t.Errorf("expected 'not connected' error")
	}
}
