// Package sales owns sales: the conversions of F14, credited to the vendor of the conversation and read by F35 and F16 (ADR-0059).
package sales

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Reach is inbox/: a sale hangs from a conversation, so who reaches it is the reach of the thread and not a copy of it (ADR-0063).
type Reach interface {
	Reachable(ctx context.Context, scope inbox.Scope, conversationID uuid.UUID) error
}

var (
	ErrNotFound = errors.New("sales: sale not found")

	// ErrConversationNotFound covers one that does not exist and one the caller does not reach, as the inbox does (ADR-0035, regla 3).
	ErrConversationNotFound = errors.New("sales: conversation not found")

	// ErrNotAssigned is a sale on a conversation of the pool: there is no vendor to credit it to, so somebody takes it first (ADR-0059).
	ErrNotAssigned = errors.New("sales: the conversation has no vendor to credit the sale to")

	// ErrNotTheVendor is a change by an operator who is not the one credited, which only the two roles above may make (ADR-0059).
	ErrNotTheVendor = errors.New("sales: only the vendor credited, a supervisor or an admin changes a sale")

	// ErrInvalidAmount is an amount that is not a positive decimal with at most two places, the numeric(14,2) of the schema (C08).
	ErrInvalidAmount = errors.New("sales: the amount is not a positive decimal with at most two places")

	// ErrInvalidCurrency is a code that is not three capital letters, the ISO 4217 the schema keeps in char(3) (C08).
	ErrInvalidCurrency = errors.New("sales: the currency is not an ISO 4217 code")
)

// Scope is who is asking, built from the actor and never from the request.
type Scope struct {
	AccountID  uuid.UUID
	OperatorID uuid.UUID
	Role       enums.OperatorRole
}

// Anexo B lets these two see the whole team, so they mark and change the sales of any vendor (ADR-0059).
func (s Scope) seesTheWholeTeam() bool {
	return s.Role == enums.OperatorRoleSupervisor || s.Role == enums.OperatorRoleAdmin
}

// Conversation is what marking a sale needs from the shared aggregate, read and never written (regla 1).
type Conversation struct {
	ID         uuid.UUID
	OperatorID *uuid.UUID
	AIAgentID  *uuid.UUID
}

// Sale is one row of sales. Amount is text and never a float, so 1234.50 is stored and read back as written (C08).
type Sale struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	OperatorID     uuid.UUID
	AIAgentID      *uuid.UUID
	Amount         string
	Currency       string
	// The channel of origin F14 links the sale to, read from the contact and not copied: F15 already keeps it (ADR-0059).
	SourceChannel enums.SourceChannel
	CreatedAt     time.Time
}
