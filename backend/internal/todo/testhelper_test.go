//go:build integration

package todo

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

	"gowiki/backend/internal/database"
)

// Per-test isolated Postgres database. Mirrors the helper in
// internal/database/testhelper_test.go — that one lives in _test.go so
// isn't importable from another package, hence the duplication.
//
// Runs locally with:
//
//	docker run --rm -d -p 55432:5432 -e POSTGRES_USER=gowiki \
//	  -e POSTGRES_PASSWORD=gowiki -e POSTGRES_DB=gowiki_test postgres:16-alpine
//	DATABASE_URL="postgres://gowiki:gowiki@localhost:55432/gowiki_test?sslmode=disable" \
//	  make test-integration

const defaultTestDSN = "postgres://gowiki:gowiki@localhost:5432/gowiki_test?sslmode=disable"

func newTestPool(t *testing.T) *database.Pool {
	t.Helper()

	baseDSN := os.Getenv("DATABASE_URL")
	if baseDSN == "" {
		baseDSN = defaultTestDSN
	}

	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	dbName := "gowiki_todo_test_" + hex.EncodeToString(buf)

	adminPool := database.NewPool()
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

	scopedDSN, err := swapDSNDatabase(baseDSN, dbName)
	if err != nil {
		t.Fatalf("swap DSN db: %v", err)
	}
	pool := database.NewPool()
	if err := pool.Connect(ctx, scopedDSN); err != nil {
		t.Fatalf("connect scoped pool: %v", err)
	}

	// Todo tests need the todo tables — not the database plugin tables.
	if err := RunMigrations(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("run migrations: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanCancel()
		admin := database.NewPool()
		if err := admin.Connect(cleanCtx, baseDSN); err != nil {
			return
		}
		defer admin.Close()
		for i := 0; i < 3; i++ {
			if _, err := admin.GetPool().Exec(cleanCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(dbName))); err == nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	})

	return pool
}

func swapDSNDatabase(dsn, newDBName string) (string, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return "", fmt.Errorf("only URL-shaped DSNs supported, got %q", dsn)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + newDBName
	return u.String(), nil
}

func quoteIdent(name string) string {
	// Postgres quoted identifier: wrap in " and double any interior ".
	// Our test DB names are hex chars, so the escape is defensive.
	return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
}
