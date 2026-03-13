package database

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps a pgxpool.Pool with thread-safe connection management.
type Pool struct {
	mu   sync.RWMutex
	pool *pgxpool.Pool
	dsn  string
}

// NewPool creates a new unconnected Pool.
func NewPool() *Pool {
	return &Pool{}
}

// Connect establishes a connection to the database using the given DSN.
// If already connected, closes the existing pool first.
func (p *Pool) Connect(ctx context.Context, dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	// Ensure search_path includes public — needed after DROP/CREATE SCHEMA public.
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if _, ok := cfg.ConnConfig.RuntimeParams["search_path"]; !ok {
		cfg.ConnConfig.RuntimeParams["search_path"] = "public"
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("ping: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pool != nil {
		p.pool.Close()
	}
	p.pool = pool
	p.dsn = dsn
	return nil
}

// TestConnection tests a DSN without saving it.
func TestConnection(ctx context.Context, dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// Ping checks the database connection.
func (p *Pool) Ping(ctx context.Context) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.pool == nil {
		return fmt.Errorf("not connected")
	}
	return p.pool.Ping(ctx)
}

// Close closes the connection pool.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.pool != nil {
		p.pool.Close()
		p.pool = nil
	}
}

// IsConnected returns whether the pool has an active connection.
func (p *Pool) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pool != nil
}

// Acquire returns a connection from the pool.
func (p *Pool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.pool == nil {
		return nil, fmt.Errorf("database not connected")
	}
	return p.pool.Acquire(ctx)
}

// GetPool returns the underlying pgxpool.Pool. Returns nil if not connected.
func (p *Pool) GetPool() *pgxpool.Pool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pool
}
