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
	Append(ctx context.Context, conversationID uuid.UUID, window time.Duration, maxMessages int) (Batch, error)
	Close(ctx context.Context, window, maxDuration time.Duration, maxMessages, limit int) ([]Batch, error)
	MarkProcessed(ctx context.Context, batchID uuid.UUID, at time.Time) error
	MarkFailed(ctx context.Context, batchID uuid.UUID, reason string) error
	LinkGeneration(ctx context.Context, generationID, messageID uuid.UUID) error
	// The escalation of F12 the brain proposed, pointing at the evidence it evaluated (decision O1c).
	RecordTransfer(ctx context.Context, conversationID, signalEvaluationID uuid.UUID, turnNumber int) error
	// The context of F04 the generation was given, made the current one in the transaction that closes the turn (ADR-0048).
	AdoptContext(ctx context.Context, generationID uuid.UUID) error
}

// Aggregate is conversations/: this package never writes messages with SQL of its own (regla 1).
type Aggregate interface {
	LinkToBatch(ctx context.Context, messageID, batchID uuid.UUID) error
	// Where a turn that produced no answer leaves the conversation, because somebody has to answer what the bot did not
	// (07-secuencia secciones 4 y 5).
	MarkWaiting(ctx context.Context, conversationID uuid.UUID) error
	// How a transfer turns the bot of that one conversation off, in the transaction that writes it (ADR-0045, section 2.7).
	SetMode(ctx context.Context, conversationID uuid.UUID, mode enums.AgentMode) error
	RecordOutbound(ctx context.Context, conversationID, aiAgentID uuid.UUID, text string, at time.Time) (conversations.Message, error)
	// The fixed text of a cut turn (decision O2), whose row names no sender: no model wrote it and no person typed it.
	RecordSystemOutbound(ctx context.Context, conversationID uuid.UUID, text string, at time.Time) (conversations.Message, error)
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

// Start takes the batch already closed: the turn is the block of fragmented messages, and until the poller cut it nobody knew
// what the customer had finished saying (ADR-0041, regla 7). Registered here and not inside the goroutine, so a shutdown that
// begins right now still finds this turn (ADR-0029).
func (o *Orchestrator) Start(ctx context.Context, batch Batch) {
	o.inFlight.Add(1)

	go func() {
		defer o.inFlight.Done()

		// Its own deadline: the poller's context is not this turn's, and an unbounded turn holds the shutdown open.
		ctx, cancel := context.WithTimeout(ctx, o.opts.Budget)
		defer cancel()

		if err := o.run(ctx, batch); err != nil {
			o.opts.Logger.Error("turn failed",
				slog.String("conversation_id", batch.ConversationID.String()),
				slog.String("batch_id", batch.ID.String()),
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

func (o *Orchestrator) run(ctx context.Context, batch Batch) error {
	conversationID := batch.ConversationID

	target, err := o.opts.Store.Target(ctx, conversationID)
	if err != nil {
		return err
	}
	// The three conditions of ADR-0038 regla 2, read again now: the batch waited up to a minute, and a pause, a removal or a
	// change of mode that committed in between has to win over what was true when the customer wrote (ADR-0041, regla 8).
	if !answers(target.Mode) || !target.AIAgentAvailable || target.AIAgentID == nil {
		o.opts.Logger.Info("turn not answered",
			slog.String("conversation_id", conversationID.String()),
			slog.String("mode", string(target.Mode)),
			slog.Bool("ai_agent_available", target.AIAgentAvailable),
		)
		// processed_at and no error_message: a closed batch that nobody processed is the marker of a turn that failed
		// (07-secuencia section 5), and a pause is a decision. The turn ran, and its outcome was not to answer.
		return o.opts.Store.MarkProcessed(ctx, batch.ID, time.Now().UTC())
	}
	aiAgentID := *target.AIAgentID

	answer, err := o.ask(ctx, batch, aiAgentID, target.Mode)
	if err != nil {
		return o.abandon(ctx, batch, err)
	}

	// The three outcomes of the contract are three different turns, and reading them as one is what this stopped doing: a cut is a
	// decision of the pipeline (07-secuencia section 4) and a failure is nobody deciding anything (section 5).
	switch {
	case answer.Outcome == brainclient.OutcomeBlocked:
		return o.blocked(ctx, target, batch, answer)
	case answer.Outcome != brainclient.OutcomeGenerated:
		// failed, or an outcome this version of the contract does not know. Either way nothing was decided and nothing can be sent.
		return o.abandon(ctx, batch, fmt.Errorf("%w: outcome %s", ErrNoAnswer, answer.Outcome))
	case answer.Text() == "":
		// Not a cut: the brain said it generated an answer and there is none, which is the two services disagreeing about the same
		// turn. A blocked answer is empty on purpose and says so in its outcome; this one does not.
		return o.abandon(ctx, batch, fmt.Errorf("%w: generated with an empty text", ErrNoAnswer))
	}

	if target.Mode == enums.AgentModeSuggestion {
		// A draft the Operator copies or ignores: nothing leaves on its own, and the turn is done (ADR-0046, sections 2.2 and 2.5).
		return o.suggested(ctx, target, batch, answer)
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
	// What the brain answered, whatever it answered: only the hop's own failures are errors here, and what the outcome means is
	// decided by run, which is the one that knows what each of the three costs.
	return res, nil
}

// The cut of 07-secuencia section 4, before generating or after: neither writes the answer, and a generation that exists keeps
// message_id NULL for ever. What goes out is the fixed text of that cut point (decision O2), and no text means the turn stays
// silent, which is decision O1c. A transfer the brain proposed is written here because the brain evaluated and the core acts.
//
// processed_at is written and error_message is not, the same reading the pause of ADR-0041 regla 8 gets: the turn ran and its
// outcome was not to answer. The conversation drops to waiting either way (ADR-0042).
func (o *Orchestrator) blocked(ctx context.Context, target Target, batch Batch, answer brainclient.TurnResponse) error {
	notice := answer.Notice()
	transfers := answer.Transfer != nil && answer.Transfer.Proposed
	if target.Mode == enums.AgentModeSuggestion {
		// Nothing reaches the customer without an Operator, and a person already holds the conversation (ADR-0046, 2.3 and 2.7).
		notice, transfers = "", false
	}
	fields := []any{
		slog.String("conversation_id", target.ConversationID.String()),
		slog.String("batch_id", batch.ID.String()),
		// Whether the customer was told anything, which is the one thing about a cut that a reader cannot infer from the rest.
		slog.Bool("notice", notice != ""),
		slog.Bool("transfer", transfers),
	}
	if answer.GenerationID != nil {
		// Only when the cut came after generating: that evidence exists and the forensics of a blocked answer start there (section 4.2).
		fields = append(fields, slog.String("generation_id", answer.GenerationID.String()))
	}
	o.opts.Logger.Info("turn blocked", fields...)

	at := time.Now().UTC()
	var message conversations.Message

	// One transaction for every write of the cut, for the reason section 2 gives about the happy path: either alone leaves a turn
	// that reads as finished and unattended, or as attended and never finished.
	err := o.opts.Tx(ctx, func(store Store, aggregate Aggregate) error {
		if notice != "" {
			var err error
			// Written before it is sent, like every other outbound row: crashing after the send would leave a delivered message
			// with no row (F29, C09).
			if message, err = aggregate.RecordSystemOutbound(ctx, target.ConversationID, notice, at); err != nil {
				return err
			}
		}
		// The action of decision O1c, and only when the brain proposed it: an evaluation that did not fire is evidence the brain
		// already wrote, and turning it into a transfer here would count an escalation nobody decided.
		if transfers {
			if err := store.RecordTransfer(ctx, target.ConversationID, answer.Transfer.SignalEvaluationID, batch.TurnNumber); err != nil {
				return err
			}
			// A transferred conversation is not answered by the AIAgent again until an Operator turns it back on (ADR-0045, 2.7).
			if err := aggregate.SetMode(ctx, target.ConversationID, enums.AgentModeOff); err != nil {
				return err
			}
		}
		if err := aggregate.MarkWaiting(ctx, target.ConversationID); err != nil {
			return err
		}
		// A cut after generating still read what the customer wrote, and the next turn is owed that context (ADR-0048).
		if err := adopt(ctx, store, answer.GenerationID); err != nil {
			return err
		}
		return store.MarkProcessed(ctx, batch.ID, at)
	})
	if err != nil {
		// Nothing committed, so this turn really was not finished by anybody, and that is what the row has to say.
		return o.abandon(ctx, batch, err)
	}

	if notice == "" {
		return nil
	}
	// Outside the transaction, because none may span the boundary with another service (ADR-0017 (i).3), and through the same send
	// as an answer: at most once, with no retry of any kind (ADR-0020).
	return o.deliver(ctx, target, message, notice)
}

// suggested closes a turn whose answer is a draft: the generation stays pending_review with no message, and only processed_at is
// written, so a restart does not ask the brain for the same suggestion twice (ADR-0046, section 2.2).
func (o *Orchestrator) suggested(ctx context.Context, target Target, batch Batch, answer brainclient.TurnResponse) error {
	o.opts.Logger.Info("turn suggested",
		slog.String("conversation_id", target.ConversationID.String()),
		slog.String("batch_id", batch.ID.String()),
		slog.Bool("generation", answer.GenerationID != nil),
	)
	err := o.opts.Tx(ctx, func(store Store, _ Aggregate) error {
		if err := adopt(ctx, store, answer.GenerationID); err != nil {
			return err
		}
		return store.MarkProcessed(ctx, batch.ID, time.Now().UTC())
	})
	if err != nil {
		return o.abandon(ctx, batch, err)
	}
	return nil
}

// adopt is AdoptContext for the turns that generated: with no generation there is no context this turn was given (ADR-0048).
func adopt(ctx context.Context, store Store, generationID *uuid.UUID) error {
	if generationID == nil {
		return nil
	}
	return store.AdoptContext(ctx, *generationID)
}

// answers is the gate of ADR-0041 regla 8 as ADR-0046 amends it: suggestion opens its turn too, because a draft needs one.
func answers(mode enums.AgentMode) bool {
	return mode == enums.AgentModeAutonomous || mode == enums.AgentModeSuggestion
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
		if err = adopt(ctx, store, answer.GenerationID); err != nil {
			return err
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
		// Which of the two things a turn can send this was: the answer of the AIAgent, or the fixed text of a cut (decision O2).
		// Without it, the line above a cut reads as if the answer the pipeline refused had gone out anyway.
		slog.String("sender_type", string(message.SenderType)),
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
