package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

var (
	ErrConversationNotFound = errors.New("turn: conversation not found")
	ErrBatchNotFound        = errors.New("turn: message batch not found")
	ErrNoAnswer             = errors.New("turn: the brain produced no answer")
	ErrGenerationNotFound   = errors.New("turn: generation not found")
)

// Indirected so a test can conduct a turn without a database; everything else takes its ids from outside.
var newID = id.New

// The collaborators are declared here because this is the side that uses them, and none of them names a concrete type.
type Store interface {
	Target(ctx context.Context, conversationID uuid.UUID) (Target, error)
	OpenBatch(ctx context.Context, conversationID uuid.UUID, at time.Time) (Batch, error)
	MarkProcessed(ctx context.Context, batchID uuid.UUID, at time.Time) error
	MarkFailed(ctx context.Context, batchID uuid.UUID, reason string) error
	LinkGeneration(ctx context.Context, generationID, messageID uuid.UUID) error
}

// Aggregate is conversations/: this package never writes messages with SQL of its own (regla 1).
type Aggregate interface {
	LinkToBatch(ctx context.Context, messageID, batchID uuid.UUID) error
	RecordOutbound(ctx context.Context, conversationID, aiAgentID uuid.UUID, text string, at time.Time) (conversations.Message, error)
	MarkSent(ctx context.Context, messageID uuid.UUID, wamid string, at time.Time) error
	MarkFailed(ctx context.Context, messageID uuid.UUID, errorCode string) error
}

type Brain interface {
	Turn(ctx context.Context, req brainclient.TurnRequest) (brainclient.TurnResponse, error)
}

type Channel interface {
	InboxByID(ctx context.Context, inboxID uuid.UUID) (whatsapp.Inbox, error)
}

// TxFunc runs fn in one transaction with both collaborators bound to it (regla 5), and is a function so a test needs no PostgreSQL.
type TxFunc func(ctx context.Context, fn func(Store, Aggregate) error) error

type Options struct {
	Tx        TxFunc
	Store     Store
	Aggregate Aggregate
	Brain     Brain
	Channel   Channel
	Sender    whatsapp.Sender

	// Bounds how long one goroutine can live, and with it how long a deployment waits before cutting (ADR-0029).
	Budget       time.Duration
	MaxRetries   int
	RetryBackoff time.Duration

	Logger *slog.Logger
}

type Orchestrator struct {
	opts Options
	// Counts the turns in flight so the shutdown can wait for them: with the 200 already sent, nobody else would.
	inFlight sync.WaitGroup
}

func New(opts Options) *Orchestrator {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Orchestrator{opts: opts}
}

// Registered here and not inside the goroutine, so a shutdown that begins right now still finds this turn (ADR-0029).
func (o *Orchestrator) Start(ctx context.Context, conversationID, messageID uuid.UUID, aiAgentID *uuid.UUID) {
	if aiAgentID == nil {
		// A conversation with no agent has nobody to answer it: the operator sees it in the inbox and that is the whole flow.
		o.opts.Logger.Info("turn skipped",
			slog.String("conversation_id", conversationID.String()),
			slog.String("reason", "the conversation has no agent"),
		)
		return
	}

	agentID := *aiAgentID
	o.inFlight.Add(1)

	go func() {
		defer o.inFlight.Done()

		// Its own deadline: the request's context is already cancelled, and an unbounded turn holds the shutdown open.
		ctx, cancel := context.WithTimeout(ctx, o.opts.Budget)
		defer cancel()

		if err := o.run(ctx, conversationID, messageID, agentID); err != nil {
			o.opts.Logger.Error("turn failed",
				slog.String("conversation_id", conversationID.String()),
				slog.String("cause", err.Error()),
			)
		}
	}()
}

// Wait blocks until every turn in flight finished or ctx expires, which is the ordered shutdown of ADR-0029.
func (o *Orchestrator) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		o.inFlight.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *Orchestrator) run(ctx context.Context, conversationID, messageID, aiAgentID uuid.UUID) error {
	target, err := o.opts.Store.Target(ctx, conversationID)
	if err != nil {
		return err
	}
	// The three modes branch the orchestrator in Fase 9; until then only the autonomous one answers by itself (F02).
	if target.Mode != enums.AgentModeAutonomous {
		o.opts.Logger.Info("turn not answered",
			slog.String("conversation_id", conversationID.String()),
			slog.String("mode", string(target.Mode)),
		)
		return nil
	}

	batch, err := o.openBatch(ctx, conversationID, messageID, time.Now().UTC())
	if err != nil {
		return err
	}

	answer, err := o.ask(ctx, batch, aiAgentID, target.Mode)
	if err != nil {
		return o.abandon(ctx, batch, err)
	}

	if answer.GenerationID == nil {
		// The customer still gets the answer, but a generation nobody can find is indistinguishable from one nobody wrote (OE2).
		o.opts.Logger.Warn("turn answered with no evidence to link",
			slog.String("conversation_id", conversationID.String()),
		)
	}

	message, err := o.persist(ctx, batch, conversationID, aiAgentID, answer, time.Now().UTC())
	if err != nil {
		return o.abandon(ctx, batch, err)
	}

	return o.deliver(ctx, target, message, answer.Text())
}

