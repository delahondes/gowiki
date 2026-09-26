//go:build integration

package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"
)

// The integration-tagged test helper hands each test its own isolated
// Postgres schema. Every test starts against a freshly-migrated, empty
// schema; concurrent tests do not step on each other because the schema
// name carries per-test entropy. Cleanup drops the schema so a run that
// crashes half-way leaves at most one leftover per test.
//
// Run locally:
//
//	DATABASE_URL="postgres://gowiki:gowiki@localhost:5432/gowiki_test?sslmode=disable" \
//	  go test -tags=integration ./internal/database/...
//
// CI wires the same DATABASE_URL via the postgres service container.

const defaultTestDSN = "postgres://gowiki:gowiki@localhost:5432/gowiki_test?sslmode=disable"

// newTestPool returns a *Pool whose search_path points at a fresh schema
// created for this test, with all migrations applied. Registers a cleanup
// that drops the schema and closes the pool.
func newTestPool(t *testing.T) *Pool {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	// Random 8-hex-char schema name — lowercase to survive Postgres's
	// unquoted identifier folding.
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	schemaName := "gowiki_test_" + hex.EncodeToString(buf)

	// Connect once with the default search_path to create the schema.
	bootstrap := NewPool()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := bootstrap.Connect(ctx, dsn); err != nil {
		t.Skipf("no postgres reachable at %s (set DATABASE_URL for local runs): %v", dsn, err)
	}
	if _, err := bootstrap.GetPool().Exec(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		bootstrap.Close()
		t.Fatalf("create test schema: %v", err)
	}
	bootstrap.Close()

	// Reconnect pointing search_path at the isolated schema.
	scopedDSN := dsn
	if containsQuery(dsn) {
		scopedDSN += "&search_path=" + schemaName
	} else {
		scopedDSN += "?search_path=" + schemaName
	}
	pool := NewPool()
	if err := pool.Connect(ctx, scopedDSN); err != nil {
		t.Fatalf("connect scoped pool: %v", err)
	}

	if err := RunMigrations(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("run migrations: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		// Best-effort DROP — a leftover schema is a nuisance but not
		// a correctness problem, so failures only log rather than fail
		// the test.
		if p := pool.GetPool(); p != nil {
			_, _ = p.Exec(cleanCtx, fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
		}
		pool.Close()
	})

	return pool
}

// containsQuery reports whether a DSN already has a `?query=…` component.
// pgx accepts both URL-shaped and key=value DSNs; the tests use the URL
// form, so a naive `?` check is enough.
func containsQuery(dsn string) bool {
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '?' {
			return true
		}
	}
	return false
}
