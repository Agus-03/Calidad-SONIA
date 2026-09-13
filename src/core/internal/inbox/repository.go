package inbox

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

// Reads four tables this package does not own: the ownership rule of regla 1 restricts writes and not reads, as turn/ already does.
//
// The last message is ordered by created_at and not by sent_at because sent_at mixes two clocks — Meta's for an inbound message, ours
// for an outbound draft — and only ours orders both directions on one line (C06). The id breaks the tie of a single transaction.
//
// Hidden messages are left out of both reads: hidden_at means "not shown in the inbox", and this is the inbox (C09).
const summarySelect = `
SELECT c.id, c.status, c.mode, c.ai_agent_id,
       ct.id, ct.name, ct.lastname, ct.phone_whatsapp,
       op.id, op.name, op.lastname,
       latest.id, latest.direction, latest.sender_type, latest.content_text, latest.content_type,
       latest.status, latest.error_code, latest.sent_at,
       failed.failed_count, failed.last_failed_at,
       c.last_inbound_at, c.window_expires_at, c.last_activity_at
FROM   conversations c
JOIN   contacts ct ON ct.id = c.contact_id
LEFT   JOIN operators op ON op.id = c.operator_id
LEFT   JOIN LATERAL (
       SELECT m.id, m.direction, m.sender_type, m.content_text, m.content_type, m.status, m.error_code, m.sent_at
       FROM   messages m
       WHERE  m.conversation_id = c.id AND m.hidden_at IS NULL
       ORDER  BY m.created_at DESC, m.id DESC
       LIMIT  1
) latest ON true
CROSS  JOIN LATERAL (
       SELECT count(*) AS failed_count, max(f.created_at) AS last_failed_at
       FROM   messages f
       WHERE  f.conversation_id = c.id AND f.status = 'failed' AND f.hidden_at IS NULL
) failed
WHERE  c.deleted_at IS NULL AND ct.account_id = $1`

// reach is built and not written as a constant because the narrowing adds an argument, and returning the args with the SQL is what
// keeps the placeholders from drifting apart.
//
// The operator sees what is assigned to it and the pool. Anexo B forbids it the conversations "de otros operadores", and one in the
// pool is nobody's; hiding the pool would leave nobody able to answer first, which is how ADR-0004 assigns at all.
func reach(s Scope) (where string, args []any) {
	args = []any{s.AccountID}
	if s.seesTheWholeTeam() {
		return "", args
	}
	args = append(args, s.OperatorID)
	return fmt.Sprintf(" AND (c.operator_id = $%d OR c.operator_id IS NULL)", len(args)), args
}

// List answers up to limit rows after the cursor, newest activity first. Deciding whether another page exists is the service's.
//
// The row comparison is what makes the cursor exact: both columns descend together, so "(a, b) < (x, y)" is "strictly after the row
// the last page ended on", and the id settles two conversations touched in the same microsecond.
func (r *Repository) List(ctx context.Context, scope Scope, after *Cursor, limit int) ([]Summary, error) {
	where, args := reach(scope)
	if after != nil {
		args = append(args, after.LastActivityAt, after.ID)
		where += fmt.Sprintf(" AND (c.last_activity_at, c.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit)
	listing := summarySelect + where + fmt.Sprintf(" ORDER BY c.last_activity_at DESC, c.id DESC LIMIT $%d", len(args))

	rows, err := r.exec.Query(ctx, listing, args...)
	if err != nil {
		return nil, fmt.Errorf("inbox: list conversations: %w", err)
	}
	defer rows.Close()

	out := []Summary{}
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inbox: read conversations: %w", err)
	}
	return out, nil
}

// Scoped by the account of the contact, like the listing: a conversation of another account is one this caller cannot know exists.
const targetSelect = `
SELECT c.id, c.inbox_id, ct.phone_whatsapp, c.operator_id, c.status, c.window_expires_at
FROM   conversations c
JOIN   contacts ct ON ct.id = c.contact_id
WHERE  c.deleted_at IS NULL AND ct.account_id = $1 AND c.id = $2`

// Target reads without the reach on purpose: whether this operator may send is a rule, and rules are the service's.
func (r *Repository) Target(ctx context.Context, accountID, conversationID uuid.UUID) (Target, error) {
	var t Target
	err := r.exec.QueryRow(ctx, targetSelect, accountID, conversationID).
		Scan(&t.ConversationID, &t.InboxID, &t.ContactPhone, &t.OperatorID, &t.Status, &t.WindowExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("inbox: scan target: %w", err)
	}
	return t, nil
}

const assignmentInsert = `
INSERT INTO conversation_assignments (id, conversation_id, from_operator_id, to_operator_id, assigned_by_operator_id, reason)
VALUES ($1, $2, $3, $4, $5, nullif($6, ''))`

// RecordAssignment is only ever an INSERT: the table is the history F26 asks for, and history is not corrected (C09).
func (r *Repository) RecordAssignment(ctx context.Context, a Assignment) error {
	if _, err := r.exec.Exec(ctx, assignmentInsert,
		a.ID, a.ConversationID, a.FromOperatorID, a.ToOperatorID, a.AssignedByOperatorID, a.Reason); err != nil {
		return fmt.Errorf("inbox: record assignment: %w", err)
	}
	return nil
}

// The two LEFT JOINs arrive as nulls, so they are read into locals and moved to the model only when the group is there.
func scanSummary(row pgx.Row) (Summary, error) {
	var s Summary

	var operatorID *uuid.UUID
	var operatorName, operatorLastname *string

	var messageID *uuid.UUID
	var direction *enums.MessageDirection
	var sender *enums.SenderType
	var text *string
	var contentType *enums.MessageContentType
	var status *enums.MessageStatus
	var errorCode *string
	var sentAt *time.Time

	err := row.Scan(&s.ID, &s.Status, &s.Mode, &s.AIAgentID,
		&s.Contact.ID, &s.Contact.Name, &s.Contact.Lastname, &s.Contact.PhoneWhatsapp,
		&operatorID, &operatorName, &operatorLastname,
		&messageID, &direction, &sender, &text, &contentType, &status, &errorCode, &sentAt,
		&s.FailedCount, &s.LastFailedAt,
		&s.LastInboundAt, &s.WindowExpiresAt, &s.LastActivityAt)
	if err != nil {
		return Summary{}, fmt.Errorf("inbox: scan conversation: %w", err)
	}

	if operatorID != nil && operatorName != nil && operatorLastname != nil {
		s.Operator = &Operator{ID: *operatorID, Name: *operatorName, Lastname: *operatorLastname}
	}
	if messageID != nil && direction != nil && sender != nil && text != nil && contentType != nil && sentAt != nil {
		s.LastMessage = &Message{
			ID: *messageID, Direction: *direction, SenderType: *sender, ContentText: *text, ContentType: *contentType,
			Status: status, ErrorCode: errorCode, SentAt: *sentAt,
		}
	}
	return s, nil
}
