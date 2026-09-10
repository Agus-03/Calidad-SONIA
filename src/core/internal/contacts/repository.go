package contacts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// The cipher is held, not reached: the four sensitive columns are bytes to this package and never leave it as bytes (ADR-0031, regla 11).
type Repository struct {
	exec   db.Executor
	cipher *encryption.Cipher
}

func NewRepository(exec db.Executor, cipher *encryption.Cipher) *Repository {
	return &Repository{exec: exec, cipher: cipher}
}

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository {
	return &Repository{exec: exec, cipher: r.cipher}
}

// Deleted rows are excluded here and nowhere else, matching the partial unique index that frees a phone on delete (C09).
const contactSelect = `
SELECT id, account_id, phone_whatsapp, name, lastname, coalesce(email, ''), type, coalesce(notes, ''),
       source_channel, consent_given_at, consent_revoked_at, opt_out, blocked
FROM   contacts
WHERE  deleted_at IS NULL`

// The reach is built and not written as a constant because the narrowing adds an argument, and an argument added in the middle
// shifts every placeholder after it. Returning the args with the SQL is what keeps the two from drifting apart.
func (r *Repository) reach(s Scope) (where string, args []any) {
	args = []any{s.AccountID}
	where = " AND contacts.account_id = $1"
	if s.reachesEveryContact() {
		return where, args
	}
	args = append(args, s.OperatorID)
	return where + " AND " + conversations.ScopedToOperator("contacts.id", len(args)), args
}

