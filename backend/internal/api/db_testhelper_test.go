//go:build integration

package api

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

// dbTestPool mirrors database.newTestPool for the api package: creates a
// fresh Postgres database per test, runs migrations, registers cleanup.
// Skips when no DB is reachable so `go test ./...` (no integration tag)
// or a devbox without Postgres still works.
const defaultAPITestDSN = "postgres://gowiki:gowiki@localhost:5432/gowiki_test?sslmode=disable"

func dbTestPool(t *testing.T) *database.Pool {
	t.Helper()

	baseDSN := os.Getenv("DATABASE_URL")
	if baseDSN == "" {
		baseDSN = defaultAPITestDSN
	}

	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand: %v", err)
	}
	dbName := "gowiki_api_test_" + hex.EncodeToString(buf)

	admin := database.NewPool()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := admin.Connect(ctx, baseDSN); err != nil {
		t.Skipf("no postgres reachable at %s: %v", baseDSN, err)
	}
	if _, err := admin.GetPool().Exec(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)); err != nil {
		admin.Close()
		t.Fatalf("create db: %v", err)
	}
	admin.Close()

	scopedDSN, err := apiSwapDSNDatabase(baseDSN, dbName)
	if err != nil {
		t.Fatalf("swap dsn: %v", err)
	}
	pool := database.NewPool()
	if err := pool.Connect(ctx, scopedDSN); err != nil {
		t.Fatalf("connect scoped: %v", err)
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrations: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		cctx, cc := context.WithTimeout(context.Background(), 15*time.Second)
		defer cc()
		a := database.NewPool()
		if err := a.Connect(cctx, baseDSN); err != nil {
			return
		}
		defer a.Close()
		for i := 0; i < 3; i++ {
			if _, err := a.GetPool().Exec(cctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s" WITH (FORCE)`, dbName)); err == nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
	return pool
}

func apiSwapDSNDatabase(dsn, newDB string) (string, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return "", fmt.Errorf("only URL-shaped DSNs supported, got %q", dsn)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + newDB
	return u.String(), nil
}
