package inbox

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// SetMode turns the agent of one conversation on or off without touching the others (F27, brought forward from Fase 10). The operator
// switches the conversations it holds and supervisor and admin any of the account (ADR-0037, regla 4); besides that, what this decides
// is which changes make sense for the conversation (ADR-0035, reglas 11 a 14).
//
// It does not take the conversation out of the pool: answering is what assigns, and turning the bot off is not an answer (ADR-0035, regla 13).
//
// The orchestrator reads the mode when a turn starts, so an answer the brain is already generating still goes out after the bot is
// turned off. The gap is a declared cost (ADR-0035 §4).
func (s *Service) SetMode(ctx context.Context, scope Scope, conversationID uuid.UUID, mode enums.AgentMode) (enums.AgentMode, error) {
	// All three of F02 since ADR-0046 amended regla 11 of ADR-0035: suggestion has its turn and its draft now.
	if !mode.IsValid() {
		return "", ErrUnknownMode
	}

	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return "", err
	}
	// Reach first, so a closed conversation of another vendor is still one that does not exist; then closed before not held, because
	// telling the vendor to take a resolved one first sends it to answer and be refused again (ADR-0035, regla 14).
	if !mayRead(scope, target) {
		return "", ErrNotFound
	}
	if target.Status == enums.ConversationStatusResolved {
		return "", ErrResolved
	}
	if err := maySwitch(scope, target); err != nil {
		return "", err
	}
	// Asking for the mode it already has is the state the caller wants, so it is answered and not written twice.
	if target.Mode == mode {
		return mode, nil
	}
	if mode != enums.AgentModeOff && target.AIAgentID == nil {
		return "", ErrNoAIAgent
	}
	// The same conditions the turn reads before answering: a switch that says the bot is on while the turn skips it lies to the
	// vendor, who then leaves the customer unanswered (ADR-0038).
	if mode != enums.AgentModeOff && !target.AIAgentAvailable {
		return "", ErrAIAgentUnavailable
	}

	switch err := s.opts.Aggregate.SetMode(ctx, conversationID, mode); {
	case err == nil:
		s.opts.Events.Changed(conversationID)
		return mode, nil
	case errors.Is(err, conversations.ErrNoAIAgent):
		// The read already refused a conversation with no agent, so the predicate refused a pause or a removal that committed in
		// between (ADR-0038).
		return "", ErrAIAgentUnavailable
	case errors.Is(err, conversations.ErrNotFound):
		return "", ErrNotFound
	default:
		return "", err
	}
}

// SetStarred marks one conversation for follow-up or clears the mark (F25). The reach is the thread's: whoever can read the
// conversation can star it, because F25 names no role and the star is a mark on what the caller already sees (ADR-0035, regla 2).
//
// A resolved conversation is starred too: a follow-up is what comes after the work, not part of it. And starring never assigns,
// for the same reason turning the bot off does not (ADR-0035, regla 13).
func (s *Service) SetStarred(ctx context.Context, scope Scope, conversationID uuid.UUID, starred bool) (bool, error) {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return false, err
	}
	if !mayRead(scope, target) {
		return false, ErrNotFound
	}
	// The same as the mode: asking for the state it already has is answered and not written twice.
	if target.Starred == starred {
		return starred, nil
	}

	if err := s.opts.Aggregate.SetStarred(ctx, conversationID, starred); err != nil {
		if errors.Is(err, conversations.ErrNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}
	s.opts.Events.Changed(conversationID)
	return starred, nil
}

// MarkRead clears the unread count when the conversation is on screen (ADR-0052). Who may clear it is who may answer it: its holder,
// or anybody when it is in the pool (ADR-0035, regla 5). A supervisor looking at a vendor's conversation does not take the vendor's
// warning away, and is told the conversation is somebody else's; an operator is told it does not exist, as always.
//
// A conversation with nothing unread is answered and not written, and nobody is told: nothing changed.
func (s *Service) MarkRead(ctx context.Context, scope Scope, conversationID uuid.UUID) error {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return err
	}
	if err := mayAnswer(scope, target); err != nil {
		return err
	}
	if target.UnreadCount == 0 {
		return nil
	}

	if err := s.opts.Aggregate.MarkRead(ctx, conversationID); err != nil {
		if errors.Is(err, conversations.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.opts.Events.Changed(conversationID)
	return nil
}

// Resolve closes the conversation by hand (ADR-0002 (d)) with the reach of the switch of the bot: the vendor its own, the two roles above any (ADR-0054).
func (s *Service) Resolve(ctx context.Context, scope Scope, conversationID uuid.UUID) error {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return err
	}
	// The order of SetMode: another vendor's does not exist, a closed one is told so before telling the vendor to take it first.
	if !mayRead(scope, target) {
		return ErrNotFound
	}
	if target.Status == enums.ConversationStatusResolved {
		return ErrResolved
	}
	if err := maySwitch(scope, target); err != nil {
		return err
	}

	switch err := s.opts.Aggregate.Resolve(ctx, conversationID, scope.OperatorID, s.opts.TurnBudget); {
	case err == nil:
		s.opts.Events.Changed(conversationID)
		return nil
	case errors.Is(err, conversations.ErrResolved):
		return ErrResolved
	case errors.Is(err, conversations.ErrTurnInProgress):
		return ErrTurnInProgress
	default:
		return err
	}
}

// Score is F13: the vendor scores the conversation it held once it is resolved, and a new score overwrites the last (ADR-0055).
func (s *Service) Score(ctx context.Context, scope Scope, conversationID uuid.UUID, score int) (int, error) {
	if score < minScore || score > maxScore {
		return 0, ErrInvalidScore
	}

	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return 0, err
	}
	// Reach first, as everywhere: another vendor's conversation does not exist for an operator (ADR-0035, regla 3).
	if !mayRead(scope, target) {
		return 0, ErrNotFound
	}
	if target.Status != enums.ConversationStatusResolved {
		return 0, ErrNotResolved
	}
	if target.OperatorID == nil || *target.OperatorID != scope.OperatorID {
		return 0, ErrNotTheVendor
	}

	if err := s.opts.Aggregate.Score(ctx, conversationID, scope.OperatorID, score); err != nil {
		if errors.Is(err, conversations.ErrNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	s.opts.Events.Changed(conversationID)
	return score, nil
}
