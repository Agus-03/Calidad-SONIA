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

// ON CONFLICT and not a rescued error: a statement that fails aborts the whole transaction, so the re-read would never run (SQLSTATE 25P02).
// The predicate is repeated because that is how Postgres infers the partial index of ADR-0002.
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

// MarkSent writes what only Meta knows, which is why the row was written without it (07-secuencia, hallazgo 3).
func (r *Repository) MarkSent(ctx context.Context, messageID uuid.UUID, wamid string, sentAt time.Time) error {
	tag, err := r.exec.Exec(ctx,
		`UPDATE messages SET wamid = $2, sent_at = $3, status = $4 WHERE id = $1`,
		messageID, wamid, sentAt, enums.MessageStatusSent)
	if err != nil {
		return fmt.Errorf("conversations: mark message sent: %w", err)
	}
	if tag.RowsAffected() == 0 {
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
