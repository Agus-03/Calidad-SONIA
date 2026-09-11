package contacts

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
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

		got, err := repo.ByID(ctx, adminScope(account), created.ID)
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

		page, err := repo.List(ctx, adminScope(account), Filter{}, 20, 0)
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

		got, err := repo.ByID(ctx, adminScope(account), created.ID)
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
		got, err = repo.ByID(ctx, adminScope(account), created.ID)
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

// assignTo seeds by hand what nothing in the system assigns yet: auto assignment to the first who answers is Fase 5 and manual
// reassignment (F26) is Fase 11. Without this the operator's book would come back empty for the right reason and the wrong one at once,
// and the case would pass while proving nothing. Written down in ADR-0032 as a problem of calendar; this is where it is paid.
func assignTo(t *testing.T, ctx context.Context, exec db.Executor, contactID uuid.UUID, operatorID *uuid.UUID) {
	t.Helper()

	var inbox uuid.UUID
	if err := exec.QueryRow(ctx, `SELECT id FROM inboxes LIMIT 1`).Scan(&inbox); err != nil {
		t.Skip("no inbox in the demo rows: load src/core/seeds/0001_demo.sql first")
	}

	_, err := exec.Exec(ctx, `
		INSERT INTO conversations (id, contact_id, inbox_id, operator_id, status, mode, last_inbound_at, window_expires_at)
		VALUES ($1, $2, $3, $4, 'active', 'autonomous', now(), now() + interval '24 hours')`,
		id.New(), contactID, inbox, operatorID)
	if err != nil {
		t.Fatalf("seed a conversation: %v", err)
	}
}

// seededOperators returns two real operator ids, because conversations.operator_id is a foreign key and a random uuid would be refused.
func seededOperators(t *testing.T, ctx context.Context, exec db.Executor, account uuid.UUID) (mine, theirs uuid.UUID) {
	t.Helper()

	mine, theirs = id.New(), id.New()
	for _, o := range []uuid.UUID{mine, theirs} {
		_, err := exec.Exec(ctx, `
			INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Prueba', 'Alcance', $3, 'not-a-real-hash', 'operator', true)`,
			o, account, o.String()+"@sonia.test")
		if err != nil {
			t.Fatalf("seed an operator: %v", err)
		}
	}
	return mine, theirs
}

