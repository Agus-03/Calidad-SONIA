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
}

// Target is what answering needs and the four ids do not carry: which number sends, which number receives, and in what mode.
type Target struct {
	ConversationID uuid.UUID
	InboxID        uuid.UUID
	ContactPhone   string
	Mode           enums.AgentMode
	// False for an agent paused (active) or removed (deleted_at): either way nobody answers for it (ADR-0038).
	AgentAvailable bool
}
