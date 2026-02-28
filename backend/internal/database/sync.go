package database

import (
	"context"
	"log"
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

// SyncPageRows extracts {database-row ...} blocks from markdown and upserts them.
func (ds *DatabaseSync) SyncPageRows(pagePath, markdownContent string) {
	if ds.schemaStore == nil || ds.dataStore == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	blocks := markdown.ExtractDatabaseRows(markdownContent)

	// Track which tables were seen so we can clean up removed rows.
	seenTables := make(map[string]bool)

	for _, block := range blocks {
		seenTables[block.TableName] = true

		// Validate that the table exists.
		_, err := ds.schemaStore.GetTableByName(ctx, block.TableName)
		if err != nil {
			log.Printf("database sync: table %q not found for page %s: %v", block.TableName, pagePath, err)
			continue
		}

		// Convert string fields to any.
		fields := make(map[string]any, len(block.Fields))
		for k, v := range block.Fields {
			fields[k] = v
		}

		if _, err := ds.dataStore.UpsertPageRow(ctx, block.TableName, pagePath, fields); err != nil {
			log.Printf("database sync: upsert failed for page %s, table %s: %v", pagePath, block.TableName, err)
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
