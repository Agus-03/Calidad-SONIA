package inbox

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// The reason says which rule chose the vendor, so the history of F26 can tell a returning customer from a turn (ADR-0037).
const (
	reasonPreviousOperator = "auto_assigned_to_previous_operator"
	reasonTurn             = "auto_assigned_by_turn"
)

// DispatchStore is what the dispatcher reads and writes inside the webhook's transaction. Exported because internal/app/deps.go binds
// it to that transaction from outside, the same way Store is (regla 5).
type DispatchStore interface {
	PreviousOperator(ctx context.Context, accountID, contactID, conversationID uuid.UUID) (*uuid.UUID, error)
	NextInTurn(ctx context.Context, accountID uuid.UUID) (*uuid.UUID, error)
	RecordAssignment(ctx context.Context, a Assignment) error
}

// Claimer is the one write of conversations/ the dispatcher needs: the column the reach reads (regla 1).
type Claimer interface {
	Claim(ctx context.Context, conversationID, operatorID uuid.UUID) error
}

// Dispatcher gives a conversation that nobody holds to a vendor the moment the customer writes, so every conversation has one from
// the start and the pool is only what arrives when nobody can receive it (ADR-0037).
type Dispatcher struct {
	store     DispatchStore
	aggregate Claimer
	log       *slog.Logger
}

func NewDispatcher(store DispatchStore, aggregate Claimer, log *slog.Logger) *Dispatcher {
	if log == nil {
		log = slog.Default()
	}
	return &Dispatcher{store: store, aggregate: aggregate, log: log}
}

// Assign runs inside the transaction of the webhook that brought the message, so the conversation, its vendor and the history row
// commit together (regla 5). Finding nobody is not an error: the conversation stays in the pool, which ADR-0004 already covers.
//
// No schedule enters the choice: office hours are out of scope (ADR-0009).
func (d *Dispatcher) Assign(ctx context.Context, accountID, contactID, conversationID uuid.UUID) error {
	operatorID, reason, err := d.choose(ctx, accountID, contactID, conversationID)
	if err != nil || operatorID == nil {
		return err
	}

	if err := d.aggregate.Claim(ctx, conversationID, *operatorID); err != nil {
		// Somebody took it between the read and this write, by answering it from the pool: it has a vendor, which is the point.
		if errors.Is(err, conversations.ErrTaken) {
			return nil
		}
		return err
	}
	// Nobody assigned it, so assigned_by is empty: the reason says it was the system, and which of its rules.
	return d.store.RecordAssignment(ctx, Assignment{
		ID:             id.New(),
		ConversationID: conversationID,
		ToOperatorID:   *operatorID,
		Reason:         reason,
	})
}

// choose prefers the vendor the customer already knows, and otherwise the next one in turn (ADR-0037, regla 1).
func (d *Dispatcher) choose(ctx context.Context, accountID, contactID, conversationID uuid.UUID) (*uuid.UUID, string, error) {
	previous, err := d.store.PreviousOperator(ctx, accountID, contactID, conversationID)
	if err != nil {
		return nil, "", err
	}
	if previous != nil {
		return previous, reasonPreviousOperator, nil
	}

	next, err := d.store.NextInTurn(ctx, accountID)
	if err != nil {
		return nil, "", err
	}
	if next == nil {
		d.log.Warn("no active vendor to receive the conversation, it stays in the pool",
			slog.String("conversation_id", conversationID.String()))
		return nil, "", nil
	}
	return next, reasonTurn, nil
}
