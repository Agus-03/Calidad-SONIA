package conversations

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// ON CONFLICT for the same reason as conversationInsert: a redelivery is routine, and a rescued unique violation would leave the
// webhook's transaction aborted, losing the event row and every other message of the payload (SQLSTATE 25P02). A draft has no
// wamid yet, and NULLs never conflict.
const messageInsert = `
INSERT INTO messages (id, conversation_id, message_batch_id, wamid, direction, sender_type,
                      sender_ai_agent_id, sender_operator_id, content_text, content_type, status, sent_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (wamid) DO NOTHING`

// AppendMessage is the only way into messages: the table admits no update of content and no delete at all (F29, C09).
func (r *Repository) AppendMessage(ctx context.Context, m Message) error {
	tag, err := r.exec.Exec(ctx, messageInsert,
		m.ID, m.ConversationID, m.MessageBatchID, m.WAMID, m.Direction, m.SenderType,
		m.SenderAIAgentID, m.SenderOperatorID, m.ContentText, m.ContentType, m.Status, m.SentAt)
	if err != nil {
		return fmt.Errorf("conversations: insert message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicateWAMID
	}
	return nil
}

// LinkToBatch is the one column of messages that is written after the fact: the batch is opened by turn/ once the message exists.
func (r *Repository) LinkToBatch(ctx context.Context, messageID, batchID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, `UPDATE messages SET message_batch_id = $2 WHERE id = $1`, messageID, batchID)
	if err != nil {
		return fmt.Errorf("conversations: link message to batch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The first send of a conversation is also its first response, and both land in one statement so neither can exist without the
// other. first_response_at does not tell the agent from the operator on purpose: F34 separates them from messages
// (05-consultas-de-metricas, F34.b supuesto S1). A failed send is not a response, which is why this is here and not where the row
// is written: the first response is the first answer the customer received, while taking the pool is the first one sent
// (ADR-0035, regla 7; F34.b supuesto S3).
//
// The one sender that does not count is system: the fixed text of a cut turn answers nothing, so taking it as the first response
// would report seconds of response time on a conversation nobody attended (ADR-0042, O2). Excluded here and in F34.b alike.
//
// It takes the database clock and not the caller's, because F34 subtracts created_at from it and both have to be one clock (C06).
//
// Counting sent is what tells a missing message apart; first runs although the SELECT never names it, as every data-modifying WITH does.
const messageMarkSent = `
WITH sent AS (
       UPDATE messages SET wamid = $2, sent_at = $3, status = $4 WHERE id = $1
       RETURNING conversation_id, sender_type
), first AS (
       UPDATE conversations c
       SET    first_response_at = now(), updated_at = now()
       FROM   sent
       WHERE  c.id = sent.conversation_id AND c.first_response_at IS NULL
         AND  sent.sender_type <> $5
)
SELECT count(*) FROM sent`

// MarkSent writes what only Meta knows, which is why the row was written without it (07-secuencia, hallazgo 3).
func (r *Repository) MarkSent(ctx context.Context, messageID uuid.UUID, wamid string, sentAt time.Time) error {
	var updated int
	if err := r.exec.QueryRow(ctx, messageMarkSent, messageID, wamid, sentAt,
		enums.MessageStatusSent, enums.SenderTypeSystem).Scan(&updated); err != nil {
		return fmt.Errorf("conversations: mark message sent: %w", err)
	}
	if updated == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkFailed leaves the failure visible instead of retrying it: ADR-0020 sends at most once and never twice.
func (r *Repository) MarkFailed(ctx context.Context, messageID uuid.UUID, errorCode string) error {
	tag, err := r.exec.Exec(ctx,
		`UPDATE messages SET status = $2, error_code = $3 WHERE id = $1`,
		messageID, enums.MessageStatusFailed, errorCode)
	if err != nil {
		return fmt.Errorf("conversations: mark message failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
