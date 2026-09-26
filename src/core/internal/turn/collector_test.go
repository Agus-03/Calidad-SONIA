package turn

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// gatherer builds the collector over the same stubs the orchestrator uses, with the transaction as a plain call: what these
// cases check is what gets written and in what order, and binding it to PostgreSQL is the pool's job.
func gatherer(target Target) (*Collector, *stubStore, *stubAggregate) {
	steps := &[]string{}
	store := &stubStore{target: target, steps: steps}
	aggregate := &stubAggregate{steps: steps}

	collector := NewCollector(CollectorOptions{
		Tx: func(_ context.Context, fn func(Store, Aggregate) error) error {
			return fn(store, aggregate)
		},
		Window:      10 * time.Second,
		MaxMessages: 10,
		Logger:      slog.New(slog.DiscardHandler),
	})
	return collector, store, aggregate
}

func answering() Target {
	return Target{
		ConversationID:   uuid.New(),
		InboxID:          uuid.New(),
		ContactPhone:     "5493510000000",
		Mode:             enums.AgentModeAutonomous,
		AIAgentAvailable: true,
	}
}

// The batch and the link are one write in two statements: a batch nobody linked leaves the brain reading a turn with no
// incoming messages, and that is the whole point of regla 2.
func TestCollectAddsTheMessageToItsBatch(t *testing.T) {
	collector, store, aggregate := gatherer(answering())
	agent := uuid.New()
	messageID := uuid.New()

	if err := collector.Collect(context.Background(), store.target.ConversationID, messageID, &agent); err != nil {
		t.Fatalf("Collect: got %v, want no error", err)
	}

	if len(store.appended) != 1 {
		t.Fatalf("the message reached %d batches, want 1", len(store.appended))
	}
	if aggregate.linked[messageID] != store.batch.ID {
		t.Fatalf("message linked to %v, want the batch just written (%v)", aggregate.linked[messageID], store.batch.ID)
	}
	// The order matters: the link names the batch, so a link written first would name a row that is not there yet.
	if steps := *store.steps; len(steps) != 2 || steps[0] != "append" || steps[1] != "link_to_batch" {
		t.Fatalf("writes %v, want append then link_to_batch", steps)
	}
}

// Regla 8: the three conditions are read at this moment and not trusted from the webhook, because a pause that committed since
// the message arrived has to win. A batch that never opens is a message the AIAgent will never answer, which is the point.
func TestCollectOpensNoBatchForAnAIAgentThatCannotAnswer(t *testing.T) {
	agent := uuid.New()

	cases := []struct {
		name   string
		target Target
		agent  *uuid.UUID
	}{
		{"a conversation with no agent has nobody to answer it", answering(), nil},
		{"a paused or removed agent answers nothing, and grouping for it would be pointless",
			Target{Mode: enums.AgentModeAutonomous, AIAgentAvailable: false}, &agent},
		{"an off conversation has no turn of any kind",
			Target{Mode: enums.AgentModeOff, AIAgentAvailable: true}, &agent},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			collector, store, aggregate := gatherer(tc.target)

			if err := collector.Collect(context.Background(), store.target.ConversationID, uuid.New(), tc.agent); err != nil {
				t.Fatalf("Collect: got %v, want no error: a pause is a decision and not a failure", err)
			}
			if len(store.appended) != 0 || len(aggregate.linked) != 0 {
				t.Fatalf("%d batches and %d links, want none of either", len(store.appended), len(aggregate.linked))
			}
		})
	}
}

// Two deliveries reach the same conversation at once, one loses the unique index, and the loser has to end up in the batch the
// winner opened instead of losing its message (ADR-0041, regla 3).
func TestCollectRunsAgainWhenAnotherDeliveryOpenedTheBatchFirst(t *testing.T) {
	collector, store, aggregate := gatherer(answering())
	store.appendErrs = []error{errBatchRaced}
	agent := uuid.New()
	messageID := uuid.New()

	if err := collector.Collect(context.Background(), store.target.ConversationID, messageID, &agent); err != nil {
		t.Fatalf("Collect: got %v, want no error: the second attempt finds the batch the winner opened", err)
	}
	if len(store.appended) != 1 {
		t.Fatalf("the message reached %d batches, want 1", len(store.appended))
	}
	if _, linked := aggregate.linked[messageID]; !linked {
		t.Fatal("the message that lost the race was never linked to any batch")
	}
}

// A race that repeats is not a race any more, and reporting it is what puts the message id in the log the operator reads.
func TestCollectGivesUpWhenTheRaceRepeats(t *testing.T) {
	collector, store, _ := gatherer(answering())
	store.appendErrs = []error{errBatchRaced, errBatchRaced}
	agent := uuid.New()

	if err := collector.Collect(context.Background(), store.target.ConversationID, uuid.New(), &agent); err == nil {
		t.Fatal("Collect: got no error, want the race reported instead of a message silently left out of every batch")
	}
}

// The conversation is read before anything is written, so a message naming one that is not there writes nothing at all.
func TestCollectReportsAConversationThatIsNotThere(t *testing.T) {
	collector, store, aggregate := gatherer(answering())
	store.targetErr = ErrConversationNotFound
	agent := uuid.New()

	if err := collector.Collect(context.Background(), store.target.ConversationID, uuid.New(), &agent); err == nil {
		t.Fatal("Collect: got no error, want ErrConversationNotFound")
	}
	if len(store.appended) != 0 || len(aggregate.linked) != 0 {
		t.Fatal("a conversation that does not exist still got a batch")
	}
}

// ADR-0046, section 2.1: suggestion groups its messages like autonomous does, because the draft is written from the whole turn.
func TestCollectGroupsForSuggestionMode(t *testing.T) {
	agent := uuid.New()
	collector, store, aggregate := gatherer(Target{Mode: enums.AgentModeSuggestion, AIAgentAvailable: true})

	if err := collector.Collect(context.Background(), store.target.ConversationID, uuid.New(), &agent); err != nil {
		t.Fatalf("Collect: got %v, want no error", err)
	}
	if len(store.appended) != 1 || len(aggregate.linked) != 1 {
		t.Fatalf("%d batches and %d links, want the message grouped into one batch", len(store.appended), len(aggregate.linked))
	}
}
