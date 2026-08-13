package database

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gowiki/backend/internal/markdown"
)

// DatabaseSync handles synchronization between page content and database rows.
type DatabaseSync struct {
	schemaStore *SchemaStore
	dataStore   *DataStore
}

// NewDatabaseSync creates a new DatabaseSync.
func NewDatabaseSync(schemaStore *SchemaStore, dataStore *DataStore) *DatabaseSync {
	return &DatabaseSync{
		schemaStore: schemaStore,
		dataStore:   dataStore,
	}
}

// ValidatePageContent checks that system columns in {database-row} blocks
// haven't been tampered with. Returns an error if the id was changed.
func (ds *DatabaseSync) ValidatePageContent(pagePath, markdownContent string) error {
	if ds.schemaStore == nil || ds.dataStore == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	blocks := markdown.ExtractDatabaseRows(markdownContent)
	for _, block := range blocks {
		userID, hasID := block.Fields["id"]
		if !hasID || userID == "" {
			continue
		}

		// Look up the actual row for this page.
		lookupPath := pagePath
		if !strings.HasPrefix(lookupPath, "/") {
			lookupPath = "/" + lookupPath
		}
		row, err := ds.dataStore.GetRowByPagePath(ctx, block.TableName, lookupPath)
		if err != nil || row == nil {
			continue // new page, no row yet — id will be ignored anyway
		}

		// Compare the user-supplied id with the actual id.
		actualID := fmt.Sprintf("%d", row.ID)
		if strings.TrimSpace(userID) != actualID {
			return fmt.Errorf("cannot change the id field (expected %s, got %s)", actualID, strings.TrimSpace(userID))
		}
	}
	return nil
}

// SyncPageRows syncs page data to the database in two ways:
// 1. Explicit: extracts {database-row ...} blocks from markdown and upserts them.
// 2. Page-bound: if a table has page_folder set and the page is in that folder,
//
//	auto-creates/updates a row even without a {database-row} block.
func (ds *DatabaseSync) SyncPageRows(pagePath, markdownContent string) {
	if ds.schemaStore == nil || ds.dataStore == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// --- Explicit {database-row} blocks ---
	blocks := markdown.ExtractDatabaseRows(markdownContent)
	seenTables := make(map[string]bool)

	for _, block := range blocks {
		seenTables[block.TableName] = true

		_, err := ds.schemaStore.GetTableByName(ctx, block.TableName)
		if err != nil {
			log.Printf("database sync: table %q not found for page %s: %v", block.TableName, pagePath, err)
			continue
		}

		fields := make(map[string]any, len(block.Fields))
		for k, v := range block.Fields {
			// System columns are read-only — never accept user-supplied values.
			if k == "id" || k == "page_path" || k == "created_at" || k == "updated_at" {
				continue
			}
			fields[k] = v
		}

		if _, err := ds.dataStore.UpsertPageRow(ctx, block.TableName, pagePath, fields); err != nil {
			log.Printf("database sync: upsert failed for page %s, table %s: %v", pagePath, block.TableName, err)
		}
	}

	// --- Page-folder-bound tables ---
	tables, err := ds.schemaStore.ListTables(ctx)
	if err != nil {
		log.Printf("database sync: list tables failed: %v", err)
		return
	}

	for _, t := range tables {
		if t.PageFolder == "" {
			continue
		}
		if seenTables[t.Name] {
			continue // already handled by explicit block
		}
		if !pageIsInFolder(pagePath, t.PageFolder) {
			continue
		}

		// Page is in this table's folder — ensure a row exists.
		// Upsert with empty fields (preserves existing data, creates row if new).
		if _, err := ds.dataStore.UpsertPageRow(ctx, t.Name, pagePath, nil); err != nil {
			log.Printf("database sync: page-folder upsert failed for page %s, table %s: %v", pagePath, t.Name, err)
		}
	}
}

// RemovePageRows removes all database rows associated with a page.
func (ds *DatabaseSync) RemovePageRows(pagePath string) {
	if ds.schemaStore == nil || ds.dataStore == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tables, err := ds.schemaStore.ListTables(ctx)
	if err != nil {
		log.Printf("database sync: list tables failed: %v", err)
		return
	}

	for _, t := range tables {
		if err := ds.dataStore.DeleteRowsByPagePath(ctx, t.Name, pagePath); err != nil {
			log.Printf("database sync: delete rows failed for page %s, table %s: %v", pagePath, t.Name, err)
		}
	}
}

// RenamePageRows updates the page_path column across all tables that currently
// reference oldPath, setting them to newPath. Preserves each row's id — use
// this on page rename in place of RemovePageRows + SyncPageRows so that stable
// references to the row (numeric id, foreign keys) remain valid.
//
// SyncPageRows should still be called after this to apply any field-level
// content changes carried by the rebased markdown.
func (ds *DatabaseSync) RenamePageRows(oldPath, newPath string) {
	if ds.schemaStore == nil || ds.dataStore == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tables, err := ds.schemaStore.ListTables(ctx)
	if err != nil {
		log.Printf("database sync: list tables failed: %v", err)
		return
	}

	for _, t := range tables {
		n, err := ds.dataStore.RenameRowsByPagePath(ctx, t.Name, oldPath, newPath)
		if err != nil {
			log.Printf("database sync: rename rows failed for %s → %s in table %s: %v", oldPath, newPath, t.Name, err)
			continue
		}
		if n > 0 {
			log.Printf("database sync: renamed %d row(s) in table %s: %s → %s", n, t.Name, oldPath, newPath)
		}
	}
}

// pageIsInFolder checks if a page path is inside a folder.
// e.g. pagePath="deviations/dev001", folder="deviations" → true
// e.g. pagePath="deviations/sub/page", folder="deviations" → true
// e.g. pagePath="other/page", folder="deviations" → false
func pageIsInFolder(pagePath, folder string) bool {
	// Ensure folder has leading slash and no trailing slash, matching /-prefixed pagePath.
	if len(folder) > 0 && folder[0] != '/' {
		folder = "/" + folder
	}
	folder = strings.TrimSuffix(folder, "/")
	if folder == "" || folder == "/" {
		return true // empty folder matches everything
	}
	return pagePath == folder || strings.HasPrefix(pagePath, folder+"/")
}
