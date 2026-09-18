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
