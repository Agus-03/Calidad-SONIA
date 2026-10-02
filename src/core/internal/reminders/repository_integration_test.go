package reminders

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var rollback = errors.New("reminders: rolling back the integration case")

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

// TestRemindersAgainstRealDDL is ADR-0058 against real DDL: the alert follows the vendor, is told once, and a moved one fires again.
func TestRemindersAgainstRealDDL(t *testing.T) {
	ctx := context.Background()
	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		var account, inbox uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT a.id, i.id FROM accounts a, inboxes i LIMIT 1`).Scan(&account, &inbox); err != nil {
			t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
		}
		person := func() uuid.UUID {
			operatorID := id.New()
			if _, err := exec.Exec(ctx, `INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
				VALUES ($1, $2, 'Prueba', 'Recordatorio', $3, 'not-a-real-hash', 'operator', true)`,
				operatorID, account, operatorID.String()+"@sonia.test"); err != nil {
				t.Fatalf("seed an operator: %v", err)
			}
			return operatorID
		}
		writer, vendor := person(), person()
		contact := id.New()
		if _, err := exec.Exec(ctx, `INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, type, source_channel)
			VALUES ($1, $2, $3, 'Prueba', 'Recordatorio', 'prospect', 'direct')`, contact, account, "54"+contact.String()); err != nil {
			t.Fatalf("seed a contact: %v", err)
		}
		repo := NewRepository(exec)

		due := Reminder{ID: id.New(), ContactID: contact, OperatorID: writer, Description: "vencido", DueAt: time.Now().Add(-time.Hour)}
		coming := Reminder{ID: id.New(), ContactID: contact, OperatorID: writer, Description: "próximo", DueAt: time.Now().Add(time.Hour)}
		for _, r := range []Reminder{due, coming} {
			if err := repo.Create(ctx, r); err != nil {
				t.Fatalf("Create: %v", err)
			}
		}

		// No conversation yet: it goes to whoever wrote it.
		if mine, err := repo.Addressed(ctx, account, writer, true); err != nil || len(mine) != 1 || mine[0].ID != due.ID {
			t.Fatalf("the writer's due: got %v and %v, want the one due", mine, err)
		}
		conversation := id.New()
		if _, err := exec.Exec(ctx, `INSERT INTO conversations (id, contact_id, inbox_id, operator_id, status, mode, last_inbound_at, window_expires_at)
			VALUES ($1, $2, $3, $4, 'active', 'off', now(), now() + interval '24 hours')`, conversation, contact, inbox, vendor); err != nil {
			t.Fatalf("seed a conversation: %v", err)
		}
		// With a vendor, the alert follows the vendor (ADR-0058).
		if mine, err := repo.Addressed(ctx, account, vendor, false); err != nil || len(mine) != 2 || mine[0].ID != due.ID {
			t.Fatalf("the vendor's: got %v and %v, want both, soonest first", mine, err)
		}
		if ofWriter, _ := repo.Addressed(ctx, account, writer, false); len(ofWriter) != 0 {
			t.Fatalf("the writer still reads %d, want none once the contact has a vendor", len(ofWriter))
		}
		if of, err := repo.ConversationOfContact(ctx, conversation, contact); err != nil || !of {
			t.Fatalf("ConversationOfContact: got %v and %v, want true", of, err)
		}

		marked, err := repo.Due(ctx, 100)
		if err != nil {
			t.Fatalf("Due: %v", err)
		}
		var ours []Due
		for _, d := range marked {
			if d.ContactID == contact {
				ours = append(ours, d)
			}
		}
		if len(ours) != 1 || ours[0].ID != due.ID || ours[0].Recipient != vendor || !ours[0].Notified || ours[0].NotifiedAt == nil {
			t.Fatalf("Due marked %+v, want the one due, for the vendor", ours)
		}
		again, err := repo.Due(ctx, 100)
		if err != nil {
			t.Fatalf("Due again: %v", err)
		}
		for _, d := range again {
			if d.ID == due.ID {
				t.Fatal("a reminder already told was told twice")
			}
		}

		if err := repo.Update(ctx, due.ID, "vencido, corrido", due.DueAt.Add(48*time.Hour)); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if moved, err := repo.ByID(ctx, account, due.ID); err != nil || moved.Notified || moved.NotifiedAt != nil {
			t.Fatalf("a moved reminder: got %+v and %v, want it pending again", moved, err)
		}
		if _, err := repo.ByID(ctx, id.New(), due.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a reminder read from another account: got %v, want ErrNotFound", err)
		}

		if err := repo.Delete(ctx, coming.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if err := repo.Delete(ctx, coming.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleting twice: got %v, want ErrNotFound", err)
		}
		if left, err := repo.OfContact(ctx, account, contact); err != nil || len(left) != 1 || left[0].ID != due.ID {
			t.Fatalf("OfContact after the delete: got %v and %v, want only the other", left, err)
		}
		var kept int
		if err := exec.QueryRow(ctx, `SELECT count(*) FROM reminders WHERE id = $1`, coming.ID).Scan(&kept); err != nil || kept != 1 {
			t.Fatalf("the deleted reminder's row: %d and %v, want it kept (C09)", kept, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
