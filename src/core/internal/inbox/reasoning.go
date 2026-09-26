package inbox

import (
	"context"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Reasoning answers F05 with the reach of the thread: what the Operator cannot read it cannot ask about. The text of each
// component stays with the admin, who is the one with access to the configuration it comes from (ADR-0049).
func (s *Service) Reasoning(ctx context.Context, scope Scope, conversationID, generationID uuid.UUID) (Reasoning, error) {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return Reasoning{}, err
	}
	if !mayRead(scope, target) {
		return Reasoning{}, ErrNotFound
	}

	r, err := s.opts.Repo.Reasoning(ctx, conversationID, generationID)
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
