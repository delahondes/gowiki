package database

import (
	"context"
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
