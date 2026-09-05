package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Skips instead of failing, so go test ./... keeps working on a machine with no stack up.
func integrationDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration")
	}
	return dsn
}

// The unit tests only prove that a broken DSN fails; this is the half that says the pool actually serves.
func TestOpenAgainstALiveDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := Open(ctx, Options{DatabaseURL: integrationDSN(t), MinSize: 1, MaxSize: 4})
	if err != nil {
		t.Fatalf("Open: got %v, want no error", err)
	}
	defer pool.Close()

	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("SELECT 1: got %v, want no error", err)
	}
	if one != 1 {
		t.Fatalf("SELECT 1: got %d, want 1", one)
	}
}

// InTx deliberately does not wrap the caller's error, and errors.Is across packages depends on that.
func TestInTxReturnsTheCallersErrorUntouched(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := Open(ctx, Options{DatabaseURL: integrationDSN(t), MinSize: 1, MaxSize: 4})
	if err != nil {
		t.Fatalf("Open: got %v, want no error", err)
	}
	defer pool.Close()

	sentinel := errors.New("db: the caller decided to fail")
	got := InTx(ctx, pool, func(Executor) error { return sentinel })

	if !errors.Is(got, sentinel) {
		t.Fatalf("InTx: got %v, want the caller's own error", got)
	}
}
