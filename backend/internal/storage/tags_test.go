package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
)

// TagIndex is a pure in-memory + on-disk index — no DB required. Every
// test here uses t.TempDir() and t.Parallel(). We assert on the
// externally-visible contract (Load/Save round-trip, UpdatePage
// replaces, RemovePage clears reverse-map cascades, GetPagesForTag
// filters + sort) and the mutex correctness under -race.

func newTestTagIndex(t *testing.T) *TagIndex {
	t.Helper()
	return NewTagIndex(t.TempDir())
}

func TestTagIndex_LoadMissingFile_IsEmpty(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	if err := idx.Load(); err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if len(idx.GetTagsForPage("/whatever")) != 0 {
		t.Errorf("expected empty index after Load-on-missing")
	}
}

func TestTagIndex_LoadCorruptJSON_ReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "_tags.json"), []byte("{not: json"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := NewTagIndex(dir)
	if err := idx.Load(); err == nil {
		t.Errorf("expected error on corrupt JSON, got nil")
	}
}

func TestTagIndex_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := NewTagIndex(dir)
	src.UpdatePage("/docs/intro", []string{"welcome", "overview"}, "Intro")
	src.UpdatePage("/docs/api", []string{"reference", "api"}, "API reference")
	src.UpdatePage("/blog/post", []string{"welcome"}, "Blog post")
	if err := src.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dst := NewTagIndex(dir)
	if err := dst.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Compare via GetTagsForPage so we go through the public surface.
	for _, p := range []string{"/docs/intro", "/docs/api", "/blog/post"} {
		want := sortedCopy(src.GetTagsForPage(p))
		got := sortedCopy(dst.GetTagsForPage(p))
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s tags: got %v, want %v", p, got, want)
		}
	}
	// Reverse index round-trips too.
	wantWelcome := pagePaths(src.GetPagesForTag("welcome", "", nil))
	gotWelcome := pagePaths(dst.GetPagesForTag("welcome", "", nil))
	if !reflect.DeepEqual(wantWelcome, gotWelcome) {
		t.Errorf("welcome pages: got %v, want %v", gotWelcome, wantWelcome)
	}
}

func TestTagIndex_SaveWritesJSONFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	idx := NewTagIndex(dir)
	idx.UpdatePage("/x", []string{"a"}, "X")
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "_tags.json"))
	if err != nil {
		t.Fatalf("stat _tags.json: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("_tags.json is empty")
	}
}

func TestTagIndex_Clear_EmptiesEverything(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/a", []string{"one", "two"}, "A")
	idx.UpdatePage("/b", []string{"one"}, "B")
	idx.Clear()
	if len(idx.GetTagsForPage("/a")) != 0 {
		t.Errorf("/a still has tags after Clear")
	}
	if len(idx.GetPagesForTag("one", "", nil)) != 0 {
		t.Errorf("tag \"one\" still lists pages after Clear")
	}
}

func TestTagIndex_ClearThenSave_PersistsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	idx := NewTagIndex(dir)
	idx.UpdatePage("/a", []string{"x"}, "A")
	if err := idx.Save(); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	idx.Clear()
	if err := idx.Save(); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	// Fresh load: the empty state must have overwritten the earlier state.
	reload := NewTagIndex(dir)
	if err := reload.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reload.GetTagsForPage("/a")) != 0 {
		t.Errorf("/a still has tags after Clear+Save+reload")
	}
}

func TestTagIndex_UpdatePage_ReplacesTagsNotAccumulates(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/p", []string{"a", "b"}, "P")
	idx.UpdatePage("/p", []string{"c"}, "P")

	got := idx.GetTagsForPage("/p")
	if !reflect.DeepEqual(got, []string{"c"}) {
		t.Errorf("tags = %v, want [c] — UpdatePage must REPLACE, not accumulate", got)
	}
	// The dropped tags should no longer list /p.
	for _, gone := range []string{"a", "b"} {
		if pages := idx.GetPagesForTag(gone, "", nil); len(pages) != 0 {
			t.Errorf("tag %q still lists pages after replacement: %v", gone, pages)
		}
	}
}

