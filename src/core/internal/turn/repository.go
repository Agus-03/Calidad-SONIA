package turn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// Repository takes the Executor and not the pool, so the transaction this package opens reaches the other repositories (regla 5).
type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

// Reads three tables this package does not own: the ownership rule of regla 1 restricts writes and not reads.
// The agent is read here and not trusted from the webhook: a pause or a removal after the message arrived still has to win (ADR-0038).
const targetSelect = `
SELECT    c.id, c.inbox_id, ct.phone_whatsapp, c.mode, c.ai_agent_id,
          COALESCE(a.active AND a.deleted_at IS NULL, false)
FROM      conversations c
JOIN      contacts ct ON ct.id = c.contact_id
LEFT JOIN ai_agents a ON a.id = c.ai_agent_id
WHERE     c.id = $1 AND c.deleted_at IS NULL`

func (r *Repository) Target(ctx context.Context, conversationID uuid.UUID) (Target, error) {
	var t Target
	err := r.exec.QueryRow(ctx, targetSelect, conversationID).
		Scan(&t.ConversationID, &t.InboxID, &t.ContactPhone, &t.Mode, &t.AIAgentID, &t.AIAgentAvailable)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrConversationNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("turn: scan target: %w", err)
	}
	return t, nil
}

// Postgres says 23505 when a unique index rejects the row: the one that rejects here is uq_message_batches_conversation_id_open,
// and it means another delivery opened the batch of this conversation in between the two statements of Append (ADR-0041, regla 3).
const uniqueViolation = "23505"

// errBatchRaced never leaves the package: a transaction that hit 23505 cannot go on, so the collector answers it by running the
// whole transaction again, and the second attempt finds the batch the winner opened.
var errBatchRaced = errors.New("turn: another delivery opened the batch first")

// The window is added by PostgreSQL and not by the process clock, because the poller compares expires_at against now() of that
// same server: two clocks would make the wait longer or shorter than the window by however much they disagree (C06).
// The count is of what is already linked, so the message on its way in is the +1 that no row holds yet (ADR-0041, regla 5).
const batchExtend = `
UPDATE message_batches b
SET    expires_at = CASE
           WHEN (SELECT count(*) FROM messages m WHERE m.message_batch_id = b.id) + 1 >= $2 THEN now()
           ELSE now() + ($3 * interval '1 millisecond')
       END
WHERE  b.conversation_id = $1 AND b.closed_at IS NULL
RETURNING b.id, b.turn_number, b.opened_at, b.expires_at`

// timer_ms_used stays at zero until the close: it records the window that was applied, and the poller is the one sentence that
// writes every column of the close at once (ADR-0041, reglas 4 y 6). opened_at is left to its DEFAULT, which is the same now().
const batchOpen = `
INSERT INTO message_batches (id, conversation_id, turn_number, timer_ms_used, expires_at)
SELECT $1, $2, COALESCE(MAX(b.turn_number), 0) + 1, 0,
       CASE WHEN 1 >= $3 THEN now() ELSE now() + ($4 * interval '1 millisecond') END
FROM   message_batches b
WHERE  b.conversation_id = $2
RETURNING id, turn_number, opened_at, expires_at`

// Append puts the message that is about to be linked into the conversation's open batch, opening one if there is none, and starts
// the window over on it (ADR-0041, regla 2). The link itself belongs to conversations/ and is written in this same transaction.
func (r *Repository) Append(ctx context.Context, conversationID uuid.UUID, window time.Duration, maxMessages int) (Batch, error) {
	batch := Batch{ConversationID: conversationID}
	windowMS := window.Milliseconds()

	err := r.exec.QueryRow(ctx, batchExtend, conversationID, maxMessages, windowMS).
		Scan(&batch.ID, &batch.TurnNumber, &batch.OpenedAt, &batch.ExpiresAt)
	if err == nil {
		return batch, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, fmt.Errorf("turn: extend the open batch: %w", err)
	}

	// No open batch, so this message is the first of a turn: which of two simultaneous ones opens it is the index's to decide.
	err = r.exec.QueryRow(ctx, batchOpen, newID(), conversationID, maxMessages, windowMS).
		Scan(&batch.ID, &batch.TurnNumber, &batch.OpenedAt, &batch.ExpiresAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return Batch{}, errBatchRaced
	}
	if err != nil {
		return Batch{}, fmt.Errorf("turn: open a batch: %w", err)
	}
	return batch, nil
}

