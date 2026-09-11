package contacts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	// ErrEmptyName is refused before the database sees it: the two columns are NOT NULL but blanks would satisfy them.
	ErrEmptyName = errors.New("contacts: the name and the lastname cannot be empty")

	// ErrEmptyPhone is its own refusal because the phone is normalized to digits: a number typed as "-" arrives here as nothing.
	ErrEmptyPhone = errors.New("contacts: the phone cannot be empty")

	// ErrUnknownMatch is refused and not defaulted: guessing which operator the caller meant would answer a different book than the one asked for.
	ErrUnknownMatch = errors.New("contacts: the match must be all or any")
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
	List(ctx context.Context, scope Scope, filter Filter, limit, offset int) (Page, error)
	ByID(ctx context.Context, scope Scope, contactID uuid.UUID) (Contact, error)
	Create(ctx context.Context, c Contact) error
	Update(ctx context.Context, accountID, contactID uuid.UUID, ch Changes) error
	UpdateSensitive(ctx context.Context, accountID, contactID uuid.UUID, s Sensitive) error
	SoftDelete(ctx context.Context, accountID, contactID uuid.UUID) error

	AssignLabel(ctx context.Context, link ContactLabel) error
	UnassignLabel(ctx context.Context, contactID, labelID uuid.UUID) error
	LabelIDsOf(ctx context.Context, contactID uuid.UUID) ([]uuid.UUID, error)
}

// catalogue is the slice of labels/ this package asks. The label is verified through its owner and never with a join of our own:
// contact_labels is ours and labels is not, and a package that reads another's table by hand is a package that will one day
// forget its predicates — the deleted ones, the account (struct.md, regla 1).
type catalogue interface {
	ByID(ctx context.Context, accountID, labelID uuid.UUID) (labels.Label, error)
	ByIDs(ctx context.Context, accountID uuid.UUID, ids []uuid.UUID) ([]labels.Label, error)
}

type Service struct {
	repo      store
	catalogue catalogue
	log       *slog.Logger
}

func NewService(repo store, tags catalogue, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, catalogue: tags, log: log}
}

// List is the only entry with a filter, and it validates the labels before querying: a segment that comes back empty because
// the id was mistyped reads exactly like a segment nobody has tagged, and the caller cannot tell the two apart (F33).
func (s *Service) List(ctx context.Context, scope Scope, filter Filter, limit, offset int) (Page, error) {
	// Empty means nobody chose, and the choice that narrows as criteria are added is the one a filter is expected to make.
	if filter.Match == "" {
		filter.Match = MatchAll
	}
	if !filter.Match.IsValid() {
		return Page{}, ErrUnknownMatch
	}

	// Deduplicated here and not in the handler, because MatchAll counts what it found against how many were asked for, and the
	// same label named twice would ask for a count no contact can reach.
	filter.LabelIDs = withoutRepeats(filter.LabelIDs)

	if len(filter.LabelIDs) > 0 {
		found, err := s.catalogue.ByIDs(ctx, scope.AccountID, filter.LabelIDs)
		if err != nil {
			return Page{}, err
		}
		if len(found) != len(filter.LabelIDs) {
			return Page{}, ErrLabelNotInAccount
		}
	}

	return s.repo.List(ctx, scope, filter, limit, offset)
}

func withoutRepeats(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	kept := make([]uuid.UUID, 0, len(ids))
	for _, one := range ids {
		if _, already := seen[one]; already {
			continue
		}
		seen[one] = struct{}{}
		kept = append(kept, one)
	}
	return kept
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

// AssignLabel reads the contact through the scope first, like every other write here: a contact out of reach cannot be tagged
// either, and the tag is what the segmentation of F33 will select by.
//
// The label is checked and then written, which leaves a window: a label retired between the two calls yields a link to a
// catalogue entry that is gone. That link never shows, because LabelsOf reads through the catalogue and the catalogue excludes
// the deleted. It is the admitted cost of asking labels/ instead of joining its table here.
func (s *Service) AssignLabel(ctx context.Context, scope Scope, contactID, labelID uuid.UUID) error {
	if _, err := s.repo.ByID(ctx, scope, contactID); err != nil {
		return err
	}
	if _, err := s.catalogue.ByID(ctx, scope.AccountID, labelID); err != nil {
		if errors.Is(err, labels.ErrNotFound) {
			return ErrLabelNotInAccount
		}
		return err
	}

	return s.repo.AssignLabel(ctx, ContactLabel{
		ID: id.New(), ContactID: contactID, LabelID: labelID, AssignedByOperatorID: scope.OperatorID,
	})
}

// UnassignLabel does not ask the catalogue: a label that is not in this account was never linked to a contact of this account,
// so the DELETE removes nothing and the end state is the one that was asked for.
func (s *Service) UnassignLabel(ctx context.Context, scope Scope, contactID, labelID uuid.UUID) error {
	if _, err := s.repo.ByID(ctx, scope, contactID); err != nil {
		return err
	}
	return s.repo.UnassignLabel(ctx, contactID, labelID)
}

// LabelsOf reads the contact through the scope first for the same reason the writes do, and pays a second read for it: which
// contacts an operator reaches is decided in one place, and a method that took the reach on trust would be the exception.
func (s *Service) LabelsOf(ctx context.Context, scope Scope, contactID uuid.UUID) ([]labels.Label, error) {
	if _, err := s.repo.ByID(ctx, scope, contactID); err != nil {
		return nil, err
	}

	ids, err := s.repo.LabelIDsOf(ctx, contactID)
	if err != nil {
		return nil, err
	}
	return s.catalogue.ByIDs(ctx, scope.AccountID, ids)
}

// Whitespace only passes the NOT NULL of the two columns and produces a contact nobody can find in a book ordered by lastname.
func validateNames(name, lastname string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(lastname) == "" {
		return ErrEmptyName
	}
	return nil
}
