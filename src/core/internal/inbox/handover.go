package inbox

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	// ErrCannotReassign is an operator asking to move a conversation: Anexo B says it "no puede reasignar", and the group kept it (ADR-0056).
	ErrCannotReassign = errors.New("inbox: only a supervisor or an admin reassigns")

	// ErrNoReason is a reassignment with no why: the history of F26 is read by people, and a row with no reason tells them nothing.
	ErrNoReason = errors.New("inbox: a reassignment needs a reason")

	// ErrAlreadyAssigned is a reassignment to whoever already holds the conversation: nothing would change but a row of history.
	ErrAlreadyAssigned = errors.New("inbox: the conversation is already assigned to that operator")

	// ErrSameOperator is a handover from a vendor to the same vendor, which moves nothing (ADR-0056).
	ErrSameOperator = errors.New("inbox: the handover names the same operator twice")

	// ErrOperatorNotFound is a person who is not of the caller's account, told apart from one who is and cannot receive (ADR-0056).
	ErrOperatorNotFound = errors.New("inbox: no such operator in the account")

	// ErrOperatorUnavailable is a destination that cannot work a conversation: disabled or taken down (ADR-0056).
	ErrOperatorUnavailable = errors.New("inbox: the operator is disabled or removed")
)

// Reassign is F26: supervisor and admin give an open conversation to another person, and all but who holds it stays (ADR-0056).
func (s *Service) Reassign(ctx context.Context, scope Scope, conversationID, to uuid.UUID, reason string) error {
	if !scope.seesTheWholeTeam() {
		return ErrCannotReassign
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrNoReason
	}

	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return err
	}
	// A resolved one gets no more messages, and its sale and score stay with whoever attended it (ADR-0056).
	if target.Status == enums.ConversationStatusResolved {
		return ErrResolved
	}
	if target.OperatorID != nil && *target.OperatorID == to {
		return ErrAlreadyAssigned
	}
	if err := s.receives(ctx, scope.AccountID, to); err != nil {
		return err
	}

	err = s.opts.Tx(ctx, func(store Store, aggregate Aggregate, _ Reviews) error {
		if err := aggregate.Reassign(ctx, conversationID, target.OperatorID, to); err != nil {
			if errors.Is(err, conversations.ErrTaken) {
				return ErrTaken
			}
			return err
		}
		return store.RecordAssignment(ctx, Assignment{
			ID: id.New(), ConversationID: conversationID, FromOperatorID: target.OperatorID, ToOperatorID: to,
			AssignedByOperatorID: &scope.OperatorID, Reason: reason,
		})
	})
	if err != nil {
		return err
	}
	// The pool's vendors lose a row they had on screen, and a held one only its former vendor does (ADR-0051 regla 9, ADR-0056).
	if target.OperatorID == nil {
		s.opts.Events.LeftPool(conversationID)
	} else {
		s.opts.Events.Reassigned(conversationID, *target.OperatorID)
	}
	return nil
}

// HandOver settles the absence of ADR-0037: every open conversation of one vendor, disabled or not, goes to another (ADR-0056).
func (s *Service) HandOver(ctx context.Context, scope Scope, from, to uuid.UUID, reason string) (int, error) {
	if !scope.seesTheWholeTeam() {
		return 0, ErrCannotReassign
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return 0, ErrNoReason
	}
	if from == to {
		return 0, ErrSameOperator
	}
	// Only that it is of the account: whether it can still work is what the handover is usually for.
	if _, err := s.opts.Repo.OperatorState(ctx, scope.AccountID, from); err != nil {
		return 0, err
	}
	if err := s.receives(ctx, scope.AccountID, to); err != nil {
		return 0, err
	}

	var moved []uuid.UUID
	err := s.opts.Tx(ctx, func(store Store, aggregate Aggregate, _ Reviews) error {
		var err error
		if moved, err = aggregate.HandOver(ctx, from, to); err != nil {
			return err
		}
		for _, conversationID := range moved {
			if err := store.RecordAssignment(ctx, Assignment{
				ID: id.New(), ConversationID: conversationID, FromOperatorID: &from, ToOperatorID: to,
				AssignedByOperatorID: &scope.OperatorID, Reason: reason,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, conversationID := range moved {
		s.opts.Events.Reassigned(conversationID, from)
	}
	return len(moved), nil
}

// Assignments is the history of F26 with the reach of the thread: who held the conversation, who moved it and why (ADR-0056).
func (s *Service) Assignments(ctx context.Context, scope Scope, conversationID uuid.UUID) ([]AssignmentEntry, error) {
	if err := s.Reachable(ctx, scope, conversationID); err != nil {
		return nil, err
	}
	return s.opts.Repo.Assignments(ctx, conversationID)
}

// receives is whether the person can take conversations now: of the account, active and not taken down (ADR-0056).
func (s *Service) receives(ctx context.Context, accountID, operatorID uuid.UUID) error {
	available, err := s.opts.Repo.OperatorState(ctx, accountID, operatorID)
	if err != nil {
		return err
	}
	if !available {
		return ErrOperatorUnavailable
	}
	return nil
}

// Reachable is the reach of the thread for what hangs from a conversation, exported for notes/ and reasoning/ (ADR-0035, ADR-0063).
func (s *Service) Reachable(ctx context.Context, scope Scope, conversationID uuid.UUID) error {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return err
	}
	if !mayRead(scope, target) {
		return ErrNotFound
	}
	return nil
}