func TestTagIndex_UpdatePage_EmptyTagsRemovesPage(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/p", []string{"a"}, "P")
	idx.UpdatePage("/p", nil, "P")
	if got := idx.GetTagsForPage("/p"); len(got) != 0 {
		t.Errorf("expected /p to have no tags, got %v", got)
	}
	if pages := idx.GetPagesForTag("a", "", nil); len(pages) != 0 {
		t.Errorf("tag a still lists /p: %v", pages)
	}
}

func TestTagIndex_UpdatePage_TitleTracking(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/p", []string{"a"}, "Original title")
	if got := titleFor(idx, "a", "/p"); got != "Original title" {
		t.Errorf("title = %q, want Original title", got)
	}
	idx.UpdatePage("/p", []string{"a"}, "New title")
	if got := titleFor(idx, "a", "/p"); got != "New title" {
		t.Errorf("title after re-update = %q, want New title", got)
	}
	// Empty title deletes the entry, and GetPagesForTag then falls back
	// to the path as the title (documented default in the impl).
	idx.UpdatePage("/p", []string{"a"}, "")
	if got := titleFor(idx, "a", "/p"); got != "/p" {
		t.Errorf("empty title fallback = %q, want /p", got)
	}
}

func TestTagIndex_RemovePage_CascadesToReverseIndex(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/a", []string{"shared", "onlyA"}, "A")
	idx.UpdatePage("/b", []string{"shared"}, "B")

	idx.RemovePage("/a")

	if got := idx.GetTagsForPage("/a"); len(got) != 0 {
		t.Errorf("/a still has tags after RemovePage: %v", got)
	}
	// Tag with a surviving page keeps that page.
	if got := pagePaths(idx.GetPagesForTag("shared", "", nil)); !reflect.DeepEqual(got, []string{"/b"}) {
		t.Errorf("shared pages = %v, want [/b]", got)
	}
	// Tag whose only page was removed disappears from the reverse index.
	if got := idx.GetPagesForTag("onlyA", "", nil); len(got) != 0 {
		t.Errorf("onlyA still lists pages after its only holder was removed: %v", got)
	}
}

func TestTagIndex_GetTagsForPage_ReturnsCopyNotSlice(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/p", []string{"a", "b"}, "P")
	got := idx.GetTagsForPage("/p")
	got[0] = "mutated"
	// A second read must return the untouched original — the returned
	// slice must not alias internal state.
	fresh := idx.GetTagsForPage("/p")
	if fresh[0] == "mutated" {
		t.Errorf("GetTagsForPage aliases internal storage; caller mutation leaked back")
	}
}

func TestTagIndex_GetPagesForTag_PathPrefixFilter(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/docs/a", []string{"t"}, "A")
	idx.UpdatePage("/docs/b", []string{"t"}, "B")
	idx.UpdatePage("/blog/c", []string{"t"}, "C")

	// Matching prefix.
	got := pagePaths(idx.GetPagesForTag("t", "/docs", nil))
	if !reflect.DeepEqual(got, []string{"/docs/a", "/docs/b"}) {
		t.Errorf("prefix /docs = %v, want [/docs/a /docs/b]", got)
	}
	// Trailing slash is normalised away.
	got = pagePaths(idx.GetPagesForTag("t", "/docs/", nil))
	if !reflect.DeepEqual(got, []string{"/docs/a", "/docs/b"}) {
		t.Errorf("prefix /docs/ = %v, want [/docs/a /docs/b]", got)
	}
	// Missing leading slash is accepted.
	got = pagePaths(idx.GetPagesForTag("t", "docs", nil))
	if !reflect.DeepEqual(got, []string{"/docs/a", "/docs/b"}) {
		t.Errorf("prefix docs (no leading /) = %v, want [/docs/a /docs/b]", got)
	}
	// Non-matching prefix returns empty.
	if got := idx.GetPagesForTag("t", "/nowhere", nil); len(got) != 0 {
		t.Errorf("prefix /nowhere = %v, want empty", got)
	}
	// Empty prefix returns all.
	got = pagePaths(idx.GetPagesForTag("t", "", nil))
	if !reflect.DeepEqual(got, []string{"/blog/c", "/docs/a", "/docs/b"}) {
		t.Errorf("empty prefix = %v, want all three sorted", got)
	}
}

