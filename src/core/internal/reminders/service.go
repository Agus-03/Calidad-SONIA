package reminders

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/contacts"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// Contacts is contacts/, which decides who reaches a contact: a reminder is reachable through its contact and never on its own (ADR-0032).
type Contacts interface {
	ByID(ctx context.Context, scope contacts.Scope, contactID uuid.UUID) (contacts.Contact, error)
}

type store interface {
	ByID(ctx context.Context, accountID, reminderID uuid.UUID) (Reminder, error)
	OfContact(ctx context.Context, accountID, contactID uuid.UUID) ([]Reminder, error)
	Addressed(ctx context.Context, accountID, operatorID uuid.UUID, onlyDue bool) ([]Reminder, error)
	ConversationOfContact(ctx context.Context, conversationID, contactID uuid.UUID) (bool, error)
	Create(ctx context.Context, r Reminder) error
	Update(ctx context.Context, reminderID uuid.UUID, description string, dueAt time.Time) error
	Delete(ctx context.Context, reminderID uuid.UUID) error
}

type Service struct {
	repo     store
	contacts Contacts
	log      *slog.Logger
	// Indirected so a test can say what now is without sleeping.
	now func() time.Time
}

func NewService(repo store, book Contacts, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, contacts: book, log: log, now: time.Now}
}

// Create hangs the reminder from a contact the caller reaches, and from one of its conversations when named (ADR-0002 (c), ADR-0058).
func (s *Service) Create(ctx context.Context, scope Scope, contactID uuid.UUID, conversationID *uuid.UUID, description string, dueAt time.Time) (Reminder, error) {
	description, err := s.valid(description, dueAt)
	if err != nil {
		return Reminder{}, err
	}
	if err := s.reaches(ctx, scope, contactID); err != nil {
		return Reminder{}, err
	}
	if conversationID != nil {
		of, err := s.repo.ConversationOfContact(ctx, *conversationID, contactID)
		if err != nil {
			return Reminder{}, err
		}
		if !of {
			return Reminder{}, ErrConversationNotOfContact
		}
	}

	r := Reminder{ID: id.New(), ContactID: contactID, ConversationID: conversationID, OperatorID: scope.OperatorID, Description: description, DueAt: dueAt}
	if err := s.repo.Create(ctx, r); err != nil {
		return Reminder{}, err
	}
	return s.repo.ByID(ctx, scope.AccountID, r.ID)
}

// OfContact has the reach of the contact: whoever sees the contact sees what is pending with it (ADR-0032).
func (s *Service) OfContact(ctx context.Context, scope Scope, contactID uuid.UUID) ([]Reminder, error) {
	if err := s.reaches(ctx, scope, contactID); err != nil {
		return nil, err
	}
	return s.repo.OfContact(ctx, scope.AccountID, contactID)
}

// Mine is what reaches the caller, so an alert missed while it was not connected is still there to read (ADR-0058).
func (s *Service) Mine(ctx context.Context, scope Scope, onlyDue bool) ([]Reminder, error) {
	return s.repo.Addressed(ctx, scope.AccountID, scope.OperatorID, onlyDue)
}

// Update takes the fields that came, and a new due date makes the reminder pending again (ADR-0058).
func (s *Service) Update(ctx context.Context, scope Scope, reminderID uuid.UUID, description *string, dueAt *time.Time) (Reminder, error) {
	current, err := s.owned(ctx, scope, reminderID)
	if err != nil {
		return Reminder{}, err
	}
	nextDescription, nextDue := current.Description, current.DueAt
	if description != nil {
		nextDescription = *description
	}
	if dueAt != nil {
		nextDue = *dueAt
	}
	if nextDescription, err = s.validDescription(nextDescription); err != nil {
		return Reminder{}, err
	}
	// Only a date that moves is checked: an edit of the text of one already due must not be refused for being due.
	if dueAt != nil && !dueAt.Equal(current.DueAt) && !dueAt.After(s.now()) {
		return Reminder{}, ErrDueInThePast
	}

	if err := s.repo.Update(ctx, reminderID, nextDescription, nextDue); err != nil {
		return Reminder{}, err
	}
	return s.repo.ByID(ctx, scope.AccountID, reminderID)
}

// Delete is how a reminder is done: it leaves the contact and the filter of F23, and the row stays (C09, ADR-0058).
func (s *Service) Delete(ctx context.Context, scope Scope, reminderID uuid.UUID) error {
	if _, err := s.owned(ctx, scope, reminderID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, reminderID)
}

// owned reads the reminder in the account and lets through its owner and the two roles that see the whole team (ADR-0058).
func (s *Service) owned(ctx context.Context, scope Scope, reminderID uuid.UUID) (Reminder, error) {
	r, err := s.repo.ByID(ctx, scope.AccountID, reminderID)
	if err != nil {
		return Reminder{}, err
	}
	if r.OperatorID != scope.OperatorID && !scope.seesTheWholeTeam() {
		return Reminder{}, ErrNotTheOwner
	}
	return r, nil
}

func (s *Service) reaches(ctx context.Context, scope Scope, contactID uuid.UUID) error {
	_, err := s.contacts.ByID(ctx, contacts.Scope{AccountID: scope.AccountID, OperatorID: scope.OperatorID, Role: scope.Role}, contactID)
	if errors.Is(err, contacts.ErrNotFound) {
		return ErrContactNotFound
	}
	return err
}

func (s *Service) valid(description string, dueAt time.Time) (string, error) {
	description, err := s.validDescription(description)
	if err != nil {
		return "", err
	}
	if !dueAt.After(s.now()) {
		return "", ErrDueInThePast
	}
	return description, nil
}

func (s *Service) validDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return "", ErrEmptyDescription
	}
	return description, nil
}
