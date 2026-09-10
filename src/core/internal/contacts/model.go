// Package contacts owns contacts, contact_attributes and contact_labels: the two union tables belong to whoever tags (struct.md 2).
package contacts

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

var (
	// ErrNotFound is returned by every read, so the caller decides what a missing contact means in its own case.
	ErrNotFound = errors.New("contacts: contact not found")

	// ErrDuplicatePhone names what the unique index rejects, so the caller can say the number is taken instead of failing blind.
	ErrDuplicatePhone = errors.New("contacts: a contact with that phone already exists")
)

// Contact deliberately carries none of the four encrypted columns: they are built in their own step, behind their own DTO (ADR-0031, ADR-0032).
type Contact struct {
	ID        uuid.UUID
	AccountID uuid.UUID

	// Immutable after creation: it is the key the webhook resolves an incoming message by, so editing it re-points a whole history (ADR-0032, regla 10).
	PhoneWhatsapp string

	Name     string
	Lastname string
	Email    string
	Type     enums.ContactType
	Notes    string

	// Immutable too, and authoritative for F15: where the contact came from is a fact of its origin, not an attribute to correct (ADR-0001).
	SourceChannel enums.SourceChannel

	// The four of ADR-0006, in the clear only inside the process: they are read and written through the cipher and never selected for an operator (ADR-0031, ADR-0032 regla 4).
	Sensitive Sensitive

	// Nil while nobody has asked the client: an act that did not happen has no instant, and a zero time would read as one that did.
	ConsentGivenAt   *time.Time
	ConsentRevokedAt *time.Time

	// Two different refusals: revoking reaches the treatment of the data, the opt-out reaches the sending (DER, entidades).
	OptOut  bool
	Blocked bool
}

// Sensitive is a struct of its own so a caller has to name it to reach it, and a listing that never fills it cannot leak it by accident.
type Sensitive struct {
	DNI  string
	CUIT string
	CUIL string
	CBU  string
}

// Page carries Total because the pagination decided is by page number, and a page cannot be numbered without knowing how many there are.
type Page struct {
	Items []Contact
	Total int
}

// normalizePhone keeps digits only, which is exactly the shape of Meta's wa_id: what the Fase 4 webhook will look the contact up by.
// It does not add a country code, so a number typed without one stays without one and will not match what Meta sends.
func normalizePhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
