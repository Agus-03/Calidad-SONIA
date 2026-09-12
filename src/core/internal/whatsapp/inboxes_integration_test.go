package whatsapp

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestOneLiveInboxPerNumber protects the index behind InboxByPhoneNumberID: with two live rows for one number the webhook would
// resolve either of them, and check the signature against either secret (ADR-0012, relaciones.md section 9).
func TestOneLiveInboxPerNumber(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		phoneNumberID := "it-" + id.New().String()
		insert := func() error {
			_, err := exec.Exec(ctx, `
				INSERT INTO inboxes (id, account_id, phone_number_id, waba_id, display_name, verify_token, status)
				VALUES ($1, $2, $3, 'it-waba', 'Numero repetido', $4, 'active')`,
				id.New(), account, phoneNumberID, "it-"+id.New().String())
			return err
		}

		if err := insert(); err != nil {
			t.Fatalf("the first inbox of the number: %v", err)
		}
		// Retiring the row frees the number, the same partial index the other three uniques of section 9 use (C09).
		if _, err := exec.Exec(ctx, `UPDATE inboxes SET deleted_at = now() WHERE phone_number_id = $1`, phoneNumberID); err != nil {
			t.Fatalf("retire the first inbox: %v", err)
		}
		if err := insert(); err != nil {
			t.Fatalf("the number after its inbox was retired: got %v, want it free again", err)
		}

		// Last on purpose: the refused statement aborts the transaction, so nothing can run after it (SQLSTATE 25P02).
		var pgErr *pgconn.PgError
		if err := insert(); !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("a second live inbox for the number: got %v, want a unique violation", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
