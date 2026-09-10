package contacts

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// rollback ends every case: the assertions run against real DDL and leave the database as they found it.
var rollback = errors.New("contacts: rolling back the integration case")

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

// A contact hangs from an account, and accounts holds a single row for the whole project (ADR-0012).
func seededAccount(t *testing.T, pool db.Pool) uuid.UUID {
	t.Helper()

	var account uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	return account
}

// Digits only and unique per case, so nothing collides with the demo rows or with another case running before it.
func uniquePhone() string {
	return normalizePhone("549351" + id.New().String())
}

// TestTheSensitiveFieldsAreUnreadableInTheDatabase is the claim the column names have been making since the first migration:
// a dump of the table must not carry the DNI, and only the repository turns the bytes back into a number (ADR-0031).
func TestTheSensitiveFieldsAreUnreadableInTheDatabase(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)
	cipher := testCipher(t)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, cipher)
		created := newContact(account, uniquePhone(), "Hugo", "Sosa")
		created.Sensitive = Sensitive{DNI: "36415782", CUIT: "20364157821", CUIL: "27364157823", CBU: "0170099220000067797151"}

		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}

		var dni, cuit, cuil, cbu []byte
		row := exec.QueryRow(ctx, `SELECT dni_encrypted, cuit_encrypted, cuil_encrypted, cbu_encrypted FROM contacts WHERE id = $1`, created.ID)
		if err := row.Scan(&dni, &cuit, &cuil, &cbu); err != nil {
			t.Fatalf("read the raw columns: %v", err)
		}
		for name, pair := range map[string]struct {
			stored []byte
			clear  string
		}{"dni": {dni, "36415782"}, "cuit": {cuit, "20364157821"}, "cuil": {cuil, "27364157823"}, "cbu": {cbu, "0170099220000067797151"}} {
			if len(pair.stored) == 0 {
				t.Fatalf("%s was stored empty, want ciphertext", name)
			}
			if bytes.Contains(pair.stored, []byte(pair.clear)) {
				t.Fatalf("%s is in the database in the clear", name)
			}
		}

		got, err := repo.ByID(ctx, account, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Sensitive != created.Sensitive {
			t.Fatalf("the card reads %+v, want %+v", got.Sensitive, created.Sensitive)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestTheListingNeverCarriesTheSensitiveFields is the other half of ADR-0032 regla 4: the page does not select those columns,
// so a mistake above cannot turn a listing into two hundred decryptions or into two hundred leaks.
func TestTheListingNeverCarriesTheSensitiveFields(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)
	cipher := testCipher(t)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, cipher)
		if _, err := exec.Exec(ctx, `UPDATE contacts SET deleted_at = now() WHERE deleted_at IS NULL`); err != nil {
			t.Fatalf("clear the book: %v", err)
		}

		created := newContact(account, uniquePhone(), "Ines", "Tapia")
		created.Sensitive = Sensitive{DNI: "36415782", CBU: "0170099220000067797151"}
		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}

		page, err := repo.List(ctx, account, 20, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("the page holds %d contacts, want 1", len(page.Items))
		}
		if page.Items[0].Sensitive != (Sensitive{}) {
			t.Fatalf("the listing carried %+v, want the four fields empty", page.Items[0].Sensitive)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestAnOrdinaryEditCannotWipeACbu is H-33 turned into a test: the update that corrects a name has nowhere to carry the four fields,
// so a form that never showed the CBU cannot send it back empty.
func TestAnOrdinaryEditCannotWipeACbu(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)
	cipher := testCipher(t)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, cipher)
		created := newContact(account, uniquePhone(), "Jose", "Vera")
		created.Sensitive = Sensitive{CBU: "0170099220000067797151"}
		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}

		// The whole Changes struct, zero values and all: this is exactly the shape a full object PUT would send.
		if err := repo.Update(ctx, account, created.ID, Changes{Name: "Jose Luis", Lastname: "Vera", Type: enums.ContactTypeClient}); err != nil {
			t.Fatalf("Update: %v", err)
		}

		got, err := repo.ByID(ctx, account, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Sensitive.CBU != created.Sensitive.CBU {
			t.Fatalf("the CBU now reads %q, want it untouched by an ordinary edit", got.Sensitive.CBU)
		}
		if got.Name != "Jose Luis" {
			t.Fatalf("the name was not corrected: %q", got.Name)
		}

		// And the deliberate write does reach them, because taking the CBU away has to be possible when it is what was asked.
		if err := repo.UpdateSensitive(ctx, account, created.ID, Sensitive{DNI: "36415782"}); err != nil {
			t.Fatalf("UpdateSensitive: %v", err)
		}
		got, err = repo.ByID(ctx, account, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Sensitive != (Sensitive{DNI: "36415782"}) {
			t.Fatalf("after the deliberate write the card reads %+v, want only the DNI", got.Sensitive)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// A key drawn per run, so the cases never depend on what the environment happens to hold.
func testCipher(t *testing.T) *encryption.Cipher {
	t.Helper()

	key := make([]byte, encryption.KeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	cipher, err := encryption.New(key)
	if err != nil {
		t.Fatalf("encryption.New: %v", err)
	}
	return cipher
}

func newContact(account uuid.UUID, phone, name, lastname string) Contact {
	return Contact{
		ID: id.New(), AccountID: account, PhoneWhatsapp: phone,
		Name: name, Lastname: lastname, Type: enums.ContactTypeClient,
		SourceChannel: enums.SourceChannelDirect,
	}
}

// TestTheSameNumberTypedTwoWaysIsOneContact is why the phone is normalized: what Meta sends is digits, and a plus sign is how a human types it.
func TestTheSameNumberTypedTwoWaysIsOneContact(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		digits := uniquePhone()

		first := newContact(account, digits, "Ana", "Perez")
		if err := repo.Create(ctx, first); err != nil {
			t.Fatalf("Create: %v", err)
		}

		for _, typed := range []string{"+" + digits, digits[:3] + " " + digits[3:], "(" + digits + ")"} {
			if err := repo.Create(ctx, newContact(account, typed, "Otra", "Persona")); !errors.Is(err, ErrDuplicatePhone) {
				t.Fatalf("Create(%q): got %v, want ErrDuplicatePhone", typed, err)
			}
		}

		got, err := repo.ByID(ctx, account, first.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.PhoneWhatsapp != digits {
			t.Fatalf("stored phone = %q, want the digits %q", got.PhoneWhatsapp, digits)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestANewContactHasNoConsentYet is the decision written into the schema: nobody has asked the client, so the column is NULL and not the load time.
func TestANewContactHasNoConsentYet(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		created := newContact(account, uniquePhone(), "Bruno", "Diaz")

		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.ByID(ctx, account, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.ConsentGivenAt != nil || got.ConsentRevokedAt != nil {
			t.Fatalf("consent came back as %v / %v, want both nil", got.ConsentGivenAt, got.ConsentRevokedAt)
		}
		if got.OptOut || got.Blocked {
			t.Fatalf("a new contact is opted out %v blocked %v, want neither", got.OptOut, got.Blocked)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestUpdateCorrectsWhatItMayAndCannotReachTheRest is the shape of regla 10: the phone and the origin have no parameter to travel in.
func TestUpdateCorrectsWhatItMayAndCannotReachTheRest(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		phone := uniquePhone()
		created := newContact(account, phone, "Carla", "Gomes")

		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}

		err := repo.Update(ctx, account, created.ID, Changes{
			Name: "  Carla  ", Lastname: "Gómez", Email: "carla@coop.test",
			Type: enums.ContactTypeProspect, Notes: "prefiere la tarde", OptOut: true,
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}

		got, err := repo.ByID(ctx, account, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Name != "Carla" || got.Lastname != "Gómez" || got.Email != "carla@coop.test" {
			t.Fatalf("the contact reads %+v, want the corrected and trimmed values", got)
		}
		if got.Type != enums.ContactTypeProspect || got.Notes != "prefiere la tarde" || !got.OptOut {
			t.Fatalf("the contact reads %+v, want the type, notes and opt out updated", got)
		}
		// The two the caller had no way to send: the update statement does not name them, so they cannot have moved.
		if got.PhoneWhatsapp != phone || got.SourceChannel != enums.SourceChannelDirect {
			t.Fatalf("phone or origin changed: %q / %q", got.PhoneWhatsapp, got.SourceChannel)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestAnEmptyEmailIsNullAndComesBackEmpty guards the round trip of the two nullable strings, which would otherwise store "" and read as a value.
func TestAnEmptyEmailIsNullAndComesBackEmpty(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		created := newContact(account, uniquePhone(), "Diego", "Luna")

		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}

		var email, notes *string
		row := exec.QueryRow(ctx, `SELECT email, notes FROM contacts WHERE id = $1`, created.ID)
		if err := row.Scan(&email, &notes); err != nil {
			t.Fatalf("read the raw columns: %v", err)
		}
		if email != nil || notes != nil {
			t.Fatalf("the columns hold %v / %v, want NULL and not an empty string", email, notes)
		}

		got, err := repo.ByID(ctx, account, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if got.Email != "" || got.Notes != "" {
			t.Fatalf("the contact reads %q / %q, want both empty", got.Email, got.Notes)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestDeletingAContactFreesItsNumber protects the partial predicate: a client who comes back must be able to be loaded again (C09).
func TestDeletingAContactFreesItsNumber(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		phone := uniquePhone()
		first := newContact(account, phone, "Elsa", "Mora")

		if err := repo.Create(ctx, first); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.SoftDelete(ctx, account, first.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		if _, err := repo.ByID(ctx, account, first.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID of a deleted contact: got %v, want ErrNotFound", err)
		}
		if err := repo.Create(ctx, newContact(account, phone, "Elsa", "Mora")); err != nil {
			t.Fatalf("Create after the delete freed the number: %v", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestTheSecondDeleteReportsAMissingContact is the RowsAffected branch: repeating the delete must not quietly move deleted_at.
func TestTheSecondDeleteReportsAMissingContact(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		created := newContact(account, uniquePhone(), "Fabio", "Nieva")

		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.SoftDelete(ctx, account, created.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		if err := repo.SoftDelete(ctx, account, created.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("the second SoftDelete: got %v, want ErrNotFound", err)
		}
		if err := repo.Update(ctx, account, created.ID, Changes{Name: "Fabio", Lastname: "Nieva", Type: enums.ContactTypeClient}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update of a deleted contact: got %v, want ErrNotFound", err)
		}
		if err := repo.UpdateSensitive(ctx, account, created.ID, Sensitive{DNI: "36415782"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("UpdateSensitive of a deleted contact: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestAnUnknownIdIsMissingAndSoIsAnotherAccounts is why every read and write takes the account: a guessed id reads as absent, never as forbidden.
func TestAnUnknownIdIsMissingAndSoIsAnotherAccounts(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		created := newContact(account, uniquePhone(), "Gaby", "Rios")

		if err := repo.Create(ctx, created); err != nil {
			t.Fatalf("Create: %v", err)
		}

		stranger := id.New()
		if _, err := repo.ByID(ctx, stranger, created.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID from another account: got %v, want ErrNotFound", err)
		}
		if err := repo.Update(ctx, stranger, created.ID, Changes{Name: "Robada", Lastname: "Ajena", Type: enums.ContactTypeClient}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update from another account: got %v, want ErrNotFound", err)
		}
		if err := repo.SoftDelete(ctx, stranger, created.ID); !errors.Is(err, ErrNotFound) {
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

// TestListPagesAndOrdersAsABook pins both halves of a page and the ordering a contact book is read in.
func TestListPagesAndOrdersAsABook(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))

		// Emptied inside the transaction that rolls back, so the totals below are exact whatever the book already held.
		if _, err := exec.Exec(ctx, `UPDATE contacts SET deleted_at = now() WHERE deleted_at IS NULL`); err != nil {
			t.Fatalf("clear the book: %v", err)
		}

		for _, who := range [][2]string{{"Zoe", "alvarez"}, {"Ana", "Bustos"}, {"Ivan", "Zapata"}} {
			if err := repo.Create(ctx, newContact(account, uniquePhone(), who[0], who[1])); err != nil {
				t.Fatalf("Create %v: %v", who, err)
			}
		}

		first, err := repo.List(ctx, account, 2, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if first.Total != 3 {
			t.Fatalf("Total = %d, want 3", first.Total)
		}
		// Ordered by lower(lastname), so alvarez comes before Bustos even though a C collation would put the capital first.
		if len(first.Items) != 2 || first.Items[0].Lastname != "alvarez" || first.Items[1].Lastname != "Bustos" {
			t.Fatalf("first page = %+v, want alvarez then Bustos", first.Items)
		}

		second, err := repo.List(ctx, account, 2, 2)
		if err != nil {
			t.Fatalf("List second page: %v", err)
		}
		if len(second.Items) != 1 || second.Items[0].Lastname != "Zapata" || second.Total != 3 {
			t.Fatalf("second page = %+v total %d, want Zapata alone out of 3", second.Items, second.Total)
		}

		past, err := repo.List(ctx, account, 2, 40)
		if err != nil {
			t.Fatalf("List past the end: %v", err)
		}
		if past.Items == nil || len(past.Items) != 0 || past.Total != 3 {
			t.Fatalf("a page past the end is %+v, want an empty non nil slice and the real total", past)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
