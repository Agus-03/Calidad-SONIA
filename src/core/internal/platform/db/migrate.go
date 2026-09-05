package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Arbitrary, and only has to be the same number in every core: two boots at once must not apply the same file twice.
const migrationLockID = 8110271

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    varchar     PRIMARY KEY,
    checksum   varchar     NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`

// Migrate applies what is missing and returns what it applied, so the caller logs it instead of guessing from the database.
func Migrate(ctx context.Context, pool Pool, fsys fs.FS) ([]string, error) {
	names, err := migrationNames(fsys)
	if err != nil {
		return nil, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: cannot acquire a connection to migrate: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return nil, fmt.Errorf("db: cannot take the migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	if _, err := conn.Exec(ctx, schemaMigrationsDDL); err != nil {
		return nil, fmt.Errorf("db: cannot create schema_migrations: %w", err)
	}

	applied, err := appliedMigrations(ctx, conn)
	if err != nil {
		return nil, err
	}

	done := []string{}
	for _, name := range names {
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("db: cannot read migration %s: %w", name, err)
		}

		sum := checksum(body)
		if previous, ok := applied[name]; ok {
			if previous != sum {
				return nil, fmt.Errorf("db: migration %s changed after being applied: the database and the repository disagree, so recreate the database or add a new migration", name)
			}
			continue
		}

		if err := applyOne(ctx, conn, name, string(body), sum); err != nil {
			return nil, err
		}
		done = append(done, name)
	}
	return done, nil
}

// Applied returns what the database says it already ran, which is what `make migrate-status` prints.
func Applied(ctx context.Context, pool Pool) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: cannot acquire a connection to read schema_migrations: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, schemaMigrationsDDL); err != nil {
		return nil, fmt.Errorf("db: cannot create schema_migrations: %w", err)
	}

	versions, err := appliedMigrations(ctx, conn)
	if err != nil {
		return nil, err
	}

	out := []string{}
	for version := range versions {
		out = append(out, version)
	}
	sort.Strings(out)
	return out, nil
}

// One transaction per file, so a migration that fails halfway leaves neither its DDL nor its row behind.
func applyOne(ctx context.Context, conn *pgxpool.Conn, name, body, sum string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: cannot begin %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("db: cannot apply %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", name, sum); err != nil {
		return fmt.Errorf("db: cannot record %s: %w", name, err)
	}
	return tx.Commit(ctx)
}

// The whole table at once, because the file count is in the dozens and asking per file is a round trip each.
func appliedMigrations(ctx context.Context, conn *pgxpool.Conn) (map[string]string, error) {
	rows, err := conn.Query(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("db: cannot read schema_migrations: %w", err)
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var version, sum string
		if err := rows.Scan(&version, &sum); err != nil {
			return nil, fmt.Errorf("db: cannot scan schema_migrations: %w", err)
		}
		out[version] = sum
	}
	return out, rows.Err()
}

// Sorted by name because the numeric prefix is the order: 0002 must never run before 0001.
func migrationNames(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("db: cannot list migrations: %w", err)
	}

	names := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Line endings are normalised first: git hands the same file as CRLF on Windows and as LF in the container.
func checksum(body []byte) string {
	sum := sha256.Sum256(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")))
	return hex.EncodeToString(sum[:])
}
