package turn

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// The order of 07-secuencia section 2, which is the whole reason this package exists.
func TestTheTurnWritesInTheOrderSectionTwoDeclares(t *testing.T) {
	h := conductor()

	h.turn()

	want := []string{"open_batch", "link_to_batch", "outbound_message", "processed_at", "message_sent"}
	if !slices.Equal(*h.steps, want) {
		t.Fatalf("steps = %v, want %v", *h.steps, want)
	}
	if h.aggregate.outbound[0] != "buenas, si tenemos" {
		t.Fatalf("the answer that was written is not the one the brain generated: %q", h.aggregate.outbound[0])
	}
	if h.sender.texts[0] != "buenas, si tenemos" {
		t.Fatalf("the answer that was sent is not the one that was written: %q", h.sender.texts[0])
	}
}

// The outbound row is written before the send and processed_at with it: sending first and crashing would leave a
// delivered message with no row, and messages admits neither correction nor deletion (F29, C09).
func TestTheAnswerIsWrittenBeforeItIsSent(t *testing.T) {
	h := conductor()

	h.turn()

	steps := *h.steps
	if slices.Index(steps, "outbound_message") > slices.Index(steps, "message_sent") {
		t.Fatalf("the message was sent before it was written: %v", steps)
	}
	if slices.Index(steps, "processed_at") > slices.Index(steps, "message_sent") {
		t.Fatalf("processed_at was written after the send, outside the transaction it belongs to: %v", steps)
	}
}

// The brain reads the turn by message_batch_id, so a batch nobody linked leaves it with no incoming messages to answer.
func TestTheIncomingMessageIsLinkedToTheBatch(t *testing.T) {
	h := conductor()

	h.turn()

	if len(h.aggregate.linked) != 1 {
		t.Fatalf("linked %d messages to a batch, want 1", len(h.aggregate.linked))
	}
	for _, batchID := range h.aggregate.linked {
		if batchID != h.store.batch.ID {
			t.Fatalf("the message was linked to %s and the batch is %s", batchID, h.store.batch.ID)
		}
	}
	if h.brain.requests[0].MessageBatchID != h.store.batch.ID {
		t.Fatal("the brain was asked about a batch other than the one that was opened")
	}
}

// A conversation with no agent has nobody to answer it, and the turn must not open a batch for a turn that will not happen.
func TestATurnWithNoAgentDoesNothing(t *testing.T) {
	h := conductor()

	h.orchestrator.Start(context.Background(), uuid.New(), uuid.New(), nil)
	_ = h.orchestrator.Wait(context.Background())

	if len(*h.steps) != 0 || h.brain.calls != 0 {
		t.Fatalf("a turn without an agent still did work: %v", *h.steps)
	}
}

// The three modes branch the orchestrator in Fase 9; until then only the autonomous one answers by itself (F02).
func TestOnlyTheAutonomousModeAnswersOnItsOwn(t *testing.T) {
	for _, mode := range []enums.AgentMode{enums.AgentModeSuggestion, enums.AgentModeOff} {
		t.Run(string(mode), func(t *testing.T) {
			h := conductor()
			h.store.target.Mode = mode

			h.turn()

			if h.brain.calls != 0 || h.sender.calls != 0 {
				t.Fatalf("mode %q reached the brain or the channel", mode)
			}
		})
	}
}

// The mode travels to the brain because section 1 makes it explicit and never deduced.
func TestTheModeTravelsToTheBrain(t *testing.T) {
	h := conductor()

	h.turn()

	if h.brain.requests[0].Mode != enums.AgentModeAutonomous {
		t.Fatalf("mode = %q, want autonomous", h.brain.requests[0].Mode)
	}
}

// The brain falling over is the one case a retry can fix, and the turn is the one that decides (ADR-0029, httpclient classifies).
func TestTheBrainIsAskedAgainOnlyWhenItCanHelp(t *testing.T) {
	cases := []struct {
		name      string
		errs      []error
		wantCalls int
		wantSent  int
	}{
		{"a 500 is the brain falling over, and the second call can succeed", []error{&httpclient.StatusError{Status: 500}, nil}, 2, 1},
		{"a transport failure never reached the brain, so it is worth asking again", []error{errBrainDown, nil}, 2, 1},
		{"a 422 is the contract being violated, which two calls do not fix", []error{&httpclient.StatusError{Status: 422}}, 1, 0},
		{"a turn the brain does not know will not appear on a second call", []error{brainclient.ErrTurnNotFound}, 1, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := conductor()
			h.brain.errs = tc.errs

			h.turn()

			if h.brain.calls != tc.wantCalls {
				t.Fatalf("brain calls = %d, want %d", h.brain.calls, tc.wantCalls)
			}
			if h.sender.calls != tc.wantSent {
				t.Fatalf("sends = %d, want %d", h.sender.calls, tc.wantSent)
			}
		})
	}
}

func TestTheBrainIsAskedNoMoreThanTheConfiguredTimes(t *testing.T) {
	h := conductor()
	h.brain.errs = []error{
		&httpclient.StatusError{Status: 500}, &httpclient.StatusError{Status: 500},
		&httpclient.StatusError{Status: 500}, &httpclient.StatusError{Status: 500},
		&httpclient.StatusError{Status: 500},
	}

	h.turn()

	// Four calls and not five: MaxRetries counts the retries, so the first attempt is not one of them.
	if h.brain.calls != 4 {
		t.Fatalf("brain calls = %d, want 1 attempt plus 3 retries", h.brain.calls)
	}
}

