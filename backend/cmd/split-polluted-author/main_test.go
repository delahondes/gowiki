package main

import "testing"

func TestSplitAuthor(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		author   string
		suffix   string
		polluted bool
	}{
		{"clean", "raynald.delahondes", "raynald.delahondes", "", false},
		{"empty", "", "", "", false},
		{"polluted", "raynald.delahondes | [AI: edit_page] change", "raynald.delahondes", "[AI: edit_page] change", true},
		{"trim whitespace", "alice |   something  ", "alice", "something", true},
		{"only first pipe splits", "bob | a | b", "bob", "a | b", true},
		{"single pipe without spaces does not split", "a|b", "a|b", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, s, p := splitAuthor(tc.in)
			if a != tc.author || s != tc.suffix || p != tc.polluted {
				t.Fatalf("got author=%q suffix=%q polluted=%v, want %q %q %v",
					a, s, p, tc.author, tc.suffix, tc.polluted)
			}
		})
	}
}
