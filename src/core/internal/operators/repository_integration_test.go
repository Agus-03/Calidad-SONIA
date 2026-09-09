package operators

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// rollback ends every case: the assertions run against real DDL and leave the database as they found it.
var rollback = errors.New("operators: rolling back the integration case")

func integrationPool(t *testing.T) db.Pool {
	t.Helper()

	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration")
	}

	pool, err := db.Open(context.Background(), db.Options{DatabaseURL: dsn, MinSize: 1, MaxSize: 4})
	if err != nil {
		t.Fatalf("Open: got %v, want no error", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seededAccount reads the single account of src/core/seeds, because every operator hangs from one.
func seededAccount(t *testing.T, pool db.Pool) uuid.UUID {
	t.Helper()

	var account uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	return account
}

// unique keeps the cases independent of each other and of whatever the admin seed already left in the table.
func unique(prefix string) string {
	return prefix + "-" + id.New().String() + "@sonia.test"
}

// TestTheCasingTypedAtLoginDoesNotDecideWhoYouAre is the reason the boundary lowercases: the unique index is over the raw column.
func TestTheCasingTypedAtLoginDoesNotDecideWhoYouAre(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		email := unique("mayusculas")

		if err := repo.Create(ctx, Operator{
			ID: id.New(), AccountID: account, Name: "Prueba", Lastname: "Integracion",
			Email: "  " + email, PasswordHash: "not-a-real-hash",
			Role: enums.OperatorRoleAdmin, Active: true,
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}

		for _, typed := range []string{email, "  " + email + " "} {
			got, err := repo.ByEmail(ctx, typed)
			if err != nil {
				t.Fatalf("ByEmail(%q): %v", typed, err)
			}
			if got.Email != email {
				t.Fatalf("stored email = %q, want the lowercased %q", got.Email, email)
			}
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestByIDCarriesWhatTheMiddlewareDecidesWith protects regla 2 of ADR-0030: role and active come from the row, not from the token.
func TestByIDCarriesWhatTheMiddlewareDecidesWith(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		operatorID := id.New()

		if err := repo.Create(ctx, Operator{
			ID: operatorID, AccountID: account, Name: "Prueba", Lastname: "Integracion",
			Email: unique("desactivado"), PasswordHash: "not-a-real-hash",
			Role: enums.OperatorRoleSupervisor, Active: false,
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.ByID(ctx, operatorID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Role != enums.OperatorRoleSupervisor || got.Active {
			t.Fatalf("ByID returned role %q active %v, want supervisor and inactive", got.Role, got.Active)
		}

		if _, err := repo.ByID(ctx, id.New()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID of an unknown id: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestADeletedOperatorIsInvisibleToBothReads protects C09: the address is freed, so the row must stop answering.
func TestADeletedOperatorIsInvisibleToBothReads(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		operatorID, email := id.New(), unique("borrado")

		if err := repo.Create(ctx, Operator{
			ID: operatorID, AccountID: account, Name: "Prueba", Lastname: "Integracion",
			Email: email, PasswordHash: "not-a-real-hash",
			Role: enums.OperatorRoleOperator, Active: true,
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := exec.Exec(ctx, `UPDATE operators SET deleted_at = now() WHERE id = $1`, operatorID); err != nil {
			t.Fatalf("soft delete: %v", err)
		}

		if _, err := repo.ByEmail(ctx, email); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByEmail of a deleted operator: got %v, want ErrNotFound", err)
		}
		if _, err := repo.ByID(ctx, operatorID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID of a deleted operator: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
