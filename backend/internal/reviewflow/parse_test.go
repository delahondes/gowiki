package reviewflow

import (
	"reflect"
	"testing"
)

func TestParseDirective_NoDirective(t *testing.T) {
	t.Parallel()
	if d, ok := ParseDirective("no directive here\n"); ok || d != nil {
		t.Errorf("expected nil,false; got %+v,%v", d, ok)
	}
}

func TestParseDirective_RolesInSourceOrder(t *testing.T) {
	t.Parallel()
	d, ok := ParseDirective("{reviewflow author=alice reviewer=bob validator=cathy}\n")
	if !ok {
		t.Fatal("expected found=true")
	}
	if !reflect.DeepEqual(d.RoleOrder, []string{"author", "reviewer", "validator"}) {
		t.Errorf("RoleOrder = %v, want [author reviewer validator]", d.RoleOrder)
	}
	if d.Roles["author"] != "alice" || d.Roles["reviewer"] != "bob" || d.Roles["validator"] != "cathy" {
		t.Errorf("Roles = %+v", d.Roles)
	}
	if d.Parallel {
		t.Errorf("Parallel default should be false")
	}
	if d.VersionTag != "" {
		t.Errorf("VersionTag = %q, want empty", d.VersionTag)
	}
}

func TestParseDirective_RoleOrderPreservedNonAlphabetical(t *testing.T) {
	t.Parallel()
	// Roles declared in reverse-alphabetical source order — RoleOrder must
	// match the source, NOT Go's random map iteration nor sort order.
	d, ok := ParseDirective("{reviewflow validator=v reviewer=r author=a}\n")
	if !ok {
		t.Fatal("expected found=true")
	}
	if !reflect.DeepEqual(d.RoleOrder, []string{"validator", "reviewer", "author"}) {
		t.Errorf("RoleOrder = %v, want [validator reviewer author]", d.RoleOrder)
	}
}

func TestParseDirective_ParallelFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  string
		want bool
	}{
		{"{reviewflow author=a reviewer=b parallel=true}\n", true},
		{"{reviewflow author=a reviewer=b parallel=1}\n", true},
		{"{reviewflow author=a reviewer=b parallel=yes}\n", true},
		{"{reviewflow author=a reviewer=b parallel=TRUE}\n", true},
		{"{reviewflow author=a reviewer=b parallel=false}\n", false},
		{"{reviewflow author=a reviewer=b parallel=no}\n", false},
		{"{reviewflow author=a reviewer=b}\n", false},
	}
	for _, tc := range cases {
		d, ok := ParseDirective(tc.src)
		if !ok {
			t.Errorf("expected found=true for %q", tc.src)
			continue
		}
		if d.Parallel != tc.want {
			t.Errorf("Parallel = %v for %q, want %v", d.Parallel, tc.src, tc.want)
		}
		// The parallel key must NOT bleed into Roles or RoleOrder.
		if _, present := d.Roles["parallel"]; present {
			t.Errorf("parallel key leaked into Roles for %q", tc.src)
		}
		for _, r := range d.RoleOrder {
			if r == "parallel" {
				t.Errorf("parallel key leaked into RoleOrder for %q", tc.src)
			}
		}
	}
}

func TestParseDirective_VersionTagExtracted(t *testing.T) {
	t.Parallel()
	d, ok := ParseDirective("{reviewflow author=a version=v1.2 reviewer=b}\n")
	if !ok {
		t.Fatal("expected found=true")
	}
	if d.VersionTag != "v1.2" {
		t.Errorf("VersionTag = %q, want v1.2", d.VersionTag)
	}
	if !reflect.DeepEqual(d.RoleOrder, []string{"author", "reviewer"}) {
		t.Errorf("RoleOrder should skip 'version', got %v", d.RoleOrder)
	}
	if _, present := d.Roles["version"]; present {
		t.Errorf("version key leaked into Roles")
	}
}

func TestParseDirective_QuotedValueWithSpace(t *testing.T) {
	t.Parallel()
	d, ok := ParseDirective(`{reviewflow author="Alice Smith" reviewer=b}` + "\n")
	if !ok {
		t.Fatal("expected found=true")
	}
	if d.Roles["author"] != "Alice Smith" {
		t.Errorf("Roles[author] = %q, want 'Alice Smith'", d.Roles["author"])
	}
}

func TestParseDirective_NoRolesReturnsNotFound(t *testing.T) {
	t.Parallel()
	// A `version=...` alone (no roles) is not a valid directive.
	if d, ok := ParseDirective("{reviewflow version=v1}\n"); ok || d != nil {
		t.Errorf("directive with no roles should be found=false, got %+v,%v", d, ok)
	}
}
