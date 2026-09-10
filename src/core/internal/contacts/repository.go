package contacts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

// The four encrypted columns are absent on purpose: nothing here can leak what it never selects (ADR-0032, regla 4).
// Deleted rows are excluded here and nowhere else, matching the partial unique index that frees a phone on delete (C09).
const contactSelect = `
SELECT id, account_id, phone_whatsapp, name, lastname, coalesce(email, ''), type, coalesce(notes, ''),
       source_channel, consent_given_at, consent_revoked_at, opt_out, blocked
FROM   contacts
WHERE  deleted_at IS NULL`

// Ordered by lastname then name because a contact book is read as a book, and created_at would scatter the same family.
const contactList = contactSelect + `
  AND    account_id = $1
ORDER BY lower(lastname), lower(name), id
LIMIT    $2 OFFSET $3`

const contactCount = `
SELECT count(*)
FROM   contacts
WHERE  deleted_at IS NULL AND account_id = $1`

// List reaches every contact of the account: the narrowing to what a role may see is a separate step, and until it exists nothing mounts this.
func (r *Repository) List(ctx context.Context, accountID uuid.UUID, limit, offset int) (Page, error) {
	rows, err := r.exec.Query(ctx, contactList, accountID, limit, offset)
	if err != nil {
		return Page{}, fmt.Errorf("contacts: list contacts: %w", err)
	}
	defer rows.Close()

	page := Page{Items: []Contact{}}
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, c)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("contacts: read contacts: %w", err)
	}

	if err := r.exec.QueryRow(ctx, contactCount, accountID).Scan(&page.Total); err != nil {
		return Page{}, fmt.Errorf("contacts: count contacts: %w", err)
	}
	return page, nil
}

// ByID is scoped by account and not only by id, so an identifier guessed from another account reads as missing rather than as forbidden.
func (r *Repository) ByID(ctx context.Context, accountID, contactID uuid.UUID) (Contact, error) {
	return scanOne(r.exec.QueryRow(ctx, contactSelect+" AND account_id = $1 AND id = $2", accountID, contactID))
}

// ON CONFLICT and not a rescued error: a failed statement aborts the surrounding transaction, and the Fase 4 webhook will create inside one (SQLSTATE 25P02).
const contactInsert = `
INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, email, type, notes,
                      source_channel, consent_given_at, consent_revoked_at, opt_out, blocked)
VALUES ($1, $2, $3, $4, $5, nullif($6, ''), $7, nullif($8, ''), $9, $10, $11, $12, $13)
ON CONFLICT (phone_whatsapp) WHERE deleted_at IS NULL DO NOTHING`

func (r *Repository) Create(ctx context.Context, c Contact) error {
	tag, err := r.exec.Exec(ctx, contactInsert,
		c.ID, c.AccountID, normalizePhone(c.PhoneWhatsapp), strings.TrimSpace(c.Name), strings.TrimSpace(c.Lastname),
		strings.TrimSpace(c.Email), c.Type, strings.TrimSpace(c.Notes),
		c.SourceChannel, c.ConsentGivenAt, c.ConsentRevokedAt, c.OptOut, c.Blocked)
	if err != nil {
		return fmt.Errorf("contacts: insert contact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicatePhone
	}
	return nil
}

// Changes names the columns that can be corrected. phone_whatsapp and source_channel are not among them and have no field here:
// a column that has nowhere to be written is harder to change by accident than one that is written and then refused (ADR-0032, reglas 10 y 9; ADR-0001).
type Changes struct {
	Name     string
	Lastname string
	Email    string
	Type     enums.ContactType
	Notes    string
	OptOut   bool
	Blocked  bool
}

const contactUpdate = `
UPDATE contacts
SET    name = $3, lastname = $4, email = nullif($5, ''), type = $6, notes = nullif($7, ''),
       opt_out = $8, blocked = $9, updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

func (r *Repository) Update(ctx context.Context, accountID, contactID uuid.UUID, ch Changes) error {
	tag, err := r.exec.Exec(ctx, contactUpdate, accountID, contactID,
		strings.TrimSpace(ch.Name), strings.TrimSpace(ch.Lastname), strings.TrimSpace(ch.Email),
		ch.Type, strings.TrimSpace(ch.Notes), ch.OptOut, ch.Blocked)
	if err != nil {
		return fmt.Errorf("contacts: update contact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The predicate repeats deleted_at IS NULL so deleting twice reports a missing contact instead of silently moving the timestamp.
const contactSoftDelete = `
UPDATE contacts
SET    deleted_at = now(), updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

// SoftDelete and not a DELETE: conversations and messages still reference the row, and F32 needs the history to survive the contact (C05, C09).
func (r *Repository) SoftDelete(ctx context.Context, accountID, contactID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, contactSoftDelete, accountID, contactID)
	if err != nil {
		return fmt.Errorf("contacts: delete contact: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanOne(row pgx.Row) (Contact, error) {
	c, err := scanContact(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contact{}, ErrNotFound
	}
	return c, err
}

// The scanner takes a pgx.Row so the single read and the loop of the listing share one column order and cannot drift apart.
func scanContact(row pgx.Row) (Contact, error) {
	var c Contact
	err := row.Scan(&c.ID, &c.AccountID, &c.PhoneWhatsapp, &c.Name, &c.Lastname, &c.Email, &c.Type, &c.Notes,
		&c.SourceChannel, &c.ConsentGivenAt, &c.ConsentRevokedAt, &c.OptOut, &c.Blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contact{}, err
	}
	if err != nil {
		return Contact{}, fmt.Errorf("contacts: scan contact: %w", err)
	}
	return c, nil
}
