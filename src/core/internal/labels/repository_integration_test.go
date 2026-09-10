package labels

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// rollback ends every case: the assertions run against real DDL and leave the database as they found it.
var rollback = errors.New("labels: rolling back the integration case")

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

// A label hangs from an account and from the operator who created it, so both foreign keys need a row that already exists.
func seededOwners(t *testing.T, pool db.Pool) (account, operator uuid.UUID) {
	t.Helper()

	ctx := context.Background()
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operator); err != nil {
		t.Skip("no operator to own a label: run core --seed-admin first")
	}
	return account, operator
}

// unique keeps the cases independent of each other and of whatever the catalogue already holds.
func unique(prefix string) string {
	return prefix + "-" + id.New().String()
}

func newLabel(account, operator uuid.UUID, name string) Label {
	return Label{ID: id.New(), AccountID: account, Name: name, Color: "#112233", CreatedByOperatorID: operator}
}

// TestTheCasingTypedDoesNotMakeASecondLabel is the reason the index folds case: to whoever tags, Ventas and ventas are one label.
func TestTheCasingTypedDoesNotMakeASecondLabel(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		typed := unique("Ventas")

		first := newLabel(account, operator, typed)
		if err := repo.Create(ctx, first); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// The whole name is lowercased and not only its first letter, because the suffix is already lowercase hex.
		if err := repo.Create(ctx, newLabel(account, operator, strings.ToLower(typed))); !errors.Is(err, ErrDuplicateName) {
			t.Fatalf("Create of the same name in another casing: got %v, want ErrDuplicateName", err)
		}

		got, err := repo.ByID(ctx, account, first.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Name != typed {
			t.Fatalf("stored name = %q, want it kept as typed %q", got.Name, typed)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestCreateTrimsTheNameBeforeTheIndexSeesIt covers the other half of normalizeName: a pasted name arrives with spaces around it.
func TestCreateTrimsTheNameBeforeTheIndexSeesIt(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		typed := unique("Urgente")

		created := newLabel(account, operator, "  "+typed+"  ")
		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.ByID(ctx, account, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Name != typed {
			t.Fatalf("stored name = %q, want the trimmed %q", got.Name, typed)
		}

		if err := repo.Create(ctx, newLabel(account, operator, typed)); !errors.Is(err, ErrDuplicateName) {
			t.Fatalf("Create of the trimmed name: got %v, want ErrDuplicateName", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestRenamingIntoATakenNameIsRefused covers the branch Create does not: UPDATE has no ON CONFLICT, so this one rescues the violation.
func TestRenamingIntoATakenNameIsRefused(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		taken, other := unique("Seguimiento"), newLabel(account, operator, unique("Perdido"))

		if err := repo.Create(ctx, newLabel(account, operator, taken)); err != nil {
			t.Fatalf("Create the taken name: %v", err)
		}
		if err := repo.Create(ctx, other); err != nil {
			t.Fatalf("Create the label to rename: %v", err)
		}

		if err := repo.Update(ctx, account, other.ID, strings.ToUpper(taken), "#445566"); !errors.Is(err, ErrDuplicateName) {
			t.Fatalf("Update into a taken name: got %v, want ErrDuplicateName", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestRenamingItsOwnCasingIsAllowed guards the boundary of the case above: the index compares rows, so a row never collides with itself.
func TestRenamingItsOwnCasingIsAllowed(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		label := newLabel(account, operator, strings.ToLower(unique("Reclamo")))

		if err := repo.Create(ctx, label); err != nil {
			t.Fatalf("Create: %v", err)
		}

		fixed := strings.ToUpper(label.Name)
		if err := repo.Update(ctx, account, label.ID, fixed, "#778899"); err != nil {
			t.Fatalf("Update to its own name in another casing: %v", err)
		}

		got, err := repo.ByID(ctx, account, label.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Name != fixed || got.Color != "#778899" {
			t.Fatalf("after Update got %q %q, want %q #778899", got.Name, got.Color, fixed)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestDeletingALabelFreesItsName protects the partial predicate: without WHERE deleted_at IS NULL the name would stay taken forever (C09).
func TestDeletingALabelFreesItsName(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		name := unique("Cerrado")
		first := newLabel(account, operator, name)

		if err := repo.Create(ctx, first); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.SoftDelete(ctx, account, first.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		if _, err := repo.ByID(ctx, account, first.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID of a deleted label: got %v, want ErrNotFound", err)
		}

		// The name comes back in a third casing, so this also proves the freed name is not freed only for an exact match.
		if err := repo.Create(ctx, newLabel(account, operator, strings.ToUpper(name))); err != nil {
			t.Fatalf("Create after the delete freed the name: %v", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestTheSecondDeleteReportsAMissingLabel is the RowsAffected branch: repeating the delete must not quietly move deleted_at.
func TestTheSecondDeleteReportsAMissingLabel(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		label := newLabel(account, operator, unique("Duplicado"))

		if err := repo.Create(ctx, label); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.SoftDelete(ctx, account, label.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		if err := repo.SoftDelete(ctx, account, label.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("the second SoftDelete: got %v, want ErrNotFound", err)
		}
		if err := repo.Update(ctx, account, label.ID, unique("Revivido"), "#000000"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update of a deleted label: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestAnUnknownIdIsMissingAndSoIsAnotherAccounts is why the writes and ByID all take the account: a guessed id must read as absent, never as forbidden.
func TestAnUnknownIdIsMissingAndSoIsAnotherAccounts(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		label := newLabel(account, operator, unique("Ajena"))

		if err := repo.Create(ctx, label); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// A random uuid stands in for another account, because accounts holds a single row for the whole project (ADR-0012).
		stranger := id.New()
		if _, err := repo.ByID(ctx, stranger, label.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID from another account: got %v, want ErrNotFound", err)
		}
		if err := repo.Update(ctx, stranger, label.ID, unique("Robada"), "#ffffff"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update from another account: got %v, want ErrNotFound", err)
		}
		if err := repo.SoftDelete(ctx, stranger, label.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("SoftDelete from another account: got %v, want ErrNotFound", err)
		}
		if _, err := repo.ByID(ctx, account, id.New()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID of an unknown id: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestListPagesAndOrdersByTheFoldedName pins both halves of a page: Total counts the catalogue, Items carries only the slice asked for.
func TestListPagesAndOrdersByTheFoldedName(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)

		// Emptied inside the transaction that rolls back, so the totals below are exact whatever the catalogue already held.
		if _, err := exec.Exec(ctx, `UPDATE labels SET deleted_at = now() WHERE deleted_at IS NULL`); err != nil {
			t.Fatalf("clear the catalogue: %v", err)
		}

		// Ordering by lower(name) and not by name is what keeps alfa before Zeta on a database collated as C.
		for _, name := range []string{"Zeta", "alfa", "Media"} {
			if err := repo.Create(ctx, newLabel(account, operator, name)); err != nil {
				t.Fatalf("Create %q: %v", name, err)
			}
		}

		first, err := repo.List(ctx, account, 2, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if first.Total != 3 {
			t.Fatalf("Total = %d, want 3", first.Total)
		}
		if len(first.Items) != 2 || first.Items[0].Name != "alfa" || first.Items[1].Name != "Media" {
			t.Fatalf("first page = %v, want alfa then Media", names(first))
		}

		second, err := repo.List(ctx, account, 2, 2)
		if err != nil {
			t.Fatalf("List second page: %v", err)
		}
		if second.Total != 3 {
			t.Fatalf("Total on the second page = %d, want 3", second.Total)
		}
		if len(second.Items) != 1 || second.Items[0].Name != "Zeta" {
			t.Fatalf("second page = %v, want Zeta alone", names(second))
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestListOfAnEmptyCatalogueIsEmptyAndNotNil keeps the controller of the next task from serializing null where a page is expected.
func TestListOfAnEmptyCatalogueIsEmptyAndNotNil(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)

	// Called for its skip and not for its ids: this case asks about an account that deliberately owns nothing.
	seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		page, err := NewRepository(exec).List(ctx, id.New(), 20, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if page.Items == nil || len(page.Items) != 0 || page.Total != 0 {
			t.Fatalf("empty page = %#v, want an empty non nil slice and Total 0", page)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

func names(p Page) []string {
	out := make([]string, 0, len(p.Items))
	for _, l := range p.Items {
		out = append(out, l.Name)
	}
	return out
}

// TestByIDsAnswersOnlyTheOwnCatalogueEntries is what contacts/ leans on to draw a card: it hands over the ids its own union
// table holds and gets back names, and the account is checked here because the union table cannot check it (struct.md, regla 1).
func TestByIDsAnswersOnlyTheOwnCatalogueEntries(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, operator := seededOwners(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)

		ventas := newLabel(account, operator, "Ventas "+id.New().String())
		moroso := newLabel(account, operator, "Moroso "+id.New().String())
		retirada := newLabel(account, operator, "Retirada "+id.New().String())
		for _, l := range []Label{ventas, moroso, retirada} {
			if err := repo.Create(ctx, l); err != nil {
				t.Fatalf("Create: %v", err)
			}
		}
		asked := []uuid.UUID{ventas.ID, moroso.ID, retirada.ID}

		// Ordered by the folded name for the same reason the listing is: a card draws its chips in the order the catalogue reads.
		found, err := repo.ByIDs(ctx, account, asked)
		if err != nil {
			t.Fatalf("ByIDs: %v", err)
		}
		if len(found) != 3 {
			t.Fatalf("the catalogue answered %d entries for three ids", len(found))
		}

		// A label retired after it was tagged stops showing, instead of drawing a chip with no name behind it (C09).
		if err := repo.SoftDelete(ctx, account, retirada.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		found, err = repo.ByIDs(ctx, account, asked)
		if err != nil {
			t.Fatalf("ByIDs after the delete: %v", err)
		}
		if len(found) != 2 {
			t.Fatalf("a deleted label is still in the answer: %d entries", len(found))
		}

		// Another account asking with the very same ids gets nothing, which is the check the foreign key never makes.
		strangers, err := repo.ByIDs(ctx, id.New(), asked)
		if err != nil {
			t.Fatalf("ByIDs for another account: %v", err)
		}
		if len(strangers) != 0 {
			t.Fatalf("another account read %d entries of this catalogue", len(strangers))
		}

		// No ids is no query: a contact nobody tagged must not cost a round trip, and must answer [] and not null.
		none, err := repo.ByIDs(ctx, account, nil)
		if err != nil {
			t.Fatalf("ByIDs with no ids: %v", err)
		}
		if none == nil || len(none) != 0 {
			t.Fatalf("ByIDs with no ids answered %v, want an empty slice", none)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
}
