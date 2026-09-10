package labels

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

// Postgres says 23505 when a unique index rejects the row, which is how a name already taken in another casing announces itself.
const uniqueViolation = "23505"

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

// Deleted rows are excluded here and nowhere else, matching the partial unique index that frees a name on delete (C09).
const labelSelect = `
SELECT id, account_id, name, color, created_by_operator_id
FROM   labels
WHERE  deleted_at IS NULL`

// Ordered by lower(name) and not by name so the listing agrees with the index that decides which names collide.
const labelList = labelSelect + `
  AND    account_id = $1
ORDER BY lower(name)
LIMIT    $2 OFFSET $3`

const labelCount = `
SELECT count(*)
FROM   labels
WHERE  deleted_at IS NULL AND account_id = $1`

// List returns the page and the total in two queries, because count() over a window would repeat the total on every row of the page.
func (r *Repository) List(ctx context.Context, accountID uuid.UUID, limit, offset int) (Page, error) {
	rows, err := r.exec.Query(ctx, labelList, accountID, limit, offset)
	if err != nil {
		return Page{}, fmt.Errorf("labels: list labels: %w", err)
	}
	defer rows.Close()

	page := Page{Items: []Label{}}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.AccountID, &l.Name, &l.Color, &l.CreatedByOperatorID); err != nil {
			return Page{}, fmt.Errorf("labels: scan label: %w", err)
		}
		page.Items = append(page.Items, l)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("labels: read labels: %w", err)
	}

	if err := r.exec.QueryRow(ctx, labelCount, accountID).Scan(&page.Total); err != nil {
		return Page{}, fmt.Errorf("labels: count labels: %w", err)
	}
	return page, nil
}

// ByID is scoped by account and not only by id, so an identifier guessed from another account reads as missing rather than as forbidden.
func (r *Repository) ByID(ctx context.Context, accountID, labelID uuid.UUID) (Label, error) {
	row := r.exec.QueryRow(ctx, labelSelect+" AND account_id = $1 AND id = $2", accountID, labelID)

	var l Label
	err := row.Scan(&l.ID, &l.AccountID, &l.Name, &l.Color, &l.CreatedByOperatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Label{}, ErrNotFound
	}
	if err != nil {
		return Label{}, fmt.Errorf("labels: scan label: %w", err)
	}
	return l, nil
}

// ByIDs answers the catalogue entries a union table points at, in one query and not one per id: the card of a contact with six
// labels is a single read.
// A label logically deleted is simply not among them, so a tag whose catalogue entry was retired stops showing instead of
// showing a blank (C09).
func (r *Repository) ByIDs(ctx context.Context, accountID uuid.UUID, ids []uuid.UUID) ([]Label, error) {
	if len(ids) == 0 {
		return []Label{}, nil
	}

	rows, err := r.exec.Query(ctx, labelSelect+" AND account_id = $1 AND id = ANY($2) ORDER BY lower(name)", accountID, ids)
	if err != nil {
		return nil, fmt.Errorf("labels: list labels by id: %w", err)
	}
	defer rows.Close()

	found := []Label{}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.AccountID, &l.Name, &l.Color, &l.CreatedByOperatorID); err != nil {
			return nil, fmt.Errorf("labels: scan label: %w", err)
		}
		found = append(found, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("labels: read labels: %w", err)
	}
	return found, nil
}

// ON CONFLICT and not a rescued error: a failed statement aborts the surrounding transaction, and this insert has to survive being called inside one (SQLSTATE 25P02).
const labelInsert = `
INSERT INTO labels (id, account_id, name, color, created_by_operator_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (account_id, lower(name)) WHERE deleted_at IS NULL DO NOTHING`

func (r *Repository) Create(ctx context.Context, l Label) error {
	tag, err := r.exec.Exec(ctx, labelInsert, l.ID, l.AccountID, normalizeName(l.Name), l.Color, l.CreatedByOperatorID)
	if err != nil {
		return fmt.Errorf("labels: insert label: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicateName
	}
	return nil
}

const labelUpdate = `
UPDATE labels
SET    name = $3, color = $4, updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

// Update rescues the violation instead of avoiding it, because UPDATE has no ON CONFLICT: renaming to a name another row already holds is the case.
func (r *Repository) Update(ctx context.Context, accountID, labelID uuid.UUID, name, color string) error {
	tag, err := r.exec.Exec(ctx, labelUpdate, accountID, labelID, normalizeName(name), color)
	if isUniqueViolation(err) {
		return ErrDuplicateName
	}
	if err != nil {
		return fmt.Errorf("labels: update label: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The predicate repeats deleted_at IS NULL so deleting twice reports a missing label instead of silently moving the timestamp.
const labelSoftDelete = `
UPDATE labels
SET    deleted_at = now(), updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

// SoftDelete and not a DELETE: the catalogue keeps its rows because the union tables still reference them (C09, C05).
func (r *Repository) SoftDelete(ctx context.Context, accountID, labelID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, labelSoftDelete, accountID, labelID)
	if err != nil {
		return fmt.Errorf("labels: delete label: %w", err)
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
