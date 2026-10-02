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

	// One flag per conversation and not per Operator, as the schema has it: whoever starred it, everyone who reaches it sees it (F25).
	Starred bool

	// The customer messages since somebody who could clear it last read the conversation: one count for all, not one per Operator (ADR-0052).
	UnreadCount int

	// F13: one to five from the vendor, nil until it scores; a new score overwrites the last one (ADR-0055).
	QualityScore *int

	// F24: the catalogue entries on the conversation, retired ones left out, so the filter by label is decided from the event (ADR-0057).
	Labels []Label

	// Whether the bot of this conversation could answer if it were on: its agent exists, is active and was not removed. It is the
	// state of the bot F22 asks to see without opening the conversation, and what tells the vendor the switch would refuse to turn
	// on (ADR-0038). The mode alone cannot say it: a paused agent leaves its conversations off, like any the vendor turned off.
	AIAgentAvailable bool

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

// Filter is the seven filters of F23, combined with AND and always inside the reach: a filter narrows what the role sees and never
// widens it. None of them changes the order, so the cursor of ADR-0036 continues a filtered listing too (ADR-0050).
type Filter struct {
	Unread       bool
	Assigned     bool
	Starred      bool
	WithReminder bool
	Status       *enums.ConversationStatus
	LabelID      *uuid.UUID
}

// ThreadMessage is a message as the thread of one conversation shows it: the message the listing draws, plus who wrote it when a
// person did, and the instant it was recorded, which is what orders the thread (C06, ADR-0036 regla 2).
type ThreadMessage struct {
	Message
	SenderOperator *Operator
	CreatedAt      time.Time
	// The generation the text came from, when the AIAgent wrote it or an Operator sent its draft: what F05 is asked about (ADR-0049).
	GenerationID *uuid.UUID
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
	// False when the agent is paused or removed, or there is none: turning the bot on then names one that cannot answer (ADR-0038).
	AIAgentAvailable bool
	Starred          bool
	UnreadCount      int
}

// Assignment is one row of conversation_assignments: immutable, because F26 asks for the history and a row that changes is not one.
type Assignment struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	// Nil when the conversation had nobody before, which is every kind this phase writes: nobody reassigns yet (ADR-0035, regla 15).
	FromOperatorID *uuid.UUID
	ToOperatorID   uuid.UUID
	// The same as ToOperatorID when an operator takes the pool by answering, since who answered is who took it (ADR-0035, regla 7);
	// nil when the dispatcher assigns, because nobody did (ADR-0037, regla 5).
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

// Suggestion is the draft of the AIAgent waiting for the Operator in suggestion mode: it is copied into the box and sent as the
// Operator's, or ignored (ADR-0046, section 2.5).
type Suggestion struct {
	GenerationID uuid.UUID
	Text         string
	CreatedAt    time.Time
}

// AssignmentEntry is one row of the history F26 asks for, with the people named so it can be read as it comes (ADR-0056).
type AssignmentEntry struct {
	ID uuid.UUID
	// Nil when the conversation came from the pool.
	From *Operator
	To   Operator
	// Nil when the dispatcher assigned it, because nobody did (ADR-0037, regla 5).
	AssignedBy *Operator
	Reason     string
	CreatedAt  time.Time
}

// Label is a catalogue entry as a conversation carries it: what the row draws and nothing of who defined it (F24, ADR-0057).
type Label struct {
	ID    uuid.UUID
	Name  string
	Color string
}
