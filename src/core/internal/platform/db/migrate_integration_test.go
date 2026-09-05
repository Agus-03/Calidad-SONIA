package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/migrations"
)

// Every case builds its own database and drops it, because a migration test that shares one leaves the next run dirty.
func scratchDatabase(t *testing.T, name string) Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := Open(ctx, Options{DatabaseURL: integrationDSN(t), MinSize: 1, MaxSize: 2})
	if err != nil {
		t.Fatalf("Open admin: got %v, want no error", err)
	}
	defer admin.Close()

	if _, err := admin.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", name)); err != nil {
		t.Fatalf("DROP DATABASE: got %v, want no error", err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", name)); err != nil {
		t.Fatalf("CREATE DATABASE: got %v, want no error", err)
	}

	pool, err := Open(ctx, Options{DatabaseURL: scratchDSN(integrationDSN(t), name), MinSize: 1, MaxSize: 4})
	if err != nil {
		t.Fatalf("Open scratch: got %v, want no error", err)
	}

	t.Cleanup(func() {
		pool.Close()

		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelCleanup()

		dropper, err := Open(cleanupCtx, Options{DatabaseURL: integrationDSN(t), MinSize: 1, MaxSize: 2})
		if err != nil {
			return
		}
		defer dropper.Close()
		_, _ = dropper.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", name))
	})
	return pool
}

// Swaps the database name in the DSN without parsing it, which is enough for the two shapes docker-compose produces.
func scratchDSN(dsn, name string) string {
	cut := strings.LastIndex(dsn, "/")
	rest := dsn[cut+1:]

	if query := strings.Index(rest, "?"); query >= 0 {
		return dsn[:cut+1] + name + rest[query:]
	}
	return dsn[:cut+1] + name
}

// The point of the whole task: the 37 tables of the DER have to actually apply, not merely read well.
func TestTheInitialSchemaApplies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := scratchDatabase(t, "sonia_migrate_apply_test")

	applied, err := Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("Migrate: got %v, want no error", err)
	}
	if len(applied) == 0 {
		t.Fatal("Migrate: got nothing applied, want at least the initial schema")
	}

	var tables int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'").Scan(&tables); err != nil {
		t.Fatalf("count tables: got %v, want no error", err)
	}

	// 37 entities of entidades.md plus schema_migrations, which is the runner's own bookkeeping.
	if tables != 38 {
		t.Fatalf("tables in public: got %d, want 38", tables)
	}
}

// Migrating on every boot is only safe if the second boot is a no-op.
func TestMigratingTwiceAppliesNothingTheSecondTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := scratchDatabase(t, "sonia_migrate_twice_test")

	if _, err := Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("first Migrate: got %v, want no error", err)
	}

	again, err := Migrate(ctx, pool, migrations.FS)
	if err != nil {
		t.Fatalf("second Migrate: got %v, want no error", err)
	}
	if len(again) != 0 {
		t.Fatalf("second Migrate: got %v applied, want nothing", again)
	}
}

// Editing an applied file is allowed until Fase 4, and this is what tells the developer the database has to be recreated.
func TestAnAppliedMigrationThatChangedStopsTheBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := scratchDatabase(t, "sonia_migrate_changed_test")

	before := fstest.MapFS{"0001_probe.sql": {Data: []byte("CREATE TABLE probe (id uuid PRIMARY KEY);")}}
	if _, err := Migrate(ctx, pool, before); err != nil {
		t.Fatalf("Migrate: got %v, want no error", err)
	}

	after := fstest.MapFS{"0001_probe.sql": {Data: []byte("CREATE TABLE probe (id uuid PRIMARY KEY, name varchar);")}}
	if _, err := Migrate(ctx, pool, after); err == nil {
		t.Fatal("Migrate: got no error for a file that changed after being applied, want one")
	}
}

// Applied is what make migrate-status reads, and an empty database has to answer with a list and not an error.
func TestAppliedListsWhatRan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := scratchDatabase(t, "sonia_migrate_applied_test")

	empty, err := Applied(ctx, pool)
	if err != nil {
		t.Fatalf("Applied on an empty database: got %v, want no error", err)
	}
	if len(empty) != 0 {
		t.Fatalf("Applied on an empty database: got %v, want nothing", empty)
	}

	if _, err := Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("Migrate: got %v, want no error", err)
	}

	after, err := Applied(ctx, pool)
	if err != nil {
		t.Fatalf("Applied: got %v, want no error", err)
	}
	if len(after) != 1 || after[0] != "0001_initial_schema.sql" {
		t.Fatalf("Applied: got %v, want the initial schema", after)
	}
}
