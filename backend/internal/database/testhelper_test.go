//go:build integration

package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// The integration-tagged test helper hands each test its own isolated
// Postgres database. Every test starts against a freshly-created,
// migrated DB; concurrent tests do not step on each other because the
// DB name carries per-test entropy. Cleanup drops the DB so a run that
// crashes half-way leaves at most one leftover per test.
//
// Per-database isolation (rather than per-schema) was chosen because pgx
// does not reliably persist `search_path` from a URL query across pool
// connections — attempting to run tests in isolated schemas resulted in
// parallel tests all writing to `public` and colliding on unique
// constraints. A fresh database gives each test its own `public` schema
// with no search-path gymnastics required.
//
// Run locally:
//
//	docker run --rm -d -p 55432:5432 -e POSTGRES_USER=gowiki \
//	  -e POSTGRES_PASSWORD=gowiki -e POSTGRES_DB=gowiki_test postgres:16-alpine
//	DATABASE_URL="postgres://gowiki:gowiki@localhost:55432/gowiki_test?sslmode=disable" \
//	  make test-integration
//
// CI wires DATABASE_URL via the postgres service container at
// localhost:5432 (no other Postgres to shadow it there).

const defaultTestDSN = "postgres://gowiki:gowiki@localhost:5432/gowiki_test?sslmode=disable"

// newTestPool returns a *Pool connected to a freshly-created, migrated
// database. Registers a cleanup that closes the pool and drops the DB.
func newTestPool(t *testing.T) *Pool {
	t.Helper()

	baseDSN := os.Getenv("DATABASE_URL")
	if baseDSN == "" {
		baseDSN = defaultTestDSN
	}

	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	dbName := "gowiki_test_" + hex.EncodeToString(buf)

	// Bootstrap connection to the DSN-named DB (typically gowiki_test).
	// Used to issue CREATE DATABASE and, on cleanup, DROP DATABASE.
	adminPool := NewPool()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := adminPool.Connect(ctx, baseDSN); err != nil {
		t.Skipf("no postgres reachable at %s (set DATABASE_URL for local runs): %v", baseDSN, err)
	}
	if _, err := adminPool.GetPool().Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(dbName))); err != nil {
		adminPool.Close()
		t.Fatalf("create test database: %v", err)
	}
	adminPool.Close()

	// Connect to the freshly-created DB.
	scopedDSN, err := swapDSNDatabase(baseDSN, dbName)
	if err != nil {
		t.Fatalf("swap DSN db: %v", err)
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
		pool.Close()
		// Drop the DB from a fresh admin connection — you cannot drop
		// a DB that has active connections, so the pool must close first.
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		admin := NewPool()
		if err := admin.Connect(cleanCtx, baseDSN); err != nil {
			return // best-effort — a leftover DB is a nuisance not a fail
		}
		defer admin.Close()
		// A pgxpool that just closed can hold reservations for a moment;
		// try a couple of times before giving up.
		var lastErr error
		for i := 0; i < 3; i++ {
			if _, err := admin.GetPool().Exec(cleanCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(dbName))); err == nil {
				return
			} else {
				lastErr = err
				time.Sleep(200 * time.Millisecond)
			}
		}
		_ = lastErr // silent — leftover is a nuisance, not a correctness bug
	})

	return pool
}

// swapDSNDatabase rewrites a URL-shaped DSN's path segment to newDBName.
// Preserves user/pass, host, and query params. Returns an error for
// non-URL DSNs (e.g. key=value form) — those aren't used in tests.
func swapDSNDatabase(dsn, newDBName string) (string, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return "", fmt.Errorf("only URL-shaped DSNs are supported in tests, got %q", dsn)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + newDBName
	return u.String(), nil
}
