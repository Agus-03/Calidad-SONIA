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

// TouchInbound moves the clocks an inbound message resets, so the 24 hour window is never read from a stale row.
//
// The window keeps Meta's clock, because Meta is the one that measures it (ADR-0008); the activity takes the database clock, the one
// TouchOutbound writes too, so the inbox never orders by two clocks at once (C06). Both only move forward: Meta may deliver an older
// message after a newer one, and that late webhook must not shorten the window or sink the conversation in the inbox.
//
// Every customer message counts one more unread, in the same statement: a redelivery never gets here, because its duplicate wamid
// refused the insert first (ADR-0052).
func (r *Repository) TouchInbound(ctx context.Context, conversationID uuid.UUID, at time.Time) error {
	tag, err := r.exec.Exec(ctx,
		`UPDATE conversations
		 SET    last_inbound_at   = GREATEST(last_inbound_at, $2),
		        window_expires_at = GREATEST(window_expires_at, $3),
		        last_activity_at  = now(),
		        unread_count      = unread_count + 1,
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

// last_activity_at is left alone, for the reason the star leaves it: reading is not activity of the channel, and moving the row would
// shift every cursor in circulation (ADR-0036, ADR-0052).
const conversationMarkRead = `
UPDATE conversations
SET    unread_count = 0, last_read_at = now(), updated_at = now()
WHERE  id = $1 AND deleted_at IS NULL`

func (r *Repository) MarkRead(ctx context.Context, conversationID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, conversationMarkRead, conversationID)
	if err != nil {
		return fmt.Errorf("conversations: mark read: %w", err)
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

// last_activity_at is left alone on purpose: the inbox is ordered by it, and a star that moved the conversation up would shift the
// pages of every cursor in circulation for something the customer did not do (ADR-0036).
const conversationSetStarred = `
UPDATE conversations
SET    starred = $2, updated_at = now()
WHERE  id = $1 AND deleted_at IS NULL`

func (r *Repository) SetStarred(ctx context.Context, conversationID uuid.UUID, starred bool) error {
	tag, err := r.exec.Exec(ctx, conversationSetStarred, conversationID, starred)
	if err != nil {
		return fmt.Errorf("conversations: set starred: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Only the status and who closed: mode, vendor, unread and last_activity_at stay, and of two closes at once one writes (ADR-0054).
// busy is a batch open, or closed and neither processed nor failed within the budget: older than that, a crash killed it (ADR-0054).
const conversationResolve = `
WITH busy AS (
    SELECT EXISTS (
           SELECT 1 FROM message_batches b
           WHERE  b.conversation_id = $1 AND b.processed_at IS NULL AND b.error_message IS NULL
             AND  (b.closed_at IS NULL OR b.closed_at > now() - ($4 * interval '1 millisecond'))) AS turn
), closed AS (
    UPDATE conversations
    SET    status = $2, resolved_at = now(), resolved_by_operator_id = $3, updated_at = now()
    FROM   busy
    WHERE  id = $1 AND status <> $2 AND deleted_at IS NULL AND NOT busy.turn
    RETURNING 1
)
SELECT (SELECT turn FROM busy), (SELECT count(*) FROM closed)`

// Nothing written and no turn in flight is read as ErrResolved: the caller read the row first, so a close in between is the likely reason.
func (r *Repository) Resolve(ctx context.Context, conversationID, operatorID uuid.UUID, turnBudget time.Duration) error {
	var busy bool
	var closed int
	if err := r.exec.QueryRow(ctx, conversationResolve, conversationID, enums.ConversationStatusResolved, operatorID,
		turnBudget.Milliseconds()).Scan(&busy, &closed); err != nil {
		return fmt.Errorf("conversations: resolve: %w", err)
	}
	switch {
	case closed > 0:
		return nil
	case busy:
		return ErrTurnInProgress
	default:
		return ErrResolved
	}
}

// The vendor and the status are in the predicate, and a new score overwrites the last with its author and instant (ADR-0055).
const conversationScore = `
UPDATE conversations
SET    quality_score = $2, scored_by_operator_id = $3, scored_at = now(), updated_at = now()
WHERE  id = $1 AND operator_id = $3 AND status = $4 AND deleted_at IS NULL`

// Nothing matched is read as ErrNotFound: the caller read a resolved conversation of its own first, and a resolved one never reopens.
func (r *Repository) Score(ctx context.Context, conversationID, operatorID uuid.UUID, score int) error {
	tag, err := r.exec.Exec(ctx, conversationScore, conversationID, score, operatorID, enums.ConversationStatusResolved)
	if err != nil {
		return fmt.Errorf("conversations: score: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The vendor the caller read is in the predicate, NULL included, so two reassignments at once write one and the other is refused (ADR-0056).
const conversationReassign = `
UPDATE conversations
SET    operator_id = $3, updated_at = now()
WHERE  id = $1 AND operator_id IS NOT DISTINCT FROM $2 AND status <> $4 AND deleted_at IS NULL`

// Nothing matched is read as ErrTaken: the caller read the vendor first, so another reassignment or the pool taken in between is why.
func (r *Repository) Reassign(ctx context.Context, conversationID uuid.UUID, from *uuid.UUID, to uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, conversationReassign, conversationID, from, to, enums.ConversationStatusResolved)
	if err != nil {
		return fmt.Errorf("conversations: reassign: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTaken
	}
	return nil
}

// Every open conversation of the vendor and none resolved: those keep who attended them, for the sale and the score (ADR-0056).
const conversationHandOver = `
UPDATE conversations
SET    operator_id = $2, updated_at = now()
WHERE  operator_id = $1 AND status <> $3 AND deleted_at IS NULL
RETURNING id`

// HandOver answers the conversations it moved, because each needs its own row of history and its own event.
func (r *Repository) HandOver(ctx context.Context, from, to uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.exec.Query(ctx, conversationHandOver, from, to, enums.ConversationStatusResolved)
	if err != nil {
		return nil, fmt.Errorf("conversations: hand over: %w", err)
	}
	defer rows.Close()

	moved := []uuid.UUID{}
	for rows.Next() {
		var conversationID uuid.UUID
		if err := rows.Scan(&conversationID); err != nil {
			return nil, fmt.Errorf("conversations: scan a conversation handed over: %w", err)
		}
		moved = append(moved, conversationID)
	}
	return moved, rows.Err()
}
