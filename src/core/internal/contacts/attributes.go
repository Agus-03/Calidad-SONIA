package contacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// The six keys of ADR-0047, section 2.2, each marked by whether it waits for the consent of section 2.6. A key outside the list is
// never stored, whatever the brain sent: the list is the rule and the brain is only its first reader.
var extractedKeys = map[string]bool{
	"dni": true, "cuit_cuil": true, "cbu": true,
	"first_name": false, "last_name": false, "email": false,
}

// sensitiveKeys are the three an operator never reads, by the same rule as the four encrypted columns (ADR-0032, regla 4).
var sensitiveKeys = []string{"dni", "cuit_cuil", "cbu"}

// Extracted is one datum the brain found in a turn, in the clear only inside the process (ADR-0047, section 2.1).
type Extracted struct {
	Key             string
	Value           string
	Confidence      float64
	SourceMessageID uuid.UUID
}

// Attribute is one live row of contact_attributes as the card shows it.
type Attribute struct {
	Key        string
	Value      string
	Source     enums.AttributeSource
	Confidence *float64
	CreatedAt  time.Time
}

// Vigente, not merely given: a revocation after the consent is an act of the customer too (ADR-0047, section 2.6).
const consentSelect = `
SELECT consent_given_at IS NOT NULL AND (consent_revoked_at IS NULL OR consent_revoked_at < consent_given_at)
FROM   contacts
WHERE  id = $1 AND deleted_at IS NULL`

const liveOfKeySelect = `
SELECT id, source, value_encrypted
FROM   contact_attributes
WHERE  contact_id = $1 AND key = $2 AND deleted_at IS NULL`

const attributesRetire = `UPDATE contact_attributes SET deleted_at = now(), updated_at = now() WHERE id = ANY($1)`

const attributeInsert = `
INSERT INTO contact_attributes (id, contact_id, key, value_encrypted, source, confidence, source_message_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)`

// RecordExtracted keeps what the brain found under the rules of ADR-0047: nothing sensitive without consent (2.6), nothing over a
// manual value (2.5), a new value retires the extracted one it replaces, and the same value writes nothing. Runs inside the
// transaction that closes the turn, so the data and the turn commit together (section 2.1).
func (r *Repository) RecordExtracted(ctx context.Context, contactID uuid.UUID, data []Extracted) error {
	if len(data) == 0 {
		return nil
	}

	var consented bool
	err := r.exec.QueryRow(ctx, consentSelect, contactID).Scan(&consented)
	if errors.Is(err, pgx.ErrNoRows) {
		// Taken down while the turn ran: there is nobody left to keep data about (C09).
		return nil
	}
	if err != nil {
		return fmt.Errorf("contacts: read consent: %w", err)
	}

	for _, d := range data {
		sensitive, known := extractedKeys[d.Key]
		if !known || d.Value == "" || (sensitive && !consented) {
			continue
		}
		if err := r.recordOne(ctx, contactID, d); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) recordOne(ctx context.Context, contactID uuid.UUID, d Extracted) error {
	rows, err := r.exec.Query(ctx, liveOfKeySelect, contactID, d.Key)
	if err != nil {
		return fmt.Errorf("contacts: read attribute: %w", err)
	}
	var retired []uuid.UUID
	manual, same := false, false
	for rows.Next() {
		var rowID uuid.UUID
		var source enums.AttributeSource
		var stored []byte
		if err := rows.Scan(&rowID, &source, &stored); err != nil {
			rows.Close()
			return fmt.Errorf("contacts: scan attribute: %w", err)
		}
		if source == enums.AttributeSourceManual {
			manual = true
			continue
		}
		value, err := r.cipher.Decrypt(stored)
		if err != nil {
			rows.Close()
			return fmt.Errorf("contacts: decrypt attribute: %w", err)
		}
		if value == d.Value {
			same = true
			continue
		}
		retired = append(retired, rowID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("contacts: read attribute: %w", err)
	}
	// Lo manual pesa más: what a person loaded after talking to the customer is not replaced by an inference (section 2.5).
	if manual || same {
		return nil
	}

	if len(retired) > 0 {
		// Taken down and not deleted: the value it had is history, and history is not erased (C09).
		if _, err := r.exec.Exec(ctx, attributesRetire, retired); err != nil {
			return fmt.Errorf("contacts: retire attribute: %w", err)
		}
	}
	stored, err := r.cipher.Encrypt(d.Value)
	if err != nil {
		return fmt.Errorf("contacts: encrypt attribute: %w", err)
	}
	if _, err := r.exec.Exec(ctx, attributeInsert, id.New(), contactID, d.Key, stored, enums.AttributeSourceLlm,
		d.Confidence, d.SourceMessageID); err != nil {
		return fmt.Errorf("contacts: insert attribute: %w", err)
	}
	return nil
}

// Without the sensitive keys when the caller may not read them: they are left out by the query and never decrypted.
const attributesSelect = `
SELECT   key, value_encrypted, source, confidence, created_at
FROM     contact_attributes
WHERE    contact_id = $1 AND deleted_at IS NULL AND ($2 OR NOT key = ANY($3))
ORDER BY key, created_at DESC`

// Attributes answers the live rows of a contact, whose reach the caller already checked.
func (r *Repository) Attributes(ctx context.Context, contactID uuid.UUID, withSensitive bool) ([]Attribute, error) {
	rows, err := r.exec.Query(ctx, attributesSelect, contactID, withSensitive, sensitiveKeys)
	if err != nil {
		return nil, fmt.Errorf("contacts: read attributes: %w", err)
	}
	defer rows.Close()

	out := []Attribute{}
	for rows.Next() {
		var a Attribute
		var stored []byte
		if err := rows.Scan(&a.Key, &stored, &a.Source, &a.Confidence, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("contacts: scan attributes: %w", err)
		}
		if a.Value, err = r.cipher.Decrypt(stored); err != nil {
			return nil, fmt.Errorf("contacts: decrypt attributes: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AttributesOf answers what the card shows about the contact: the extracted data within the reach of the caller, and the three
// sensitive ones only for the two roles ADR-0032 regla 4 names (ADR-0047, section 2.7).
func (s *Service) AttributesOf(ctx context.Context, scope Scope, contactID uuid.UUID) ([]Attribute, error) {
	if _, err := s.repo.ByID(ctx, scope, contactID); err != nil {
		return nil, err
	}
	return s.repo.Attributes(ctx, contactID, seesSensitive(scope.Role))
}
