// Package conversations owns the shared aggregate: turn/, inbox/ and whatsapp/ all write these two tables through it (regla 1).
package conversations

import (
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Conversation carries its holder because the channel decides on it: a nil OperatorID is the pool, which since ADR-0037 only happens
// when no vendor was active to receive the conversation.
type Conversation struct {
	ID              uuid.UUID
	ContactID       uuid.UUID
	InboxID         uuid.UUID
	AIAgentID       *uuid.UUID
	OperatorID      *uuid.UUID
	Status          enums.ConversationStatus
	Mode            enums.AgentMode
	LastInboundAt   time.Time
	WindowExpiresAt time.Time
}

// Message carries pointers where the channel decides later: an outbound row exists before Meta has named or graded it.
type Message struct {
	ID               uuid.UUID
	ConversationID   uuid.UUID
	MessageBatchID   *uuid.UUID
	WAMID            *string
	Direction        enums.MessageDirection
	SenderType       enums.SenderType
	SenderAIAgentID  *uuid.UUID
	SenderOperatorID *uuid.UUID
	ContentText      string
	ContentType      enums.MessageContentType
	Status           *enums.MessageStatus
	SentAt           time.Time
}