// TestTheOperatorReachesOnlyTheContactsOfItsOwnConversations is the rule of ADR-0032 regla 2, and the case the whole task exists for.
//
// It asserts both directions on purpose. A filter that is too loose answers 200 with the whole book, and a filter that is too tight
// answers 200 with nothing: the two mistakes are opposite, both silent, and only one of them would be caught by asking "does it hide?".
func TestTheOperatorReachesOnlyTheContactsOfItsOwnConversations(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)
	cipher := testCipher(t)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, cipher)
		if _, err := exec.Exec(ctx, `UPDATE contacts SET deleted_at = now() WHERE deleted_at IS NULL`); err != nil {
			t.Fatalf("clear the book: %v", err)
		}

		mine, theirs := seededOperators(t, ctx, exec, account)

		assigned := newContact(account, uniquePhone(), "Ana", "Asignada")
		other := newContact(account, uniquePhone(), "Beto", "Ajeno")
		pooled := newContact(account, uniquePhone(), "Cora", "EnElPool")
		for _, c := range []Contact{assigned, other, pooled} {
			if err := repo.Create(ctx, c); err != nil {
				t.Fatalf("Create %s: %v", c.Lastname, err)
			}
		}

		assignTo(t, ctx, exec, assigned.ID, &mine)
		assignTo(t, ctx, exec, other.ID, &theirs)
		// Unassigned, which is how every incoming conversation arrives until somebody answers (ADR-0004).
		assignTo(t, ctx, exec, pooled.ID, nil)

		for _, tc := range []struct {
			role      enums.OperatorRole
			wantNames []string
		}{
			{enums.OperatorRoleOperator, []string{"Asignada"}},
			{enums.OperatorRoleSupervisor, []string{"Ajeno", "Asignada", "EnElPool"}},
			{enums.OperatorRoleAdmin, []string{"Ajeno", "Asignada", "EnElPool"}},
		} {
			t.Run(string(tc.role), func(t *testing.T) {
				scope := Scope{AccountID: account, OperatorID: mine, Role: tc.role}

				page, err := repo.List(ctx, scope, Filter{}, 20, 0)
				if err != nil {
					t.Fatalf("List: %v", err)
				}
				if got := lastnames(page); !equal(got, tc.wantNames) {
					t.Fatalf("the book holds %v, want %v", got, tc.wantNames)
				}
				// The total counts the same reach: a page saying "1 of 3" would leak how many the role cannot see.
				if page.Total != len(tc.wantNames) {
					t.Fatalf("Total = %d, want %d", page.Total, len(tc.wantNames))
				}

				// The card must narrow exactly like the listing, or guessing an id walks straight past it.
				if _, err := repo.ByID(ctx, scope, assigned.ID); err != nil {
					t.Fatalf("ByID of a contact in reach: %v", err)
				}
				_, err = repo.ByID(ctx, scope, other.ID)
				if tc.role == enums.OperatorRoleOperator {
					if !errors.Is(err, ErrNotFound) {
						t.Fatalf("ByID of another operator's contact: got %v, want ErrNotFound", err)
					}
				} else if err != nil {
					t.Fatalf("ByID as %s: %v", tc.role, err)
				}
			})
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestAReassignedContactLeavesTheBookAtOnce is the negative consequence of regla 3 written as a test, so nobody reads it as a bug later.
func TestAReassignedContactLeavesTheBookAtOnce(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)
	cipher := testCipher(t)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, cipher)
		mine, theirs := seededOperators(t, ctx, exec, account)

		contact := newContact(account, uniquePhone(), "Dora", "Reasignada")
		if err := repo.Create(ctx, contact); err != nil {
			t.Fatalf("Create: %v", err)
		}
		assignTo(t, ctx, exec, contact.ID, &mine)

		scope := Scope{AccountID: account, OperatorID: mine, Role: enums.OperatorRoleOperator}
		if _, err := repo.ByID(ctx, scope, contact.ID); err != nil {
			t.Fatalf("ByID while it is assigned to me: %v", err)
		}

		// What F26 does: the supervisor hands the conversation to somebody else. The reach is the current assignment, not the history.
		if _, err := exec.Exec(ctx, `UPDATE conversations SET operator_id = $2 WHERE contact_id = $1`, contact.ID, theirs); err != nil {
			t.Fatalf("reassign: %v", err)
		}

		if _, err := repo.ByID(ctx, scope, contact.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID after the reassignment: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestARoleNobodyDeclaredSeesNothing is the safe side of the default: an empty role must narrow, never open.
func TestARoleNobodyDeclaredSeesNothing(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)
	cipher := testCipher(t)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, cipher)
		mine, _ := seededOperators(t, ctx, exec, account)

		contact := newContact(account, uniquePhone(), "Elsa", "SinRol")
		if err := repo.Create(ctx, contact); err != nil {
			t.Fatalf("Create: %v", err)
		}
		assignTo(t, ctx, exec, contact.ID, &mine)

		// No role at all, and an operator who is not the one holding the conversation: nothing must come back.
		page, err := repo.List(ctx, Scope{AccountID: account, OperatorID: id.New()}, Filter{}, 20, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page.Items) != 0 || page.Total != 0 {
			t.Fatalf("an undeclared role reached %d contacts, want none", page.Total)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

func lastnames(p Page) []string {
	out := make([]string, 0, len(p.Items))
	for _, c := range p.Items {
		out = append(out, c.Lastname)
	}
	return out
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The cases that are not about the rule read as an admin, which is the reach that does not narrow: what they assert is unaffected by it.
func adminScope(account uuid.UUID) Scope {
	return Scope{AccountID: account, OperatorID: id.New(), Role: enums.OperatorRoleAdmin}
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

		got, err := repo.ByID(ctx, adminScope(account), first.ID)
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

		got, err := repo.ByID(ctx, adminScope(account), created.ID)
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

		got, err := repo.ByID(ctx, adminScope(account), created.ID)
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

		got, err := repo.ByID(ctx, adminScope(account), created.ID)
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
		if _, err := repo.ByID(ctx, adminScope(account), first.ID); !errors.Is(err, ErrNotFound) {
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
		if _, err := repo.ByID(ctx, adminScope(stranger), created.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ByID from another account: got %v, want ErrNotFound", err)
		}
		if err := repo.Update(ctx, stranger, created.ID, Changes{Name: "Robada", Lastname: "Ajena", Type: enums.ContactTypeClient}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update from another account: got %v, want ErrNotFound", err)
		}
		if err := repo.SoftDelete(ctx, stranger, created.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("SoftDelete from another account: got %v, want ErrNotFound", err)
		}
		if _, err := repo.ByID(ctx, adminScope(account), id.New()); !errors.Is(err, ErrNotFound) {
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

		first, err := repo.List(ctx, adminScope(account), Filter{}, 2, 0)
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

		second, err := repo.List(ctx, adminScope(account), Filter{}, 2, 2)
		if err != nil {
			t.Fatalf("List second page: %v", err)
		}
		if len(second.Items) != 1 || second.Items[0].Lastname != "Zapata" || second.Total != 3 {
			t.Fatalf("second page = %+v total %d, want Zapata alone out of 3", second.Items, second.Total)
		}

		past, err := repo.List(ctx, adminScope(account), Filter{}, 2, 40)
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

// seededLabel puts one entry in the catalogue, because contact_labels.label_id is a foreign key and no link can be written
// against a label that is not there (C05).
func seededLabel(t *testing.T, ctx context.Context, exec db.Executor, account, byOperator uuid.UUID, name string) uuid.UUID {
	t.Helper()

	labelID := id.New()
	_, err := exec.Exec(ctx, `
		INSERT INTO labels (id, account_id, name, color, created_by_operator_id)
		VALUES ($1, $2, $3, '#ff8800', $4)`, labelID, account, name, byOperator)
	if err != nil {
		t.Fatalf("seed a label: %v", err)
	}
	return labelID
}

// TestTaggingTwiceWritesOneLink is the case the stub cannot prove: whether ON CONFLICT (contact_id, label_id) actually infers
// contact_labels_unique. If the inference were wrong the statement would fail, not silently write a second row.
func TestTaggingTwiceWritesOneLink(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		mine, _ := seededOperators(t, ctx, exec, account)

		contact := newContact(account, uniquePhone(), "Julia", "Ledesma")
		if err := repo.Create(ctx, contact); err != nil {
			t.Fatalf("Create: %v", err)
		}
		labelID := seededLabel(t, ctx, exec, account, mine, "Ventas "+id.New().String())

		for i := range 2 {
			if err := repo.AssignLabel(ctx, ContactLabel{
				ID: id.New(), ContactID: contact.ID, LabelID: labelID, AssignedByOperatorID: mine,
			}); err != nil {
				t.Fatalf("AssignLabel number %d: %v", i+1, err)
			}
		}

		var links int
		if err := exec.QueryRow(ctx, `SELECT count(*) FROM contact_labels WHERE contact_id = $1`, contact.ID).Scan(&links); err != nil {
			t.Fatalf("count the links: %v", err)
		}
		if links != 1 {
			t.Fatalf("the contact carries %d links, want exactly one", links)
		}

		// And the removal is physical, because the table has no deleted_at to move.
		if err := repo.UnassignLabel(ctx, contact.ID, labelID); err != nil {
			t.Fatalf("UnassignLabel: %v", err)
		}
		if err := exec.QueryRow(ctx, `SELECT count(*) FROM contact_labels WHERE contact_id = $1`, contact.ID).Scan(&links); err != nil {
			t.Fatalf("count the links again: %v", err)
		}
		if links != 0 {
			t.Fatalf("%d links survived the removal", links)
		}

		// Removing one that is not there is not an error, so untagging twice does not need the caller to remember.
		if err := repo.UnassignLabel(ctx, contact.ID, labelID); err != nil {
			t.Fatalf("UnassignLabel of a link already gone: %v", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
}

// TestATagAppliedByNobodyIsAllowed pins the nullable column against the zero uuid: writing that instead of NULL would be a
// foreign key pointing at an operator that cannot exist, which is what Fase 8 will hit when the brain tags on its own (F31).
func TestATagAppliedByNobodyIsAllowed(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		mine, _ := seededOperators(t, ctx, exec, account)

		contact := newContact(account, uniquePhone(), "Karina", "Ferro")
		if err := repo.Create(ctx, contact); err != nil {
			t.Fatalf("Create: %v", err)
		}
		labelID := seededLabel(t, ctx, exec, account, mine, "Automatica "+id.New().String())

		if err := repo.AssignLabel(ctx, ContactLabel{ID: id.New(), ContactID: contact.ID, LabelID: labelID}); err != nil {
			t.Fatalf("AssignLabel with no operator behind it: %v", err)
		}

		var by *uuid.UUID
		row := exec.QueryRow(ctx, `SELECT assigned_by_operator_id FROM contact_labels WHERE contact_id = $1`, contact.ID)
		if err := row.Scan(&by); err != nil {
			t.Fatalf("read the assigning operator: %v", err)
		}
		if by != nil {
			t.Fatalf("the link names operator %v, want NULL", *by)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
}

// TestTheTagsOfAContactAreItsOwn checks the two halves the card rests on: the ids come from this package's table, and the
// catalogue answers only the entries of the asking account.
func TestTheTagsOfAContactAreItsOwn(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		mine, _ := seededOperators(t, ctx, exec, account)

		tagged := newContact(account, uniquePhone(), "Lucia", "Bravo")
		untouched := newContact(account, uniquePhone(), "Mario", "Cano")
		for _, c := range []Contact{tagged, untouched} {
			if err := repo.Create(ctx, c); err != nil {
				t.Fatalf("Create: %v", err)
			}
		}

		ventas := seededLabel(t, ctx, exec, account, mine, "Ventas "+id.New().String())
		moroso := seededLabel(t, ctx, exec, account, mine, "Moroso "+id.New().String())
		for _, labelID := range []uuid.UUID{ventas, moroso} {
			if err := repo.AssignLabel(ctx, ContactLabel{
				ID: id.New(), ContactID: tagged.ID, LabelID: labelID, AssignedByOperatorID: mine,
			}); err != nil {
				t.Fatalf("AssignLabel: %v", err)
			}
		}

		ids, err := repo.LabelIDsOf(ctx, tagged.ID)
		if err != nil {
			t.Fatalf("LabelIDsOf: %v", err)
		}
		if len(ids) != 2 {
			t.Fatalf("the contact carries %d tags, want two", len(ids))
		}

		// The one nobody tagged answers an empty slice and not nil, so the card renders [] and not null.
		none, err := repo.LabelIDsOf(ctx, untouched.ID)
		if err != nil {
			t.Fatalf("LabelIDsOf of an untagged contact: %v", err)
		}
		if none == nil || len(none) != 0 {
			t.Fatalf("an untagged contact answered %v, want an empty slice", none)
		}

		// ByIDs is where the ids become names, and it is also where the account is checked a second time (struct.md, regla 1).
		found, err := labels.NewRepository(exec).ByIDs(ctx, account, ids)
		if err != nil {
			t.Fatalf("ByIDs: %v", err)
		}
		if len(found) != 2 {
			t.Fatalf("the catalogue answered %d entries for two ids", len(found))
		}
		strangers, err := labels.NewRepository(exec).ByIDs(ctx, id.New(), ids)
		if err != nil {
			t.Fatalf("ByIDs for another account: %v", err)
		}
		if len(strangers) != 0 {
			t.Fatalf("another account read %d entries of this catalogue", len(strangers))
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
}

// TestAContactWithSeveralOfTheLabelsAppearsOnce is the bug this query is written around: a JOIN against contact_labels returns
// the contact once per matching label, and the Total counts it that many times with it (F33).
func TestAContactWithSeveralOfTheLabelsAppearsOnce(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		mine, _ := seededOperators(t, ctx, exec, account)

		both := newContact(account, uniquePhone(), "Nadia", "Quiroga")
		one := newContact(account, uniquePhone(), "Omar", "Rivas")
		none := newContact(account, uniquePhone(), "Pia", "Sanz")
		for _, c := range []Contact{both, one, none} {
			if err := repo.Create(ctx, c); err != nil {
				t.Fatalf("Create: %v", err)
			}
		}

		ventas := seededLabel(t, ctx, exec, account, mine, "Ventas "+id.New().String())
		moroso := seededLabel(t, ctx, exec, account, mine, "Moroso "+id.New().String())
		tag := func(c Contact, labelID uuid.UUID) {
			if err := repo.AssignLabel(ctx, ContactLabel{
				ID: id.New(), ContactID: c.ID, LabelID: labelID, AssignedByOperatorID: mine,
			}); err != nil {
				t.Fatalf("AssignLabel: %v", err)
			}
		}
		tag(both, ventas)
		tag(both, moroso)
		tag(one, ventas)

		asked := []uuid.UUID{ventas, moroso}

		// Any: the two that carry at least one, and each of them exactly once.
		page, err := repo.List(ctx, adminScope(account), Filter{LabelIDs: asked, Match: MatchAny}, 20, 0)
		if err != nil {
			t.Fatalf("List with match any: %v", err)
		}
		if seen := timesListed(page, both.ID); seen != 1 {
			t.Fatalf("the contact carrying both labels appears %d times, want once", seen)
		}
		if timesListed(page, one.ID) != 1 || timesListed(page, none.ID) != 0 {
			t.Fatalf("match any listed the wrong contacts: %v", listedNames(page))
		}
		if page.Total != 2 {
			t.Fatalf("match any totals %d, want 2 - the total must count what the page shows", page.Total)
		}

		// All: only the one that carries every label asked for.
		page, err = repo.List(ctx, adminScope(account), Filter{LabelIDs: asked, Match: MatchAll}, 20, 0)
		if err != nil {
			t.Fatalf("List with match all: %v", err)
		}
		if timesListed(page, both.ID) != 1 || timesListed(page, one.ID) != 0 {
			t.Fatalf("match all listed the wrong contacts: %v", listedNames(page))
		}
		if page.Total != 1 {
			t.Fatalf("match all totals %d, want 1", page.Total)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
}

// TestTheFilterCannotWidenWhatTheScopeNarrowed is the property that keeps the segment from being a side door: an operator
// filtering by a label still sees only the contacts of its own conversations (ADR-0032, regla 2).
func TestTheFilterCannotWidenWhatTheScopeNarrowed(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		mine, theirs := seededOperators(t, ctx, exec, account)

		assigned := newContact(account, uniquePhone(), "Rosa", "Tello")
		somebodyElses := newContact(account, uniquePhone(), "Sergio", "Ugarte")
		for _, c := range []Contact{assigned, somebodyElses} {
			if err := repo.Create(ctx, c); err != nil {
				t.Fatalf("Create: %v", err)
			}
		}

		// Both carry the label, so what separates them in the answer can only be the reach.
		ventas := seededLabel(t, ctx, exec, account, mine, "Ventas "+id.New().String())
		for _, c := range []Contact{assigned, somebodyElses} {
			if err := repo.AssignLabel(ctx, ContactLabel{
				ID: id.New(), ContactID: c.ID, LabelID: ventas, AssignedByOperatorID: mine,
			}); err != nil {
				t.Fatalf("AssignLabel: %v", err)
			}
		}
		assignTo(t, ctx, exec, assigned.ID, &mine)
		assignTo(t, ctx, exec, somebodyElses.ID, &theirs)

		scope := Scope{AccountID: account, OperatorID: mine, Role: enums.OperatorRoleOperator}
		page, err := repo.List(ctx, scope, Filter{LabelIDs: []uuid.UUID{ventas}}, 20, 0)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if timesListed(page, assigned.ID) != 1 {
			t.Fatalf("the operator lost its own tagged contact: %v", listedNames(page))
		}
		if timesListed(page, somebodyElses.ID) != 0 {
			t.Fatalf("the filter reached a contact of another operator: %v", listedNames(page))
		}
		if page.Total != 1 {
			t.Fatalf("the total is %d, want 1: it must count the same reach and the same filter as the page", page.Total)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
}

func timesListed(page Page, contactID uuid.UUID) int {
	seen := 0
	for _, c := range page.Items {
		if c.ID == contactID {
			seen++
		}
	}
	return seen
}

func listedNames(page Page) []string {
	names := make([]string, 0, len(page.Items))
	for _, c := range page.Items {
		names = append(names, c.Lastname)
	}
	return names
}

// TestTheSearchReachesTheNameAndThePhoneAndNothingElse pins the two criteria that were asked for, and the traps a LIKE brings
// with it: a term of punctuation must not become a wildcard, and a term with no digits must not be compared against the phone (F30).
func TestTheSearchReachesTheNameAndThePhoneAndNothingElse(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))

		phone := uniquePhone()
		ana := newContact(account, phone, "Ana", "Valdez")
		ana.Email, ana.Notes = "ana@coop.test", "la clave es Valdez"
		other := newContact(account, uniquePhone(), "Bruno", "Zarate")
		for _, c := range []Contact{ana, other} {
			if err := repo.Create(ctx, c); err != nil {
				t.Fatalf("Create: %v", err)
			}
		}

		found := func(term string) Page {
			page, err := repo.List(ctx, adminScope(account), Filter{Search: term}, 50, 0)
			if err != nil {
				t.Fatalf("List searching %q: %v", term, err)
			}
			return page
		}

		for _, term := range []string{"Valdez", "valdez", "Ana", "Ana Valdez", phone, phone[len(phone)-6:]} {
			if timesListed(found(term), ana.ID) != 1 {
				t.Fatalf("searching %q did not find her: %v", term, listedNames(found(term)))
			}
		}

		// Folded for case and not for accents, the same invariant the catalogue holds.
		if timesListed(found("Váldez"), ana.ID) != 0 {
			t.Fatalf("the search folded an accent, which the catalogue does not")
		}

		// The email and the notes are not criteria: they were deliberately left out, and a LIKE over every column would drag them in.
		for _, term := range []string{"coop.test", "la clave"} {
			if timesListed(found(term), ana.ID) != 0 {
				t.Fatalf("searching %q reached a column that is not a criterion", term)
			}
		}

		// A wildcard typed by hand is a character and not a pattern, or every search box is a way to list the whole book.
		if page := found("%"); page.Total != 0 {
			t.Fatalf("a per cent sign matched %d contacts, want 0", page.Total)
		}
		if page := found("_"); page.Total != 0 {
			t.Fatalf("an underscore matched %d contacts, want 0", page.Total)
		}

		// A term with no digits must not be compared against the phone: normalizing it gives nothing, and LIKE '%%' is everyone.
		if timesListed(found("Ana"), other.ID) != 0 {
			t.Fatalf("a search with no digits matched a contact by phone")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
}
