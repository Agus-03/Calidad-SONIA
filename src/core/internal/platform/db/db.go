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

// Open builds the pool and pings it: an unverified pool fails on the first customer message.
func Open(ctx context.Context, databaseURL string) (Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db: cannot parse DATABASE_URL: %w", err)
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