// A turn that could not be answered leaves processed_at NULL and its reason written: with closed_at set, that is the
// one marker of a turn nobody finished (07-secuencia section 5).
func TestAFailedTurnLeavesItselfMarkedAndUnprocessed(t *testing.T) {
	cases := []struct {
		name     string
		response brainclient.TurnResponse
		errs     []error
	}{
		{"the brain never answered", brainclient.TurnResponse{}, []error{&httpclient.StatusError{Status: 422}}},
		{"the brain answered failed, which carries no text to send", brainclient.TurnResponse{Outcome: brainclient.OutcomeFailed}, nil},
		{"the guardrail blocked it, which is a decision and still nothing to send", brainclient.TurnResponse{Outcome: brainclient.OutcomeBlocked}, nil},
		{"generated with an empty text is neither an answer nor a rejection", generated(""), nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := conductor()
			h.brain.response, h.brain.errs = tc.response, tc.errs

			h.turn()

			if len(h.store.processed) != 0 {
				t.Fatal("processed_at was written for a turn that produced no answer")
			}
			if h.store.failed[h.store.batch.ID] == "" {
				t.Fatal("the batch was left without error_message, which is what diagnoses the failure")
			}
			if h.sender.calls != 0 {
				t.Fatal("something was sent for a turn that had no answer")
			}
		})
	}
}

// ADR-0020 sends at most once: the failure stays visible on the row and nobody tries again by itself.
func TestASendThatFailsIsMarkedAndNeverRetried(t *testing.T) {
	h := conductor()
	h.sender.err = errors.New("meta: 131047")

	h.turn()

	if h.sender.calls != 1 {
		t.Fatalf("sends = %d, want exactly 1", h.sender.calls)
	}
	if len(h.aggregate.failed) != 1 {
		t.Fatal("the outbound message was not marked failed, so the failure is invisible")
	}
	if len(h.aggregate.sent) != 0 {
		t.Fatal("a message that never left was marked as sent")
	}
	// The turn did run: processed_at separates a turn that was processed from one that never was, and this one was.
	if len(h.store.processed) != 1 {
		t.Fatal("processed_at is missing for a turn whose answer was written")
	}
}

// An inbox that cannot be read is the same kind of failure as a send that was refused, and it must not lose the row either.
func TestAnUnreadableInboxLeavesTheMessageMarked(t *testing.T) {
	h := conductor()
	h.channel.err = errors.New("whatsapp: inbox not found")

	h.turn()

	if h.sender.calls != 0 {
		t.Fatal("something was sent without resolving which number sends it")
	}
	if len(h.aggregate.failed) != 1 {
		t.Fatal("the outbound message was not marked failed")
	}
}

// With the 200 already sent, nobody but the shutdown is waiting for a turn (ADR-0029).
func TestShutdownWaitsForTheTurnsInFlight(t *testing.T) {
	h := conductor()
	release := make(chan struct{})
	h.orchestrator.opts.Sender = &blockingSender{release: release}

	agent := uuid.New()
	h.orchestrator.Start(context.Background(), h.store.target.ConversationID, uuid.New(), &agent)

	quick, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := h.orchestrator.Wait(quick); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() returned %v while a turn was still running", err)
	}

	close(release)
	if err := h.orchestrator.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() = %v after the turn finished, want nil", err)
	}
}

// Start registers the turn before returning, so a shutdown that begins the instant it returns still finds it.
func TestStartRegistersTheTurnBeforeItReturns(t *testing.T) {
	h := conductor()
	release := make(chan struct{})
	defer close(release)
	h.orchestrator.opts.Sender = &blockingSender{release: release}

	agent := uuid.New()
	h.orchestrator.Start(context.Background(), h.store.target.ConversationID, uuid.New(), &agent)

	immediate, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.orchestrator.Wait(immediate); err == nil {
		t.Fatal("Wait() returned at once, so the turn was not registered when Start returned")
	}
}

// The request's context dies when the webhook answers, and the turn outlives it by seconds (ADR-0029).
func TestTheTurnSurvivesTheRequestThatStartedIt(t *testing.T) {
	h := conductor()
	requestCtx, cancel := context.WithCancel(context.Background())

	agent := uuid.New()
	h.orchestrator.Start(context.WithoutCancel(requestCtx), h.store.target.ConversationID, uuid.New(), &agent)
	cancel()
	_ = h.orchestrator.Wait(context.Background())

	if len(h.aggregate.sent) != 1 {
		t.Fatal("the turn died with the request that started it")
	}
}

func TestWaitReturnsAtOnceWhenNothingIsRunning(t *testing.T) {
	if err := New(Options{Logger: slog.New(slog.DiscardHandler)}).Wait(context.Background()); err != nil {
		t.Fatalf("Wait() = %v with no turns in flight, want nil", err)
	}
}

type blockingSender struct {
	release chan struct{}
	once    sync.Once
}

func (b *blockingSender) Send(context.Context, whatsapp.Inbox, string, string) (string, error) {
	b.once.Do(func() { <-b.release })
	return "wamid.HBg", nil
}
