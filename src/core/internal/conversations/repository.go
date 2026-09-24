package conversations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

const conversationSelect = `
SELECT id, contact_id, inbox_id, ai_agent_id, operator_id, status, mode, last_inbound_at, window_expires_at
FROM   conversations
WHERE  deleted_at IS NULL`

// OpenByContact reads the at most one unresolved conversation the partial unique index of ADR-0002 allows.
func (r *Repository) OpenByContact(ctx context.Context, contactID uuid.UUID) (Conversation, error) {
	row := r.exec.QueryRow(ctx, conversationSelect+" AND contact_id = $1 AND status <> 'resolved'", contactID)

	var c Conversation
	err := row.Scan(&c.ID, &c.ContactID, &c.InboxID, &c.AIAgentID, &c.OperatorID, &c.Status, &c.Mode, &c.LastInboundAt, &c.WindowExpiresAt)
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
// It takes the database clock and not the caller's, because F34 subtracts created_at from it and both have to be one clock (C06).
//
// Counting sent is what tells a missing message apart; first runs although the SELECT never names it, as every data-modifying WITH does.
const messageMarkSent = `
WITH sent AS (
       UPDATE messages SET wamid = $2, sent_at = $3, status = $4 WHERE id = $1
       RETURNING conversation_id
), first AS (
       UPDATE conversations c
       SET    first_response_at = now(), updated_at = now()
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
//
// The window keeps Meta's clock, because Meta is the one that measures it (ADR-0008); the activity takes the database clock, the one
// TouchOutbound writes too, so the inbox never orders by two clocks at once (C06). Both only move forward: Meta may deliver an older
// message after a newer one, and that late webhook must not shorten the window or sink the conversation in the inbox.
func (r *Repository) TouchInbound(ctx context.Context, conversationID uuid.UUID, at time.Time) error {
	tag, err := r.exec.Exec(ctx,
		`UPDATE conversations
		 SET    last_inbound_at   = GREATEST(last_inbound_at, $2),
		        window_expires_at = GREATEST(window_expires_at, $3),
		        last_activity_at  = now(),
		        updated_at        = now()
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

// The state a turn nobody answered leaves behind: the customer wrote, the bot did not reply, and somebody has to
// (07-secuencia secciones 4 y 5). The value travels as $2 and is never a literal in the statement, so a rename in enums.yaml
// cannot write a status nothing else recognises (C07). The clock is the database's, the one every writer of this row shares.
//
// A conversation somebody already resolved moves back to waiting too, and that is deliberate: the message that was not answered
// is unanswered whatever anybody had decided about the conversation a minute earlier.
const conversationMarkWaiting = `
UPDATE conversations
SET    status = $2, updated_at = now()
WHERE  id = $1 AND deleted_at IS NULL`

// MarkWaiting is the status half of a turn that ends without an answer, and the caller holds it in the same transaction as
// message_batches.processed_at: one of the two written alone is a turn that reads as finished and unattended, or the reverse.
func (r *Repository) MarkWaiting(ctx context.Context, conversationID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, conversationMarkWaiting, conversationID, enums.ConversationStatusWaiting)
	if err != nil {
		return fmt.Errorf("conversations: mark waiting: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The pool is in the predicate and not in an earlier read: two operators answering at once both reach this UPDATE, the second
// waits on the row lock of the first, re-reads operator_id once it commits, and matches nothing (ADR-0004, ADR-0035 regla 8).
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

// Any mode but off needs an agent that can answer —there, active and not removed, the same three conditions the turn reads— and
// that is in the predicate and not in an earlier read, so a pause or a removal committed after the caller's read still wins: a mode
// naming a bot that cannot answer would read as one that is silent (ADR-0035 regla 12, ADR-0038). The off value travels as $3 and
// is not written, so a renamed value in enums.yaml cannot silently stop matching (C07).
//
// FOR SHARE on the agent: a pause holds its row while it turns every bot of it off, so a switch turned on at the same instant waits
// for the pause and then reads it, instead of reading the agent as it was and leaving one bot on after the pause (ADR-0038).
const conversationSetMode = `
UPDATE conversations
SET    mode = $2, updated_at = now()
WHERE  id = $1 AND deleted_at IS NULL
  AND  ($2 = $3 OR EXISTS (
           SELECT 1 FROM ai_agents a
           WHERE  a.id = conversations.ai_agent_id AND a.active AND a.deleted_at IS NULL
           FOR SHARE))`

// Every conversation the agent holds and not only the open ones, the same as the switch of each: a resolved conversation still
// shows its switch, and one that says the bot answers would be the only one telling the truth wrong (ADR-0038). The off value
// travels as $2, for the reason SetMode gives.
const conversationTurnOffAIAgent = `
UPDATE conversations
SET    mode = $2, updated_at = now()
WHERE  ai_agent_id = $1 AND mode <> $2 AND deleted_at IS NULL
RETURNING id`

// TurnOffAIAgent answers the conversations it turned off, because the audit of the pause or the removal is the one place that
// records them: resuming the agent turns none back on.
func (r *Repository) TurnOffAIAgent(ctx context.Context, aiAgentID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.exec.Query(ctx, conversationTurnOffAIAgent, aiAgentID, enums.AgentModeOff)
	if err != nil {
		return nil, fmt.Errorf("conversations: turn off the bots of an ai agent: %w", err)
	}
	defer rows.Close()

	out := []uuid.UUID{}
	for rows.Next() {
		var conversationID uuid.UUID
		if err := rows.Scan(&conversationID); err != nil {
			return nil, fmt.Errorf("conversations: scan a conversation turned off: %w", err)
		}
		out = append(out, conversationID)
	}
	return out, rows.Err()
}

// SetMode answers ErrNoAIAgent when nothing matched a mode that needs one: the caller read the conversation first, so a missing row
// here is the rarer case, and turning off can only miss for that reason.
func (r *Repository) SetMode(ctx context.Context, conversationID uuid.UUID, mode enums.AgentMode) error {
	tag, err := r.exec.Exec(ctx, conversationSetMode, conversationID, mode, enums.AgentModeOff)
	if err != nil {
		return fmt.Errorf("conversations: set mode: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if mode == enums.AgentModeOff {
		return ErrNotFound
	}
	return ErrNoAIAgent
}
