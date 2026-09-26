package turn

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Twice and not more: the only way to lose the unique index is to another delivery opening the batch, and the attempt that
// follows finds that batch open. A third try would only cover a poller closing it in between, which is the next message's turn.
const collectAttempts = 2

// Collector is what the webhook hands each inbound message to. It answers nothing: it only decides which turn the message
// belongs to and how long that turn still waits. Answering is the poller's, when the batch closes (ADR-0041, regla 7).
type Collector struct {
	tx          TxFunc
	window      time.Duration
	maxMessages int
	log         *slog.Logger
}

type CollectorOptions struct {
	Tx TxFunc

	// The window every message pushes the expiry to, and the cap that makes the batch expire at once (ADR-0041, reglas 2 y 5).
	Window      time.Duration
	MaxMessages int

	Logger *slog.Logger
}

func NewCollector(opts CollectorOptions) *Collector {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Collector{tx: opts.Tx, window: opts.Window, maxMessages: opts.MaxMessages, log: opts.Logger}
}

// Collect adds the message to its batch and starts the window over, and the webhook waits for it: a message that reached no
// batch is one nobody will ever answer, and Meta will not deliver it again (ADR-0041, regla 2).
func (c *Collector) Collect(ctx context.Context, conversationID, messageID uuid.UUID, aiAgentID *uuid.UUID) error {
	if aiAgentID == nil {
		// A conversation with no agent has nobody to answer it: the operator sees it in the inbox and that is the whole flow.
		c.log.Info("message not batched",
			slog.String("conversation_id", conversationID.String()),
			slog.String("reason", "the conversation has no agent"),
		)
		return nil
	}

	var err error
	for attempt := 0; attempt < collectAttempts; attempt++ {
		// The whole transaction runs again and not just the statement: PostgreSQL refuses every command after the 23505.
		if err = c.collect(ctx, conversationID, messageID); !errors.Is(err, errBatchRaced) {
			return err
		}
	}
	return err
}

// One transaction for the batch and the link, because a batch nobody linked leaves the brain reading a turn with no incoming
// messages, and a link to a batch that rolled back would point at nothing (regla 2, regla 5).
func (c *Collector) collect(ctx context.Context, conversationID, messageID uuid.UUID) error {
	return c.tx(ctx, func(store Store, aggregate Aggregate, _ Attributes) error {
		target, err := store.Target(ctx, conversationID)
		if err != nil {
			return err
		}
		// The same gate that answers, read here and not trusted from the webhook: a pause committed since must win (ADR-0038,
		// regla 2). It is read again when the batch closes, because the window leaves up to a minute in between.
		if !answers(target.Mode) || !target.AIAgentAvailable {
			c.log.Info("message not batched",
				slog.String("conversation_id", conversationID.String()),
				slog.String("mode", string(target.Mode)),
				slog.Bool("ai_agent_available", target.AIAgentAvailable),
			)
			return nil
		}

		batch, err := store.Append(ctx, conversationID, c.window, c.maxMessages)
		if err != nil {
			return err
		}
		return aggregate.LinkToBatch(ctx, messageID, batch.ID)
	})
}
