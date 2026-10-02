// Package reasoning is F05: which components of the prompt an answer was assembled from, read with the reach of the thread (ADR-0049).
package reasoning

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// ErrGenerationNotFound covers a generation of another conversation too: F05 reads one answer of the thread in the path.
var ErrGenerationNotFound = errors.New("reasoning: generation not found")

// Reach is inbox/: what the Operator cannot read it cannot ask about, which is the reach of the thread (ADR-0035, ADR-0063).
type Reach interface {
	Reachable(ctx context.Context, scope inbox.Scope, conversationID uuid.UUID) error
}

// Reasoning is F05: the components a generation was assembled from, in order, since generation_components is the evidence (ADR-0049).
type Reasoning struct {
	GenerationID uuid.UUID
	Status       enums.GenerationStatus
	LLMModel     string
	CreatedAt    time.Time
	Components   []UsedComponent
}

// UsedComponent is one version as it was assembled, frozen: a component edited or removed afterwards still shows what was used.
type UsedComponent struct {
	Type          enums.PromptComponentType
	Position      int
	VersionNumber int
	// Nil for everyone but the admin, who is the one with access to the configuration it comes from (ADR-0049).
	Content *string
}
