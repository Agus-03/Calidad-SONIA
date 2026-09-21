// Package turn is the one orchestrator of the system (regla 7): what the brain runs is the pipeline, and it is not this.
package turn

import (
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Batch is the turn itself: ADR-0022 makes message_batches the source of truth of the turn number, and this package owns it.
type Batch struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	TurnNumber     int
	OpenedAt       time.Time
	// The whole timer of F10: every message pushes it forward, and the poller closes the batch once it is past (ADR-0041, regla 1).
	ExpiresAt time.Time

	// The three the close writes and nothing reads until then: how many messages the customer sent, the window that was applied
	// and what stopped the wait. Empty while the batch is open, because none of the three is known yet (ADR-0041, reglas 4 y 6).
	MessagesCount int
	TimerMsUsed   int
	ClosedReason  enums.BatchCloseReason
}

// Target is what answering needs and the closed batch does not carry: which number sends, which number receives, and in what mode.
type Target struct {
	ConversationID uuid.UUID
	InboxID        uuid.UUID
	ContactPhone   string
	Mode           enums.AgentMode
	// Read here and no longer carried from the webhook: the batch closes up to a minute after the message that opened it, and
	// the admin may have given the conversation another agent, or none, in between (ADR-0041, regla 8).
	AIAgentID *uuid.UUID
	// False for an agent paused (active) or removed (deleted_at): either way nobody answers for it (ADR-0038).
	AIAgentAvailable bool
}
