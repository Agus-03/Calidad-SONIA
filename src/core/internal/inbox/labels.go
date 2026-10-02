package inbox

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// ErrLabelNotFound is a label that is not in the catalogue of the account, or was retired from it: F24 tags from the catalogue only.
var ErrLabelNotFound = errors.New("inbox: no such label in the catalogue")

// Catalogue is labels/, the owner of the catalogue: the inbox asks it before linking and never checks the table itself (regla 1).
type Catalogue interface {
	ByID(ctx context.Context, accountID, labelID uuid.UUID) (labels.Label, error)
}

// AssignLabel is F24 with the reach of the thread, resolved ones included; tagging twice answers the state it asked for (ADR-0057).
func (s *Service) AssignLabel(ctx context.Context, scope Scope, conversationID, labelID uuid.UUID) ([]Label, error) {
	if err := s.reachable(ctx, scope, conversationID); err != nil {
		return nil, err
	}
	if _, err := s.opts.Catalogue.ByID(ctx, scope.AccountID, labelID); err != nil {
		if errors.Is(err, labels.ErrNotFound) {
			return nil, ErrLabelNotFound
		}
		return nil, err
	}

	changed, err := s.opts.Repo.AssignLabel(ctx, id.New(), conversationID, labelID, scope.OperatorID)
	if err != nil {
		return nil, err
	}
	return s.labelled(ctx, conversationID, changed)
}

// UnassignLabel does not ask the catalogue: a label of another account was never on a conversation of this one, so nothing is removed.
func (s *Service) UnassignLabel(ctx context.Context, scope Scope, conversationID, labelID uuid.UUID) ([]Label, error) {
	if err := s.reachable(ctx, scope, conversationID); err != nil {
		return nil, err
	}

	changed, err := s.opts.Repo.UnassignLabel(ctx, conversationID, labelID)
	if err != nil {
		return nil, err
	}
	return s.labelled(ctx, conversationID, changed)
}

// labelled tells the hub only when a link moved, because the labels are on the row now and nothing else of it changed (ADR-0057).
func (s *Service) labelled(ctx context.Context, conversationID uuid.UUID, changed bool) ([]Label, error) {
	if changed {
		s.opts.Events.Changed(conversationID)
	}
	return s.opts.Repo.LabelsOf(ctx, conversationID)
}
