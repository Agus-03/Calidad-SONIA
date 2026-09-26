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

	// No open_batch and no link_to_batch any more: the batch arrives closed and its messages were linked as they came in (T3).
	want := []string{"outbound_message", "generation_linked", "processed_at", "message_sent"}
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

// The outbound row precedes the send: crashing after it would leave a delivered message with no row (F29, C09).
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

// The brain is asked about the batch the poller closed, which is the turn: a different id would answer somebody else's messages.
func TestTheBrainIsAskedAboutTheBatchThatClosed(t *testing.T) {
	h := conductor()

	h.turn()

	if h.brain.requests[0].MessageBatchID != h.store.batch.ID {
		t.Fatal("the brain was asked about a batch other than the one the poller closed")
	}
	if h.brain.requests[0].ConversationID != h.store.target.ConversationID {
		t.Fatal("the batch and the conversation the brain was told about are not the same turn")
	}
}

// Regla 8: the gate is read again when the batch closes, because the customer wrote up to a minute earlier. All three ways of
// failing it end the same: processed_at written, no error_message, and the brain never called. A pause is a decision.
func TestAGateThatClosedWhileTheBatchWaitedEndsTheTurnAsProcessed(t *testing.T) {
	cases := []struct {
		name  string
		close func(*Target)
	}{
		{"the admin turned the conversation off", func(tg *Target) { tg.Mode = enums.AgentModeOff }},
		{"the admin paused or removed the agent", func(tg *Target) { tg.AIAgentAvailable = false }},
		{"the admin took the agent away from the conversation", func(tg *Target) { tg.AIAgentID = nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := conductor()
			tc.close(&h.store.target)

			h.turn()

			if h.brain.calls != 0 || h.sender.calls != 0 {
				t.Fatalf("the brain was called %d times and the channel %d, want neither", h.brain.calls, h.sender.calls)
			}
			if len(h.store.processed) != 1 {
				t.Fatal("processed_at is missing, and without it section 5 reads this pause as a turn nobody finished")
			}
			if len(h.store.failed) != 0 {
				t.Fatalf("error_message was written for a decision of the administrator: %v", h.store.failed)
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

// With closed_at set, processed_at NULL and a reason written is the one marker of a turn nobody finished (07-secuencia 5).
func TestAFailedTurnLeavesItselfMarkedAndUnprocessed(t *testing.T) {
	cases := []struct {
		name     string
		response brainclient.TurnResponse
		errs     []error
	}{
		{"the brain never answered", brainclient.TurnResponse{}, []error{&httpclient.StatusError{Status: 422}}},
		{"the brain answered failed, which carries no text to send", brainclient.TurnResponse{Outcome: brainclient.OutcomeFailed}, nil},
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
	h.sender.err = &httpclient.StatusError{Status: 400, Body: `{"error":{"message":"(#131047) Re-engagement message","code":131047}}`}

	h.turn()

	if h.sender.calls != 1 {
		t.Fatalf("sends = %d, want exactly 1", h.sender.calls)
	}
	if len(h.aggregate.failed) != 1 {
		t.Fatal("the outbound message was not marked failed, so the failure is invisible")
	}
	// Meta's code and not the body it answered with: the row is what the operator reads in the inbox.
	for _, code := range h.aggregate.failed {
		if code != "131047" {
			t.Fatalf("error_code = %q, want Meta's code", code)
		}
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
	h.channel.err = whatsapp.ErrInboxNotFound

	h.turn()

	if h.sender.calls != 0 {
		t.Fatal("something was sent without resolving which number sends it")
	}
	if len(h.aggregate.failed) != 1 {
		t.Fatal("the outbound message was not marked failed")
	}
	for _, code := range h.aggregate.failed {
		if code != "inbox_not_found" {
			t.Fatalf("error_code = %q, want inbox_not_found", code)
		}
	}
}

// With the 200 already sent, nobody but the shutdown is waiting for a turn (ADR-0029).
func TestShutdownWaitsForTheTurnsInFlight(t *testing.T) {
	h := conductor()
	release := make(chan struct{})
	h.orchestrator.opts.Sender = &blockingSender{release: release}

	h.orchestrator.Start(context.Background(), h.closed())

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

	h.orchestrator.Start(context.Background(), h.closed())

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

	h.orchestrator.Start(context.WithoutCancel(requestCtx), h.closed())
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

func (b *blockingSender) Ready(whatsapp.Inbox) error { return nil }

func (b *blockingSender) Send(context.Context, whatsapp.Inbox, string, string) (string, error) {
	b.once.Do(func() { <-b.release })
	return "wamid.HBg", nil
}

// Without generations.message_id the evidence and the answer are two rows that nothing connects (OE2, F35).
func TestTheGenerationIsLinkedToTheMessageItProduced(t *testing.T) {
	h := conductor()

	h.turn()

	if len(h.store.linked) != 1 {
		t.Fatalf("linked %d generations, want 1", len(h.store.linked))
	}
	wantID := *h.brain.response.GenerationID
	messageID, ok := h.store.linked[wantID]
	if !ok {
		t.Fatalf("the generation the brain answered with was not linked: %s", wantID)
	}
	for sentID := range h.aggregate.sent {
		if messageID != sentID {
			t.Fatalf("the generation was linked to %s and the message sent is %s", messageID, sentID)
		}
	}
}

// Inside the transaction of section 2: a link written outside could be lost, and the answer would go out unnamed.
func TestALinkThatFailsAbandonsTheTurn(t *testing.T) {
	h := conductor()
	h.store.linkErr = errors.New("turn: link generation: connection reset")

	h.turn()

	if h.sender.calls != 0 {
		t.Fatal("the answer was sent although the transaction of section 2 did not commit")
	}
	if len(h.store.failed) != 1 {
		t.Fatal("the batch was not marked failed, so nothing says why the turn stopped (section 5)")
	}
	if slices.Contains(*h.steps, "processed_at") {
		t.Fatalf("processed_at was written after the link failed: %v", *h.steps)
	}
}

// The customer comes first: a brain that answered without leaving evidence is a gap to log, not a reason to stay silent.
func TestAnAnswerWithNoEvidenceIsStillDelivered(t *testing.T) {
	h := conductor()
	h.brain.response = brainclient.TurnResponse{
		Outcome:        brainclient.OutcomeGenerated,
		CompletionText: h.brain.response.CompletionText,
	}

	h.turn()

	if h.sender.calls != 1 {
		t.Fatal("the answer was not sent because the brain left no generation to link")
	}
	if slices.Contains(*h.steps, "generation_linked") {
		t.Fatalf("a generation nobody named was linked: %v", *h.steps)
	}
}

// Section 4: a cut is a decision of the pipeline and not a failure of the turn, so it gets the reading a pause gets (ADR-0041,
// regla 8) — processed_at written, no error_message — plus the one thing a pause does not need: somebody has to answer what the
// bot refused to, so the conversation drops to waiting.
func TestABlockedTurnIsProcessedAndLeavesTheConversationWaiting(t *testing.T) {
	generation := uuid.New()
	cases := []struct {
		name     string
		response brainclient.TurnResponse
	}{
		{"cut before generating, so there is no evidence to point at (section 4.1)",
			brainclient.TurnResponse{Outcome: brainclient.OutcomeBlocked}},
		{"cut after generating, where the evidence exists and its message_id stays NULL for ever (section 4.2)",
			brainclient.TurnResponse{Outcome: brainclient.OutcomeBlocked, GenerationID: &generation}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := conductor()
			h.brain.response = tc.response

			h.turn()

			// The two writes and nothing else, in one transaction: either alone leaves a turn that reads as finished and
			// unattended, or as attended and never finished.
			if got := *h.steps; len(got) != 2 || got[0] != "conversation_waiting" || got[1] != "processed_at" {
				t.Fatalf("the writes of a blocked turn were %v, want conversation_waiting and processed_at alone", got)
			}
			if len(h.store.failed) != 0 {
				t.Fatalf("error_message was written for a cut the pipeline decided: %v", h.store.failed)
			}
			if h.sender.calls != 0 {
				t.Fatal("a blocked answer reached the customer")
			}
			if len(h.store.linked) != 0 {
				t.Fatal("a blocked generation was linked to a message that was never written")
			}
			// Neither response carries a text, which is the silence of decision O1c: the customer is told nothing and a person
			// picks the conversation up. What the brain does send, when it sends one, is the case below.
			if len(h.aggregate.notices) != 0 {
				t.Fatalf("a cut the brain answered with silence sent a text anyway: %v", h.aggregate.notices)
			}
		})
	}
}

// Decision O2, which is what amends section 4.1: a cut used to leave the customer talking to nobody, and now it leaves one message
// that the pipeline chose and no model wrote. The conversation still drops to waiting, because the fixed text answers no question.
func TestACutTurnSendsTheFixedTextAndStillWaitsForAPerson(t *testing.T) {
	notice := "le paso tu mensaje a una persona del equipo"
	refused := "esto es lo que el guardrail frenó"
	h := conductor()
	h.brain.response = brainclient.TurnResponse{
		Outcome:        brainclient.OutcomeBlocked,
		NoticeText:     &notice,
		CompletionText: &refused,
	}

	h.turn()

	// The notice first and inside the same transaction as the two writes of the cut: written before it is sent, like every other
	// outbound row, so a crash after the send cannot leave a delivered message with no row.
	want := []string{"notice_message", "conversation_waiting", "processed_at", "message_sent"}
	if got := *h.steps; !slices.Equal(got, want) {
		t.Fatalf("the writes of a cut with a text were %v, want %v", got, want)
	}
	if len(h.sender.texts) != 1 || h.sender.texts[0] != notice {
		t.Fatalf("what reached the customer was %v, want the fixed text of the cut point", h.sender.texts)
	}
	// The rule of section 4.2, and the reason the two texts travel in two fields: the answer the pipeline refused is unsendable,
	// whatever else the same response carries.
	if slices.Contains(h.sender.texts, refused) {
		t.Fatal("the answer the pipeline refused reached the customer")
	}
	if len(h.aggregate.outbound) != 0 {
		t.Fatalf("the fixed text was written as an answer of the AIAgent: %v", h.aggregate.outbound)
	}
}

// Decision O1c on this side of the hop: the brain evaluated the signal and this is the action it proposed, written in the same
// transaction that moves the conversation. The event points at the evidence and duplicates none of it (H-18).
func TestACutThatProposesATransferWritesTheEventPointingAtTheEvidence(t *testing.T) {
	signal := uuid.New()
	h := conductor()
	h.brain.response = brainclient.TurnResponse{
		Outcome:  brainclient.OutcomeBlocked,
		Transfer: &brainclient.TransferProposal{Proposed: true, SignalEvaluationID: signal},
	}

	h.turn()

	// No message of any kind: a question outside the domain is answered by a person and by nothing else (O1c). And the bot of
	// this conversation goes off with the event, so the AIAgent does not answer the next message (ADR-0045, section 2.7).
	want := []string{"transfer_event", "conversation_mode", "conversation_waiting", "processed_at"}
	if got := *h.steps; !slices.Equal(got, want) {
		t.Fatalf("the writes of a cut that delegates were %v, want %v", got, want)
	}
	if len(h.store.transfers) != 1 {
		t.Fatalf("transfers = %v, want the one the brain proposed", h.store.transfers)
	}
	event := h.store.transfers[0]
	if event.SignalEvaluationID != signal {
		t.Fatalf("signal_evaluation_id = %v, want the evidence the brain named (%v)", event.SignalEvaluationID, signal)
	}
	if event.ConversationID != h.store.target.ConversationID || event.TurnNumber != h.store.batch.TurnNumber {
		t.Fatalf("the event named %+v, want the conversation and the turn of this batch", event)
	}
	if got := h.aggregate.modes[h.store.target.ConversationID]; got != enums.AgentModeOff {
		t.Fatalf("mode after the transfer = %q, want off: the AIAgent would answer a conversation handed to a person", got)
	}
}

// An evaluation that did not fire is evidence the brain wrote and an action nobody decided: the administrator disabled the signal,
// or never configured it. The conversation still waits for a person, because that is what every cut does.
func TestAnEvaluationThatDidNotFireWritesNoTransfer(t *testing.T) {
	h := conductor()
	h.brain.response = brainclient.TurnResponse{
		Outcome:  brainclient.OutcomeBlocked,
		Transfer: &brainclient.TransferProposal{Proposed: false, SignalEvaluationID: uuid.New()},
	}

	h.turn()

	if len(h.store.transfers) != 0 {
		t.Fatalf("a transfer was written for an evaluation that did not fire: %v", h.store.transfers)
	}
	want := []string{"conversation_waiting", "processed_at"}
	if got := *h.steps; !slices.Equal(got, want) {
		t.Fatalf("the writes of a cut that proposed nothing were %v, want %v", got, want)
	}
}

// generated and empty is the two services disagreeing about the same turn, and that is a failure: a cut says so in its outcome.
func TestAnAnswerThatCameEmptyIsAFailureAndNotACut(t *testing.T) {
	h := conductor()
	h.brain.response = generated("")

	h.turn()

	if len(h.aggregate.waiting) != 0 {
		t.Fatal("an inconsistency between the two services was recorded as a decision of the pipeline")
	}
	if h.store.failed[h.store.batch.ID] == "" {
		t.Fatal("the batch was left without error_message, which is what diagnoses the failure")
	}
}

// A cut whose two writes did not commit is not a decision that landed: nobody finished this turn, and that is what the row says.
func TestABlockedTurnThatCouldNotBeWrittenIsMarkedAsFailed(t *testing.T) {
	h := conductor()
	h.brain.response = brainclient.TurnResponse{Outcome: brainclient.OutcomeBlocked}
	h.store.processedErr = errors.New("turn: the batch could not be marked")

	h.turn()

	if h.store.failed[h.store.batch.ID] == "" {
		t.Fatal("the cut never committed and the batch was left saying nothing about it")
	}
	if len(h.store.processed) != 0 {
		t.Fatal("processed_at was recorded for a transaction that did not commit")
	}
}

// ADR-0046, section 2.2: in suggestion mode the brain is asked, the draft stays pending_review, and nothing is written as a message
// or sent. processed_at is the only write, so a restart does not ask for the same suggestion twice.
func TestASuggestionIsNeitherWrittenAsAMessageNorSent(t *testing.T) {
	h := conductor()
	h.store.target.Mode = enums.AgentModeSuggestion

	h.turn()

	if h.brain.calls != 1 || h.brain.requests[0].Mode != enums.AgentModeSuggestion {
		t.Fatalf("the brain was called %d times, want once and told the mode is suggestion", h.brain.calls)
	}
	if h.sender.calls != 0 || len(h.aggregate.outbound) != 0 {
		t.Fatalf("a suggestion reached the customer: %d sends, %v written", h.sender.calls, h.aggregate.outbound)
	}
	if got := *h.steps; !slices.Equal(got, []string{"processed_at"}) {
		t.Fatalf("the writes of a suggested turn were %v, want only processed_at", got)
	}
}

// ADR-0046, sections 2.3 and 2.7: a cut in suggestion mode tells the customer nothing and transfers nothing, because a person
// already holds the conversation and nothing leaves without one.
func TestACutInSuggestionModeSendsNoTextAndTransfersNothing(t *testing.T) {
	notice := "No podemos procesar ese mensaje."
	h := conductor()
	h.store.target.Mode = enums.AgentModeSuggestion
	h.brain.response = brainclient.TurnResponse{
		Outcome:    brainclient.OutcomeBlocked,
		NoticeText: &notice,
		Transfer:   &brainclient.TransferProposal{Proposed: true, SignalEvaluationID: uuid.New()},
	}

	h.turn()

	if h.sender.calls != 0 || len(h.aggregate.notices) != 0 {
		t.Fatal("the fixed text of a cut reached the customer without an Operator")
	}
	if len(h.store.transfers) != 0 || len(h.aggregate.modes) != 0 {
		t.Fatalf("a transfer was written in suggestion mode (H-30): %v, modes %v", h.store.transfers, h.aggregate.modes)
	}
	if got := *h.steps; !slices.Equal(got, []string{"conversation_waiting", "processed_at"}) {
		t.Fatalf("the writes of a cut in suggestion mode were %v", got)
	}
}