func TestTagIndex_GetPagesForTag_ExcludeTagsFilter(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/a", []string{"topic", "draft"}, "A")
	idx.UpdatePage("/b", []string{"topic"}, "B")
	idx.UpdatePage("/c", []string{"topic", "archived"}, "C")

	got := pagePaths(idx.GetPagesForTag("topic", "", []string{"draft"}))
	if !reflect.DeepEqual(got, []string{"/b", "/c"}) {
		t.Errorf("exclude draft = %v, want [/b /c]", got)
	}
	// Multiple excludes drop pages carrying ANY of them.
	got = pagePaths(idx.GetPagesForTag("topic", "", []string{"draft", "archived"}))
	if !reflect.DeepEqual(got, []string{"/b"}) {
		t.Errorf("exclude draft+archived = %v, want [/b]", got)
	}
	// Empty excludeTags is a no-op.
	got = pagePaths(idx.GetPagesForTag("topic", "", nil))
	if !reflect.DeepEqual(got, []string{"/a", "/b", "/c"}) {
		t.Errorf("no excludes = %v, want all three sorted", got)
	}
}

func TestTagIndex_GetPagesForTag_UnknownTagReturnsEmpty(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	idx.UpdatePage("/p", []string{"a"}, "P")
	if got := idx.GetPagesForTag("nonexistent", "", nil); len(got) != 0 {
		t.Errorf("unknown tag returned %v, want empty", got)
	}
}

func TestTagIndex_GetPagesForTag_TitleFallbackToPath(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	// No title supplied → fallback is the path itself.
	idx.UpdatePage("/p", []string{"a"}, "")
	entries := idx.GetPagesForTag("a", "", nil)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Title != "/p" {
		t.Errorf("title = %q, want /p (path fallback)", entries[0].Title)
	}
	// With a title, that title comes through.
	idx.UpdatePage("/p", []string{"a"}, "P Title")
	entries = idx.GetPagesForTag("a", "", nil)
	if entries[0].Title != "P Title" {
		t.Errorf("title = %q, want P Title", entries[0].Title)
	}
}

// The mutex must survive concurrent readers and writers on distinct
// pages. Run with `-race` in CI. Any data race here would surface as a
// failure — the assertion is process survival.
func TestTagIndex_Concurrent_UpdateAndQuery(t *testing.T) {
	t.Parallel()
	idx := newTestTagIndex(t)
	const n = 8
	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				page := fmt.Sprintf("/w%d/%d", i, j)
				idx.UpdatePage(page, []string{"shared", fmt.Sprintf("t%d", i)}, "")
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = idx.GetPagesForTag("shared", "", nil)
				_ = idx.GetTagsForPage(fmt.Sprintf("/w%d/%d", i, j))
			}
		}()
	}
	wg.Wait()

	// Sanity: after all writers finish, every /wI/J page (0..49 per
	// writer) carries the "shared" tag.
	pages := idx.GetPagesForTag("shared", "", nil)
	if len(pages) != n*50 {
		t.Errorf("shared pages = %d, want %d", len(pages), n*50)
	}
}

// ─── helpers ─────────────────────────────────────────────

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func pagePaths(entries []PageEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out
}

func titleFor(idx *TagIndex, tag, path string) string {
	for _, e := range idx.GetPagesForTag(tag, "", nil) {
		if e.Path == path {
			return e.Title
		}
	}
	return ""
}
