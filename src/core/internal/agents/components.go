package agents

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	// ErrComponentNotFound covers a removed component too, and one of a removed agent (C09).
	ErrComponentNotFound = errors.New("agents: prompt component not found")

	ErrUnknownComponentType = errors.New("agents: the component type is not one of the six of F01")

	// ErrEmptyContent is refused before the database sees it: the column is NOT NULL but blanks would satisfy it, and a blank block
	// prints its heading with nothing under it.
	ErrEmptyContent = errors.New("agents: the content of a component is empty")

	// The ceiling is the column's: smallint in 0001.
	ErrInvalidPosition = errors.New("agents: the position has to be a whole number between 1 and 32767")

	// ErrComponentTypeTaken is a second block of a type the agent already has: the panel writes one of each, and changing it is a new
	// version of the same block (ADR-0039).
	ErrComponentTypeTaken = errors.New("agents: the ai agent already has a component of that type")

	// ErrComponentPositionTaken is two blocks at one place, whose order in the prompt nothing would decide.
	ErrComponentPositionTaken = errors.New("agents: the ai agent already has a component at that position")
)

// componentReader is the pool-bound slice of the repository: the reads, which need no transaction of their own.
type componentReader interface {
	ByID(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error)
	Components(ctx context.Context, accountID, agentID uuid.UUID) ([]Component, error)
	Component(ctx context.Context, accountID, agentID, componentID uuid.UUID) (Component, error)
	Versions(ctx context.Context, componentID uuid.UUID) ([]ComponentVersion, error)
}

// ComponentStore is what the components write inside the transaction they conduct. Exported because internal/app/deps.go binds it to
// that transaction from outside, the same way Store is (regla 5).
type ComponentStore interface {
	ByIDForUpdate(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error)
	ComponentForUpdate(ctx context.Context, accountID, agentID, componentID uuid.UUID) (Component, error)
	ComponentTaken(ctx context.Context, agentID uuid.UUID, t enums.PromptComponentType, position int16) (bool, bool, error)
	CreateComponent(ctx context.Context, c Component) (Component, error)
	UpdateComponent(ctx context.Context, componentID uuid.UUID, content string, active bool) (Component, error)
	SoftDeleteComponent(ctx context.Context, componentID uuid.UUID) error
	AppendVersion(ctx context.Context, v ComponentVersion) (int, error)
}

// ComponentTxFunc runs fn in one transaction (regla 5): a block and its version commit together, or neither does.
type ComponentTxFunc func(ctx context.Context, fn func(ComponentStore) error) error

type ComponentOptions struct {
	Repo   componentReader
	Tx     ComponentTxFunc
	Logger *slog.Logger
}

// Components manages the six blocks of F01. It is a service of its own and not methods of Service, because nothing of it asks the
// brain, and its audit is the version table and not audit_logs (ADR-0039).
type Components struct{ opts ComponentOptions }

func NewComponents(opts ComponentOptions) *Components {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Components{opts: opts}
}

// List reads the agent first, so the blocks of an agent out of reach are a 404 and not an empty list that says it has none.
func (s *Components) List(ctx context.Context, accountID, agentID uuid.UUID) ([]Component, error) {
	if _, err := s.opts.Repo.ByID(ctx, accountID, agentID); err != nil {
		return nil, err
	}
	return s.opts.Repo.Components(ctx, accountID, agentID)
}

// Versions reads the component first, so the history of a block out of reach is as missing as the block.
func (s *Components) Versions(ctx context.Context, accountID, agentID, componentID uuid.UUID) ([]ComponentVersion, error) {
	if _, err := s.opts.Repo.Component(ctx, accountID, agentID, componentID); err != nil {
		return nil, err
	}
	return s.opts.Repo.Versions(ctx, componentID)
}

