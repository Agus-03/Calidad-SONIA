package inbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

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

// The vendor of the contact's latest earlier conversation, if it still is one: a person who is active, not taken down and of the
// operator role (ADR-0037, reglas 1 y 2). An earlier conversation nobody held says nothing, so it is skipped; but the latest vendor
// being gone means the turn decides, and not a vendor from further back.
const previousOperatorSelect = `
SELECT o.id
FROM   (
       SELECT c.operator_id
       FROM   conversations c
       WHERE  c.contact_id = $1 AND c.id <> $2 AND c.deleted_at IS NULL AND c.operator_id IS NOT NULL
       ORDER  BY c.created_at DESC, c.id DESC
       LIMIT  1
) latest
JOIN   operators o ON o.id = latest.operator_id
WHERE  o.account_id = $3 AND o.role = $4 AND o.active AND o.deleted_at IS NULL`

// PreviousOperator answers nil when the contact has no earlier vendor who can still receive it.
func (r *Repository) PreviousOperator(ctx context.Context, accountID, contactID, conversationID uuid.UUID) (*uuid.UUID, error) {
	return r.oneOperator(ctx, "previous operator", previousOperatorSelect, contactID, conversationID, accountID, enums.OperatorRoleOperator)
}

// The vendor who has gone longest without receiving a conversation, and first of all one who never received any. Every assignment
// counts, whatever its reason: a returning customer or a conversation taken from the pool is work received too (ADR-0037, regla 1).
//
// The last assignment is read as a row and not as max(created_at): now() is the start of the transaction, so the assignments of one
// payload share an instant, and only their v7 id still says which came first. Without it, the lowest operator id would take every
// conversation of the payload after the first round.
const nextInTurnSelect = `
SELECT o.id
FROM   operators o
LEFT   JOIN LATERAL (
       SELECT a.created_at, a.id
       FROM   conversation_assignments a
       WHERE  a.to_operator_id = o.id
       ORDER  BY a.created_at DESC, a.id DESC
       LIMIT  1
) last ON true
WHERE  o.account_id = $1 AND o.role = $2 AND o.active AND o.deleted_at IS NULL
ORDER  BY last.created_at ASC NULLS FIRST, last.id ASC NULLS FIRST, o.id
LIMIT  1`

// NextInTurn answers nil when no vendor is active, which leaves the conversation in the pool (ADR-0037, regla 3).
func (r *Repository) NextInTurn(ctx context.Context, accountID uuid.UUID) (*uuid.UUID, error) {
	return r.oneOperator(ctx, "next operator in turn", nextInTurnSelect, accountID, enums.OperatorRoleOperator)
}

func (r *Repository) oneOperator(ctx context.Context, what, query string, args ...any) (*uuid.UUID, error) {
	var operatorID uuid.UUID
	err := r.exec.QueryRow(ctx, query, args...).Scan(&operatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inbox: read the %s: %w", what, err)
	}
	return &operatorID, nil
}

// Whether the person can take conversations now, read in the caller's account: one of another account is one that does not exist.
const operatorState = `
SELECT o.active AND o.deleted_at IS NULL
FROM   operators o
WHERE  o.id = $1 AND o.account_id = $2`

func (r *Repository) OperatorState(ctx context.Context, accountID, operatorID uuid.UUID) (bool, error) {
	var available bool
	err := r.exec.QueryRow(ctx, operatorState, operatorID, accountID).Scan(&available)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrOperatorNotFound
	}
	if err != nil {
		return false, fmt.Errorf("inbox: read the operator: %w", err)
	}
	return available, nil
}

// Oldest first, which is how a history is read; the id breaks the tie of rows written in one transaction, like a handover's.
const assignmentsSelect = `
SELECT a.id, COALESCE(a.reason, ''), a.created_at,
       f.id, f.name, f.lastname,
       t.id, t.name, t.lastname,
       b.id, b.name, b.lastname
FROM   conversation_assignments a
JOIN   operators t ON t.id = a.to_operator_id
LEFT   JOIN operators f ON f.id = a.from_operator_id
LEFT   JOIN operators b ON b.id = a.assigned_by_operator_id
WHERE  a.conversation_id = $1
ORDER  BY a.created_at, a.id`

func (r *Repository) Assignments(ctx context.Context, conversationID uuid.UUID) ([]AssignmentEntry, error) {
	rows, err := r.exec.Query(ctx, assignmentsSelect, conversationID)
	if err != nil {
		return nil, fmt.Errorf("inbox: read the assignments: %w", err)
	}
	defer rows.Close()

	out := []AssignmentEntry{}
	for rows.Next() {
		var e AssignmentEntry
		var fromID, byID *uuid.UUID
		var fromName, fromLastname, byName, byLastname *string
		if err := rows.Scan(&e.ID, &e.Reason, &e.CreatedAt, &fromID, &fromName, &fromLastname,
			&e.To.ID, &e.To.Name, &e.To.Lastname, &byID, &byName, &byLastname); err != nil {
			return nil, fmt.Errorf("inbox: scan an assignment: %w", err)
		}
		e.From = named(fromID, fromName, fromLastname)
		e.AssignedBy = named(byID, byName, byLastname)
		out = append(out, e)
	}
	return out, rows.Err()
}

// named is the person of a LEFT JOIN, which arrives as three nulls when there is none.
func named(operatorID *uuid.UUID, name, lastname *string) *Operator {
	if operatorID == nil || name == nil || lastname == nil {
		return nil
	}
	return &Operator{ID: *operatorID, Name: *name, Lastname: *lastname}
}