// One statement for the whole close, which is what makes a closed batch a fact and not a race (ADR-0041, regla 4). SKIP LOCKED
// so a tick that ran long and the next one never fight over the same row, and so two instances would not close it twice.
// The reasons are ordered, not exclusive: a batch can be over the cap and over the maximum duration at once, and attributing to
// silence a wait the cap deliberately cut would hide the one case F10 does not describe (regla 5).
// A batch is due for either reason, and the second one is why max_duration is a cap and not just a label: whoever writes every
// nine seconds never lets expires_at pass, so without it that batch stays open forever and never gets answered (regla 5).
const batchClose = `
WITH due AS (
    SELECT b.id
    FROM   message_batches b
    WHERE  b.closed_at IS NULL
    AND    (b.expires_at <= now()
            OR b.opened_at + ($4 * interval '1 millisecond') <= now())
    ORDER  BY b.expires_at
    LIMIT  $1
    FOR    UPDATE SKIP LOCKED
)
UPDATE message_batches b
SET    closed_at      = now(),
       messages_count = counted.total,
       timer_ms_used  = $2,
       closed_reason  = CASE
           WHEN counted.total >= $3                                      THEN 'max_messages'
           WHEN b.opened_at + ($4 * interval '1 millisecond') <= now()   THEN 'max_duration'
           ELSE                                                               'silence'
       END
FROM   due, LATERAL (SELECT count(*) FROM messages m WHERE m.message_batch_id = due.id) AS counted (total)
WHERE  b.id = due.id
RETURNING b.id, b.conversation_id, b.turn_number, b.opened_at, b.expires_at,
          b.messages_count, b.timer_ms_used, b.closed_reason`

// Close takes at most limit batches per tick. ADR-0029 asks for one goroutine per turn and no queue, and this adds none: it only
// bounds how many a single tick may open, so a core that was down for an hour drains its backlog over several ticks instead of
// calling the LLM a hundred times in the same second.
func (r *Repository) Close(ctx context.Context, window, maxDuration time.Duration, maxMessages, limit int) ([]Batch, error) {
	rows, err := r.exec.Query(ctx, batchClose, limit, window.Milliseconds(), maxMessages, maxDuration.Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("turn: close the due batches: %w", err)
	}
	defer rows.Close()

	closed := []Batch{}
	for rows.Next() {
		var b Batch
		if err := rows.Scan(&b.ID, &b.ConversationID, &b.TurnNumber, &b.OpenedAt, &b.ExpiresAt,
			&b.MessagesCount, &b.TimerMsUsed, &b.ClosedReason); err != nil {
			return nil, fmt.Errorf("turn: scan a closed batch: %w", err)
		}
		closed = append(closed, b)
	}
	return closed, rows.Err()
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

// RecordTransfer writes the escalation of F12 that the brain proposed, in the transaction that also moves the conversation: the
// event points at the evidence and never duplicates it (H-18), so the signal, its score and the threshold that was in force stay in
// signal_evaluations and F36 reaches the type of signal by join.
//
// to_operator_id stays NULL because nobody in particular is taking it: MarkWaiting runs in this same transaction and whoever is on
// the inbox picks the conversation up. An assignment would be claiming a person who has not seen it.
func (r *Repository) RecordTransfer(ctx context.Context, conversationID, signalEvaluationID uuid.UUID, turnNumber int) error {
	_, err := r.exec.Exec(ctx, `
		INSERT INTO transfer_events (id, conversation_id, signal_evaluation_id, turn_number)
		VALUES ($1, $2, $3, $4)`,
		id.New(), conversationID, signalEvaluationID, turnNumber)
	if err != nil {
		return fmt.Errorf("turn: record transfer: %w", err)
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
