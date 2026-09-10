package contacts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	// ErrEmptyName is refused before the database sees it: the two columns are NOT NULL but blanks would satisfy them.
	ErrEmptyName = errors.New("contacts: the name and the lastname cannot be empty")

	// ErrEmptyPhone is its own refusal because the phone is normalized to digits: a number typed as "-" arrives here as nothing.
	ErrEmptyPhone = errors.New("contacts: the phone cannot be empty")
)

// ErrFieldNotWritable names the field the role may not touch, so the answer can say which one instead of refusing the whole edit (ADR-0032, regla 14).
type ErrFieldNotWritable struct{ Field string }

func (e ErrFieldNotWritable) Error() string {
	return "contacts: this role cannot write " + e.Field
}

// The names are the ones the JSON uses, because the check happens against the keys that actually arrived in the body.
const (
	FieldName     = "name"
	FieldLastname = "lastname"
	FieldEmail    = "email"
	FieldType     = "type"
	FieldNotes    = "notes"
	FieldOptOut   = "opt_out"
	FieldBlocked  = "blocked"
)

// writableBy is the table of ADR-0032 reglas 8 y 9 as data, not as a chain of ifs: the decision is a table, so it is read as one.
//
// The operator edits the name and the lastname and nothing else. The reason is H-33: a form that shows a masked CBU and sends the whole
// object back wipes it, and the answer is not a more careful form, it is a role that writes fewer fields than it reads.
var writableBy = map[enums.OperatorRole][]string{
	enums.OperatorRoleOperator:   {FieldName, FieldLastname},
	enums.OperatorRoleSupervisor: {FieldName, FieldLastname, FieldEmail, FieldType, FieldNotes, FieldOptOut, FieldBlocked},
	enums.OperatorRoleAdmin:      {FieldName, FieldLastname, FieldEmail, FieldType, FieldNotes, FieldOptOut, FieldBlocked},
}

// Neither phone_whatsapp nor source_channel is in any row above, and neither has a parameter in Changes: two immutables that
// are not refused so much as unreachable (ADR-0032 regla 10, ADR-0001).

// knownFields is every name a contact answers to in JSON, editable or not. It exists so a field that is simply not part of a contact
// is told apart from one the role may not write: "apodo" is a typo and "phone_whatsapp" is a rule, and the two deserve different answers.
var knownFields = []string{
	FieldName, FieldLastname, FieldEmail, FieldType, FieldNotes, FieldOptOut, FieldBlocked,
	// Real columns of a contact that no edit may reach: immutable by ADR-0032 regla 10 and ADR-0001, or written by nobody yet.
	"phone_whatsapp", "source_channel", "consent_given_at", "consent_revoked_at",
}

// ErrUnknownField is a bad request and not a refusal: nothing about the role would make it work.
type ErrUnknownField struct{ Field string }

func (e ErrUnknownField) Error() string {
	return "contacts: " + e.Field + " is not part of a contact"
}

// CheckKnown runs before CheckWritable, because "this role cannot write apodo" would tell the caller that apodo exists.
func CheckKnown(fields []string) error {
	for _, f := range fields {
		if !slices.Contains(knownFields, f) {
			return ErrUnknownField{Field: f}
		}
	}
	return nil
}

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	List(ctx context.Context, scope Scope, limit, offset int) (Page, error)
	ByID(ctx context.Context, scope Scope, contactID uuid.UUID) (Contact, error)
	Create(ctx context.Context, c Contact) error
	Update(ctx context.Context, accountID, contactID uuid.UUID, ch Changes) error
	UpdateSensitive(ctx context.Context, accountID, contactID uuid.UUID, s Sensitive) error
	SoftDelete(ctx context.Context, accountID, contactID uuid.UUID) error
}

type Service struct {
	repo store
	log  *slog.Logger
}

func NewService(repo store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, log: log}
}

func (s *Service) List(ctx context.Context, scope Scope, limit, offset int) (Page, error) {
	return s.repo.List(ctx, scope, limit, offset)
}

func (s *Service) ByID(ctx context.Context, scope Scope, contactID uuid.UUID) (Contact, error) {
	return s.repo.ByID(ctx, scope, contactID)
}

