package operators

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// The roster's own filter: optional pieces, so $2 and $3 stay NULL when the admin asks for everybody.
const rosterFilter = `
  AND account_id = $1
  AND ($2::text IS NULL OR role = $2::text)
  AND ($3::boolean IS NULL OR active = $3::boolean)`

// Ordered by name and then id, so two people called the same keep their order between pages.
const rosterList = operatorSelect + rosterFilter + `
ORDER BY lower(lastname), lower(name), id
LIMIT    $4 OFFSET $5`

const rosterCount = `
SELECT count(*)
FROM   operators
WHERE  deleted_at IS NULL` + rosterFilter

// List returns the page and the total in two queries, as labels/ does, because a window count repeats the total on every row.
func (r *Repository) List(ctx context.Context, accountID uuid.UUID, f Filter, limit, offset int) (Page, error) {
	var role *string
	if f.Role != nil {
		value := string(*f.Role)
		role = &value
	}
	rows, err := r.exec.Query(ctx, rosterList, accountID, role, f.Active, limit, offset)
	if err != nil {
		return Page{}, fmt.Errorf("operators: list operators: %w", err)
	}
	defer rows.Close()

	page := Page{Items: []Operator{}}
	for rows.Next() {
		o, err := r.scanOne(rows)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, o)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("operators: read operators: %w", err)
	}
	if err := r.exec.QueryRow(ctx, rosterCount, accountID, role, f.Active).Scan(&page.Total); err != nil {
		return Page{}, fmt.Errorf("operators: count operators: %w", err)
	}
	return page, nil
}

// LockAdmins locks the active admins in id order and before the target, so two admins demoting each other queue (ADR-0062).
func (r *Repository) LockAdmins(ctx context.Context, accountID uuid.UUID) (int, error) {
	rows, err := r.exec.Query(ctx, `
SELECT id
FROM   operators
WHERE  account_id = $1 AND role = $2 AND active AND deleted_at IS NULL
ORDER BY id
FOR UPDATE`, accountID, enums.OperatorRoleAdmin)
	if err != nil {
		return 0, fmt.Errorf("operators: lock the admins: %w", err)
	}
	defer rows.Close()

	locked := 0
	for rows.Next() {
		locked++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("operators: read the admins: %w", err)
	}
	return locked, nil
}

// ForUpdate is ByID scoped by account and locked, so the change and its audit are made against the row nobody else is changing.
func (r *Repository) ForUpdate(ctx context.Context, accountID, operatorID uuid.UUID) (Operator, error) {
	return r.scanOne(r.exec.QueryRow(ctx, operatorSelect+" AND account_id = $1 AND id = $2 FOR UPDATE", accountID, operatorID))
}

// ON CONFLICT and not a rescued error, so the insert never aborts the transaction its audit runs in (SQLSTATE 25P02).
const rosterInsert = operatorInsert + `
ON CONFLICT (email) WHERE deleted_at IS NULL DO NOTHING`

// Register is Create for the ABM: a taken address is ErrDuplicateEmail and not a failure, because the admin can fix it.
func (r *Repository) Register(ctx context.Context, o Operator) error {
	tag, err := r.exec.Exec(ctx, rosterInsert,
		o.ID, o.AccountID, o.Name, o.Lastname, normalizeEmail(o.Email), o.PasswordHash, o.Role, o.Active)
	if err != nil {
		return fmt.Errorf("operators: insert operator: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicateEmail
	}
	return nil
}

// The NOT EXISTS stands in for ON CONFLICT, which UPDATE lacks: a violation would abort the transaction the audit shares.
const rosterUpdate = `
UPDATE operators
SET    name = $3, lastname = $4, email = $5, role = $6, active = $7, updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL
  AND  NOT EXISTS (SELECT 1 FROM operators o WHERE o.email = $5 AND o.id <> $2 AND o.deleted_at IS NULL)`

// Update writes the whole public row the service already merged; zero rows on a locked, existing row can only be a taken address.
func (r *Repository) Update(ctx context.Context, o Operator) error {
	tag, err := r.exec.Exec(ctx, rosterUpdate,
		o.AccountID, o.ID, o.Name, o.Lastname, normalizeEmail(o.Email), o.Role, o.Active)
	if err != nil {
		return fmt.Errorf("operators: update operator: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicateEmail
	}
	return nil
}

// The hash it replaces is part of the predicate, so a password changed meanwhile by someone else is not overwritten in silence.
const rosterPassword = `
UPDATE operators
SET    password_hash = $3, updated_at = now()
WHERE  account_id = $1 AND id = $2 AND password_hash = $4 AND deleted_at IS NULL`

func (r *Repository) SetPassword(ctx context.Context, accountID, operatorID uuid.UUID, hash, replaced string) error {
	tag, err := r.exec.Exec(ctx, rosterPassword, accountID, operatorID, hash, replaced)
	if err != nil {
		return fmt.Errorf("operators: set password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPasswordChanged
	}
	return nil
}

// active goes false with the delete, so a token still in flight is refused by Resolve the same as a deactivated one (ADR-0030).
const rosterSoftDelete = `
UPDATE operators
SET    deleted_at = now(), active = false, updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

// SoftDelete and not a DELETE: conversations, sales and the audit still point at the row (C09).
func (r *Repository) SoftDelete(ctx context.Context, accountID, operatorID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, rosterSoftDelete, accountID, operatorID)
	if err != nil {
		return fmt.Errorf("operators: delete operator: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The same predicate the hand-over moves with, so the number told to the admin is the number the hand-over would move (ADR-0056).
func (r *Repository) HeldConversations(ctx context.Context, operatorID uuid.UUID) (int, error) {
	var held int
	err := r.exec.QueryRow(ctx, `
SELECT count(*)
FROM   conversations
WHERE  operator_id = $1 AND status <> $2 AND deleted_at IS NULL`, operatorID, enums.ConversationStatusResolved).Scan(&held)
	if err != nil {
		return 0, fmt.Errorf("operators: count the conversations held: %w", err)
	}
	return held, nil
}
