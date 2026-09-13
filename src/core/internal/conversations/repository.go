package conversations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Postgres says 23505 when a unique index rejects the row, which is how the open conversation race announces itself.
const uniqueViolation = "23505"

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

const conversationSelect = `
SELECT id, contact_id, inbox_id, ai_agent_id, status, mode, last_inbound_at, window_expires_at
FROM   conversations
WHERE  deleted_at IS NULL`

// OpenByContact reads the at most one unresolved conversation the partial unique index of ADR-0002 allows.
func (r *Repository) OpenByContact(ctx context.Context, contactID uuid.UUID) (Conversation, error) {
	row := r.exec.QueryRow(ctx, conversationSelect+" AND contact_id = $1 AND status <> 'resolved'", contactID)

	var c Conversation
	err := row.Scan(&c.ID, &c.ContactID, &c.InboxID, &c.AIAgentID, &c.Status, &c.Mode, &c.LastInboundAt, &c.WindowExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	if err != nil {
		return Conversation{}, fmt.Errorf("conversations: scan conversation: %w", err)
	}
	return c, nil
}

// ON CONFLICT and not a rescued error: a failed statement aborts the transaction, so the re-read would never run (SQLSTATE 25P02).
const conversationInsert = `
INSERT INTO conversations (id, contact_id, inbox_id, ai_agent_id, status, mode, last_inbound_at, window_expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (contact_id) WHERE status <> 'resolved' AND deleted_at IS NULL DO NOTHING`

// Create returns ErrAlreadyOpen when it loses the race, because the index and not the earlier read is what decides.
func (r *Repository) Create(ctx context.Context, c Conversation) error {
	tag, err := r.exec.Exec(ctx, conversationInsert,
		c.ID, c.ContactID, c.InboxID, c.AIAgentID, c.Status, c.Mode, c.LastInboundAt, c.WindowExpiresAt)
	if err != nil {
		return fmt.Errorf("conversations: insert conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAlreadyOpen
	}
	return nil
}

const messageInsert = `
INSERT INTO messages (id, conversation_id, message_batch_id, wamid, direction, sender_type,
                      sender_ai_agent_id, sender_operator_id, content_text, content_type, status, sent_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

// AppendMessage is the only way into messages: the table admits no update of content and no delete at all (F29, C09).
func (r *Repository) AppendMessage(ctx context.Context, m Message) error {
	_, err := r.exec.Exec(ctx, messageInsert,
		m.ID, m.ConversationID, m.MessageBatchID, m.WAMID, m.Direction, m.SenderType,
		m.SenderAIAgentID, m.SenderOperatorID, m.ContentText, m.ContentType, m.Status, m.SentAt)
	if isUniqueViolation(err) {
		return ErrDuplicateWAMID
	}
	if err != nil {
		return fmt.Errorf("conversations: insert message: %w", err)
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
// other. first_response_at does not tell the agent from the operator on purpose (05-consultas-de-metricas, supuesto S1): F34
// separates them from messages. A failed send is not a response, which is why this is here and not where the row is written.
//
// The count is of the messages updated, so a missing message still reads as one; the second statement runs whether or not the
// select names it, as every data-modifying WITH does.
const messageMarkSent = `
WITH sent AS (
       UPDATE messages SET wamid = $2, sent_at = $3, status = $4 WHERE id = $1
       RETURNING conversation_id
), first AS (
       UPDATE conversations c
       SET    first_response_at = $3, updated_at = now()
       FROM   sent
       WHERE  c.id = sent.conversation_id AND c.first_response_at IS NULL
)
SELECT count(*) FROM sent`

// MarkSent writes what only Meta knows, which is why the row was written without it (07-secuencia, hallazgo 3).
func (r *Repository) MarkSent(ctx context.Context, messageID uuid.UUID, wamid string, sentAt time.Time) error {
	var updated int
	if err := r.exec.QueryRow(ctx, messageMarkSent, messageID, wamid, sentAt, enums.MessageStatusSent).Scan(&updated); err != nil {
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

// TouchInbound moves the clocks an inbound message resets, so the 24 hour window is never read from a stale row.
func (r *Repository) TouchInbound(ctx context.Context, conversationID uuid.UUID, at time.Time) error {
	tag, err := r.exec.Exec(ctx,
		`UPDATE conversations
		 SET    last_inbound_at = $2, window_expires_at = $3, last_activity_at = $2, updated_at = now()
		 WHERE  id = $1`,
		conversationID, at, at.Add(ServiceWindow))
	if err != nil {
		return fmt.Errorf("conversations: touch inbound: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchOutbound takes the database clock and not the caller's, because it is the one clock every writer of this column shares
// (migrations/README.md).
func (r *Repository) TouchOutbound(ctx context.Context, conversationID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx,
		`UPDATE conversations SET last_activity_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		conversationID)
	if err != nil {
		return fmt.Errorf("conversations: touch outbound: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The pool is in the predicate and not in an earlier read: two operators answering at once both reach this UPDATE, the second
// waits on the row lock of the first, re-reads operator_id once it commits, and matches nothing (ADR-0004).
const conversationClaim = `
UPDATE conversations
SET    operator_id = $2, updated_at = now()
WHERE  id = $1 AND operator_id IS NULL AND deleted_at IS NULL`

func (r *Repository) Claim(ctx context.Context, conversationID, operatorID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, conversationClaim, conversationID, operatorID)
	if err != nil {
		return fmt.Errorf("conversations: claim conversation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTaken
	}
	return nil
}

// Any mode but off needs an agent, and that is in the predicate and not in an earlier read, the same as the invariant EnsureOpen
// keeps when it opens a conversation with none: a mode naming a bot that does not exist would read as one that is silent.
const conversationSetMode = `
UPDATE conversations
SET    mode = $2, updated_at = now()
WHERE  id = $1 AND deleted_at IS NULL AND ($2 = 'off' OR ai_agent_id IS NOT NULL)`

// SetMode answers ErrNoAgent when nothing matched a mode that needs one: the caller read the conversation first, so a missing row
// here is the rarer case, and turning off can only miss for that reason.
func (r *Repository) SetMode(ctx context.Context, conversationID uuid.UUID, mode enums.AgentMode) error {
	tag, err := r.exec.Exec(ctx, conversationSetMode, conversationID, mode)
	if err != nil {
		return fmt.Errorf("conversations: set mode: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if mode == enums.AgentModeOff {
		return ErrNotFound
	}
	return ErrNoAgent
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
