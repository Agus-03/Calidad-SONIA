// Package db opens the pool against the shared PostgreSQL (ADR-0017).
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the type the repositories receive.
type Pool = *pgxpool.Pool

// Options carries only what the pool needs, so this package never imports the application config.
type Options struct {
	DatabaseURL string
	MinSize     int
	MaxSize     int
}

// Open builds the pool and pings it: an unverified pool fails on the first customer message.
func Open(ctx context.Context, opts Options) (Pool, error) {
	cfg, err := pgxpool.ParseConfig(opts.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: cannot parse DATABASE_URL: %w", err)
	}

	if opts.MinSize > 0 {
		cfg.MinConns = int32(opts.MinSize)
	}
	if opts.MaxSize > 0 {
		cfg.MaxConns = int32(opts.MaxSize)
	}

	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: cannot create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: cannot reach PostgreSQL: %w", err)
	}
	return pool, nil
}