// Create adds a block with its first version, in one transaction and with the agent locked: two blocks of one type sent at once are
// serialised there, and the second sees the first (ADR-0039).
func (s *Components) Create(ctx context.Context, accountID, operatorID, agentID uuid.UUID, n NewComponent) (Component, error) {
	n.Content = strings.TrimSpace(n.Content)
	n.ChangeNote = note(n.ChangeNote)
	switch {
	case !n.Type.IsValid():
		return Component{}, ErrUnknownComponentType
	case n.Content == "":
		return Component{}, ErrEmptyContent
	case n.Position < 1 || n.Position > math.MaxInt16:
		return Component{}, ErrInvalidPosition
	}

	var created Component
	err := s.opts.Tx(ctx, func(store ComponentStore) error {
		if _, err := store.ByIDForUpdate(ctx, accountID, agentID); err != nil {
			return err
		}
		typeTaken, positionTaken, err := store.ComponentTaken(ctx, agentID, n.Type, int16(n.Position))
		if err != nil {
			return err
		}
		if typeTaken {
			return ErrComponentTypeTaken
		}
		if positionTaken {
			return ErrComponentPositionTaken
		}
		if created, err = store.CreateComponent(ctx, Component{
			ID: id.New(), AIAgentID: agentID, Type: n.Type, Content: n.Content, Position: int16(n.Position),
		}); err != nil {
			return err
		}
		created.Version, err = store.AppendVersion(ctx, ComponentVersion{
			ID: id.New(), ComponentID: created.ID, Content: created.Content, Active: created.Active,
			ChangedBy: &operatorID, ChangeNote: n.ChangeNote,
		})
		return err
	})
	if err != nil {
		return Component{}, err
	}

	s.opts.Logger.Info("prompt component created",
		slog.String("ai_agent_id", agentID.String()), slog.String("prompt_component_id", created.ID.String()),
		slog.String("type", string(created.Type)), slog.String("operator_id", operatorID.String()))
	return created, nil
}

// Update writes a new version when the text or the switch changed, and nothing at all when neither did: a version per click of
// "save" would bury the changes OE2 has to see (03-capa-prompts, Regla 2).
func (s *Components) Update(ctx context.Context, accountID, operatorID, agentID, componentID uuid.UUID, e ComponentEdit) (Component, error) {
	if e.Content != nil {
		trimmed := strings.TrimSpace(*e.Content)
		if trimmed == "" {
			return Component{}, ErrEmptyContent
		}
		e.Content = &trimmed
	}

	var updated Component
	changed := false
	err := s.opts.Tx(ctx, func(store ComponentStore) error {
		current, err := store.ComponentForUpdate(ctx, accountID, agentID, componentID)
		if err != nil {
			return err
		}
		content, active := current.Content, current.Active
		if e.Content != nil {
			content = *e.Content
		}
		if e.Active != nil {
			active = *e.Active
		}
		if content == current.Content && active == current.Active {
			updated = current
			return nil
		}

		if updated, err = store.UpdateComponent(ctx, componentID, content, active); err != nil {
			return err
		}
		changed = true
		updated.Version, err = store.AppendVersion(ctx, ComponentVersion{
			ID: id.New(), ComponentID: componentID, Content: content, Active: active,
			ChangedBy: &operatorID, ChangeNote: note(e.ChangeNote),
		})
		return err
	})
	if err != nil {
		return Component{}, err
	}

	if changed {
		s.opts.Logger.Info("prompt component changed",
			slog.String("prompt_component_id", componentID.String()), slog.Int("version", updated.Version),
			slog.String("operator_id", operatorID.String()))
	}
	return updated, nil
}

// Delete closes the block with a last version, off and with who removed it: the row has no deleted_by, and without that version
// nothing would say who took the block out of the prompt (C09, ADR-0039).
func (s *Components) Delete(ctx context.Context, accountID, operatorID, agentID, componentID uuid.UUID) error {
	err := s.opts.Tx(ctx, func(store ComponentStore) error {
		current, err := store.ComponentForUpdate(ctx, accountID, agentID, componentID)
		if err != nil {
			return err
		}
		if _, err := store.AppendVersion(ctx, ComponentVersion{
			ID: id.New(), ComponentID: componentID, Content: current.Content, Active: false, ChangedBy: &operatorID,
		}); err != nil {
			return err
		}
		return store.SoftDeleteComponent(ctx, componentID)
	})
	if err != nil {
		return err
	}

	s.opts.Logger.Info("prompt component removed",
		slog.String("prompt_component_id", componentID.String()), slog.String("operator_id", operatorID.String()))
	return nil
}

// A blank note is no note: stored as NULL, so the history does not show an empty reason as if somebody wrote one.
func note(n *string) *string {
	if n == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*n)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