// Both writes together: a batch nobody linked leaves the brain reading a turn with no incoming messages.
func (o *Orchestrator) openBatch(ctx context.Context, conversationID, messageID uuid.UUID, at time.Time) (Batch, error) {
	var batch Batch

	err := o.opts.Tx(ctx, func(store Store, aggregate Aggregate) error {
		var err error
		if batch, err = store.OpenBatch(ctx, conversationID, at); err != nil {
			return err
		}
		return aggregate.LinkToBatch(ctx, messageID, batch.ID)
	})
	return batch, err
}

func (o *Orchestrator) ask(ctx context.Context, batch Batch, aiAgentID uuid.UUID, mode enums.AgentMode) (brainclient.TurnResponse, error) {
	req := brainclient.TurnRequest{
		MessageBatchID: batch.ID,
		ConversationID: batch.ConversationID,
		AIAgentID:      aiAgentID,
		Mode:           mode,
	}

	var res brainclient.TurnResponse
	err := Do(ctx, o.opts.MaxRetries, o.opts.RetryBackoff, func(ctx context.Context) error {
		var err error
		res, err = o.opts.Brain.Turn(ctx, req)
		return err
	})
	if err != nil {
		return brainclient.TurnResponse{}, err
	}

	// blocked and failed both mean there is nothing to send, and generated with an empty text is neither an answer nor a rejection.
	if res.Outcome != brainclient.OutcomeGenerated || res.Text() == "" {
		return brainclient.TurnResponse{}, fmt.Errorf("%w: outcome %s", ErrNoAnswer, res.Outcome)
	}
	return res, nil
}

// The transaction of 07-secuencia section 2: without it a restart cannot tell a finished turn from one that never ran.
func (o *Orchestrator) persist(
	ctx context.Context, batch Batch, conversationID, aiAgentID uuid.UUID, answer brainclient.TurnResponse, at time.Time,
) (conversations.Message, error) {
	var message conversations.Message

	err := o.opts.Tx(ctx, func(store Store, aggregate Aggregate) error {
		var err error
		// Written before it is sent: crashing after the send would leave a delivered message with no row (F29, C09).
		if message, err = aggregate.RecordOutbound(ctx, conversationID, aiAgentID, answer.Text(), at); err != nil {
			return err
		}
		// Linked here and not afterwards: a restart in between would leave message_id NULL, which section 5 reads as never sent.
		if answer.GenerationID != nil {
			if err = store.LinkGeneration(ctx, *answer.GenerationID, message.ID); err != nil {
				return err
			}
		}
		return store.MarkProcessed(ctx, batch.ID, at)
	})
	return message, err
}

// Outside the transaction, because none may span the boundary with another service (ADR-0017 (i).3).
func (o *Orchestrator) deliver(ctx context.Context, target Target, message conversations.Message, text string) error {
	inbox, err := o.opts.Channel.InboxByID(ctx, target.InboxID)
	if err != nil {
		return o.undelivered(ctx, message, err)
	}

	// No retry of any kind: ADR-0020 sends at most once, and a duplicate answer to a customer cannot be taken back.
	wamid, err := o.opts.Sender.Send(ctx, inbox, target.ContactPhone, text)
	if err != nil {
		return o.undelivered(ctx, message, err)
	}

	if err := o.opts.Aggregate.MarkSent(ctx, message.ID, wamid, time.Now().UTC()); err != nil {
		return err
	}

	o.opts.Logger.Info("turn answered",
		slog.String("conversation_id", target.ConversationID.String()),
		slog.String("wamid", wamid),
	)
	return nil
}

// A timeout is the most likely cause of the two writes below, so neither can run on the context that just expired.
const writeGrace = 5 * time.Second

// The failure stays visible on the row instead of being retried, which is what ADR-0020 asks of an outbound message.
func (o *Orchestrator) undelivered(ctx context.Context, message conversations.Message, cause error) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeGrace)
	defer cancel()

	// The row keeps the code and the returned error keeps the cause, which is what reaches the log.
	if err := o.opts.Aggregate.MarkFailed(writeCtx, message.ID, whatsapp.FailureCode(cause)); err != nil {
		return fmt.Errorf("turn: send failed (%w) and the row could not be marked: %w", cause, err)
	}
	return fmt.Errorf("turn: send: %w", cause)
}

// processed_at is left NULL and the reason written: with closed_at set, that is the marker of a turn nobody finished (section 5).
func (o *Orchestrator) abandon(ctx context.Context, batch Batch, cause error) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeGrace)
	defer cancel()

	if err := o.opts.Store.MarkFailed(writeCtx, batch.ID, cause.Error()); err != nil {
		return fmt.Errorf("turn: failed (%w) and the batch could not be marked: %w", cause, err)
	}
	return cause
}
