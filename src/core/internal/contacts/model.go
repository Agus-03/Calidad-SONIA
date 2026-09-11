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

	// ErrLabelNotInAccount is what the foreign key cannot say: it proves the label exists, never that it belongs to this account.
	ErrLabelNotInAccount = errors.New("contacts: that label is not in this account's catalogue")
)

// ContactLabel is the link row and not an entity: it has no deleted_at, so untagging is a DELETE. A link kept with a timestamp
// would leave the contact inside the segmentation of F33 while the tag no longer shows on its card (C09, ADR-0003).
type ContactLabel struct {
	ID        uuid.UUID
	ContactID uuid.UUID
	LabelID   uuid.UUID

	// Nullable in the schema because a tag the system applies has no operator behind it, which F31 will need (ADR-0003).
	AssignedByOperatorID uuid.UUID
}

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

// Scope is who is asking and with what role, so a read says in its own signature that it narrows and does not merely take an account (ADR-0032, regla 2).
type Scope struct {
	AccountID  uuid.UUID
	OperatorID uuid.UUID
	Role       enums.OperatorRole
}

// Only the two roles that supervise the whole team are exempt, so an empty or unknown role falls to the narrow side and sees nothing.
func (s Scope) reachesEveryContact() bool {
	return s.Role == enums.OperatorRoleSupervisor || s.Role == enums.OperatorRoleAdmin
}

// Match is how several labels combine. It is a word and not a boolean because the caller has to say which it meant: a flag
// named "and" reads backwards half the time, and the two answers to "Ventas and Moroso" are different books.
type Match string

const (
	// MatchAll narrows as criteria are added, which is what a filter is expected to do: every label asked for must be on the contact.
	MatchAll Match = "all"

	// MatchAny widens: any one of them is enough.
	MatchAny Match = "any"
)

func (m Match) IsValid() bool { return m == MatchAll || m == MatchAny }

// Filter is a struct and not a growing parameter list, so the listing does not change signature again when the search UI learns
// to combine criteria. What it holds today is one group of labels joined by one operator (F33).
//
// What it deliberately does not hold is a tree: criteria nested with parentheses, or a label group mixed with a text group under
// an operator of their own. That is the shape the search of the application will eventually need, and this is the type it would
// grow into — a node with an operator and children, with this struct as the leaf. Nothing asks for it yet, and a tree nobody
// builds is a parser with no caller.
type Filter struct {
	// Deduplicated before it gets here, because MatchAll counts what it found against how many were asked for.
	LabelIDs []uuid.UUID
	Match    Match

	// Search reaches the name, the lastname and the phone, and nothing else: those are the criteria that were asked for (F30).
	Search string
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
