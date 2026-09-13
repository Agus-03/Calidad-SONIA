// Package inbox is the operator's side of a conversation: it owns conversation_assignments, internal_notes and conversation_labels,
// and reads the shared aggregate to draw the inbox without writing it (struct.md 2, F21, F22).
package inbox

import (
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Scope is who is asking, so the listing says in its own signature that it narrows and does not merely take an account (Anexo B).
type Scope struct {
	AccountID  uuid.UUID
	OperatorID uuid.UUID
	Role       enums.OperatorRole
}

// Anexo B lets the supervisor see the conversations of the whole team and gives the admin all the supervisor has, so only those two
// are exempt: an empty or unknown role falls to the narrow side, the same default contacts/ keeps (ADR-0035, regla 1).
func (s Scope) seesTheWholeTeam() bool {
	return s.Role == enums.OperatorRoleSupervisor || s.Role == enums.OperatorRoleAdmin
}

// Summary is one row of the inbox: what F22 asks to see without opening the conversation — the last message, the waiting time, the
// operator assigned and the state of the bot.
type Summary struct {
	ID        uuid.UUID
	Status    enums.ConversationStatus
	Mode      enums.AgentMode
	AIAgentID *uuid.UUID
	Contact   Contact

	// Nil is the pool: every incoming conversation arrives unassigned until somebody answers it (ADR-0004).
	Operator *Operator

	// Nil only for a conversation nobody wrote in, which the webhook never leaves behind but a seed can.
	LastMessage *Message

	// Counted apart from the last message, because an inbound one arriving after a failed send would otherwise bury the failure the
	// operator has to decide about (ADR-0020).
	FailedCount  int
	LastFailedAt *time.Time

	// The waiting time of F22 is read off these and not stored: nothing defines it as a metric yet, and C06 persists only what one is.
	LastInboundAt   time.Time
	WindowExpiresAt time.Time
	LastActivityAt  time.Time
}

// Contact is not the entity of contacts/: it is the three fields the row draws, and none of the four sensitive ones for any role (ADR-0035, regla 4).
type Contact struct {
	ID            uuid.UUID
	Name          string
	Lastname      string
	PhoneWhatsapp string
}

// Operator is who the conversation is assigned to right now, named so the inbox can say it: an id alone is nothing a person can read.
type Operator struct {
	ID       uuid.UUID
	Name     string
	Lastname string
}

// Message is a message as the inbox draws it: the last one of a row, or the one an operator just sent. It carries its delivery
// status and error, because a failed send nobody sees is the loss ADR-0020 refuses to hide.
type Message struct {
	ID          uuid.UUID
	Direction   enums.MessageDirection
	SenderType  enums.SenderType
	ContentText string
	ContentType enums.MessageContentType
	Status      *enums.MessageStatus
	ErrorCode   *string
	SentAt      time.Time
}

// Cursor is the row a page ended on, its instant and its id to break the tie. A page continues after it instead of skipping a count,
// because rows keep arriving at the top and an offset would show one twice and skip another (ADR-0036, reglas 2 y 3).
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

// ThreadMessage is a message as the thread of one conversation shows it: the message the listing draws, plus who wrote it when a
// person did, and the instant it was recorded, which is what orders the thread (C06, ADR-0036 regla 2).
type ThreadMessage struct {
	Message
	SenderOperator *Operator
	CreatedAt      time.Time
}

// ThreadPage is newest first, so the first page is what the operator reads before answering and the cursor loads what came before.
type ThreadPage struct {
	Items []ThreadMessage
	Next  *Cursor
}

// Target is what acting on a conversation needs and its id does not carry: where an answer goes, who holds it, whether Meta still
// accepts a free form answer (ADR-0008), and which bot answers in what mode (F27).
type Target struct {
	ConversationID  uuid.UUID
	InboxID         uuid.UUID
	ContactPhone    string
	OperatorID      *uuid.UUID
	Status          enums.ConversationStatus
	WindowExpiresAt time.Time
	AIAgentID       *uuid.UUID
	Mode            enums.AgentMode
}

// Assignment is one row of conversation_assignments: immutable, because F26 asks for the history and a row that changes is not one.
type Assignment struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	// Nil when the conversation comes from the pool, which is the only kind this phase writes (ADR-0035, regla 15).
	FromOperatorID *uuid.UUID
	ToOperatorID   uuid.UUID
	// The same as ToOperatorID in an auto assignment: the operator who answered is the one who took it (ADR-0035, regla 7).
	AssignedByOperatorID *uuid.UUID
	Reason               string
}

// Page has no total on purpose: F22 does not ask for one, and counting the whole inbox on every page is the query a cursor exists
// to avoid (ADR-0036, regla 7).
type Page struct {
	Items []Summary

	// Nil when the page reached the end, so "there is no more" is told apart from "the next page happens to be empty" (ADR-0036, regla 6).
	Next *Cursor
}
