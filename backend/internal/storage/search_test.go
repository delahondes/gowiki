package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchIndex_IndexAndSearch(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "search.bleve")

	idx, err := OpenSearchIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenSearchIndex: %v", err)
	}
	defer idx.Close()

	// Index a few pages.
	if err := idx.IndexPage("docs/intro", "Introduction", "Welcome to the wiki. This is the introduction page."); err != nil {
		t.Fatalf("IndexPage: %v", err)
	}
	if err := idx.IndexPage("docs/setup", "Setup Guide", "How to install and configure the application."); err != nil {
		t.Fatalf("IndexPage: %v", err)
	}
	if err := idx.IndexPage("home", "Home", "The main landing page of the wiki."); err != nil {
		t.Fatalf("IndexPage: %v", err)
	}

	// Search for "introduction".
	results, err := idx.Search("introduction", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'introduction'")
	}
	if results[0].Path != "docs/intro" {
		t.Errorf("expected first result path 'docs/intro', got %q", results[0].Path)
	}

	// Search for "install".
	results, err = idx.Search("install", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'install'")
	}
	if results[0].Path != "docs/setup" {
		t.Errorf("expected first result path 'docs/setup', got %q", results[0].Path)
	}

	// Search for something that doesn't exist.
	results, err = idx.Search("nonexistentterm123", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected zero results, got %d", len(results))
	}
}

func TestSearchIndex_DeletePage(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "search.bleve")

	idx, err := OpenSearchIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenSearchIndex: %v", err)
	}
	defer idx.Close()

	if err := idx.IndexPage("test/page", "Test Page", "Some searchable content here."); err != nil {
		t.Fatalf("IndexPage: %v", err)
	}

	// Verify it's searchable.
	results, err := idx.Search("searchable", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Delete it.
	if err := idx.DeletePage("test/page"); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}

	// Verify it's gone.
	results, err = idx.Search("searchable", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}

func TestSearchIndex_RebuildFromDir(t *testing.T) {
	dir := t.TempDir()
	contentDir := filepath.Join(dir, "content")
	indexPath := filepath.Join(dir, "search.bleve")

	// Create some test markdown files.
	if err := os.MkdirAll(filepath.Join(contentDir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "home.md"), []byte("# Home\n\nWelcome to the wiki."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "docs", "index.md"), []byte("# Documentation\n\nAll the docs are here."), 0o644); err != nil {
		t.Fatal(err)
	}
	// Also create a non-md file that should be ignored.
	if err := os.WriteFile(filepath.Join(contentDir, "image.png"), []byte("not-a-png"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := OpenSearchIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenSearchIndex: %v", err)
	}
	defer idx.Close()

	if err := idx.RebuildFromDir(contentDir); err != nil {
		t.Fatalf("RebuildFromDir: %v", err)
	}

	// Search for "wiki".
	results, err := idx.Search("wiki", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for 'wiki'")
	}

	// Verify the namespace index was indexed with trimmed path.
	results, err = idx.Search("documentation", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected result for 'documentation'")
	}
	if results[0].Path != "/docs" {
		t.Errorf("expected path '/docs' for namespace index, got %q", results[0].Path)
	}
	if results[0].Title != "Documentation" {
		t.Errorf("expected title 'Documentation', got %q", results[0].Title)
	}
}

func TestSearchIndex_Reopen(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "search.bleve")

	// Create and index.
	idx, err := OpenSearchIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenSearchIndex: %v", err)
	}
	if err := idx.IndexPage("test", "Test", "Reopen test content"); err != nil {
		t.Fatalf("IndexPage: %v", err)
	}
	idx.Close()

	// Reopen and verify data persisted.
	idx2, err := OpenSearchIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenSearchIndex (reopen): %v", err)
	}
	defer idx2.Close()

	results, err := idx2.Search("reopen", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result after reopen, got %d", len(results))
	}
}
