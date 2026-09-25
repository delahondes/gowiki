package database

import (
	"reflect"
	"testing"
	"time"
)

func TestDedupPreserveOrder(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{}, []string{}},
		{[]string{"a"}, []string{"a"}},
		{[]string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{[]string{"", "", "x"}, []string{"", "x"}},
	}
	for _, tc := range cases {
		got := dedupPreserveOrder(tc.in)
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("dedupPreserveOrder(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestStringifyFieldValue(t *testing.T) {
	tz := time.FixedZone("UTC", 0)
	d := time.Date(2026, 3, 15, 12, 34, 56, 0, tz)
	cases := []struct {
		v         any
		fieldType string
		want      string
	}{
		{nil, FieldTypeText, ""},
		{"hello", FieldTypeText, "hello"},
		{int64(42), FieldTypeInteger, "42"},
		{d, FieldTypeDate, "2026-03-15"},
		{d, FieldTypeDatetime, "2026-03-15T12:34:56Z"},
		{"2026-03-15T00:00:00Z", FieldTypeDate, "2026-03-15"},
		{[]string{"a", "b"}, FieldTypeMultiEnum, "a, b"},
		{[]any{"x", "y"}, FieldTypeMultiEnum, "x, y"},
	}
	for _, tc := range cases {
		got := stringifyFieldValue(tc.v, tc.fieldType)
		if got != tc.want {
			t.Errorf("stringifyFieldValue(%v, %q) = %q, want %q", tc.v, tc.fieldType, got, tc.want)
		}
	}
}

func TestSortAxis_LookupPrefersLabel(t *testing.T) {
	// Two lookup entries whose ids sort differently from their labels; label
	// ordering should win because that's what the reader sees.
	values := []PivotAxisValue{
		{Key: "10", Label: "alpha"},
		{Key: "2", Label: "beta"},
	}
	sortAxis(values, FieldTypeLookup, "same_axis", "same_axis", "asc")
	if values[0].Label != "alpha" || values[1].Label != "beta" {
		t.Fatalf("expected alpha before beta, got %v", values)
	}
	sortAxis(values, FieldTypeLookup, "same_axis", "same_axis", "desc")
	if values[0].Label != "beta" || values[1].Label != "alpha" {
		t.Fatalf("expected desc beta before alpha, got %v", values)
	}
}

func TestSortAxis_NumericByKey(t *testing.T) {
	values := []PivotAxisValue{
		{Key: "10", Label: "10"},
		{Key: "2", Label: "2"},
	}
	sortAxis(values, FieldTypeInteger, "same_axis", "same_axis", "asc")
	if values[0].Key != "2" || values[1].Key != "10" {
		t.Fatalf("expected numeric ascending 2 then 10, got %v", values)
	}
}

func TestApplyAxisLabels_NullFallback(t *testing.T) {
	values := []PivotAxisValue{
		{Key: "Y", Label: "Y"},
		{Key: PivotNullKey, Label: PivotNullKey},
	}
	// No caller overrides → the @null bucket falls back to "(empty)".
	m := applyAxisLabels(values, nil)
	if m["Y"].Label != "Y" {
		t.Errorf("Y label: got %q, want Y", m["Y"].Label)
	}
	if m[PivotNullKey].Label != pivotNullDefaultLabel {
		t.Errorf("@null default label: got %q, want %q", m[PivotNullKey].Label, pivotNullDefaultLabel)
	}
}

func TestApplyAxisLabels_MergesSynonyms(t *testing.T) {
	values := []PivotAxisValue{
		{Key: "Y", Label: "Y"},
		{Key: "N", Label: "N"},
		{Key: PivotNullKey, Label: PivotNullKey},
	}
	m := applyAxisLabels(values, map[string]string{
		"Y":          "Archived",
		"N":          "Active",
		PivotNullKey: "Active",
	})
	if m["Y"].Key != "Archived" {
		t.Errorf("Y → Archived, got %q", m["Y"].Key)
	}
	if m["N"].Key != "Active" || m[PivotNullKey].Key != "Active" {
		t.Errorf("N and @null should both map to Active, got %q and %q", m["N"].Key, m[PivotNullKey].Key)
	}
}

func TestUniqueMappedKeys_Merging(t *testing.T) {
	raw := []string{"Y", "N", PivotNullKey}
	mapping := map[string]axisMapping{
		"Y":          {Key: "Archived", Label: "Archived"},
		"N":          {Key: "Active", Label: "Active"},
		PivotNullKey: {Key: "Active", Label: "Active"},
	}
	got := uniqueMappedKeys(raw, mapping)
	// Y unique, N & @null both merged into Active — 2 distinct groups.
	if len(got) != 2 {
		t.Fatalf("expected 2 unique mapped keys, got %d: %v", len(got), got)
	}
	if got[0] != "Archived" || got[1] != "Active" {
		t.Errorf("encounter order preserved: got %v", got)
	}
}

func TestAxisValuesFromMap_ClearsPagePathOnMerge(t *testing.T) {
	rawValues := []PivotAxisValue{
		{Key: "1", Label: "One", PagePath: "/one"},
		{Key: "2", Label: "Two", PagePath: "/two"},
	}
	mapping := map[string]axisMapping{
		"1": {Key: "Group", Label: "Group"},
		"2": {Key: "Group", Label: "Group"},
	}
	got := axisValuesFromMap([]string{"Group"}, mapping, rawValues)
	if len(got) != 1 {
		t.Fatalf("expected 1 merged axis value, got %d", len(got))
	}
	// Merge collapses two page-bound rows into one axis entry — page_path
	// would be ambiguous, so it must be cleared.
	if got[0].PagePath != "" {
		t.Errorf("expected page_path cleared on merge, got %q", got[0].PagePath)
	}
}

func TestAxisValuesFromMap_KeepsPagePathWhenUnmerged(t *testing.T) {
	rawValues := []PivotAxisValue{
		{Key: "1", Label: "One", PagePath: "/one"},
	}
	mapping := map[string]axisMapping{
		"1": {Key: "One", Label: "One"},
	}
	got := axisValuesFromMap([]string{"One"}, mapping, rawValues)
	if len(got) != 1 || got[0].PagePath != "/one" {
		t.Errorf("single-source axis entry should keep its page_path, got %+v", got)
	}
}
