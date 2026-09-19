package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

var (
	// ErrDuplicateDraftType is two drafts of one type in the same preview: a prompt the panel could not save (ADR-0039).
	ErrDuplicateDraftType = errors.New("agents: two drafts of one component type")

	// ErrDuplicateDraftPosition is two drafts at one place, whose order in the prompt nothing would decide.
	ErrDuplicateDraftPosition = errors.New("agents: two drafts at one position")

	// ErrPreviewUnavailable is the brain failing to assemble: the panel says so and the next keystroke asks again.
	ErrPreviewUnavailable = errors.New("agents: the brain could not assemble the preview")
)

// Assembler is the brain's preview. The core never assembles a prompt itself: the assembler has one implementation, in Python, or
// OE1 would measure a render other than the one the administrator sees (ADR-0017, ADR-0040).
type Assembler interface {
	Preview(ctx context.Context, req brainclient.PreviewRequest) (brainclient.PreviewResponse, error)
}

type agentReader interface {
	ByID(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error)
}

type PreviewOptions struct {
	Repo   agentReader
	Brain  Assembler
	Logger *slog.Logger
}

// Previews shows the administrator the system the model would read with the blocks on screen (F03). It is a service of its own
// because it writes nothing and it is the only one of the package that waits on the brain for a read.
type Previews struct{ opts PreviewOptions }

func NewPreviews(opts PreviewOptions) *Previews {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Previews{opts: opts}
}

// Draft is a block as it is on screen, saved or not. A blank one is allowed: its section disappears, the same as in a turn.
type Draft struct {
	Type     enums.PromptComponentType
	Content  string
	Position int
}

// Preview is the system alone: the envelope of a customer's message is not previewed (ADR-0040).
type Preview struct {
	System       string
	RulesVersion int
	Components   []enums.PromptComponentType
}

// Assemble validates the drafts with the rules of a save, checks the agent is in reach and asks the brain. Nothing is written and
// nothing is audited: a preview is neither configuration nor evidence (ADR-0040).
func (s *Previews) Assemble(ctx context.Context, accountID, agentID uuid.UUID, drafts []Draft) (Preview, error) {
	types := map[enums.PromptComponentType]bool{}
	positions := map[int]bool{}
	// Never nil: the contract declares a list, and a nil slice travels as null.
	out := make([]brainclient.DraftComponent, 0, len(drafts))
	for _, d := range drafts {
		switch {
		case !d.Type.IsValid():
			return Preview{}, ErrUnknownComponentType
		case d.Position < 1 || d.Position > math.MaxInt16:
			return Preview{}, ErrInvalidPosition
		case types[d.Type]:
			return Preview{}, ErrDuplicateDraftType
		case positions[d.Position]:
			return Preview{}, ErrDuplicateDraftPosition
		}
		types[d.Type], positions[d.Position] = true, true
		out = append(out, brainclient.DraftComponent{Type: d.Type, Content: d.Content, Position: d.Position})
	}

	// Read here and not left to the brain, because the brain does not know accounts: an agent of another account is a 404 here.
	if _, err := s.opts.Repo.ByID(ctx, accountID, agentID); err != nil {
		return Preview{}, err
	}

	res, err := s.opts.Brain.Preview(ctx, brainclient.PreviewRequest{AIAgentID: agentID, Components: out})
	switch {
	case errors.Is(err, brainclient.ErrAIAgentNotFound):
		return Preview{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	case err != nil:
		return Preview{}, fmt.Errorf("%w: %w", ErrPreviewUnavailable, err)
	}
	return Preview{System: res.System, RulesVersion: res.RulesVersion, Components: res.Components}, nil
}