// CheckWritable is asked by the controller with the keys the body actually carried, which is the only way to tell a field
// that was sent from one that was left out: refusing on a zero value would refuse every edit that does not send everything.
func CheckWritable(role enums.OperatorRole, fields []string) error {
	allowed := writableBy[role]
	for _, f := range fields {
		if !slices.Contains(allowed, f) {
			return ErrFieldNotWritable{Field: f}
		}
	}
	return nil
}

// New is what a creation carries: the two immutables travel here and only here, because after this they have nowhere to be written.
type New struct {
	PhoneWhatsapp string
	Name          string
	Lastname      string
	Email         string
	Type          enums.ContactType
	Notes         string
	SourceChannel enums.SourceChannel
	Sensitive     Sensitive
}

// Create answers the row it wrote so the response carries the id the caller did not choose (C04).
func (s *Service) Create(ctx context.Context, scope Scope, n New) (Contact, error) {
	if err := validateNames(n.Name, n.Lastname); err != nil {
		return Contact{}, err
	}
	if normalizePhone(n.PhoneWhatsapp) == "" {
		return Contact{}, ErrEmptyPhone
	}

	contact := Contact{
		ID: id.New(), AccountID: scope.AccountID, PhoneWhatsapp: n.PhoneWhatsapp,
		Name: n.Name, Lastname: n.Lastname, Email: n.Email, Type: n.Type, Notes: n.Notes,
		SourceChannel: n.SourceChannel, Sensitive: n.Sensitive,
		// Nil on purpose: nobody has asked the client, and the load time would read as a consent that happened.
		ConsentGivenAt: nil,
	}
	if err := s.repo.Create(ctx, contact); err != nil {
		return Contact{}, err
	}

	s.log.Info("contact created",
		slog.String("contact_id", contact.ID.String()), slog.String("operator_id", scope.OperatorID.String()))
	return contact, nil
}

// Apply is a partial update built on the row as it stands: it reads through the scope first, so a contact out of reach is
// missing before anything is written, and the fields nobody sent keep the value they had.
func (s *Service) Apply(ctx context.Context, scope Scope, contactID uuid.UUID, edit func(*Changes)) (Contact, error) {
	current, err := s.repo.ByID(ctx, scope, contactID)
	if err != nil {
		return Contact{}, err
	}

	changes := Changes{
		Name: current.Name, Lastname: current.Lastname, Email: current.Email,
		Type: current.Type, Notes: current.Notes, OptOut: current.OptOut, Blocked: current.Blocked,
	}
	edit(&changes)

	if err := validateNames(changes.Name, changes.Lastname); err != nil {
		return Contact{}, err
	}
	if !changes.Type.IsValid() {
		return Contact{}, fmt.Errorf("contacts: %q is not a contact type", changes.Type)
	}

	if err := s.repo.Update(ctx, scope.AccountID, contactID, changes); err != nil {
		return Contact{}, err
	}
	return s.repo.ByID(ctx, scope, contactID)
}

// UpdateSensitive reads through the scope first for the same reason Apply does, and is a separate call because the four fields
// are written deliberately or not at all (H-33).
func (s *Service) UpdateSensitive(ctx context.Context, scope Scope, contactID uuid.UUID, sensitive Sensitive) (Contact, error) {
	if _, err := s.repo.ByID(ctx, scope, contactID); err != nil {
		return Contact{}, err
	}
	if err := s.repo.UpdateSensitive(ctx, scope.AccountID, contactID, sensitive); err != nil {
		return Contact{}, err
	}

	s.log.Info("contact sensitive fields written",
		slog.String("contact_id", contactID.String()), slog.String("operator_id", scope.OperatorID.String()))
	return s.repo.ByID(ctx, scope, contactID)
}

// Delete is logical and reads through the scope first, so a contact the role cannot see cannot be taken from the whole team either.
func (s *Service) Delete(ctx context.Context, scope Scope, contactID uuid.UUID) error {
	if _, err := s.repo.ByID(ctx, scope, contactID); err != nil {
		return err
	}
	return s.repo.SoftDelete(ctx, scope.AccountID, contactID)
}

// Whitespace only passes the NOT NULL of the two columns and produces a contact nobody can find in a book ordered by lastname.
func validateNames(name, lastname string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(lastname) == "" {
		return ErrEmptyName
	}
	return nil
}