// The listing does not select the four sensitive columns, so a page of two hundred contacts never decrypts anything (ADR-0031, ADR-0032 regla 4).
// Ordered by lastname then name because a contact book is read as a book, and created_at would scatter the same family.
// Total counts the same reach as Items: a page that said "3 of 40" while showing the operator its own 3 would be worse than no total at all.
func (r *Repository) List(ctx context.Context, scope Scope, limit, offset int) (Page, error) {
	where, args := r.reach(scope)
	listing := contactSelect + where +
		fmt.Sprintf(" ORDER BY lower(lastname), lower(name), contacts.id LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)

	rows, err := r.exec.Query(ctx, listing, append(append([]any{}, args...), limit, offset)...)
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

	counting := "SELECT count(*) FROM contacts WHERE contacts.deleted_at IS NULL" + where
	if err := r.exec.QueryRow(ctx, counting, args...).Scan(&page.Total); err != nil {
		return Page{}, fmt.Errorf("contacts: count contacts: %w", err)
	}
	return page, nil
}

// One row is the only read that decrypts, which is what keeps the cost and the exposure on the card and off the listing.
const contactByIDSelect = `
SELECT contacts.id, contacts.account_id, contacts.phone_whatsapp, contacts.name, contacts.lastname,
       coalesce(contacts.email, ''), contacts.type, coalesce(contacts.notes, ''),
       contacts.source_channel, contacts.consent_given_at, contacts.consent_revoked_at, contacts.opt_out, contacts.blocked,
       contacts.dni_encrypted, contacts.cuit_encrypted, contacts.cuil_encrypted, contacts.cbu_encrypted
FROM   contacts
WHERE  contacts.deleted_at IS NULL`

// ByID carries the same reach as the listing, and that is the point: without it, guessing an id would walk straight past the filter
// the listing applies, and the card is the read that decrypts (ADR-0032, regla 2).
// A contact out of reach answers ErrNotFound and never a refusal, because saying "forbidden" confirms that the contact exists.
// It answers the four in the clear: which roles may see them is decided above, and the DTO is what keeps them off the wire (regla 4).
func (r *Repository) ByID(ctx context.Context, scope Scope, contactID uuid.UUID) (Contact, error) {
	where, args := r.reach(scope)
	query := contactByIDSelect + where + fmt.Sprintf(" AND contacts.id = $%d", len(args)+1)

	row := r.exec.QueryRow(ctx, query, append(append([]any{}, args...), contactID)...)

	var c Contact
	var dni, cuit, cuil, cbu []byte
	err := row.Scan(&c.ID, &c.AccountID, &c.PhoneWhatsapp, &c.Name, &c.Lastname, &c.Email, &c.Type, &c.Notes,
		&c.SourceChannel, &c.ConsentGivenAt, &c.ConsentRevokedAt, &c.OptOut, &c.Blocked,
		&dni, &cuit, &cuil, &cbu)
	if errors.Is(err, pgx.ErrNoRows) {
		return Contact{}, ErrNotFound
	}
	if err != nil {
		return Contact{}, fmt.Errorf("contacts: scan contact: %w", err)
	}

	if c.Sensitive, err = r.decryptAll(dni, cuit, cuil, cbu); err != nil {
		return Contact{}, err
	}
	return c, nil
}

// ON CONFLICT and not a rescued error: a failed statement aborts the surrounding transaction, and the Fase 4 webhook will create inside one (SQLSTATE 25P02).
const contactInsert = `
INSERT INTO contacts (id, account_id, phone_whatsapp, name, lastname, email, type, notes,
                      source_channel, consent_given_at, consent_revoked_at, opt_out, blocked,
                      dni_encrypted, cuit_encrypted, cuil_encrypted, cbu_encrypted)
VALUES ($1, $2, $3, $4, $5, nullif($6, ''), $7, nullif($8, ''), $9, $10, $11, $12, $13, $14, $15, $16, $17)
ON CONFLICT (phone_whatsapp) WHERE deleted_at IS NULL DO NOTHING`

func (r *Repository) Create(ctx context.Context, c Contact) error {
	dni, cuit, cuil, cbu, err := r.encryptAll(c.Sensitive)
	if err != nil {
		return err
	}

	tag, err := r.exec.Exec(ctx, contactInsert,
		c.ID, c.AccountID, normalizePhone(c.PhoneWhatsapp), strings.TrimSpace(c.Name), strings.TrimSpace(c.Lastname),
		strings.TrimSpace(c.Email), c.Type, strings.TrimSpace(c.Notes),
		c.SourceChannel, c.ConsentGivenAt, c.ConsentRevokedAt, c.OptOut, c.Blocked,
		dni, cuit, cuil, cbu)
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

// UpdateSensitive is a method of its own and not a field of Changes, because the two together are what wipes a CBU:
// an ordinary edit sends a struct with empty strings in it, and a column that has nowhere to travel cannot be cleared by one (H-33, ADR-0032 regla 8).
const contactUpdateSensitive = `
UPDATE contacts
SET    dni_encrypted = $3, cuit_encrypted = $4, cuil_encrypted = $5, cbu_encrypted = $6, updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

func (r *Repository) UpdateSensitive(ctx context.Context, accountID, contactID uuid.UUID, s Sensitive) error {
	dni, cuit, cuil, cbu, err := r.encryptAll(s)
	if err != nil {
		return err
	}

	tag, err := r.exec.Exec(ctx, contactUpdateSensitive, accountID, contactID, dni, cuit, cuil, cbu)
	if err != nil {
		return fmt.Errorf("contacts: update sensitive fields: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The four travel together because they are written by one statement: encrypting three and failing on the fourth must leave the row untouched.
func (r *Repository) encryptAll(s Sensitive) (dni, cuit, cuil, cbu []byte, err error) {
	for _, f := range []struct {
		into  *[]byte
		value string
	}{{&dni, s.DNI}, {&cuit, s.CUIT}, {&cuil, s.CUIL}, {&cbu, s.CBU}} {
		if *f.into, err = r.cipher.Encrypt(strings.TrimSpace(f.value)); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("contacts: encrypt a sensitive field: %w", err)
		}
	}
	return dni, cuit, cuil, cbu, nil
}

// A row that fails to decrypt is refused whole: answering a card with three of the four fields would hide that one is unreadable (ADR-0031, regla 2).
func (r *Repository) decryptAll(dni, cuit, cuil, cbu []byte) (Sensitive, error) {
	var s Sensitive
	for _, f := range []struct {
		into   *string
		stored []byte
	}{{&s.DNI, dni}, {&s.CUIT, cuit}, {&s.CUIL, cuil}, {&s.CBU, cbu}} {
		value, err := r.cipher.Decrypt(f.stored)
		if err != nil {
			return Sensitive{}, fmt.Errorf("contacts: decrypt a sensitive field: %w", err)
		}
		*f.into = value
	}
	return s, nil
}

// Only the listing scans through here now: the card has its own scan because it reads four columns more and has to decrypt them.
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
