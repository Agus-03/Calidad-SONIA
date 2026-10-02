package reasoning

import (
	"context"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	Reasoning(ctx context.Context, conversationID, generationID uuid.UUID) (Reasoning, error)
}

type Service struct {
	repo  store
	reach Reach
}

func NewService(repo store, reach Reach) *Service { return &Service{repo: repo, reach: reach} }

// Reasoning answers F05 with the reach of the thread, and the text of each component only to the admin, who owns it (ADR-0049).
func (s *Service) Reasoning(ctx context.Context, scope inbox.Scope, conversationID, generationID uuid.UUID) (Reasoning, error) {
	if err := s.reach.Reachable(ctx, scope, conversationID); err != nil {
		return Reasoning{}, err
	}

	r, err := s.repo.Reasoning(ctx, conversationID, generationID)
	if err != nil {
		return Reasoning{}, err
	}
	if scope.Role != enums.OperatorRoleAdmin {
		// A copy and not the slice the store handed over, so the rule cannot leak into whatever else holds it.
		used := make([]UsedComponent, len(r.Components))
		for i, c := range r.Components {
			c.Content = nil
			used[i] = c
		}
		r.Components = used
	}
	return r, nil
}
