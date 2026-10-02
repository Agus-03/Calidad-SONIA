// Package reminders owns reminders: the dated alerts of F28, hung from the contact (ADR-0002 (c)) and told to its vendor when due (ADR-0058).
package reminders

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

var (
	ErrNotFound = errors.New("reminders: reminder not found")

	// ErrContactNotFound is a contact the caller does not reach, which for it does not exist (ADR-0032).
	ErrContactNotFound = errors.New("reminders: contact not found")

	// ErrConversationNotOfContact is a conversation named that is not one of the contact the reminder hangs from (ADR-0002 (c)).
	ErrConversationNotOfContact = errors.New("reminders: the conversation is not one of the contact")

	ErrEmptyDescription = errors.New("reminders: the reminder has no description")

	// ErrDueInThePast is an alert that would be due before it exists: the poller would fire it at once, which nobody asked for.
	ErrDueInThePast = errors.New("reminders: the reminder is due in the past")

	// ErrNotTheOwner is a change by anybody but who created it or the two roles above, who the group let reorganise the team (ADR-0058).
	ErrNotTheOwner = errors.New("reminders: only its owner, a supervisor or an admin changes a reminder")
)

// Scope is who is asking, built from the actor and never from the request.
type Scope struct {
	AccountID  uuid.UUID
	OperatorID uuid.UUID
	Role       enums.OperatorRole
}

// Anexo B lets these two see the whole team, so they are the ones who may change a reminder they did not write.
func (s Scope) seesTheWholeTeam() bool {
	return s.Role == enums.OperatorRoleSupervisor || s.Role == enums.OperatorRoleAdmin
}

// Reminder is one row of reminders. OperatorID is who created it, not who is told: that is decided when it falls due (ADR-0058).
type Reminder struct {
	ID             uuid.UUID
	ContactID      uuid.UUID
	ConversationID *uuid.UUID
	OperatorID     uuid.UUID
	Description    string
	DueAt          time.Time
	Notified       bool
	NotifiedAt     *time.Time
	CreatedAt      time.Time
}

// Due is a reminder the poller just marked, with the person it goes to.
type Due struct {
	Reminder
	Recipient uuid.UUID
}
