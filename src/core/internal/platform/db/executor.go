package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Executor is what a repository receives instead of the pool, so a transaction can cross packages (regla 5).
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

var (
	_ Executor = (Pool)(nil)
	_ Executor = (pgx.Tx)(nil)
)

// InTx runs fn inside one transaction, so the caller that conducts the turn owns it and no repository does (regla 5).
func InTx(ctx context.Context, pool Pool, fn func(Executor) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("db: cannot begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: cannot commit transaction: %w", err)
	}
	return nil
}
