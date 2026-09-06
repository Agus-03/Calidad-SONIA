package turn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

// Repository takes the Executor and not the pool, so the transaction this package opens reaches the other repositories (regla 5).
type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

// Reads two tables this package does not own: the ownership rule of regla 1 restricts writes and not reads.
const targetSelect = `
SELECT c.id, c.inbox_id, ct.phone_whatsapp, c.mode
FROM   conversations c
JOIN   contacts ct ON ct.id = c.contact_id
WHERE  c.id = $1 AND c.deleted_at IS NULL`

func (r *Repository) Target(ctx context.Context, conversationID uuid.UUID) (Target, error) {
	var t Target
	err := r.exec.QueryRow(ctx, targetSelect, conversationID).
		Scan(&t.ConversationID, &t.InboxID, &t.ContactPhone, &t.Mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrConversationNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("turn: scan target: %w", err)
	}
	return t, nil
}

// The unique index of ADR-0022 is the arbiter of the turn number, not the read that preceded it.
const batchInsert = `
INSERT INTO message_batches (id, conversation_id, messages_count, turn_number, timer_ms_used, opened_at, closed_at)
SELECT $1, $2, 1, COALESCE(MAX(b.turn_number), 0) + 1, 0, $3, $3
FROM   message_batches b
WHERE  b.conversation_id = $2
RETURNING turn_number`

// closed_reason stays NULL on purpose: its three values all describe a timer, and there is none to describe yet (F10).
func (r *Repository) OpenBatch(ctx context.Context, conversationID uuid.UUID, at time.Time) (Batch, error) {
	batch := Batch{ID: newID(), ConversationID: conversationID, OpenedAt: at}

	err := r.exec.QueryRow(ctx, batchInsert, batch.ID, conversationID, at).Scan(&batch.TurnNumber)
	if err != nil {
		return Batch{}, fmt.Errorf("turn: insert message batch: %w", err)
	}
	return batch, nil
}

// MarkProcessed is what closes the turn, and 07-secuencia requires it in the same transaction as the outbound message.
func (r *Repository) MarkProcessed(ctx context.Context, batchID uuid.UUID, at time.Time) error {
	tag, err := r.exec.Exec(ctx, `UPDATE message_batches SET processed_at = $2 WHERE id = $1`, batchID, at)
	if err != nil {
		return fmt.Errorf("turn: mark batch processed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBatchNotFound
	}
	return nil
}

// MarkFailed leaves processed_at NULL, which with closed_at set is the one marker of a turn nobody finished (07-secuencia section 5).
func (r *Repository) MarkFailed(ctx context.Context, batchID uuid.UUID, reason string) error {
	tag, err := r.exec.Exec(ctx, `UPDATE message_batches SET error_message = $2 WHERE id = $1`, batchID, reason)
	if err != nil {
		return fmt.Errorf("turn: mark batch failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBatchNotFound
	}
	return nil
}

// The one column of an evidence table the core writes (ADR-0017 (i).2): that row does not exist until section 2 writes it.
const generationLink = `UPDATE generations SET message_id = $2 WHERE id = $1`

func (r *Repository) LinkGeneration(ctx context.Context, generationID, messageID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, generationLink, generationID, messageID)
	if err != nil {
		return fmt.Errorf("turn: link generation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrGenerationNotFound
	}
	return nil
}
