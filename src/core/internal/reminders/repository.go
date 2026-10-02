package reminders

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// recipient is the active vendor of the contact's latest conversation that has one, or whoever created the reminder (ADR-0058).
const recipient = `
COALESCE((
       SELECT c.operator_id
       FROM   conversations c
       JOIN   operators o ON o.id = c.operator_id AND o.active AND o.deleted_at IS NULL
       WHERE  c.contact_id = r.contact_id AND c.deleted_at IS NULL
       ORDER  BY c.created_at DESC, c.id DESC
       LIMIT  1), r.operator_id)`

// The account is read through the contact, so a reminder of another account is one that does not exist (ADR-0032).
const reminderSelect = `
SELECT r.id, r.contact_id, r.conversation_id, r.operator_id, r.description, r.due_at, r.notified, r.notified_at, r.created_at
FROM   reminders r
JOIN   contacts ct ON ct.id = r.contact_id AND ct.deleted_at IS NULL
WHERE  r.deleted_at IS NULL AND ct.account_id = $1`

func scanReminder(row pgx.Row) (Reminder, error) {
	var r Reminder
	err := row.Scan(&r.ID, &r.ContactID, &r.ConversationID, &r.OperatorID, &r.Description, &r.DueAt, &r.Notified, &r.NotifiedAt, &r.CreatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, fmt.Errorf("reminders: scan reminder: %w", err)
	}
	return r, err
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]Reminder, error) {
	rows, err := r.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reminders: list: %w", err)
	}
	defer rows.Close()

	out := []Reminder{}
	for rows.Next() {
		rem, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rem)
	}
	return out, rows.Err()
}

func (r *Repository) ByID(ctx context.Context, accountID, reminderID uuid.UUID) (Reminder, error) {
	rem, err := scanReminder(r.exec.QueryRow(ctx, reminderSelect+" AND r.id = $2", accountID, reminderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, ErrNotFound
	}
	return rem, err
}

// OfContact is soonest first, which is the order a person works through what is coming.
func (r *Repository) OfContact(ctx context.Context, accountID, contactID uuid.UUID) ([]Reminder, error) {
	return r.list(ctx, reminderSelect+" AND r.contact_id = $2 ORDER BY r.due_at, r.id", accountID, contactID)
}

// Addressed is what reaches this person when it falls due, read with the same rule the poller tells it by (ADR-0058).
func (r *Repository) Addressed(ctx context.Context, accountID, operatorID uuid.UUID, onlyDue bool) ([]Reminder, error) {
	query := reminderSelect + " AND " + recipient + " = $2"
	if onlyDue {
		query += " AND r.due_at <= now()"
	}
	return r.list(ctx, query+" ORDER BY r.due_at, r.id", accountID, operatorID)
}

// ConversationOfContact reads conversations without writing them: the reminder only points at one (ADR-0002 (c)).
func (r *Repository) ConversationOfContact(ctx context.Context, conversationID, contactID uuid.UUID) (bool, error) {
	var of bool
	err := r.exec.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM conversations WHERE id = $1 AND contact_id = $2 AND deleted_at IS NULL)`,
		conversationID, contactID).Scan(&of)
	if err != nil {
		return false, fmt.Errorf("reminders: read the conversation: %w", err)
	}
	return of, nil
}

func (r *Repository) Create(ctx context.Context, rem Reminder) error {
	if _, err := r.exec.Exec(ctx, `
		INSERT INTO reminders (id, contact_id, conversation_id, operator_id, description, due_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		rem.ID, rem.ContactID, rem.ConversationID, rem.OperatorID, rem.Description, rem.DueAt); err != nil {
		return fmt.Errorf("reminders: create: %w", err)
	}
	return nil
}

// A new due date makes it pending again, read off the old value in the same statement, so a moved alert fires once more (ADR-0058).
const reminderUpdate = `
UPDATE reminders
SET    description = $2, due_at = $3,
       notified    = CASE WHEN due_at = $3 THEN notified ELSE false END,
       notified_at = CASE WHEN due_at = $3 THEN notified_at ELSE NULL END,
       updated_at  = now()
WHERE  id = $1 AND deleted_at IS NULL`

func (r *Repository) Update(ctx context.Context, reminderID uuid.UUID, description string, dueAt time.Time) error {
	return r.write(ctx, "update", reminderUpdate, reminderID, description, dueAt)
}

// Deleting is the one way a reminder is done, and the row stays (C09, ADR-0050 regla 6).
func (r *Repository) Delete(ctx context.Context, reminderID uuid.UUID) error {
	return r.write(ctx, "delete",
		`UPDATE reminders SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, reminderID)
}

func (r *Repository) write(ctx context.Context, what, query string, args ...any) error {
	tag, err := r.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("reminders: %s: %w", what, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// One statement marks and answers, SKIP LOCKED like the turn's poller, so an alert is told at most once (ADR-0041 regla 4, ADR-0058).
const reminderDue = `
WITH due AS (
    SELECT r.id
    FROM   reminders r
    WHERE  r.deleted_at IS NULL AND NOT r.notified AND r.due_at <= now()
    ORDER  BY r.due_at
    LIMIT  $1
    FOR    UPDATE SKIP LOCKED
)
UPDATE reminders r
SET    notified = true, notified_at = now(), updated_at = now()
FROM   due
WHERE  r.id = due.id
RETURNING r.id, r.contact_id, r.conversation_id, r.operator_id, r.description, r.due_at, r.notified, r.notified_at, r.created_at,
          ` + recipient

func (r *Repository) Due(ctx context.Context, limit int) ([]Due, error) {
	rows, err := r.exec.Query(ctx, reminderDue, limit)
	if err != nil {
		return nil, fmt.Errorf("reminders: mark the due: %w", err)
	}
	defer rows.Close()

	out := []Due{}
	for rows.Next() {
		var d Due
		if err := rows.Scan(&d.ID, &d.ContactID, &d.ConversationID, &d.OperatorID, &d.Description, &d.DueAt, &d.Notified,
			&d.NotifiedAt, &d.CreatedAt, &d.Recipient); err != nil {
			return nil, fmt.Errorf("reminders: scan a due reminder: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
