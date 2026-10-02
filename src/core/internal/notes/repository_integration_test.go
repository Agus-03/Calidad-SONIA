package notes

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var rollback = errors.New("notes: rolling back the integration case")

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

// TestNotesAgainstRealDDL is ADR-0056 through the repository: notes kept by their author, signed, and kept when deleted (C09).
func TestNotesAgainstRealDDL(t *testing.T) {
	ctx := context.Background()
	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		var account, inbox uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT a.id, i.id FROM accounts a, inboxes i LIMIT 1`).Scan(&account, &inbox); err != nil {
			t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
		}
		mine, theirs := id.New(), id.New()
		for _, o := range []uuid.UUID{mine, theirs} {
			if _, err := exec.Exec(ctx, `INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
				VALUES ($1, $2, 'Prueba', 'Notas', $3, 'not-a-real-hash', 'operator', true)`, o, account, o.String()+"@sonia.test"); err != nil {
				t.Fatalf("seed an operator: %v", err)
			}
		}
		conversation := func(holder uuid.UUID) uuid.UUID {
			contact, conv := id.New(), id.New()
			if _, err := exec.Exec(ctx, `INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel)
				VALUES ($1, $2, $3, 'Prueba', 'Notas', 'prospect', 'direct')`, contact, account, "54"+contact.String()); err != nil {
				t.Fatalf("seed a contact: %v", err)
			}
			if _, err := exec.Exec(ctx, `INSERT INTO conversations (id, contact_id, inbox_id, operator_id, status, mode, last_inbound_at, window_expires_at)
				VALUES ($1, $2, $3, $4, 'active', 'off', now(), now() + interval '24 hours')`, conv, contact, inbox, holder); err != nil {
				t.Fatalf("seed a conversation: %v", err)
			}
			return conv
		}
		mineConv, theirsConv := conversation(mine), conversation(theirs)
		repo := NewRepository(exec)

		first, second := id.New(), id.New()
		for _, n := range []Note{
			{ID: first, ConversationID: mineConv, Author: Author{ID: mine}, Content: "primera"},
			{ID: second, ConversationID: mineConv, Author: Author{ID: theirs}, Content: "segunda"},
		} {
			if err := repo.AddNote(ctx, n); err != nil {
				t.Fatalf("AddNote: %v", err)
			}
		}
		notes, err := repo.Notes(ctx, mineConv)
		if err != nil || len(notes) != 2 || notes[0].ID != first || notes[0].Author.Name != "Prueba" {
			t.Fatalf("Notes: got %+v and %v, want both, oldest first, signed", notes, err)
		}
		if err := repo.EditNote(ctx, second, mine, "ajena"); !errors.Is(err, ErrNoteNotFound) {
			t.Fatalf("editing another's note: got %v, want ErrNoteNotFound", err)
		}
		if err := repo.EditNote(ctx, first, mine, "corregida"); err != nil {
			t.Fatalf("EditNote: %v", err)
		}
		if n, err := repo.NoteByID(ctx, mineConv, first); err != nil || n.Content != "corregida" {
			t.Fatalf("NoteByID after the edit: got %+v and %v", n, err)
		}
		if err := repo.DeleteNote(ctx, first, mine); err != nil {
			t.Fatalf("DeleteNote: %v", err)
		}
		if _, err := repo.NoteByID(ctx, mineConv, first); !errors.Is(err, ErrNoteNotFound) {
			t.Fatalf("a deleted note: got %v, want ErrNoteNotFound", err)
		}
		if _, err := repo.NoteByID(ctx, theirsConv, second); !errors.Is(err, ErrNoteNotFound) {
			t.Fatalf("a note read through another conversation: got %v, want ErrNoteNotFound", err)
		}
		var kept int
		if err := exec.QueryRow(ctx, `SELECT count(*) FROM internal_notes WHERE id = $1`, first).Scan(&kept); err != nil || kept != 1 {
			t.Fatalf("the deleted note's row: %d and %v, want it kept (C09)", kept, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
