package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

// Named so cleanup can tell the account this test installed from one that was already there and must survive it.
const seedTestAccountName = "SONIA - test"

// seedEnv sets what the seeder reads plus what config demands of any boot, and returns the address it was told to seed.
func seedEnv(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration")
	}

	// A fresh address on every run, so the test never depends on what a previous one left in the table.
	email := "seed-" + time.Now().UTC().Format("20060102150405.000000") + "@coop.test"

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("BRAIN_BASE_URL", "http://127.0.0.1:8000")
	t.Setenv("BRAIN_INTERNAL_TOKEN", "test-token")
	t.Setenv("JWT_SECRET", "test-secret-that-is-long-enough-for-hs256")
	t.Setenv("SEED_ADMIN_EMAIL", email)
	t.Setenv("SEED_ADMIN_PASSWORD", "la-del-primer-login")
	t.Setenv("SEED_ADMIN_ACCOUNT_NAME", seedTestAccountName)

	// The seeder writes for real and has no transaction to roll back, so the test undoes the rows it may have created.
	t.Cleanup(func() { removeWhatWasSeeded(t, dsn, email) })

	return email
}

// Leaving the rows behind would be worse than untidy: the account carries a single-row invariant, so a leftover one owns the install.
func removeWhatWasSeeded(t *testing.T, dsn, email string) {
	t.Helper()

	ctx := context.Background()
	pool, err := db.Open(ctx, db.Options{DatabaseURL: dsn, MinSize: 1, MaxSize: 2})
	if err != nil {
		t.Fatalf("opening the pool to clean up: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `DELETE FROM operators WHERE email = $1`, email); err != nil {
		t.Fatalf("removing the seeded operator: %v", err)
	}
	// Guarded twice over: only the name this test uses, and only while no operator is left hanging from it.
	if _, err := pool.Exec(ctx,
		`DELETE FROM accounts WHERE name = $1 AND NOT EXISTS (SELECT 1 FROM operators)`, seedTestAccountName); err != nil {
		t.Fatalf("removing the seeded account: %v", err)
	}
}

// TestTheSeededAdminIsOneTheLoginAccepts is the point of the whole task: a hash written by the seed has to be one Verify reads.
func TestTheSeededAdminIsOneTheLoginAccepts(t *testing.T) {
	ctx := context.Background()
	email := seedEnv(t)

	if err := SeedAdmin(ctx); err != nil {
		t.Fatalf("SeedAdmin: got %v, want no error", err)
	}

	pool := seedPool(t, ctx)
	seeded, err := operators.NewRepository(pool).ByEmail(ctx, email)
	if err != nil {
		t.Fatalf("reading back the seeded admin: %v", err)
	}
	if seeded.Role != "admin" || !seeded.Active {
		t.Fatalf("the seed wrote %q active=%v, want an active admin", seeded.Role, seeded.Active)
	}
	if seeded.AccountID.String() == "" {
		t.Fatalf("the seeded admin hangs from no account: %+v", seeded)
	}
	// The plaintext must live only in the environment, so what landed in the column cannot be it.
	if seeded.PasswordHash == "la-del-primer-login" || !strings.HasPrefix(seeded.PasswordHash, "$2") {
		t.Fatalf("password_hash is not a bcrypt hash: %q", seeded.PasswordHash)
	}
	if err := operators.Verify(seeded.PasswordHash, "la-del-primer-login"); err != nil {
		t.Fatalf("the seeded hash refuses the seeded password: %v", err)
	}
}

// TestSeedingTwiceNeverResetsThePassword protects the idempotency the Makefile promises: running it again is a normal thing to do.
func TestSeedingTwiceNeverResetsThePassword(t *testing.T) {
	ctx := context.Background()
	email := seedEnv(t)

	if err := SeedAdmin(ctx); err != nil {
		t.Fatalf("first SeedAdmin: got %v, want no error", err)
	}

	pool := seedPool(t, ctx)
	people := operators.NewRepository(pool)
	first, err := people.ByEmail(ctx, email)
	if err != nil {
		t.Fatalf("reading back the seeded admin: %v", err)
	}

	// A different password on the second run: if the seed wrote again, the hash would change and the old one would stop working.
	t.Setenv("SEED_ADMIN_PASSWORD", "otra-distinta")
	if err := SeedAdmin(ctx); err != nil {
		t.Fatalf("second SeedAdmin: got %v, want no error", err)
	}

	second, err := people.ByEmail(ctx, email)
	if err != nil {
		t.Fatalf("reading back after the second run: %v", err)
	}
	if second.ID != first.ID || second.PasswordHash != first.PasswordHash {
		t.Fatalf("the second run rewrote the admin: %v -> %v", first.ID, second.ID)
	}
}

// TestSeedingWithoutACredentialRefusesBeforeTouchingTheDatabase is the guard against installing a door instead of an admin.
func TestSeedingWithoutACredentialRefusesBeforeTouchingTheDatabase(t *testing.T) {
	ctx := context.Background()
	seedEnv(t)
	t.Setenv("SEED_ADMIN_PASSWORD", "")

	if err := SeedAdmin(ctx); err == nil {
		t.Fatal("SeedAdmin with no password: got no error, want a refusal")
	}
}

func seedPool(t *testing.T, ctx context.Context) db.Pool {
	t.Helper()

	pool, err := db.Open(ctx, db.Options{DatabaseURL: os.Getenv("DATABASE_URL"), MinSize: 1, MaxSize: 2})
	if err != nil {
		t.Fatalf("opening the pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
