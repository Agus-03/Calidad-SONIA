package operators

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

// Deleted rows are excluded here and nowhere else, matching the partial unique index that frees an address on delete (C09).
const operatorSelect = `
SELECT id, account_id, name, lastname, email, password_hash, role, active
FROM   operators
WHERE  deleted_at IS NULL`

// ByEmail returns the inactive operator too: telling a disabled account apart from a wrong password is policy, and policy lives in the service.
func (r *Repository) ByEmail(ctx context.Context, email string) (Operator, error) {
	return r.scanOne(r.exec.QueryRow(ctx, operatorSelect+" AND email = $1", normalizeEmail(email)))
}

// ByID is the read the middleware runs on every request, which is what makes role and active current instead of frozen at login (ADR-0030).
func (r *Repository) ByID(ctx context.Context, operatorID uuid.UUID) (Operator, error) {
	return r.scanOne(r.exec.QueryRow(ctx, operatorSelect+" AND id = $1", operatorID))
}

const operatorInsert = `
INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

// Create exists for the seed of the admin and not for an ABM: creating operators from a screen is Fase 11 (F18).
func (r *Repository) Create(ctx context.Context, o Operator) error {
	_, err := r.exec.Exec(ctx, operatorInsert,
		o.ID, o.AccountID, o.Name, o.Lastname, normalizeEmail(o.Email), o.PasswordHash, o.Role, o.Active)
	if err != nil {
		return fmt.Errorf("operators: insert operator: %w", err)
	}
	return nil
}

func (r *Repository) scanOne(row pgx.Row) (Operator, error) {
	var o Operator
	err := row.Scan(&o.ID, &o.AccountID, &o.Name, &o.Lastname, &o.Email, &o.PasswordHash, &o.Role, &o.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return Operator{}, ErrNotFound
	}
	if err != nil {
		return Operator{}, fmt.Errorf("operators: scan operator: %w", err)
	}
	return o, nil
}
