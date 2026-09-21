package conversations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestTheBoundAggregateWritesInsideTheTransaction is regla 5 seen from the aggregate: turn/ builds the service on the pool and binds
// it to the transaction it opens, so the message and its link to the batch commit or roll back with the rest of the turn.
func TestTheBoundAggregateWritesInsideTheTransaction(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)
	onThePool := NewService(NewRepository(pool), nil)

	var messageID uuid.UUID
	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		service := onThePool.WithExecutor(exec)

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		at := time.Now().UTC()
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, true, at)
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}
		message, err := service.RecordInbound(ctx, conversation, "wamid."+id.New().String(), "hola", enums.MessageContentTypeText, at)
		if err != nil {
			t.Fatalf("RecordInbound: %v", err)
		}
		messageID = message.ID

		// The batch is turn/'s table, so it is written here by hand: what is under test is the one column of messages it points from.
		batch := id.New()
		if _, err := exec.Exec(ctx,
			`INSERT INTO message_batches (id, conversation_id, turn_number, timer_ms_used, expires_at) VALUES ($1, $2, 1, 0, now())`,
			batch, conversation.ID); err != nil {
			t.Fatalf("insert batch: %v", err)
		}

		if err := service.LinkToBatch(ctx, message.ID, batch); err != nil {
			t.Fatalf("LinkToBatch: %v", err)
		}
		var linked *uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT message_batch_id FROM messages WHERE id = $1`, message.ID).Scan(&linked); err != nil {
			t.Fatalf("read the message: %v", err)
		}
		if linked == nil || *linked != batch {
			t.Fatalf("message_batch_id = %v, want %v", linked, batch)
		}

		if err := service.LinkToBatch(ctx, id.New(), batch); !errors.Is(err, ErrNotFound) {
			t.Fatalf("linking a message that does not exist: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}

	// Had the bound service written through the pool, the message would have outlived the rollback.
	var survived bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM messages WHERE id = $1)`, messageID).Scan(&survived); err != nil {
		t.Fatalf("look for the message: %v", err)
	}
	if survived {
		t.Fatal("the message outlived the rollback: the bound service wrote outside the transaction")
	}
}
